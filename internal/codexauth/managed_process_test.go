package codexauth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// managedProcessTestDouble is deliberately small but implements the complete
// private process seam. Its counters record lifecycle transitions, rather than
// merely method invocations, so the tests prove the state-machine branches.
type managedProcessTestDouble struct {
	mu             sync.Mutex
	available      bool
	experimental   bool
	closed         bool
	done           chan struct{}
	exitOnce       sync.Once
	exitErr        error
	pid            int
	closeCount     int
	handler        func(Event)
	callErr        error
	launchErr      error
	renameErr      error
	interruptErr   error
	closeErr       error
	launchCalls    int
	renameCalls    int
	interruptCalls int
}

type capturedLifecycleRecord struct {
	message string
	attrs   map[string]slog.Value
}

type captureLifecycleHandler struct {
	mu      sync.Mutex
	records []capturedLifecycleRecord
}

func (h *captureLifecycleHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureLifecycleHandler) Handle(_ context.Context, record slog.Record) error {
	captured := capturedLifecycleRecord{message: record.Message, attrs: make(map[string]slog.Value)}
	record.Attrs(func(attr slog.Attr) bool {
		captured.attrs[attr.Key] = attr.Value
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, captured)
	h.mu.Unlock()
	return nil
}

func (h *captureLifecycleHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *captureLifecycleHandler) WithGroup(string) slog.Handler { return h }

func (h *captureLifecycleHandler) snapshot() []capturedLifecycleRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]capturedLifecycleRecord, len(h.records))
	for index, record := range h.records {
		result[index] = capturedLifecycleRecord{message: record.message, attrs: make(map[string]slog.Value, len(record.attrs))}
		for key, value := range record.attrs {
			result[index].attrs[key] = value
		}
	}
	return result
}

func newManagedProcessTestDouble(available, experimental bool) *managedProcessTestDouble {
	return &managedProcessTestDouble{available: available, experimental: experimental, done: make(chan struct{})}
}

func (p *managedProcessTestDouble) Available() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.available && !p.closed
}

func (p *managedProcessTestDouble) SetEventHandler(handler func(Event)) {
	p.mu.Lock()
	p.handler = handler
	p.mu.Unlock()
}

func (p *managedProcessTestDouble) Close() error {
	p.mu.Lock()
	err := p.closeErr
	if !p.closed {
		p.closed = true
		p.available = false
		p.closeCount++
	}
	p.mu.Unlock()
	if p.done != nil {
		p.exitOnce.Do(func() { close(p.done) })
	}
	return err
}

func (p *managedProcessTestDouble) Done() <-chan struct{} { return p.done }

func (p *managedProcessTestDouble) ExitReason() error {
	p.mu.Lock()
	err := p.exitErr
	p.mu.Unlock()
	if err == nil {
		return ErrProcessExited
	}
	return err
}

func (p *managedProcessTestDouble) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

func (p *managedProcessTestDouble) crash(err error) {
	if err == nil {
		err = ErrProcessExited
	}
	p.mu.Lock()
	p.exitErr = err
	p.available = false
	p.closed = true
	p.mu.Unlock()
	if p.done != nil {
		p.exitOnce.Do(func() { close(p.done) })
	}
}

func (p *managedProcessTestDouble) StartDeviceCode(context.Context) (DeviceCode, error) {
	return DeviceCode{}, p.methodError()
}

func (p *managedProcessTestDouble) PollLogin(context.Context, string) (LoginResult, error) {
	return LoginResult{}, p.methodError()
}

func (p *managedProcessTestDouble) CancelLogin(context.Context, string) error {
	return p.methodError()
}

func (p *managedProcessTestDouble) AccountState(context.Context) (AccountState, error) {
	return AccountUnknown, p.methodError()
}

func (p *managedProcessTestDouble) ListWorkspaces(context.Context) ([]Workspace, error) {
	return nil, p.methodError()
}

func (p *managedProcessTestDouble) SupportsModel(context.Context, string, string) (bool, error) {
	return false, p.methodError()
}

func (p *managedProcessTestDouble) LaunchTask(context.Context, string, string, string, string, string) (TaskLaunch, error) {
	p.mu.Lock()
	p.launchCalls++
	err := p.launchErr
	p.mu.Unlock()
	return TaskLaunch{}, err
}

func (p *managedProcessTestDouble) ExperimentalDynamicTools() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.experimental && !p.closed
}

func (p *managedProcessTestDouble) LaunchHistorianTask(context.Context, string, string, string, string, string, string, HistorianPolicy, DynamicToolHandler, TurnTerminalHandler) (TaskLaunch, error) {
	p.mu.Lock()
	p.launchCalls++
	err := p.launchErr
	p.mu.Unlock()
	return TaskLaunch{}, err
}

func (p *managedProcessTestDouble) FindHistorianTask(context.Context, string, string) (TaskLaunch, bool, error) {
	return TaskLaunch{}, false, p.methodError()
}

