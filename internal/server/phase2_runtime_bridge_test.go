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

type phase2RuntimeGateClient struct {
	calls atomic.Int32
}

func (*phase2RuntimeGateClient) ListWorkspaces(context.Context) ([]codexauth.Workspace, error) {
	return nil, nil
}

func (c *phase2RuntimeGateClient) SupportsModel(context.Context, string, string) (bool, error) {
	c.calls.Add(1)
	return true, nil
}

func (*phase2RuntimeGateClient) LaunchTask(context.Context, string, string, string, string, string) (codexauth.TaskLaunch, error) {
	return codexauth.TaskLaunch{}, codexauth.ErrUnavailable
}

func (*phase2RuntimeGateClient) ExperimentalDynamicTools() bool { return true }

func (*phase2RuntimeGateClient) LaunchHistorianTask(context.Context, string, string, string, string, string, string, codexauth.HistorianPolicy, codexauth.DynamicToolHandler, codexauth.TurnTerminalHandler) (codexauth.TaskLaunch, error) {
	return codexauth.TaskLaunch{}, codexauth.ErrUnavailable
}

func (*phase2RuntimeGateClient) InterruptTurn(context.Context, string, string) error {
	return codexauth.ErrUnavailable
}

func (c *phase2RuntimeGateClient) supportCalls() int { return int(c.calls.Load()) }

func TestPhase2BridgeSchedulerGateBlocksSignedOutAndIncompatibleClaims(t *testing.T) {
	client := &phase2RuntimeGateClient{}
	var gate atomic.Bool
	bridge := &codexArchaeologyBridge{client: client, schedulerEligible: gate.Load}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	state := phase2HealthyRuntimeProbeState()
	monitor := newRuntimeHealthMonitor(runtimeHealthOptions{
		CodexConfigured: true,
		RequiredCodex:   false,
		Probe:           state.probe(),
		Now:             func() time.Time { return now },
		OnSnapshot:      func(snapshot runtimehealth.Snapshot) { gate.Store(snapshot.SchedulerEligible) },
	})

	state.account = codexauth.AccountSignedOut
	monitor.probeAndPublish(context.Background())
	if monitor.RuntimeSnapshot().SchedulerEligible || !errors.Is(bridge.Available(context.Background()), codexauth.ErrUnavailable) {
		t.Fatalf("signed-out runtime became claimable: snapshot=%+v", monitor.RuntimeSnapshot())
	}
	if got := client.supportCalls(); got != 0 {
		t.Fatalf("signed-out request performed SupportsModel I/O: %d", got)
	}

	state.account = codexauth.AccountSignedIn
	state.compatible = false
	state.supervisor.Generation = 2
	now = now.Add(time.Second)
	monitor.probeAndPublish(context.Background())
	if monitor.RuntimeSnapshot().SchedulerEligible || !errors.Is(bridge.Available(context.Background()), codexauth.ErrUnavailable) {
		t.Fatalf("incompatible runtime became claimable: snapshot=%+v", monitor.RuntimeSnapshot())
	}
	if got := client.supportCalls(); got != 0 {
		t.Fatalf("incompatible request performed SupportsModel I/O: %d", got)
	}
}

func TestPhase2BridgeSchedulerGateAvoidsRequestModelProbeAndWakeOrdering(t *testing.T) {
	client := &phase2RuntimeGateClient{}
	var gate atomic.Bool
	bridge := &codexArchaeologyBridge{client: client, schedulerEligible: gate.Load}
	state := phase2HealthyRuntimeProbeState()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	wakes := 0
	monitor := newRuntimeHealthMonitor(runtimeHealthOptions{
		CodexConfigured: true,
		RequiredCodex:   false,
		Probe:           state.probe(),
		Now:             func() time.Time { return now },
		OnSnapshot:      func(snapshot runtimehealth.Snapshot) { gate.Store(snapshot.SchedulerEligible) },
		OnSchedulerReady: func() {
			wakes++
			if !gate.Load() {
				t.Fatal("scheduler wake ran before the runtime eligibility gate was published")
			}
			if err := bridge.Available(context.Background()); err != nil {
				t.Fatalf("gate-authorized bridge availability=%v", err)
			}
		},
	})

	state.supervisor = runtimeSupervisorResult{Configured: true, Generation: 1, RecoveryActive: true, State: codexauth.ProcessStateRecovering}
	monitor.probeAndPublish(context.Background())
	if wakes != 0 || gate.Load() {
		t.Fatalf("recovery state incorrectly enabled scheduler: wakes=%d gate=%v", wakes, gate.Load())
	}
	state.supervisor = runtimeSupervisorResult{Configured: true, Available: true, Generation: 2, State: codexauth.ProcessStateAvailable}
	now = now.Add(time.Second)
	monitor.probeAndPublish(context.Background())
	if wakes != 1 || !gate.Load() {
		t.Fatalf("recovery transition did not publish gate before wake: wakes=%d gate=%v", wakes, gate.Load())
	}
	if got := client.supportCalls(); got != 0 {
		t.Fatalf("gate-authorized bridge request still performed SupportsModel I/O: %d", got)
	}
}

func TestPhase2RuntimeSupervisorStateAndTimestampMapping(t *testing.T) {
	observedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	healthyAt := observedAt.Add(-time.Minute)
	recoveryAt := observedAt.Add(-30 * time.Second)
	cases := []struct {
		name  string
		state codexauth.ProcessState
		want  runtimehealth.SupervisorState
	}{
		{name: "starting", state: codexauth.ProcessStateStarting, want: runtimehealth.SupervisorStarting},
		{name: "available", state: codexauth.ProcessStateAvailable, want: runtimehealth.SupervisorAvailable},
		{name: "degraded", state: codexauth.ProcessStateDegraded, want: runtimehealth.SupervisorDegraded},
		{name: "recovering", state: codexauth.ProcessStateRecovering, want: runtimehealth.SupervisorRecovering},
		{name: "exhausted", state: codexauth.ProcessStateExhausted, want: runtimehealth.SupervisorExhausted},
		{name: "closed", state: codexauth.ProcessStateClosed, want: runtimehealth.SupervisorClosed},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := runtimeSupervisorObservation(runtimeSupervisorResult{
				State:         test.state,
				Generation:    9,
				LastHealthyAt: healthyAt,
				RecoverySince: recoveryAt,
			}, nil, observedAt)
			if got.Status != test.want || got.Generation != 9 || !got.LastSuccessAt.Equal(healthyAt) || !got.LastFailureAt.Equal(recoveryAt) {
				t.Fatalf("observation=%+v, want state=%q generation=9 healthy=%s recovery=%s", got, test.want, healthyAt, recoveryAt)
			}
		})
	}

	future := runtimeSupervisorObservation(runtimeSupervisorResult{
		State:         codexauth.ProcessStateRecovering,
		LastHealthyAt: observedAt.Add(time.Hour),
		RecoverySince: observedAt.Add(time.Hour),
	}, nil, observedAt)
	if !future.LastSuccessAt.Equal(observedAt) || !future.LastFailureAt.Equal(observedAt) {
		t.Fatalf("future timestamps were not clamped: %+v", future)
	}
	missing := runtimeSupervisorObservation(runtimeSupervisorResult{State: codexauth.ProcessStateRecovering}, nil, observedAt)
	if !missing.LastFailureAt.Equal(observedAt) {
		t.Fatalf("missing recovery timestamp did not use observation time: %+v", missing)
	}
}
