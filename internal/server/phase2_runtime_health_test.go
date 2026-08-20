package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/runtimehealth"
)

type phase2RuntimeProbeState struct {
	dbErr        error
	persistence  runtimePersistenceResult
	supervisor   runtimeSupervisorResult
	account      codexauth.AccountState
	compatible   bool
	dbCalls      int
	accountCalls int
	modelCalls   int
}

func (s *phase2RuntimeProbeState) probe() runtimeHealthProbe {
	return runtimeHealthProbe{
		DB: func(context.Context) error {
			s.dbCalls++
			return s.dbErr
		},
		Persistence: func(context.Context) (runtimePersistenceResult, error) {
			return s.persistence, nil
		},
		Supervisor: func(context.Context) (runtimeSupervisorResult, error) {
			return s.supervisor, nil
		},
		Account: func(context.Context) (codexauth.AccountState, error) {
			s.accountCalls++
			return s.account, nil
		},
		Compatibility: func(context.Context) (bool, error) {
			s.modelCalls++
			return s.compatible, nil
		},
	}
}

func phase2HealthyRuntimeProbeState() *phase2RuntimeProbeState {
	return &phase2RuntimeProbeState{
		persistence: runtimePersistenceResult{Healthy: true, Reconciliation: "healthy"},
		supervisor:  runtimeSupervisorResult{Configured: true, Available: true, Generation: 1, State: codexauth.ProcessStateAvailable},
		account:     codexauth.AccountSignedIn,
		compatible:  true,
	}
}

func phase2RuntimeMonitor(state *phase2RuntimeProbeState, required bool, now *time.Time, wake func()) *runtimeHealthMonitor {
	return newRuntimeHealthMonitorForTest(runtimeHealthOptions{
		CodexConfigured:  true,
		RequiredCodex:    required,
		Probe:            state.probe(),
		Now:              func() time.Time { return *now },
		RecoveryGrace:    10 * time.Second,
		OnSchedulerReady: wake,
	})
}

func TestPhase2RuntimeHealthConservativeStartupAndPureHTTPProjection(t *testing.T) {
	state := phase2HealthyRuntimeProbeState()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	monitor := phase2RuntimeMonitor(state, true, &now, nil)

	initial := monitor.RuntimeSnapshot()
	if initial.Ready || initial.Live || initial.ObservedAt.After(now) || initial.State != runtimehealth.StateStarting {
		t.Fatalf("initial snapshot is not conservative: %+v", initial)
	}
	_ = monitor.Snapshot()
	if state.dbCalls != 0 || state.accountCalls != 0 || state.modelCalls != 0 {
		t.Fatalf("HTTP/provider reads performed I/O before the first probe: db=%d account=%d model=%d", state.dbCalls, state.accountCalls, state.modelCalls)
	}

	monitor.probeAndPublish(context.Background())
	ready := monitor.RuntimeSnapshot()
	if !ready.Ready || !ready.Live || ready.State != runtimehealth.StateReady {
		t.Fatalf("healthy first probe did not become ready: %+v", ready)
	}
	if got := monitor.Snapshot(); got.Components["account"].Required != true || got.Components["model"].Required != true {
		t.Fatalf("required account/model projection lost policy: %+v", got.Components)
	}
}

func TestPhase2RuntimeHealthRequiredOptionalTransitionsAndExhaustion(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	requiredState := phase2HealthyRuntimeProbeState()
	required := phase2RuntimeMonitor(requiredState, true, &now, nil)
	required.probeAndPublish(context.Background())

	requiredState.supervisor = runtimeSupervisorResult{
		Configured: true, Generation: 1, RecoveryActive: true,
		RecoverySince: now, State: codexauth.ProcessStateRecovering,
	}
	now = now.Add(time.Second)
	required.probeAndPublish(context.Background())
	if snapshot := required.RuntimeSnapshot(); snapshot.Ready || snapshot.State != runtimehealth.StateDegraded {
		t.Fatalf("required recovery did not leave readiness: %+v", snapshot)
	}
	if got := required.watchdogSnapshot(); !got.RequiredRecoveryActive || got.RecoveryExhausted {
		t.Fatalf("required recovery grace state=%+v", got)
	}

	now = now.Add(10 * time.Second)
	required.probeAndPublish(context.Background())
	if got := required.watchdogSnapshot(); !got.RecoveryExhausted {
		t.Fatalf("required recovery did not exhaust after grace: %+v", got)
	}

	optionalState := phase2HealthyRuntimeProbeState()
	optional := phase2RuntimeMonitor(optionalState, false, &now, nil)
	optional.probeAndPublish(context.Background())
	optionalState.supervisor = runtimeSupervisorResult{Configured: true, Generation: 1, RecoveryActive: true, State: codexauth.ProcessStateRecovering}
	now = now.Add(time.Second)
	optional.probeAndPublish(context.Background())
	if snapshot := optional.RuntimeSnapshot(); !snapshot.Ready || !snapshot.Live {
		t.Fatalf("optional recovery incorrectly removed service readiness: %+v", snapshot)
	}
}