func (p *managedProcessTestDouble) ListHistorianTasks(context.Context, string) ([]TaskIdentity, error) {
	return nil, p.methodError()
}

func (p *managedProcessTestDouble) VerifiedHistorianSettings(string) (TaskLaunch, bool) {
	return TaskLaunch{}, false
}

func (p *managedProcessTestDouble) RenameHistorianTask(context.Context, string, string) error {
	p.mu.Lock()
	p.renameCalls++
	err := p.renameErr
	p.mu.Unlock()
	return err
}

func (p *managedProcessTestDouble) InterruptTurn(context.Context, string, string) error {
	p.mu.Lock()
	p.interruptCalls++
	err := p.interruptErr
	p.mu.Unlock()
	return err
}

func (p *managedProcessTestDouble) methodError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrUnavailable
	}
	return p.callErr
}

func (p *managedProcessTestDouble) emit(event Event) {
	p.mu.Lock()
	handler := p.handler
	p.mu.Unlock()
	if handler != nil {
		handler(event)
	}
}

func managedProcessTestManager(initial managedProcess, factory processFactory) *ManagedProcessClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &ManagedProcessClient{
		ctx:                 ctx,
		cancel:              cancel,
		client:              initial,
		maxRecoveryAttempts: 3,
		backoff:             func(int) time.Duration { return 0 },
		now:                 time.Now,
		factory:             factory,
	}
}

func processCounter(t *testing.T, process *managedProcessTestDouble) (closeCount, launchCalls, interruptCalls int) {
	t.Helper()
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.closeCount, process.launchCalls, process.interruptCalls
}

func processRenameCounter(t *testing.T, process *managedProcessTestDouble) int {
	t.Helper()
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.renameCalls
}

func TestManagedAvailableIsObservational(t *testing.T) {
	failed := newManagedProcessTestDouble(false, false)
	var spawns atomic.Int32
	m := managedProcessTestManager(failed, func(context.Context, ProcessConfig) (managedProcess, error) {
		spawns.Add(1)
		return newManagedProcessTestDouble(true, false), nil
	})
	if m.Available() {
		t.Fatal("failed process reported available")
	}
	if got := spawns.Load(); got != 0 {
		t.Fatalf("Available spawned %d children, want 0", got)
	}
	if closeCount, _, _ := processCounter(t, failed); closeCount != 0 {
		t.Fatalf("Available closed failed process %d times, want 0", closeCount)
	}
	if m.Available() {
		t.Fatal("second observational check reported available")
	}
	if got := spawns.Load(); got != 0 {
		t.Fatalf("second Available spawned %d children, want 0", got)
	}
	_ = m.Close()
}

func TestManagedRetrySingleAttemptSpawnCloseAndReplay(t *testing.T) {
	initial := newManagedProcessTestDouble(true, false)
	replacement := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
		spawns.Add(1)
		return replacement, nil
	})
	defer m.Close()

	var calls int
	err := m.withRetry(context.Background(), func(process managedProcess) error {
		calls++
		if calls == 1 {
			return ErrProcessExited
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withRetry error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("callback attempts = %d, want exactly 2", calls)
	}
	if got := spawns.Load(); got != 1 {
		t.Fatalf("spawn count = %d, want 1", got)
	}
	if got, _, _ := processCounter(t, initial); got != 1 {
		t.Fatalf("old close transitions = %d, want 1", got)
	}
	m.mu.Lock()
	generation, attempts := m.generation, m.recoveryAttempts
	m.mu.Unlock()
	if generation != 1 || attempts != 0 {
		t.Fatalf("state after healthy replacement = generation %d attempts %d, want 1/0", generation, attempts)
	}
}

func TestManagedConcurrentRetryUsesOneRecoveryOwner(t *testing.T) {
	initial := newManagedProcessTestDouble(true, false)
	replacement := newManagedProcessTestDouble(true, false)
	const callers = 20
	var firstCalls atomic.Int32
	allFirstCalls := make(chan struct{})
	var closeAll sync.Once
	factoryStarted := make(chan struct{})
	releaseFactory := make(chan struct{})
	var spawns atomic.Int32
	m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
		spawns.Add(1)
		close(factoryStarted)
		<-releaseFactory
		return replacement, nil
	})
	m.backoff = func(int) time.Duration { return 0 }
	defer m.Close()

	results := make(chan error, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			err := m.withRetry(context.Background(), func(process managedProcess) error {
				if process != initial {
					return nil
				}
				if firstCalls.Add(1) == callers {
					closeAll.Do(func() { close(allFirstCalls) })
				}
				return ErrProcessExited
			})
			results <- err
		}()
	}
	select {
	case <-allFirstCalls:
	case <-time.After(time.Second):
		t.Fatal("not all callers observed the failed generation")
	}
	select {
	case <-factoryStarted:
	case <-time.After(time.Second):
		t.Fatal("recovery owner did not start factory")
	}
	if got := spawns.Load(); got != 1 {
		t.Fatalf("spawn count while owner blocked = %d, want 1", got)
	}
	close(releaseFactory)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("converged retry error = %v", err)
		}
	}
	if got := spawns.Load(); got != 1 {
		t.Fatalf("spawn count after join = %d, want 1", got)
	}
	if got, _, _ := processCounter(t, initial); got != 1 {
		t.Fatalf("old close transitions = %d, want 1", got)
	}
}

