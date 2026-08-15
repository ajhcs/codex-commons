package codexauth

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// managedProcess is the complete capability set used by the managed wrapper.
// Keeping it private lets tests inject a deterministic process without
// widening the public Client API.
type managedProcess interface {
	Client
	ExperimentalArchaeologyClient
	HistorianTaskFinder
	HistorianTaskInventory
	HistorianTaskRenamer
}

type processFactory func(context.Context, ProcessConfig) (managedProcess, error)

// ManagedProcessClient supervises one App Server process generation. A
// retryable request failure may trigger one bounded retry, but only one
// goroutine owns recovery for a failed generation. Recovery state is reset
// only after a successful managed call proves the generation healthy.
type ManagedProcessClient struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	config  ProcessConfig
	client  managedProcess
	handler func(Event)
	closed  bool

	generation          uint64
	recoveryAttempts    int
	nextRecoveryAt      time.Time
	maxRecoveryAttempts int
	backoff             func(attempt int) time.Duration
	now                 func() time.Time
	factory             processFactory
	recovery            *managedRecovery
}

type managedRecovery struct {
	generation uint64
	done       chan struct{}
	client     managedProcess
	err        error
}

const (
	defaultMaxRecoveryAttempts = 3
	defaultRecoveryBackoff     = 250 * time.Millisecond
	maxRecoveryBackoff         = 2 * time.Second
)

func defaultProcessFactory(ctx context.Context, config ProcessConfig) (managedProcess, error) {
	return NewProcess(ctx, config)
}

func defaultRecoveryBackoffFor(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := defaultRecoveryBackoff
	for step := 1; step < attempt && delay < maxRecoveryBackoff; step++ {
		if delay > maxRecoveryBackoff/2 {
			return maxRecoveryBackoff
		}
		delay *= 2
	}
	if delay > maxRecoveryBackoff {
		return maxRecoveryBackoff
	}
	return delay
}

func NewManagedProcess(ctx context.Context, config ProcessConfig) (*ManagedProcessClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	client, err := defaultProcessFactory(runCtx, config)
	if err != nil {
		cancel()
		return nil, err
	}
	if client == nil {
		cancel()
		return nil, ErrUnavailable
	}
	return &ManagedProcessClient{
		ctx:                 runCtx,
		cancel:              cancel,
		config:              config,
		client:              client,
		maxRecoveryAttempts: defaultMaxRecoveryAttempts,
		backoff:             defaultRecoveryBackoffFor,
		now:                 time.Now,
		factory:             defaultProcessFactory,
	}, nil
}

func (m *ManagedProcessClient) current() managedProcess {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	return m.client
}

func retryableProcessError(err error) bool {
	return errors.Is(err, ErrUnavailable) || errors.Is(err, ErrProcessExited) ||
		errors.Is(err, ErrProtocol) || errors.Is(err, ErrLineTooLarge) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe)
}

func (m *ManagedProcessClient) nowFunc() time.Time {
	if m != nil && m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *ManagedProcessClient) factoryFunc() processFactory {
	if m != nil && m.factory != nil {
		return m.factory
	}
	return defaultProcessFactory
}

func (m *ManagedProcessClient) backoffFunc(attempt int) time.Duration {
	if m != nil && m.backoff != nil {
		delay := m.backoff(attempt)
		if delay < 0 {
			return 0
		}
		return delay
	}
	return defaultRecoveryBackoffFor(attempt)
}

// withRetry performs one bounded idempotent retry on a retryable process
// failure. The callback is never replayed more than once by this invocation.
// Accepted-launch and other uncertain mutations intentionally do not use this
// helper; see LaunchTask, LaunchHistorianTask, and InterruptTurn below.
func (m *ManagedProcessClient) withRetry(ctx context.Context, call func(managedProcess) error) error {
	if m == nil {
		return ErrUnavailable
	}
	client := m.current()
	if client == nil {
		return ErrUnavailable
	}
	err := call(client)
	if err == nil {
		m.markHealthy(client)
		return nil
	}
	if !retryableProcessError(err) || ctx.Err() != nil {
		return err
	}
	return m.recover(ctx, client, err, call)
}

