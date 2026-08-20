package runtimehealth

import (
	"reflect"
	"testing"
	"time"
)

var (
	testObservedAt = time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	testSuccessAt  = time.Date(2026, time.August, 15, 11, 59, 0, 0, time.UTC)
	testFailureAt  = time.Date(2026, time.August, 15, 11, 58, 0, 0, time.UTC)
)

func readyInput() Input {
	return Input{
		ObservedAt: testObservedAt,
		Database: DatabaseObservation{
			Status:        DatabaseHealthy,
			LastSuccessAt: testSuccessAt,
		},
		Codex: CodexObservation{
			Configured: true,
			Required:   true,
			Available:  true,
		},
		Supervisor: SupervisorObservation{
			Status:     SupervisorRunning,
			Generation: 7,
		},
		Account:        AccountObservation{Status: AccountReady},
		Model:          ModelObservation{Status: ModelCompatible},
		Reconciliation: HealthObservation{Status: HealthHealthy},
		Persistence:    HealthObservation{Status: HealthHealthy},
	}
}

func TestEvaluateTable(t *testing.T) {
	base := readyInput()
	tests := []struct {
		name                 string
		input                Input
		wantState            State
		wantReady            bool
		wantLive             bool
		wantWatchdog         bool
		wantScheduler        bool
		wantReason           ReasonCode
		wantCodexStatus      ComponentState
		wantSupervisorStatus ComponentState
	}{
		{
			name:                 "startup with no observations",
			input:                Input{ObservedAt: testObservedAt},
			wantState:            StateStarting,
			wantReason:           ReasonDatabaseUnknown,
			wantCodexStatus:      ComponentDegraded,
			wantSupervisorStatus: ComponentUnknown,
		},
		{
			name:                 "required all healthy",
			input:                base,
			wantState:            StateReady,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantScheduler:        true,
			wantReason:           ReasonHealthy,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "Codex-style signed-in and available aliases are healthy",
			input: func() Input {
				value := base
				value.Account.Status = AccountSignedIn
				value.Supervisor.Status = SupervisorAvailable
				return value
			}(),
			wantState:            StateReady,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantScheduler:        true,
			wantReason:           ReasonHealthy,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "optional Codex not configured keeps service ready",
			input: func() Input {
				value := base
				value.Codex = CodexObservation{}
				value.Account = AccountObservation{}
				value.Model = ModelObservation{}
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexOptionalNotConfigured,
			wantCodexStatus:      ComponentDegraded,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "optional Codex without a supervisor remains core ready",
			input: func() Input {
				value := base
				value.Codex = CodexObservation{}
				value.Account = AccountObservation{}
				value.Model = ModelObservation{}
				value.Supervisor = SupervisorObservation{}
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexOptionalNotConfigured,
			wantCodexStatus:      ComponentDegraded,
			wantSupervisorStatus: ComponentUnknown,
		},
		{
			name: "optional Codex unavailable keeps service ready",
			input: func() Input {
				value := base
				value.Codex.Required = false
				value.Codex.Available = false
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexOptionalUnavailable,
			wantCodexStatus:      ComponentDegraded,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "optional unconfigured Codex ignores recovering supervisor",
			input: func() Input {
				value := base
				value.Codex = CodexObservation{}
				value.Account = AccountObservation{}
				value.Model = ModelObservation{}
				value.Supervisor.Status = SupervisorRecovering
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexOptionalNotConfigured,
			wantCodexStatus:      ComponentDegraded,
			wantSupervisorStatus: ComponentRecovering,
		},
		{
			name: "required Codex not configured fails readiness",
			input: func() Input {
				value := base
				value.Codex = CodexObservation{Required: true}
				value.Account = AccountObservation{}
				value.Model = ModelObservation{}
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexRequiredNotConfigured,
			wantCodexStatus:      ComponentFailed,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "required Codex unavailable fails readiness",
			input: func() Input {
				value := base
				value.Codex.Available = false
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexRequiredUnavailable,
			wantCodexStatus:      ComponentFailed,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "database failure always fails readiness and liveness",
			input: func() Input {
				value := base
				value.Database.Status = DatabaseFailed
				value.Database.LastFailureAt = testFailureAt
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             false,
			wantWatchdog:         false,
			wantReason:           ReasonDatabaseFailed,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "database unavailable is distinct and unsafe",
			input: func() Input {
				value := base
				value.Database.Status = DatabaseUnavailable
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             false,
			wantWatchdog:         false,
			wantReason:           ReasonDatabaseUnavailable,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "supervisor starting",
			input: func() Input {
				value := base
				value.Supervisor.Status = SupervisorStarting
				return value
			}(),
			wantState:            StateStarting,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         false,
			wantReason:           ReasonSupervisorStarting,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentStarting,
		},
		{
			name: "required supervisor recovering",
			input: func() Input {
				value := base
				value.Supervisor.Status = SupervisorRecovering
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         false,
			wantReason:           ReasonSupervisorRecovering,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentRecovering,
		},
		{
			name: "optional supervisor recovering remains service ready",
			input: func() Input {
				value := base
				value.Codex.Required = false
				value.Supervisor.Status = SupervisorRecovering
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonSupervisorRecovering,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentRecovering,
		},
		{
			name: "required supervisor exhausted",
			input: func() Input {
				value := base
				value.Supervisor.Status = SupervisorExhausted
				return value
			}(),
			wantState:            StateExhausted,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         false,
			wantReason:           ReasonSupervisorExhausted,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentExhausted,
		},
		{
			name: "optional supervisor exhausted remains service ready",
			input: func() Input {
				value := base
				value.Codex.Required = false
				value.Supervisor.Status = SupervisorExhausted
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonSupervisorExhausted,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentExhausted,
		},
		{
			name: "stopping always wins",
			input: func() Input {
				value := base
				value.Supervisor.Status = SupervisorStopping
				return value
			}(),
			wantState:            StateStopping,
			wantReady:            false,
			wantLive:             false,
			wantWatchdog:         false,
			wantReason:           ReasonSupervisorStopping,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentStopping,
		},
		{
			name: "account not ready required",
			input: func() Input {
				value := base
				value.Account.Status = AccountNotReady
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexRequiredNotReady,
			wantCodexStatus:      ComponentFailed,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "account not ready optional",
			input: func() Input {
				value := base
				value.Codex.Required = false
				value.Account.Status = AccountNotReady
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexOptionalNotReady,
			wantCodexStatus:      ComponentDegraded,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "model incompatible required",
			input: func() Input {
				value := base
				value.Model.Status = ModelIncompatible
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonCodexRequiredIncompatible,
			wantCodexStatus:      ComponentFailed,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "model unavailable optional",
			input: func() Input {
				value := base
				value.Codex.Required = false
				value.Model.Status = ModelUnavailable
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantReason:           ReasonModelUnavailable,
			wantCodexStatus:      ComponentDegraded,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "reconciliation attention is visible but service ready",
			input: func() Input {
				value := base
				value.Reconciliation.Status = HealthAttention
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            true,
			wantLive:             true,
			wantWatchdog:         true,
			wantScheduler:        false,
			wantReason:           ReasonReconciliationAttention,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "reconciliation failure blocks readiness",
			input: func() Input {
				value := base
				value.Reconciliation.Status = HealthFailed
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         false,
			wantReason:           ReasonReconciliationFailed,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "persistence attention blocks scheduler",
			input: func() Input {
				value := base
				value.Persistence.Status = HealthAttention
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             true,
			wantWatchdog:         false,
			wantScheduler:        false,
			wantReason:           ReasonPersistenceAttention,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentHealthy,
		},
		{
			name: "persistence failure is not live",
			input: func() Input {
				value := base
				value.Persistence.Status = HealthFailed
				return value
			}(),
			wantState:            StateDegraded,
			wantReady:            false,
			wantLive:             false,
			wantWatchdog:         false,
			wantReason:           ReasonPersistenceFailed,
			wantCodexStatus:      ComponentHealthy,
			wantSupervisorStatus: ComponentHealthy,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Evaluate(test.input)
			if got.State != test.wantState || got.Status != test.wantState || got.Ready != test.wantReady || got.Live != test.wantLive || got.Liveness != test.wantLive {
				t.Fatalf("state=%q status=%q ready=%v live=%v liveness=%v, want state=%q ready=%v live=%v", got.State, got.Status, got.Ready, got.Live, got.Liveness, test.wantState, test.wantReady, test.wantLive)
			}
			if got.WatchdogEligible != test.wantWatchdog || got.SchedulerEligible != test.wantScheduler || got.Reason != test.wantReason {
				t.Fatalf("watchdog=%v scheduler=%v reason=%q, want watchdog=%v scheduler=%v reason=%q", got.WatchdogEligible, got.SchedulerEligible, got.Reason, test.wantWatchdog, test.wantScheduler, test.wantReason)
			}
			if got.Components.Codex.Status != test.wantCodexStatus || got.Components.Supervisor.Status != test.wantSupervisorStatus {
				t.Fatalf("codex=%q supervisor=%q, want codex=%q supervisor=%q", got.Components.Codex.Status, got.Components.Supervisor.Status, test.wantCodexStatus, test.wantSupervisorStatus)
			}
			if !got.ObservedAt.Equal(test.input.ObservedAt.UTC()) {
				t.Fatalf("observed_at=%s, want %s", got.ObservedAt, test.input.ObservedAt.UTC())
			}
		})
	}
}

func TestEvaluateGenerationAndTimestamps(t *testing.T) {
	first := readyInput()
	first.Supervisor.Generation = 11
	first.LastSuccessAt = time.Date(2026, time.August, 15, 11, 57, 0, 0, time.UTC)
	first.LastFailureAt = time.Date(2026, time.August, 15, 11, 56, 0, 0, time.UTC)
	first.Supervisor.LastSuccessAt = time.Date(2026, time.August, 15, 11, 55, 0, 0, time.UTC)
	first.Database.LastFailureAt = time.Date(2026, time.August, 15, 11, 58, 0, 0, time.UTC)
	gotFirst := Evaluate(first)
	if gotFirst.Generation != 11 || !gotFirst.LastSuccessAt.Equal(testSuccessAt) || !gotFirst.LastFailureAt.Equal(testFailureAt) {
		t.Fatalf("first snapshot generation=%d success=%s failure=%s", gotFirst.Generation, gotFirst.LastSuccessAt, gotFirst.LastFailureAt)
	}
	if !gotFirst.Components.Database.ObservedAt.Equal(testObservedAt.UTC()) || !gotFirst.Components.Database.LastFailureAt.Equal(testFailureAt) {
		t.Fatalf("database component timestamps=%+v", gotFirst.Components.Database)
	}
	if !gotFirst.Components.Supervisor.LastSuccessAt.Equal(first.Supervisor.LastSuccessAt) || !gotFirst.Components.Supervisor.LastFailureAt.IsZero() {
		t.Fatalf("supervisor component timestamps=%+v", gotFirst.Components.Supervisor)
	}

	second := first
	second.Supervisor.Generation = 12
	second.ObservedAt = testObservedAt.Add(time.Minute)
	second.Database.LastSuccessAt = second.ObservedAt
	gotSecond := Evaluate(second)
	if gotSecond.Generation != 12 || gotSecond.Generation == gotFirst.Generation {
		t.Fatalf("generation did not follow observation: first=%d second=%d", gotFirst.Generation, gotSecond.Generation)
	}
	if !gotSecond.ObservedAt.Equal(second.ObservedAt.UTC()) || !gotSecond.LastSuccessAt.Equal(second.ObservedAt.UTC()) {
		t.Fatalf("second snapshot timestamps=%+v", gotSecond)
	}

	future := readyInput()
	future.LastSuccessAt = future.ObservedAt.Add(time.Hour)
	future.Database.LastSuccessAt = future.ObservedAt.Add(time.Hour)
	future.Database.LastFailureAt = future.ObservedAt.Add(time.Hour)
	gotFuture := Evaluate(future)
	if !gotFuture.LastSuccessAt.IsZero() || !gotFuture.LastFailureAt.IsZero() {
		t.Fatalf("future evidence was not omitted: success=%s failure=%s", gotFuture.LastSuccessAt, gotFuture.LastFailureAt)
	}
}

func TestEvaluateNormalizesUnknownValuesToSafeCodes(t *testing.T) {
	input := readyInput()
	input.Database.Status = DatabaseState("db-error-with-payload")
	input.Supervisor.Status = SupervisorState("recovering: secret/payload")
	input.Account.Status = AccountState("account-error-with-payload")
	input.Model.Status = ModelState("model-error-with-payload")
	input.Reconciliation.Status = HealthState("reconciliation-error-with-payload")
	input.Persistence.Status = HealthState("persistence-error-with-payload")
	got := Evaluate(input)
	if got.State != StateStarting || got.Reason != ReasonDatabaseUnknown || got.Ready || got.Live {
		t.Fatalf("safe unknown result=%+v", got)
	}
	for name, component := range map[string]ComponentStatus{
		"database":       got.Components.Database,
		"supervisor":     got.Components.Supervisor,
		"account":        got.Components.Account,
		"model":          got.Components.Model,
		"reconciliation": got.Components.Reconciliation,
		"persistence":    got.Components.Persistence,
	} {
		if component.Status == ComponentState("recovering: secret/payload") || component.Reason == ReasonCode("reconciliation-error-with-payload") {
			t.Fatalf("%s copied unsafe status: %+v", name, component)
		}
	}
}

func TestWatchdogSemantics(t *testing.T) {
	base := readyInput()
	tests := []struct {
		name       string
		mutate     func(*Input)
		want       bool
		wantReason ReasonCode
	}{
		{name: "running healthy", mutate: func(*Input) {}, want: true, wantReason: ReasonHealthy},
		{name: "starting", mutate: func(input *Input) { input.Supervisor.Status = SupervisorStarting }, want: false, wantReason: ReasonSupervisorStarting},
		{name: "recovering", mutate: func(input *Input) { input.Supervisor.Status = SupervisorRecovering }, want: false, wantReason: ReasonSupervisorRecovering},
		{name: "exhausted", mutate: func(input *Input) { input.Supervisor.Status = SupervisorExhausted }, want: false, wantReason: ReasonSupervisorExhausted},
		{name: "stopping", mutate: func(input *Input) { input.Supervisor.Status = SupervisorStopping }, want: false, wantReason: ReasonSupervisorStopping},
		{name: "stopped", mutate: func(input *Input) { input.Supervisor.Status = SupervisorStopped }, want: false, wantReason: ReasonSupervisorStopped},
		{name: "closed", mutate: func(input *Input) { input.Supervisor.Status = SupervisorClosed }, want: false, wantReason: ReasonSupervisorStopped},
		{name: "database failure", mutate: func(input *Input) { input.Database.Status = DatabaseFailed }, want: false, wantReason: ReasonDatabaseFailed},
		{name: "persistence failure", mutate: func(input *Input) { input.Persistence.Status = HealthFailed }, want: false, wantReason: ReasonPersistenceFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			got := Evaluate(input)
			if got.WatchdogEligible != test.want || got.Reason != test.wantReason {
				t.Fatalf("watchdog=%v reason=%q, want watchdog=%v reason=%q", got.WatchdogEligible, got.Reason, test.want, test.wantReason)
			}
		})
	}
}

func TestSnapshotStorePublishesWholeValues(t *testing.T) {
	var zero SnapshotStore
	if got := zero.Load(); got != (Snapshot{}) {
		t.Fatalf("zero store load=%+v", got)
	}

	initial := Evaluate(readyInput())
	store := NewSnapshotStore(initial)
	if got := store.Load(); got != initial {
		t.Fatalf("initial snapshot changed: got=%+v want=%+v", got, initial)
	}

	nextInput := readyInput()
	nextInput.Supervisor.Generation = 99
	next := Evaluate(nextInput)
	store.Publish(next)
	if got := store.Load(); got != next || got.Generation != 99 {
		t.Fatalf("published snapshot=%+v", got)
	}

	var nilStore *SnapshotStore
	if got := nilStore.Load(); got != (Snapshot{}) {
		t.Fatalf("nil store load=%+v", got)
	}
	nilStore.Store(next)
}

func TestSnapshotHasNoExportedMutableCollections(t *testing.T) {
	typeOfSnapshot := reflect.TypeOf(Snapshot{})
	for index := 0; index < typeOfSnapshot.NumField(); index++ {
		field := typeOfSnapshot.Field(index)
		switch field.Type.Kind() {
		case reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
			t.Fatalf("snapshot field %s has mutable/non-value kind %s", field.Name, field.Type.Kind())
		}
	}
	if got, want := EvaluateSnapshot(readyInput()), Evaluate(readyInput()); got != want {
		t.Fatalf("EvaluateSnapshot differs: got=%+v want=%+v", got, want)
	}
}