func TestManagedSeparateFailureEpisodesResetBudget(t *testing.T) {
	initial := newManagedProcessTestDouble(true, false)
	firstReplacement := newManagedProcessTestDouble(true, false)
	secondReplacement := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
		switch spawns.Add(1) {
		case 1:
			return firstReplacement, nil
		case 2:
			return secondReplacement, nil
		default:
			return nil, errors.New("unexpected third spawn")
		}
	})
	defer m.Close()

	if err := m.withRetry(context.Background(), func(process managedProcess) error {
		if process == initial {
			return ErrProcessExited
		}
		return nil
	}); err != nil {
		t.Fatalf("first episode = %v", err)
	}
	if err := m.withRetry(context.Background(), func(process managedProcess) error {
		if process == firstReplacement {
			return ErrProtocol
		}
		return nil
	}); err != nil {
		t.Fatalf("second episode = %v", err)
	}
	if got := spawns.Load(); got != 2 {
		t.Fatalf("spawn count for two episodes = %d, want 2", got)
	}
	m.mu.Lock()
	generation, attempts := m.generation, m.recoveryAttempts
	m.mu.Unlock()
	if generation != 2 || attempts != 0 {
		t.Fatalf("state after two healthy resets = generation %d attempts %d, want 2/0", generation, attempts)
	}
}

func TestManagedFailedReplayRetainsBudgetUntilSuccessfulCall(t *testing.T) {
	initial := newManagedProcessTestDouble(true, false)
	firstReplacement := newManagedProcessTestDouble(true, false)
	secondReplacement := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
		switch spawns.Add(1) {
		case 1:
			return firstReplacement, nil
		case 2:
			return secondReplacement, nil
		default:
			return nil, errors.New("unexpected third spawn")
		}
	})
	defer m.Close()
	fakeNow := time.Unix(200, 0)
	m.now = func() time.Time { return fakeNow }
	m.backoff = func(int) time.Duration { return time.Second }

	// The replacement handshakes as available but fails its first replay. That
	// must not reset the failed-episode budget.
	if err := m.withRetry(context.Background(), func(process managedProcess) error {
		switch process {
		case initial:
			return ErrProcessExited
		case firstReplacement:
			return ErrProtocol
		default:
			return nil
		}
	}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("failed replacement replay = %v, want ErrProtocol", err)
	}
	m.mu.Lock()
	attempts := m.recoveryAttempts
	next := m.nextRecoveryAt
	m.mu.Unlock()
	if attempts != 1 || !next.After(fakeNow) {
		t.Fatalf("failed replay state attempts=%d next=%v, want 1 and future cooldown", attempts, next)
	}

	// A retryable call during the cooldown preserves the same episode and does
	// not spawn another child.
	if err := m.withRetry(context.Background(), func(managedProcess) error { return ErrProtocol }); !errors.Is(err, ErrProtocol) {
		t.Fatalf("cooldown replay = %v, want ErrProtocol", err)
	}
	if got := spawns.Load(); got != 1 {
		t.Fatalf("spawn count during failed-replay cooldown = %d, want 1", got)
	}

	fakeNow = fakeNow.Add(2 * time.Second)
	if err := m.withRetry(context.Background(), func(process managedProcess) error {
		if process == firstReplacement {
			return ErrProtocol
		}
		return nil
	}); err != nil {
		t.Fatalf("successful recovery after cooldown = %v", err)
	}
	if got := spawns.Load(); got != 2 {
		t.Fatalf("spawn count after cooldown = %d, want 2", got)
	}
	m.mu.Lock()
	attempts = m.recoveryAttempts
	m.mu.Unlock()
	if attempts != 0 {
		t.Fatalf("successful managed call left attempts=%d, want 0", attempts)
	}
}