func (m *ManagedProcessClient) recover(ctx context.Context, failed managedProcess, originalErr error, call func(managedProcess) error) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return originalErr
	}

	// A pending recovery must be observed before cooldown. Otherwise joiners
	// would return early during the owner's cooldown window instead of
	// converging on its result.
	if pending := m.recovery; pending != nil {
		m.mu.Unlock()
		return m.waitForRecovery(ctx, pending, originalErr, call)
	}

	current := m.client
	if current != failed {
		m.mu.Unlock()
		if current == nil || ctx.Err() != nil {
			return originalErr
		}
		// Another caller completed recovery before this stale failure reached
		// the coordinator. It gets its single replay on that generation.
		err := call(current)
		if err == nil {
			m.markHealthy(current)
		}
		return err
	}

	now := m.nowFunc()
	if m.maxRecoveryAttempts <= 0 || m.recoveryAttempts >= m.maxRecoveryAttempts ||
		(!m.nextRecoveryAt.IsZero() && now.Before(m.nextRecoveryAt)) {
		m.mu.Unlock()
		return originalErr
	}

	m.recoveryAttempts++
	attempt := m.recoveryAttempts
	m.nextRecoveryAt = now.Add(m.backoffFunc(attempt))
	pending := &managedRecovery{
		generation: m.generation,
		done:       make(chan struct{}),
	}
	m.recovery = pending
	factory := m.factoryFunc()
	parentCtx := m.ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	config := m.config
	old := current
	m.mu.Unlock()

	// Close outside the manager lock. Client close drains pipes and can invoke
	// callbacks; holding mu here would permit a close/recovery deadlock.
	if old != nil {
		_ = old.Close()
	}
	candidate, err := factory(parentCtx, config)
	if err == nil && candidate == nil {
		err = ErrUnavailable
	}
	if err != nil {
		if candidate != nil {
			_ = candidate.Close()
		}
		candidate = nil
	}
	if candidate != nil && !candidate.Available() {
		_ = candidate.Close()
		candidate = nil
		if err == nil {
			err = ErrUnavailable
		}
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		if candidate != nil {
			_ = candidate.Close()
		}
		m.finishRecovery(pending, nil, ErrUnavailable)
		return originalErr
	}
	if m.client != old || m.generation != pending.generation {
		m.mu.Unlock()
		if candidate != nil {
			_ = candidate.Close()
		}
		m.finishRecovery(pending, nil, ErrUnavailable)
		return originalErr
	}
	if err != nil || candidate == nil {
		// Keep the failed generation as the episode anchor. This allows a
		// later, cooldown-eligible call to make the next bounded attempt rather
		// than permanently losing the recovery path after one spawn error.
		m.mu.Unlock()
		m.finishRecovery(pending, nil, err)
		return originalErr
	}

	// Read and install the latest handler while mu is held. SetEventHandler
	// uses the same ordering, so a concurrent update cannot be lost on the
	// newly-installed generation.
	candidate.SetEventHandler(m.handler)
	m.client = candidate
	m.generation++
	m.mu.Unlock()
	m.finishRecovery(pending, candidate, nil)

	if ctx.Err() != nil || !m.isCurrent(candidate) {
		return originalErr
	}
	err = call(candidate)
	if err == nil {
		m.markHealthy(candidate)
	}
	return err
}

func (m *ManagedProcessClient) waitForRecovery(ctx context.Context, pending *managedRecovery, originalErr error, call func(managedProcess) error) error {
	select {
	case <-pending.done:
		if ctx.Err() != nil || pending.client == nil || !m.isCurrent(pending.client) {
			return originalErr
		}
		err := call(pending.client)
		if err == nil {
			m.markHealthy(pending.client)
		}
		return err
	case <-ctx.Done():
		return originalErr
	}
}

func (m *ManagedProcessClient) finishRecovery(pending *managedRecovery, client managedProcess, err error) {
	m.mu.Lock()
	pending.client = client
	pending.err = err
	if m.recovery == pending {
		m.recovery = nil
	}
	close(pending.done)
	m.mu.Unlock()
}

func (m *ManagedProcessClient) isCurrent(client managedProcess) bool {
	if m == nil || client == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.closed && m.client == client
}

// markHealthy is the explicit episode reset point. A successful managed call
// proves more than a completed process handshake, so a replacement that fails
// its first replay retains its recovery budget and cooldown.
func (m *ManagedProcessClient) markHealthy(client managedProcess) {
	if m == nil || client == nil {
		return
	}
	m.mu.Lock()
	if !m.closed && m.client == client {
		m.recoveryAttempts = 0
		m.nextRecoveryAt = time.Time{}
	}
	m.mu.Unlock()
}

func (m *ManagedProcessClient) Available() bool {
	client := m.current()
	return client != nil && client.Available()
}

func (m *ManagedProcessClient) StartDeviceCode(ctx context.Context) (DeviceCode, error) {
	var result DeviceCode
	err := m.withRetry(ctx, func(client managedProcess) error {
		var callErr error
		result, callErr = client.StartDeviceCode(ctx)
		return callErr
	})
	return result, err
}

func (m *ManagedProcessClient) PollLogin(ctx context.Context, loginID string) (LoginResult, error) {
	var result LoginResult
	err := m.withRetry(ctx, func(client managedProcess) error {
		var callErr error
		result, callErr = client.PollLogin(ctx, loginID)
		return callErr
	})
	return result, err
}

func (m *ManagedProcessClient) CancelLogin(ctx context.Context, loginID string) error {
	return m.withRetry(ctx, func(client managedProcess) error { return client.CancelLogin(ctx, loginID) })
}

