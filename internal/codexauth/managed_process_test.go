package codexauth

import (
	"context"
	"errors"
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

func newManagedProcessTestDouble(available, experimental bool) *managedProcessTestDouble {
	return &managedProcessTestDouble{available: available, experimental: experimental}
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
	return err
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