func TestManagedFailedCreationCooldownAndBudget(t *testing.T) {
	initial := newManagedProcessTestDouble(true, false)
	initial.callErr = ErrProcessExited
	var nowMu sync.Mutex
	now := time.Unix(100, 0)
	var spawns atomic.Int32
	m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
		spawns.Add(1)
		return nil, errors.New("factory unavailable")
	})
	m.maxRecoveryAttempts = 2
	m.backoff = func(int) time.Duration { return time.Second }
	m.now = func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}
	defer m.Close()
	_, err := m.AccountState(context.Background())
	if !errors.Is(err, ErrProcessExited) {
		t.Fatalf("first factory failure = %v, want original error", err)
	}
	if got := spawns.Load(); got != 1 {
		t.Fatalf("first spawn count = %d, want 1", got)
	}
	_, err = m.AccountState(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cooldown error = %v, want original error", err)
	}
	if got := spawns.Load(); got != 1 {
		t.Fatalf("spawn count during cooldown = %d, want 1", got)
	}
	nowMu.Lock()
	now = now.Add(2 * time.Second)
	nowMu.Unlock()
	_, err = m.AccountState(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second factory failure = %v, want original error", err)
	}
	if got := spawns.Load(); got != 2 {
		t.Fatalf("spawn count after cooldown = %d, want 2", got)
	}
	nowMu.Lock()
	now = now.Add(2 * time.Second)
	nowMu.Unlock()
	_, err = m.AccountState(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("budget exhaustion error = %v, want original error", err)
	}
	if got := spawns.Load(); got != 2 {
		t.Fatalf("spawn count after budget exhaustion = %d, want 2", got)
	}
	if got, _, _ := processCounter(t, initial); got != 1 {
		t.Fatalf("failed generation close transitions = %d, want 1", got)
	}
}

func TestManagedCloseRaceDoesNotInstallLateChild(t *testing.T) {
	initial := newManagedProcessTestDouble(true, false)
	candidate := newManagedProcessTestDouble(true, false)
	factoryStarted := make(chan struct{})
	releaseFactory := make(chan struct{})
	m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
		close(factoryStarted)
		<-releaseFactory
		return candidate, nil
	})
	defer m.Close()

	result := make(chan error, 1)
	go func() {
		result <- m.withRetry(context.Background(), func(managedProcess) error { return ErrProcessExited })
	}()
	select {
	case <-factoryStarted:
	case <-time.After(time.Second):
		t.Fatal("factory did not start")
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}
	close(releaseFactory)
	if err := <-result; !errors.Is(err, ErrProcessExited) {
		t.Fatalf("close-raced retry = %v, want original error", err)
	}
	if candidate.Available() {
		t.Fatal("late factory child remained available after Close")
	}
	if got, _, _ := processCounter(t, candidate); got != 1 {
		t.Fatalf("late child close transitions = %d, want 1", got)
	}
	m.mu.Lock()
	installed := m.client != nil
	closed := m.closed
	m.mu.Unlock()
	if installed || !closed {
		t.Fatalf("close state installed=%v closed=%v, want false/true", installed, closed)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second Close error = %v", err)
	}
}

func TestManagedClosePreservesChildCloseError(t *testing.T) {
	initial := newManagedProcessTestDouble(true, false)
	closeErr := errors.New("child close failed")
	initial.closeErr = closeErr
	m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
		t.Fatal("Close must not start recovery")
		return nil, nil
	})
	if err := m.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close error = %v, want child close error %v", err, closeErr)
	}
	if got, _, _ := processCounter(t, initial); got != 1 {
		t.Fatalf("child close transitions = %d, want 1", got)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second Close error = %v, want nil", err)
	}
}