func (m *ManagedProcessClient) AccountState(ctx context.Context) (AccountState, error) {
	state := AccountUnknown
	err := m.withRetry(ctx, func(client managedProcess) error {
		var callErr error
		state, callErr = client.AccountState(ctx)
		return callErr
	})
	return state, err
}

func (m *ManagedProcessClient) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var result []Workspace
	err := m.withRetry(ctx, func(client managedProcess) error {
		var callErr error
		result, callErr = client.ListWorkspaces(ctx)
		return callErr
	})
	return result, err
}

func (m *ManagedProcessClient) SupportsModel(ctx context.Context, model, effort string) (bool, error) {
	var result bool
	err := m.withRetry(ctx, func(client managedProcess) error {
		var callErr error
		result, callErr = client.SupportsModel(ctx, model, effort)
		return callErr
	})
	return result, err
}

// LaunchTask deliberately does not use the managed retry path. A transport
// failure after thread/start may mean Codex accepted the task, so retrying
// could launch a duplicate; callers persist an uncertain state instead.
func (m *ManagedProcessClient) LaunchTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID string) (TaskLaunch, error) {
	client := m.current()
	if client == nil {
		return TaskLaunch{}, ErrUnavailable
	}
	return client.LaunchTask(ctx, cwd, model, effort, prompt, clientUserMessageID)
}

func (m *ManagedProcessClient) ExperimentalDynamicTools() bool {
	client := m.current()
	return client != nil && client.ExperimentalDynamicTools()
}

// LaunchHistorianTask is an accepted-launch boundary and therefore remains
// outside recovery, just like LaunchTask.
func (m *ManagedProcessClient) LaunchHistorianTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID, title string, policy HistorianPolicy, dynamic DynamicToolHandler, terminal TurnTerminalHandler) (TaskLaunch, error) {
	client := m.current()
	if client == nil || !client.ExperimentalDynamicTools() {
		return TaskLaunch{}, ErrUnavailable
	}
	return client.LaunchHistorianTask(ctx, cwd, model, effort, prompt, clientUserMessageID, title, policy, dynamic, terminal)
}

func (m *ManagedProcessClient) FindHistorianTask(ctx context.Context, cwd, title string) (TaskLaunch, bool, error) {
	var result TaskLaunch
	var found bool
	err := m.withRetry(ctx, func(client managedProcess) error {
		var callErr error
		result, found, callErr = client.FindHistorianTask(ctx, cwd, title)
		return callErr
	})
	return result, found, err
}

func (m *ManagedProcessClient) ListHistorianTasks(ctx context.Context, cwd string) ([]TaskIdentity, error) {
	var result []TaskIdentity
	err := m.withRetry(ctx, func(client managedProcess) error {
		var callErr error
		result, callErr = client.ListHistorianTasks(ctx, cwd)
		return callErr
	})
	return result, err
}

func (m *ManagedProcessClient) VerifiedHistorianSettings(threadID string) (TaskLaunch, bool) {
	client := m.current()
	if client == nil {
		return TaskLaunch{}, false
	}
	return client.VerifiedHistorianSettings(threadID)
}

// RenameHistorianTask is part of the accepted-launch boundary and deliberately
// avoids managed retry. A transport failure after thread/name/set may mean the
// rename succeeded, so the scheduler must preserve uncertainty instead of
// replaying the mutation against a restarted App Server.
func (m *ManagedProcessClient) RenameHistorianTask(ctx context.Context, threadID, title string) error {
	client := m.current()
	if client == nil {
		return ErrUnavailable
	}
	return client.RenameHistorianTask(ctx, threadID, title)
}

func (m *ManagedProcessClient) SetEventHandler(handler func(Event)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.handler = handler
	client := m.client
	m.mu.Unlock()
	if client != nil {
		client.SetEventHandler(handler)
	}
}

func (m *ManagedProcessClient) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	client := m.client
	m.client = nil
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if client != nil {
		return client.Close()
	}
	// An in-flight factory observes closed under mu and closes any candidate it
	// returns. Do not wait here: a test or factory callback may call Close
	// reentrantly, and no child can be installed after the closed check.
	return nil
}

var _ Client = (*ManagedProcessClient)(nil)
var _ ArchaeologyClient = (*ManagedProcessClient)(nil)
var _ ExperimentalArchaeologyClient = (*ManagedProcessClient)(nil)
var _ HistorianTaskFinder = (*ManagedProcessClient)(nil)
var _ HistorianTaskInventory = (*ManagedProcessClient)(nil)
var _ HistorianTaskRenamer = (*ManagedProcessClient)(nil)

// InterruptTurn is intentionally not retried. Codex may accept an interrupt
// before the transport reports an error, so replaying it crosses an uncertain
// mutation boundary.
func (m *ManagedProcessClient) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	client := m.current()
	if client == nil {
		return ErrUnavailable
	}
	return client.InterruptTurn(ctx, threadID, turnID)
}
