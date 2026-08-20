package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
	"codex-commons/internal/runtimehealth"
)

// phase2AcceptanceChild is a complete public Codex capability double. Its
// lifecycle channel is what the real managed supervisor watches, so this
// acceptance test exercises autonomous replacement rather than request-driven
// recovery.
type phase2AcceptanceChild struct {
	mu         sync.Mutex
	available  bool
	closed     bool
	exitErr    error
	done       chan struct{}
	exitOnce   sync.Once
	workspace  string
	launches   *atomic.Int32
	closeCount atomic.Int32
}

func newPhase2AcceptanceChild(workspace string, launches *atomic.Int32) *phase2AcceptanceChild {
	return &phase2AcceptanceChild{
		available: true, done: make(chan struct{}), workspace: workspace, launches: launches,
	}
}

func (c *phase2AcceptanceChild) Available() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.available && !c.closed
}

func (c *phase2AcceptanceChild) StartDeviceCode(context.Context) (codexauth.DeviceCode, error) {
	return codexauth.DeviceCode{}, c.methodError()
}

func (c *phase2AcceptanceChild) PollLogin(context.Context, string) (codexauth.LoginResult, error) {
	return codexauth.LoginResult{}, c.methodError()
}

func (c *phase2AcceptanceChild) CancelLogin(context.Context, string) error { return c.methodError() }

func (c *phase2AcceptanceChild) AccountState(context.Context) (codexauth.AccountState, error) {
	if err := c.methodError(); err != nil {
		return codexauth.AccountUnknown, err
	}
	return codexauth.AccountSignedIn, nil
}

func (c *phase2AcceptanceChild) SetEventHandler(func(codexauth.Event)) {}

func (c *phase2AcceptanceChild) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.available = false
		c.closeCount.Add(1)
	}
	c.mu.Unlock()
	c.exitOnce.Do(func() { close(c.done) })
	return nil
}

func (c *phase2AcceptanceChild) Done() <-chan struct{} { return c.done }

func (c *phase2AcceptanceChild) ExitReason() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exitErr != nil {
		return c.exitErr
	}
	return codexauth.ErrProcessExited
}

func (c *phase2AcceptanceChild) ListWorkspaces(context.Context) ([]codexauth.Workspace, error) {
	if err := c.methodError(); err != nil {
		return nil, err
	}
	return []codexauth.Workspace{{CWD: c.workspace, UpdatedAt: time.Now().UTC()}}, nil
}

func (c *phase2AcceptanceChild) SupportsModel(context.Context, string, string) (bool, error) {
	if err := c.methodError(); err != nil {
		return false, err
	}
	return true, nil
}

func (c *phase2AcceptanceChild) LaunchTask(context.Context, string, string, string, string, string) (codexauth.TaskLaunch, error) {
	return codexauth.TaskLaunch{}, c.methodError()
}

func (c *phase2AcceptanceChild) ExperimentalDynamicTools() bool { return c.Available() }

func (c *phase2AcceptanceChild) LaunchHistorianTask(context.Context, string, string, string, string, string, string, codexauth.HistorianPolicy, codexauth.DynamicToolHandler, codexauth.TurnTerminalHandler) (codexauth.TaskLaunch, error) {
	if err := c.methodError(); err != nil {
		return codexauth.TaskLaunch{}, err
	}
	call := c.launches.Add(1)
	return codexauth.TaskLaunch{
		ThreadID:  "acceptance-thread-" + string(rune('a'+call-1)),
		SessionID: "acceptance-session",
		TurnID:    "acceptance-turn",
	}, nil
}

func (c *phase2AcceptanceChild) InterruptTurn(context.Context, string, string) error {
	return c.methodError()
}

func (c *phase2AcceptanceChild) FindHistorianTask(context.Context, string, string) (codexauth.TaskLaunch, bool, error) {
	if err := c.methodError(); err != nil {
		return codexauth.TaskLaunch{}, false, err
	}
	return codexauth.TaskLaunch{}, false, nil
}