func TestManagedRecoveryPropagatesLatestHandler(t *testing.T) {
	initial := newManagedProcessTestDouble(true, false)
	replacement := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
		spawns.Add(1)
		return replacement, nil
	})
	defer m.Close()

	firstEvent := make(chan Event, 1)
	m.SetEventHandler(func(event Event) { firstEvent <- event })
	var calls int
	if err := m.withRetry(context.Background(), func(process managedProcess) error {
		calls++
		if process == initial {
			return ErrProcessExited
		}
		return nil
	}); err != nil {
		t.Fatalf("recovery with first handler = %v", err)
	}
	replacement.emit(Event{Kind: "first"})
	select {
	case event := <-firstEvent:
		if event.Kind != "first" {
			t.Fatalf("first event kind = %q, want first", event.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not inherit first handler")
	}
	secondEvent := make(chan Event, 1)
	m.SetEventHandler(func(event Event) { secondEvent <- event })
	replacement.emit(Event{Kind: "second"})
	select {
	case event := <-secondEvent:
		if event.Kind != "second" {
			t.Fatalf("second event kind = %q, want second", event.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not receive updated handler")
	}
	if got := spawns.Load(); got != 1 || calls != 2 {
		t.Fatalf("handler recovery evidence spawns=%d calls=%d, want 1/2", got, calls)
	}
}

func TestManagedUncertainMutationsDoNotRetry(t *testing.T) {
	tests := []struct {
		name         string
		call         func(*ManagedProcessClient, context.Context) error
		count        func(*managedProcessTestDouble) int
		experimental bool
	}{
		{
			name: "launch task",
			call: func(m *ManagedProcessClient, ctx context.Context) error {
				_, err := m.LaunchTask(ctx, "/tmp/project", "model", "max", "prompt", "message")
				return err
			},
			count: func(p *managedProcessTestDouble) int { _, calls, _ := processCounter(t, p); return calls },
		},
		{
			name: "launch historian task",
			call: func(m *ManagedProcessClient, ctx context.Context) error {
				_, err := m.LaunchHistorianTask(ctx, "/tmp/project", "model", "max", "prompt", "message", "title", HistorianPolicy{}, nil, nil)
				return err
			},
			count:        func(p *managedProcessTestDouble) int { _, calls, _ := processCounter(t, p); return calls },
			experimental: true,
		},
		{
			name: "rename historian task",
			call: func(m *ManagedProcessClient, ctx context.Context) error {
				return m.RenameHistorianTask(ctx, "thread", "title")
			},
			count: func(p *managedProcessTestDouble) int { return processRenameCounter(t, p) },
		},
		{
			name: "interrupt turn",
			call: func(m *ManagedProcessClient, ctx context.Context) error {
				return m.InterruptTurn(ctx, "thread", "turn")
			},
			count:        func(p *managedProcessTestDouble) int { _, _, calls := processCounter(t, p); return calls },
			experimental: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initial := newManagedProcessTestDouble(true, test.experimental)
			initial.launchErr = ErrProcessExited
			initial.renameErr = ErrProcessExited
			initial.interruptErr = ErrProcessExited
			var spawns atomic.Int32
			m := managedProcessTestManager(initial, func(context.Context, ProcessConfig) (managedProcess, error) {
				spawns.Add(1)
				return newManagedProcessTestDouble(true, test.experimental), nil
			})
			defer m.Close()
			if err := test.call(m, context.Background()); !errors.Is(err, ErrProcessExited) {
				t.Fatalf("mutation error = %v, want original process error", err)
			}
			if got := spawns.Load(); got != 0 {
				t.Fatalf("uncertain mutation spawned %d replacements, want 0", got)
			}
			if got := test.count(initial); got != 1 {
				t.Fatalf("uncertain mutation attempts = %d, want 1", got)
			}
		})
	}
}

func waitManagedInitial(t *testing.T, manager *ManagedProcessClient) {
	t.Helper()
	select {
	case err := <-manager.initialReady:
		if err != nil {
			t.Fatalf("initial supervisor spawn = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initial supervisor spawn timed out")
	}
}

func waitManagedSnapshot(t *testing.T, manager *ManagedProcessClient, predicate func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := manager.Snapshot()
		if predicate(snapshot) {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("supervisor snapshot did not converge: %+v", manager.Snapshot())
	return Snapshot{}
}

func TestManagedSupervisorAutonomousExitRecovery(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	second := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		if spawns.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, ManagedProcessOptions{Backoff: func(int) time.Duration { return 0 }, HealthyInterval: time.Hour})
	defer manager.Close()
	waitManagedInitial(t, manager)
	if got := manager.Snapshot(); got.State != ProcessStateAvailable || got.Generation != 1 {
		t.Fatalf("initial snapshot = %+v, want available generation 1", got)
	}

	// No request is made after this point. The child lifecycle channel alone
	// must drive replacement.
	first.crash(ErrProcessExited)
	snapshot := waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.State == ProcessStateAvailable && snapshot.Generation == 2
	})
	if got := spawns.Load(); got != 2 {
		t.Fatalf("autonomous spawn count = %d, want 2", got)
	}
	if snapshot.LastExitReason != "process_exited" {
		t.Fatalf("last exit reason = %q, want process_exited", snapshot.LastExitReason)
	}
}

func TestManagedSupervisorStateChangesWakeAndClose(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	second := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		if spawns.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, ManagedProcessOptions{Backoff: func(int) time.Duration { return 0 }, HealthyInterval: time.Hour})
	waitManagedInitial(t, manager)
	stateChange := manager.StateChanges()
	if stateChange == nil {
		t.Fatal("StateChanges returned nil for autonomous manager")
	}
	first.crash(ErrProcessExited)
	select {
	case <-stateChange:
	case <-time.After(time.Second):
		t.Fatal("state-change channel did not wake after child exit")
	}
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.Generation == 2 && snapshot.State == ProcessStateAvailable
	})

	closeChange := manager.StateChanges()
	if err := manager.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	select {
	case <-closeChange:
	case <-time.After(time.Second):
		t.Fatal("state-change channel did not wake on Close")
	}
}