func TestPhase2RuntimeHealthCachesModelByGenerationRefreshesAccount(t *testing.T) {
	state := phase2HealthyRuntimeProbeState()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	monitor := phase2RuntimeMonitor(state, true, &now, nil)
	monitor.probeAndPublish(context.Background())
	if state.accountCalls != 1 || state.modelCalls != 1 {
		t.Fatalf("first probe calls account/model=%d/%d, want 1/1", state.accountCalls, state.modelCalls)
	}

	state.account = codexauth.AccountSignedOut
	now = now.Add(time.Second)
	monitor.probeAndPublish(context.Background())
	if state.accountCalls != 2 || state.modelCalls != 1 {
		t.Fatalf("same generation cache account/model=%d/%d, want 2/1", state.accountCalls, state.modelCalls)
	}
	if monitor.RuntimeSnapshot().Ready {
		t.Fatal("account sign-out within generation remained ready")
	}

	state.account = codexauth.AccountSignedIn
	state.supervisor.Generation = 2
	now = now.Add(time.Second)
	monitor.probeAndPublish(context.Background())
	if state.accountCalls != 3 || state.modelCalls != 2 {
		t.Fatalf("generation change did not invalidate model cache account/model=%d/%d, want 3/2", state.accountCalls, state.modelCalls)
	}
}

func TestPhase2RuntimeHealthDatabaseFailureRecoveryAndSchedulerWake(t *testing.T) {
	state := phase2HealthyRuntimeProbeState()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	var wakeCount atomic.Int32
	monitor := phase2RuntimeMonitor(state, false, &now, func() { wakeCount.Add(1) })
	monitor.probeAndPublish(context.Background())
	if wakeCount.Load() != 1 {
		t.Fatalf("first scheduler-ready transition wake count=%d, want 1", wakeCount.Load())
	}
	monitor.probeAndPublish(context.Background())
	if wakeCount.Load() != 1 {
		t.Fatalf("steady scheduler-ready state woke again: %d", wakeCount.Load())
	}

	state.dbErr = errors.New("probe failure")
	now = now.Add(time.Second)
	monitor.probeAndPublish(context.Background())
	failed := monitor.RuntimeSnapshot()
	if failed.Ready || failed.Live || failed.WatchdogEligible {
		t.Fatalf("database failure remained healthy: %+v", failed)
	}
	if got := monitor.watchdogSnapshot(); !got.Ready || got.DatabaseHealthy || got.CoreHealthy {
		t.Fatalf("terminal database failure was not surfaced to notifier: %+v", got)
	}

	state.dbErr = nil
	now = now.Add(time.Second)
	monitor.probeAndPublish(context.Background())
	if snapshot := monitor.RuntimeSnapshot(); !snapshot.Ready || !snapshot.Live {
		t.Fatalf("database recovery did not restore readiness: %+v", snapshot)
	}
}

func TestPhase2RuntimeHealthPreservesUncertaintyAndPersistenceFault(t *testing.T) {
	state := phase2HealthyRuntimeProbeState()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	state.persistence.Uncertain = 1
	monitor := phase2RuntimeMonitor(state, true, &now, nil)
	monitor.probeAndPublish(context.Background())
	attention := monitor.RuntimeSnapshot()
	if attention.Components.Reconciliation.Status != runtimehealth.ComponentDegraded || !attention.Ready || attention.SchedulerEligible {
		t.Fatalf("uncertainty was not preserved as a scheduler safety gate: %+v", attention)
	}

	state.persistence.Uncertain = 0
	state.persistence.PersistenceFault = true
	now = now.Add(time.Second)
	monitor.probeAndPublish(context.Background())
	fault := monitor.RuntimeSnapshot()
	if fault.Ready || fault.Live || fault.Components.Persistence.Status != runtimehealth.ComponentFailed {
		t.Fatalf("scheduler persistence fault was not fail-closed: %+v", fault)
	}
}

func TestPhase2RuntimeHealthCancellationStopsProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	state := phase2HealthyRuntimeProbeState()
	stateProbe := state.probe()
	stateProbe.DB = func(probeCtx context.Context) error {
		close(started)
		<-probeCtx.Done()
		return probeCtx.Err()
	}
	monitor := newRuntimeHealthMonitor(runtimeHealthOptions{Parent: ctx, Probe: stateProbe, Interval: time.Hour})
	monitor.Start()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("monitor did not start its probe")
	}
	monitor.Close()
	select {
	case <-monitor.done:
	default:
		t.Fatal("monitor Close returned before loop completion")
	}
}