func (c *phase2AcceptanceChild) ListHistorianTasks(context.Context, string) ([]codexauth.TaskIdentity, error) {
	if err := c.methodError(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (c *phase2AcceptanceChild) VerifiedHistorianSettings(string) (codexauth.TaskLaunch, bool) {
	return codexauth.TaskLaunch{}, false
}

func (c *phase2AcceptanceChild) RenameHistorianTask(context.Context, string, string) error {
	return c.methodError()
}

func (c *phase2AcceptanceChild) methodError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || !c.available {
		return codexauth.ErrUnavailable
	}
	return nil
}

func (c *phase2AcceptanceChild) crash(err error) {
	if err == nil {
		err = codexauth.ErrProcessExited
	}
	c.mu.Lock()
	c.exitErr = err
	c.available = false
	c.closed = true
	c.mu.Unlock()
	c.exitOnce.Do(func() { close(c.done) })
}

type phase2AcceptanceFactory struct {
	workspace       string
	launches        atomic.Int32
	factoryCalls    atomic.Int32
	children        chan *phase2AcceptanceChild
	recoveryStarted chan struct{}
	allowRecovery   chan struct{}
	recoveryOnce    sync.Once
	allowOnce       sync.Once
}

func newPhase2AcceptanceFactory(workspace string) *phase2AcceptanceFactory {
	return &phase2AcceptanceFactory{
		workspace: workspace, children: make(chan *phase2AcceptanceChild, 2),
		recoveryStarted: make(chan struct{}), allowRecovery: make(chan struct{}),
	}
}

func (f *phase2AcceptanceFactory) create(ctx context.Context, _ codexauth.ProcessConfig) (codexauth.Client, error) {
	call := f.factoryCalls.Add(1)
	switch call {
	case 1:
		child := newPhase2AcceptanceChild(f.workspace, &f.launches)
		f.children <- child
		return child, nil
	case 2:
		f.recoveryOnce.Do(func() { close(f.recoveryStarted) })
		select {
		case <-f.allowRecovery:
			child := newPhase2AcceptanceChild(f.workspace, &f.launches)
			f.children <- child
			return child, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	default:
		return nil, errors.New("acceptance factory exhausted")
	}
}

func (f *phase2AcceptanceFactory) allow() { f.allowOnce.Do(func() { close(f.allowRecovery) }) }

func phase2AcceptanceWait(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal(message)
		case <-ticker.C:
		}
	}
}

func phase2AcceptanceAssertStable(t *testing.T, duration time.Duration, want int32, read func() int32, message string) {
	t.Helper()
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if got := read(); got != want {
			t.Fatalf("%s: got %d want %d", message, got, want)
		}
		select {
		case <-deadline.C:
			return
		case <-ticker.C:
		}
	}
}

func phase2AcceptanceReadiness(t *testing.T, app *App) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+app.config.Listen+"/v1/internal/readiness", nil)
	req.Host = app.config.Listen
	req.RemoteAddr = "127.0.0.1:43127"
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	return rec
}