func TestManagedSupervisorInitialFactoryFailureRecoversWithoutTraffic(t *testing.T) {
	candidate := newManagedProcessTestDouble(true, false)
	var calls atomic.Int32
	secondFactoryStarted := make(chan struct{})
	releaseSecondFactory := make(chan struct{})
	manager := NewManagedProcessAsyncWithOptions(context.Background(), ProcessConfig{}, ManagedProcessOptions{
		MaxRestartAttempts: 3,
		Backoff:            func(int) time.Duration { return 20 * time.Millisecond },
		HealthyInterval:    time.Hour,
		Factory: func(context.Context, ProcessConfig) (Client, error) {
			switch calls.Add(1) {
			case 1:
				return nil, errors.New("initial factory unavailable")
			case 2:
				close(secondFactoryStarted)
				<-releaseSecondFactory
				return candidate, nil
			default:
				return nil, errors.New("unexpected extra initial factory call")
			}
		},
	})
	defer manager.Close()
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.State == ProcessStateDegraded && snapshot.Generation == 0 && snapshot.Attempts == 1
	})
	select {
	case <-secondFactoryStarted:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not retry initial factory")
	}
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool { return snapshot.State == ProcessStateRecovering })
	close(releaseSecondFactory)
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.State == ProcessStateAvailable && snapshot.Generation == 1
	})
	if got := calls.Load(); got != 2 {
		t.Fatalf("initial recovery factory calls = %d, want 2", got)
	}
}

func TestManagedSupervisorInitialFactoryFailuresExhaustWithoutTraffic(t *testing.T) {
	var calls atomic.Int32
	manager := NewManagedProcessAsyncWithOptions(context.Background(), ProcessConfig{}, ManagedProcessOptions{
		MaxRestartAttempts: 2,
		Backoff:            func(int) time.Duration { return 0 },
		Factory: func(context.Context, ProcessConfig) (Client, error) {
			calls.Add(1)
			return nil, errors.New("initial factory unavailable")
		},
	})
	defer manager.Close()
	snapshot := waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool { return snapshot.State == ProcessStateExhausted })
	if got := calls.Load(); got != 2 {
		t.Fatalf("initial exhaustion factory calls = %d, want 2", got)
	}
	if snapshot.Generation != 0 || snapshot.RestartBudget != 0 || snapshot.Reason != "factory_failure" {
		t.Fatalf("initial exhaustion snapshot = %+v", snapshot)
	}
}

func TestManagedSupervisorLifecycleLogsAreSanitized(t *testing.T) {
	handler := &captureLifecycleHandler{}
	logger := slog.New(handler)
	first := newManagedProcessTestDouble(true, false)
	second := newManagedProcessTestDouble(true, false)
	const exitSecret = "exit secret prompt=super-secret"
	const factorySecret = "factory secret --token=super-secret"
	var calls atomic.Int32
	manager := NewManagedProcessAsyncWithOptions(context.Background(), ProcessConfig{}, ManagedProcessOptions{
		Logger:             logger,
		MaxRestartAttempts: 2,
		Backoff:            func(int) time.Duration { return 0 },
		HealthyInterval:    time.Hour,
		Factory: func(context.Context, ProcessConfig) (Client, error) {
			switch calls.Add(1) {
			case 1:
				return first, nil
			case 2:
				return second, nil
			default:
				return nil, errors.New(factorySecret)
			}
		},
	})
	defer manager.Close()
	waitManagedInitial(t, manager)
	first.crash(errors.New(exitSecret))
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.State == ProcessStateAvailable && snapshot.Generation == 2
	})
	second.crash(errors.New(exitSecret))
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.State == ProcessStateExhausted
	})

	records := handler.snapshot()
	seen := make(map[string]int)
	allowedKeys := map[string]bool{
		"state": true, "generation": true, "pid": true, "attempt": true,
		"max_attempts": true, "reason": true, "phase": true,
	}
	for _, record := range records {
		seen[record.message]++
		if strings.Contains(record.message, "secret") {
			t.Fatalf("lifecycle message contains unsanitized text: %q", record.message)
		}
		for key, value := range record.attrs {
			if !allowedKeys[key] {
				t.Fatalf("lifecycle record contains unapproved field %q in %q", key, record.message)
			}
			if value.Kind() == slog.KindString && (strings.Contains(value.String(), exitSecret) || strings.Contains(value.String(), factorySecret)) {
				t.Fatalf("lifecycle field %q contains unsanitized text", key)
			}
		}
		for _, key := range []string{"state", "generation", "pid", "attempt", "max_attempts", "reason", "phase"} {
			if _, ok := record.attrs[key]; !ok {
				t.Fatalf("lifecycle record %q missing %q", record.message, key)
			}
		}
	}
	for _, message := range []string{lifecycleChildExit, lifecycleRestartAttempt, lifecycleRestartResult, lifecycleRestartExhausted} {
		if seen[message] == 0 {
			t.Fatalf("lifecycle logs missing %q; records=%v", message, seen)
		}
	}
	if seen[lifecycleChildExit] < 2 {
		t.Fatalf("child exit logs=%d, want at least two", seen[lifecycleChildExit])
	}
	if seen[lifecycleRestartExhausted] != 1 {
		t.Fatalf("terminal exhaustion logs=%d, want one", seen[lifecycleRestartExhausted])
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("factory calls=%d, want initial plus one replacement and one failed attempt", got)
	}
}