func TestPhase2RuntimeHealthStateChangeTriggersImmediateReprobeAndClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	state := phase2HealthyRuntimeProbeState()
	var supervisor atomic.Value
	supervisor.Store(state.supervisor)
	firstChange := make(chan struct{})
	var currentChanges atomic.Value
	currentChanges.Store((<-chan struct{})(firstChange))
	probe := state.probe()
	probe.Supervisor = func(context.Context) (runtimeSupervisorResult, error) {
		return supervisor.Load().(runtimeSupervisorResult), nil
	}
	probe.StateChanges = func() <-chan struct{} { return currentChanges.Load().(<-chan struct{}) }
	monitor := newRuntimeHealthMonitor(runtimeHealthOptions{
		Parent:          ctx,
		CodexConfigured: true,
		RequiredCodex:   true,
		Probe:           probe,
		Interval:        time.Hour,
		Now:             func() time.Time { return now },
	})
	monitor.Start()
	readyCtx, readyCancel := context.WithTimeout(context.Background(), time.Second)
	if err := monitor.WaitReady(readyCtx); err != nil {
		readyCancel()
		monitor.Close()
		t.Fatal(err)
	}
	readyCancel()

	supervisor.Store(runtimeSupervisorResult{Configured: true, Generation: 1, RecoveryActive: true, State: codexauth.ProcessStateRecovering, RecoverySince: now})
	close(firstChange)
	deadline := time.After(time.Second)
	for monitor.RuntimeSnapshot().Ready {
		select {
		case <-deadline:
			monitor.Close()
			t.Fatal("state change did not trigger recovery probe")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	secondChange := make(chan struct{})
	currentChanges.Store((<-chan struct{})(secondChange))
	supervisor.Store(state.supervisor)
	close(secondChange)
	deadline = time.After(time.Second)
	for !monitor.RuntimeSnapshot().Ready {
		select {
		case <-deadline:
			monitor.Close()
			t.Fatal("state change did not trigger immediate recovery probe")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	monitor.Close()
	select {
	case <-monitor.done:
	default:
		t.Fatal("monitor close left its loop running")
	}
}

func TestPhase2RuntimeHealthClosedStateReporterBacksOffAndCancels(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reporterCalls atomic.Int32
	var probeCalls atomic.Int32
	delays := make(chan time.Duration, 8)
	monitor := newRuntimeHealthMonitor(runtimeHealthOptions{
		Parent:   parent,
		Interval: time.Hour,
		Probe: runtimeHealthProbe{
			DB: func(context.Context) error {
				probeCalls.Add(1)
				return nil
			},
			StateChanges: func() <-chan struct{} {
				reporterCalls.Add(1)
				closed := make(chan struct{})
				close(closed)
				return closed
			},
		},
		StateChangeBackoffWait: func(_ context.Context, delay time.Duration) bool {
			delays <- delay
			if delay == 40*time.Millisecond {
				cancel()
				return false
			}
			return true
		},
	})
	monitor.Start()
	monitor.Close()

	gotDelays := make([]time.Duration, 0, 3)
	for {
		select {
		case delay := <-delays:
			gotDelays = append(gotDelays, delay)
		default:
			goto collected
		}
	}
collected:
	wantDelays := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}
	if len(gotDelays) != len(wantDelays) {
		t.Fatalf("closed reporter backoff delays=%v, want %v", gotDelays, wantDelays)
	}
	for index := range wantDelays {
		if gotDelays[index] != wantDelays[index] {
			t.Fatalf("closed reporter backoff delays=%v, want %v", gotDelays, wantDelays)
		}
	}
	if got := reporterCalls.Load(); got != int32(len(wantDelays)) {
		t.Fatalf("closed reporter callback calls=%d, want %d", got, len(wantDelays))
	}
	if got := probeCalls.Load(); got > int32(len(wantDelays)+1) {
		t.Fatalf("closed reporter caused probe spin: calls=%d", got)
	}
}

func TestPhase2ServeResultPropagatesNotifierFatal(t *testing.T) {
	fatal := ErrNotifierExhausted
	if got := serveResult(context.Canceled, fatal); !errors.Is(got, fatal) {
		t.Fatalf("serveResult fatal=%v, want %v", got, fatal)
	}
	if got := serveResult(nil, context.Canceled); got != nil {
		t.Fatalf("serveResult cancellation=%v, want nil", got)
	}
}