func phase2AcceptanceWeb(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>acceptance</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPhase2DisposableAcceptanceManagedRecoveryAndExhaustion(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := filepath.Abs(filepath.Join(workingDirectory, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	factory := newPhase2AcceptanceFactory(workspace)
	manager := codexauth.NewManagedProcessAsyncWithOptions(ctx, codexauth.ProcessConfig{Executable: "/usr/bin/codex", EnableExperimentalDynamicTools: true}, codexauth.ManagedProcessOptions{
		Factory:            factory.create,
		MaxRestartAttempts: 1,
		Backoff:            func(int) time.Duration { return 0 },
		// The monitor's successful account/model observations reset this test
		// manager's episode budget immediately after a recovered generation.
		HealthyInterval: -time.Nanosecond,
	})

	config := DefaultConfig()
	config.Listen = "127.0.0.1:0"
	config.DatabasePath = filepath.Join(t.TempDir(), "acceptance.sqlite")
	config.WebDir = phase2AcceptanceWeb(t)
	config.CodexAuth = true
	config.RequireCodexReady = true
	config.CodexBin = "/usr/bin/codex"
	config.CodexClient = manager
	config.EnableExperimentalHistorian = true
	config.CodexBindingKeySet = true
	config.CodexBindingKey[0] = 1
	app, err := New(ctx, config, nil)
	if err != nil {
		factory.allow()
		_ = manager.Close()
		t.Fatal(err)
	}
	defer func() {
		factory.allow()
		_ = app.Close()
	}()

	var first *phase2AcceptanceChild
	select {
	case first = <-factory.children:
	case <-time.After(time.Second):
		t.Fatal("managed supervisor did not create its initial child")
	}
	phase2AcceptanceWait(t, time.Second, manager.Available, "initial managed generation did not become available")
	select {
	case <-manager.StateChanges():
	default:
	}
	select {
	case <-app.runtime.trigger:
	default:
		app.runtime.trigger <- struct{}{}
	}
	phase2AcceptanceWait(t, time.Second, func() bool { return app.runtime.RuntimeSnapshot().Ready }, "required runtime did not become ready")
	if got := phase2AcceptanceReadiness(t, app); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"ready":true`) {
		t.Fatalf("healthy readiness code=%d body=%s", got.Code, got.Body.String())
	}

	// Queue one native historian through the real application scheduler. The
	// bridge's cached runtime gate allows exactly one accepted launch.
	discovered, err := app.service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "acceptance-discover")
	if err != nil || len(discovered.Discovery.Candidates) != 1 {
		t.Fatalf("discovery=%+v err=%v", discovered, err)
	}
	configured, err := app.service.ConfigureArchaeologySession(ctx, domain.HumanLocalPrincipal, "acceptance-config", application.ArchaeologyConfigRequest{
		SelectedProjectIDs: []string{discovered.Discovery.Candidates[0].ID}, Depth: "quick", Sources: application.ArchaeologySources{Git: true}, MaxConcurrency: 1, BaseRevision: discovered.Revision,
	})
	if err != nil {
		t.Fatalf("configure archaeology: %v candidate=%+v revision=%d", err, discovered.Discovery.Candidates[0], discovered.Revision)
	}
	if _, err := app.service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "acceptance-start", application.ArchaeologyTransitionRequest{BaseRevision: configured.Revision}); err != nil {
		t.Fatalf("start archaeology: %v", err)
	}
	phase2AcceptanceWait(t, time.Second, func() bool { return factory.launches.Load() == 1 }, "scheduler did not perform its single accepted launch")
	assertSingleActiveAcceptanceJob := func() {
		t.Helper()
		var jobs, active int
		if err := app.store.DB().QueryRowContext(ctx, `SELECT count(*),sum(state='active') FROM archaeology_native_jobs`).Scan(&jobs, &active); err != nil {
			t.Fatalf("read native scheduler ledger: %v", err)
		}
		if jobs != 1 || active != 1 {
			var state string
			_ = app.store.DB().QueryRowContext(ctx, `SELECT state FROM archaeology_native_jobs LIMIT 1`).Scan(&state)
			t.Fatalf("native scheduler ledger jobs=%d active=%d state=%q, want one active job", jobs, active, state)
		}
	}
	nativeJobActive := func() bool {
		var jobs, active int
		return app.store.DB().QueryRowContext(ctx, `SELECT count(*),sum(state='active') FROM archaeology_native_jobs`).Scan(&jobs, &active) == nil && jobs == 1 && active == 1
	}
	phase2AcceptanceWait(t, time.Second, nativeJobActive, "accepted scheduler launch did not become active")
	// Phase 3 exposes pending, leased, and blocked durable intents as
	// persistence attention. Let the launch's bind/activate ledger writes settle
	// before inducing Codex recovery so this assertion isolates notifier recovery
	// rather than racing the scheduler's persistence drain.
	phase2AcceptanceWait(t, time.Second, func() bool {
		status, statusErr := app.store.ArchaeologyNativePersistenceStatus(ctx)
		return statusErr == nil && status.Healthy()
	}, "accepted scheduler launch persistence ledger did not settle")
	assertSingleActiveAcceptanceJob()

	// Hold the replacement factory in recovery long enough for the required
	// runtime, notifier, HTTP readiness, and scheduler gate to observe it.
	now := time.Now().UTC()
	ticker := newPhase2NotifierTicker()
	notifierRecorder := &phase2NotifierRecorder{}
	notifier := newServiceNotifierForTest(nil, serviceNotifierOptions{
		Sender:   notifierRecorder.send,
		Snapshot: app.runtime.watchdogSnapshot,
		NewTicker: func(time.Duration) notifierTicker {
			return ticker
		},
		Now:              func() time.Time { return now },
		WatchdogInterval: time.Hour,
		RecoveryGrace:    time.Minute,
	})
	notifierCtx, notifierCancel := context.WithCancel(context.Background())
	notifierDone := phase2NotifierRun(t, notifier, notifierCtx)
	notifierDoneConsumed := false
	defer func() {
		notifierCancel()
		if !notifierDoneConsumed {
			select {
			case <-notifierDone:
			case <-time.After(time.Second):
				t.Error("acceptance notifier did not stop during cleanup")
			}
		}
		notifier.close()
	}()
	phase2NotifierWaitState(t, notifierRecorder, "READY=1\nSTATUS=Ready")

	first.crash(codexauth.ErrProcessExited)
	select {
	case <-factory.recoveryStarted:
	case <-time.After(time.Second):
		t.Fatal("managed supervisor did not enter autonomous recovery")
	}
	select {
	case app.runtime.trigger <- struct{}{}:
	default:
	}
	phase2AcceptanceWait(t, time.Second, func() bool {
		snapshot := app.runtime.RuntimeSnapshot()
		return !snapshot.Ready && (snapshot.Components.Supervisor.Status == runtimehealth.ComponentRecovering || snapshot.Components.Supervisor.Status == runtimehealth.ComponentDegraded)
	}, "required runtime did not publish degradation during recovery: runtime="+string(app.runtime.RuntimeSnapshot().Reason)+" supervisor="+string(app.runtime.RuntimeSnapshot().Components.Supervisor.Status)+" manager="+string(manager.Snapshot().State))
	if got := phase2AcceptanceReadiness(t, app); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("degraded readiness code=%d body=%s", got.Code, got.Body.String())
	}
	now = time.Now().UTC()
	ticker.ch <- now
	phase2NotifierWaitState(t, notifierRecorder, "STATUS=Recovering")
	if got := phase2NotifierCount(notifierRecorder.snapshot(), "READY=1\nSTATUS=Ready"); got != 1 {
		t.Fatalf("degraded transition emitted READY count=%d", got)
	}
	// An explicit scheduler wake while the process is recovering must stop at
	// the cached gate before ClaimArchaeologyNativeJob or a second launch.
	app.service.WakeNativeProjectArchaeologyScheduler()
	// The scheduler wake is bounded and nonblocking; observe one complete
	// drain window without sleeping a fixed interval or invoking a claim from
	// the test itself.
	phase2AcceptanceAssertStable(t, 50*time.Millisecond, 1, factory.launches.Load, "degraded scheduler crossed non-idempotent launch boundary")
	assertSingleActiveAcceptanceJob()

	factory.allow()
	var replacement *phase2AcceptanceChild
	select {
	case replacement = <-factory.children:
	case <-time.After(time.Second):
		t.Fatal("managed supervisor did not install its replacement child")
	}
	_ = replacement
	phase2AcceptanceWait(t, time.Second, func() bool { return manager.Snapshot().State == codexauth.ProcessStateAvailable }, "managed supervisor did not recover")
	select {
	case app.runtime.trigger <- struct{}{}:
	default:
	}
	phase2AcceptanceWait(t, time.Second, func() bool { return app.runtime.RuntimeSnapshot().Ready }, "runtime did not recover with managed supervisor")
	if got := phase2AcceptanceReadiness(t, app); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"ready":true`) {
		t.Fatalf("recovered readiness code=%d body=%s", got.Code, got.Body.String())
	}
	now = time.Now().UTC()
	ticker.ch <- now
	phase2NotifierWaitState(t, notifierRecorder, "STATUS=Ready")
	if got := phase2NotifierCount(notifierRecorder.snapshot(), "READY=1\nSTATUS=Ready"); got != 1 {
		t.Fatalf("recovery emitted duplicate READY count=%d", got)
	}
	assertSingleActiveAcceptanceJob()

	// A new child exit followed by a failed factory is the separate exhaustion
	// path. It must fail closed without replaying the accepted launch or claim.
	replacement.crash(codexauth.ErrProcessExited)
	phase2AcceptanceWait(t, time.Second, func() bool { return manager.Snapshot().State == codexauth.ProcessStateExhausted }, "managed supervisor did not exhaust its bounded recovery budget")
	select {
	case app.runtime.trigger <- struct{}{}:
	default:
	}
	phase2AcceptanceWait(t, time.Second, func() bool {
		snapshot := app.runtime.RuntimeSnapshot()
		return snapshot.State == runtimehealth.StateExhausted && !snapshot.Ready
	}, "runtime did not publish required exhaustion")
	if got := phase2AcceptanceReadiness(t, app); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("exhausted readiness code=%d body=%s", got.Code, got.Body.String())
	}
	launchesBeforeExhaustion := factory.launches.Load()
	watchdogsBeforeExhaustion := phase2NotifierCount(notifierRecorder.snapshot(), "WATCHDOG=1")
	now = time.Now().UTC()
	ticker.ch <- now
	select {
	case got := <-notifierDone:
		notifierDoneConsumed = true
		if !errors.Is(got, ErrNotifierExhausted) {
			t.Fatalf("exhaustion notifier error=%v, want %v", got, ErrNotifierExhausted)
		}
	case <-time.After(time.Second):
		t.Fatal("notifier did not fail on required exhaustion")
	}
	if got := factory.launches.Load(); got != launchesBeforeExhaustion || got != 1 {
		t.Fatalf("exhaustion replayed accepted launch: before=%d after=%d", launchesBeforeExhaustion, got)
	}
	assertSingleActiveAcceptanceJob()
	if states := notifierRecorder.snapshot(); phase2NotifierCount(states, "WATCHDOG=1") != watchdogsBeforeExhaustion || !phase2NotifierHas(states, "STATUS=Exhausted") {
		t.Fatalf("watchdog/status after exhaustion=%v, before_watchdogs=%d", states, watchdogsBeforeExhaustion)
	}
	notifierCancel()
}