func TestManagedSupervisorConcurrentCallersHaveSingleSpawnOwner(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	second := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		if spawns.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, ManagedProcessOptions{Backoff: func(int) time.Duration { return 0 }, HealthyInterval: time.Hour})
	defer manager.Close()
	waitManagedInitial(t, manager)
	first.crash(ErrProcessExited)
	const callers = 24
	results := make(chan error, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			_, err := manager.AccountState(context.Background())
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent read = %v", err)
		}
	}
	if got := spawns.Load(); got != 2 {
		t.Fatalf("concurrent supervisor spawn count = %d, want 2", got)
	}
}

func TestManagedSupervisorConcurrentFailuresDoNotDropGenerationEvent(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	first.callErr = ErrProcessExited
	second := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		if spawns.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, ManagedProcessOptions{MaxRestartAttempts: 1, Backoff: func(int) time.Duration { return 0 }, HealthyInterval: time.Hour})
	defer manager.Close()
	waitManagedInitial(t, manager)

	// More failures than the supervisor event buffer can hold arrive together.
	// Every caller must converge on the one degraded episode and its one
	// replacement, without relying on a later request to notice the failure.
	const callers = 64
	start := make(chan struct{})
	results := make(chan error, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := manager.AccountState(ctx)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent read = %v", err)
		}
	}
	snapshot := waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.State == ProcessStateAvailable && snapshot.Generation == 2
	})
	if got := spawns.Load(); got != 2 {
		t.Fatalf("concurrent failure spawn count = %d, want 2", got)
	}
	if snapshot.Attempts != 1 {
		t.Fatalf("concurrent failure attempts = %d, want one shared attempt", snapshot.Attempts)
	}
}

func TestManagedSupervisorRepeatedCrashesExhaustSharedBudget(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	second := newManagedProcessTestDouble(true, false)
	third := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		switch spawns.Add(1) {
		case 1:
			return first, nil
		case 2:
			return second, nil
		case 3:
			return third, nil
		default:
			return nil, errors.New("unexpected fourth spawn")
		}
	}, ManagedProcessOptions{MaxRestartAttempts: 2, Backoff: func(int) time.Duration { return 0 }, HealthyInterval: time.Hour})
	defer manager.Close()
	waitManagedInitial(t, manager)
	first.crash(ErrProcessExited)
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.Generation == 2 && snapshot.State == ProcessStateAvailable
	})
	second.crash(ErrProtocol)
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.Generation == 3 && snapshot.State == ProcessStateAvailable
	})
	third.crash(ErrProcessExited)
	snapshot := waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool { return snapshot.State == ProcessStateExhausted })
	if got := spawns.Load(); got != 3 {
		t.Fatalf("exhausted spawn count = %d, want 3", got)
	}
	if snapshot.RestartBudget != 0 || snapshot.Attempts != 2 {
		t.Fatalf("exhausted budget snapshot = %+v, want attempts 2/budget 0", snapshot)
	}
}

func TestManagedSupervisorResetsBudgetOnlyAfterSustainedHealth(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	second := newManagedProcessTestDouble(true, false)
	third := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	now := time.Unix(100, 0)
	var nowMu sync.Mutex
	clock := func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		switch spawns.Add(1) {
		case 1:
			return first, nil
		case 2:
			return second, nil
		default:
			return third, nil
		}
	}, ManagedProcessOptions{MaxRestartAttempts: 1, Backoff: func(int) time.Duration { return 0 }, HealthyInterval: 10 * time.Second, Now: clock})
	defer manager.Close()
	waitManagedInitial(t, manager)
	first.crash(ErrProcessExited)
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool { return snapshot.Generation == 2 && snapshot.Attempts == 1 })

	// A successful RPC before the interval does not replenish the budget.
	if _, err := manager.AccountState(context.Background()); err != nil {
		t.Fatalf("pre-interval read = %v", err)
	}
	if got := manager.Snapshot(); got.Attempts != 1 {
		t.Fatalf("pre-interval attempts = %d, want 1", got.Attempts)
	}
	nowMu.Lock()
	now = now.Add(11 * time.Second)
	nowMu.Unlock()
	if _, err := manager.AccountState(context.Background()); err != nil {
		t.Fatalf("post-interval read = %v", err)
	}
	if got := manager.Snapshot(); got.Attempts != 0 || got.RestartBudget != 1 {
		t.Fatalf("post-interval snapshot = %+v, want reset budget", got)
	}
	second.crash(ErrProcessExited)
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.Generation == 3 && snapshot.State == ProcessStateAvailable
	})
	if got := spawns.Load(); got != 3 {
		t.Fatalf("post-reset spawn count = %d, want 3", got)
	}
}

func TestManagedSupervisorCloseDuringBackoffAndSpawn(t *testing.T) {
	t.Run("backoff", func(t *testing.T) {
		first := newManagedProcessTestDouble(true, false)
		var spawns atomic.Int32
		manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
			spawns.Add(1)
			return first, nil
		}, ManagedProcessOptions{Backoff: func(int) time.Duration { return time.Hour }})
		waitManagedInitial(t, manager)
		first.crash(ErrProcessExited)
		waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool { return snapshot.State == ProcessStateDegraded })
		if err := manager.Close(); err != nil {
			t.Fatalf("close during backoff = %v", err)
		}
		if got := spawns.Load(); got != 1 {
			t.Fatalf("backoff close spawn count = %d, want 1", got)
		}
		if got := manager.Snapshot(); got.State != ProcessStateClosed {
			t.Fatalf("backoff close state = %q, want closed", got.State)
		}
	})

	t.Run("spawn", func(t *testing.T) {
		first := newManagedProcessTestDouble(true, false)
		candidate := newManagedProcessTestDouble(true, false)
		factoryStarted := make(chan struct{})
		releaseFactory := make(chan struct{})
		var spawns atomic.Int32
		manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
			if spawns.Add(1) == 1 {
				return first, nil
			}
			close(factoryStarted)
			<-releaseFactory
			return candidate, nil
		}, ManagedProcessOptions{Backoff: func(int) time.Duration { return 0 }})
		waitManagedInitial(t, manager)
		first.crash(ErrProcessExited)
		select {
		case <-factoryStarted:
		case <-time.After(time.Second):
			t.Fatal("replacement factory did not start")
		}
		if err := manager.Close(); err != nil {
			t.Fatalf("close during spawn = %v", err)
		}
		close(releaseFactory)
		waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool { return snapshot.State == ProcessStateClosed && !candidate.Available() })
		if got := spawns.Load(); got != 2 {
			t.Fatalf("spawn-race count = %d, want 2", got)
		}
	})
}

func TestManagedSupervisorIgnoresStaleGenerationEvent(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	second := newManagedProcessTestDouble(true, false)
	var spawns atomic.Int32
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		if spawns.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, ManagedProcessOptions{Backoff: func(int) time.Duration { return 0 }})
	defer manager.Close()
	waitManagedInitial(t, manager)
	first.crash(ErrProcessExited)
	waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.Generation == 2 && snapshot.State == ProcessStateAvailable
	})
	manager.supervisorEvents <- supervisorEvent{generation: 1, client: first, err: ErrProtocol}
	time.Sleep(10 * time.Millisecond)
	snapshot := manager.Snapshot()
	if snapshot.State != ProcessStateAvailable || snapshot.Generation != 2 {
		t.Fatalf("stale event changed snapshot = %+v", snapshot)
	}
	if got := spawns.Load(); got != 2 {
		t.Fatalf("stale event spawned %d children, want 2", got)
	}
}

func TestManagedSupervisorRejectsCandidateWithoutLifecycle(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	replacement := newManagedProcessTestDouble(true, false)
	// A nil Done channel is not an autonomous lifecycle signal. The supervisor
	// must close and reject this candidate instead of silently losing recovery.
	replacement.done = nil
	var spawns atomic.Int32
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		if spawns.Add(1) == 1 {
			return first, nil
		}
		return replacement, nil
	}, ManagedProcessOptions{MaxRestartAttempts: 1, Backoff: func(int) time.Duration { return 0 }, HealthyInterval: time.Hour})
	defer manager.Close()
	waitManagedInitial(t, manager)
	first.crash(ErrProcessExited)
	snapshot := waitManagedSnapshot(t, manager, func(snapshot Snapshot) bool {
		return snapshot.State == ProcessStateExhausted
	})
	if got := spawns.Load(); got != 2 {
		t.Fatalf("lifecycle-less candidate spawn count = %d, want 2", got)
	}
	if replacement.Available() {
		t.Fatal("lifecycle-less candidate remained available")
	}
	if snapshot.Generation != 1 || snapshot.Attempts != 1 {
		t.Fatalf("lifecycle-less candidate snapshot = %+v, want generation 1/attempt 1", snapshot)
	}
}

func TestManagedSupervisorNonIdempotentLaunchNeverReplays(t *testing.T) {
	first := newManagedProcessTestDouble(true, false)
	first.launchErr = ErrProcessExited
	var spawns atomic.Int32
	manager := newAutonomousManagedProcess(context.Background(), ProcessConfig{}, func(context.Context, ProcessConfig) (managedProcess, error) {
		spawns.Add(1)
		return first, nil
	}, ManagedProcessOptions{Backoff: func(int) time.Duration { return 0 }})
	defer manager.Close()
	waitManagedInitial(t, manager)
	if _, err := manager.LaunchTask(context.Background(), "/tmp/project", "model", "max", "prompt", "message"); !errors.Is(err, ErrProcessExited) {
		t.Fatalf("launch error = %v, want ErrProcessExited", err)
	}
	if got := spawns.Load(); got != 1 {
		t.Fatalf("non-idempotent launch factory calls = %d, want 1 initial call only", got)
	}
	if _, calls, _ := processCounter(t, first); calls != 1 {
		t.Fatalf("non-idempotent launch calls = %d, want 1", calls)
	}
}
