// Package runtimehealth contains the storage-neutral runtime health model.
//
// Callers collect observations outside this package and pass one Input to
// Evaluate. Evaluate is deterministic and has no I/O, clock reads, or
// goroutines. The returned Snapshot contains only bounded status/reason codes,
// booleans, a generation, and timestamps supplied by the caller, so it can be
// published as one value through SnapshotStore.
package runtimehealth

import (
	"sync/atomic"
	"time"
)

// State is the service-level lifecycle/health state.
type State string

const (
	StateStarting  State = "starting"
	StateReady     State = "ready"
	StateDegraded  State = "degraded"
	StateExhausted State = "exhausted"
	StateStopping  State = "stopping"
)

// Status is an alias for State. It lets transport adapters call the
// service-level value either State or Status without introducing another
// string vocabulary.
type Status = State

const (
	StatusStarting  = StateStarting
	StatusReady     = StateReady
	StatusDegraded  = StateDegraded
	StatusExhausted = StateExhausted
	StatusStopping  = StateStopping
)

// ReasonCode is a bounded, safe explanation for the service-level result.
// It intentionally has no field for an error or payload supplied by a
// process, database, account, or request.
type ReasonCode string

// Reason is a short alias for ReasonCode used by transport-facing adapters.
type Reason = ReasonCode

const (
	ReasonHealthy                    ReasonCode = "healthy"
	ReasonStarting                   ReasonCode = "starting"
	ReasonDatabaseUnknown            ReasonCode = "database_unknown"
	ReasonDatabaseFailed             ReasonCode = "database_failed"
	ReasonDatabaseUnavailable        ReasonCode = "database_unavailable"
	ReasonPersistenceUnknown         ReasonCode = "persistence_unknown"
	ReasonPersistenceAttention       ReasonCode = "persistence_attention"
	ReasonPersistenceFailed          ReasonCode = "persistence_failed"
	ReasonReconciliationUnknown      ReasonCode = "reconciliation_unknown"
	ReasonReconciliationAttention    ReasonCode = "reconciliation_attention"
	ReasonReconciliationFailed       ReasonCode = "reconciliation_failed"
	ReasonCodexOptionalNotConfigured ReasonCode = "codex_optional_not_configured"
	ReasonCodexRequiredNotConfigured ReasonCode = "codex_required_not_configured"
	ReasonCodexOptionalUnavailable   ReasonCode = "codex_optional_unavailable"
	ReasonCodexRequiredUnavailable   ReasonCode = "codex_required_unavailable"
	ReasonCodexOptionalNotReady      ReasonCode = "codex_optional_not_ready"
	ReasonCodexRequiredNotReady      ReasonCode = "codex_required_not_ready"
	ReasonCodexOptionalIncompatible  ReasonCode = "codex_optional_incompatible"
	ReasonCodexRequiredIncompatible  ReasonCode = "codex_required_incompatible"
	ReasonAccountUnknown             ReasonCode = "account_unknown"
	ReasonAccountNotReady            ReasonCode = "account_not_ready"
	ReasonModelUnknown               ReasonCode = "model_unknown"
	ReasonModelIncompatible          ReasonCode = "model_incompatible"
	ReasonModelUnavailable           ReasonCode = "model_unavailable"
	ReasonSupervisorUnknown          ReasonCode = "supervisor_unknown"
	ReasonSupervisorStarting         ReasonCode = "supervisor_starting"
	ReasonSupervisorDegraded         ReasonCode = "supervisor_degraded"
	ReasonSupervisorRecovering       ReasonCode = "supervisor_recovering"
	ReasonSupervisorExhausted        ReasonCode = "supervisor_exhausted"
	ReasonSupervisorStopping         ReasonCode = "supervisor_stopping"
	ReasonSupervisorStopped          ReasonCode = "supervisor_stopped"
)

// ComponentState is the safe state of one runtime component.
type ComponentState string

const (
	ComponentUnknown    ComponentState = "unknown"
	ComponentStarting   ComponentState = "starting"
	ComponentHealthy    ComponentState = "healthy"
	ComponentDegraded   ComponentState = "degraded"
	ComponentFailed     ComponentState = "failed"
	ComponentRecovering ComponentState = "recovering"
	ComponentExhausted  ComponentState = "exhausted"
	ComponentStopping   ComponentState = "stopping"
	ComponentDisabled   ComponentState = "disabled"
)

// ComponentStatus is a value-only, metadata-only component result. A zero
// timestamp means that the caller did not have that observation.
type ComponentStatus struct {
	Status        ComponentState `json:"status"`
	Reason        ReasonCode     `json:"reason"`
	ObservedAt    time.Time      `json:"observed_at"`
	LastSuccessAt time.Time      `json:"last_success_at,omitempty"`
	LastFailureAt time.Time      `json:"last_failure_at,omitempty"`
}

// ComponentSet keeps component health in a fixed-shape struct. It has no map
// or slice, which makes a Snapshot safe to publish as one atomic value.
type ComponentSet struct {
	Database       ComponentStatus `json:"database"`
	Codex          ComponentStatus `json:"codex"`
	Supervisor     ComponentStatus `json:"supervisor"`
	Account        ComponentStatus `json:"account"`
	Model          ComponentStatus `json:"model"`
	Reconciliation ComponentStatus `json:"reconciliation"`
	Persistence    ComponentStatus `json:"persistence"`
}

// ComponentName names one member of ComponentSet for adapters that prefer a
// generic lookup over a named field.
type ComponentName string

const (
	ComponentDatabase       ComponentName = "database"
	ComponentCodex          ComponentName = "codex"
	ComponentSupervisor     ComponentName = "supervisor"
	ComponentAccount        ComponentName = "account"
	ComponentModel          ComponentName = "model"
	ComponentReconciliation ComponentName = "reconciliation"
	ComponentPersistence    ComponentName = "persistence"
)

// Get returns a component status. Unknown names return an explicitly unknown
// status rather than copying an arbitrary caller string into a result.
func (c ComponentSet) Get(name ComponentName) ComponentStatus {
	switch name {
	case ComponentDatabase:
		return c.Database
	case ComponentCodex:
		return c.Codex
	case ComponentSupervisor:
		return c.Supervisor
	case ComponentAccount:
		return c.Account
	case ComponentModel:
		return c.Model
	case ComponentReconciliation:
		return c.Reconciliation
	case ComponentPersistence:
		return c.Persistence
	default:
		return ComponentStatus{Status: ComponentUnknown, Reason: ReasonStarting}
	}
}

// DatabaseState is the result of a database ping/readiness observation.
type DatabaseState string

// DatabaseStatus is a descriptive alias for DatabaseState.
type DatabaseStatus = DatabaseState

const (
	DatabaseUnknown     DatabaseState = "unknown"
	DatabaseHealthy     DatabaseState = "healthy"
	DatabaseFailed      DatabaseState = "failed"
	DatabaseUnavailable DatabaseState = "unavailable"
	DatabaseOK          DatabaseState = DatabaseHealthy
)

// DatabaseObservation describes the database without carrying a handle,
// error, query, or payload.
type DatabaseObservation struct {
	Status        DatabaseState
	LastSuccessAt time.Time
	LastFailureAt time.Time
}

// CodexObservation describes the configured/required capability and whether
// its managed process is currently usable. Account and model observations are
// separate Input fields so the server can update them independently.
type CodexObservation struct {
	Configured    bool
	Required      bool
	Available     bool
	LastSuccessAt time.Time
	LastFailureAt time.Time
}

// SupervisorState is the lifecycle state of the managed Codex supervisor.
type SupervisorState string

// SupervisorStatus is a descriptive alias for SupervisorState.
type SupervisorStatus = SupervisorState

const (
	SupervisorUnknown    SupervisorState = "unknown"
	SupervisorStarting   SupervisorState = "starting"
	SupervisorRunning    SupervisorState = "running"
	SupervisorAvailable  SupervisorState = "available"
	SupervisorDegraded   SupervisorState = "degraded"
	SupervisorRecovering SupervisorState = "recovering"
	SupervisorExhausted  SupervisorState = "exhausted"
	SupervisorStopping   SupervisorState = "stopping"
	SupervisorStopped    SupervisorState = "stopped"
	SupervisorClosed     SupervisorState = "closed"
)

// SupervisorObservation includes the monotonically increasing process
// generation. Generation zero is valid for an initial/unknown observation;
// Evaluate preserves it exactly.
type SupervisorObservation struct {
	Status        SupervisorState
	Generation    uint64
	LastSuccessAt time.Time
	LastFailureAt time.Time
}

// AccountState describes only the readiness fact needed by the scheduler.
type AccountState string

// AccountStatus is a descriptive alias for AccountState.
type AccountStatus = AccountState

const (
	AccountUnknown   AccountState = "unknown"
	AccountReady     AccountState = "ready"
	AccountSignedIn  AccountState = "signed_in"
	AccountNotReady  AccountState = "not_ready"
	AccountSignedOut AccountState = "signed_out"
	AccountFailed    AccountState = "failed"
)

type AccountObservation struct {
	Status        AccountState
	LastSuccessAt time.Time
	LastFailureAt time.Time
}

// ModelState describes compatibility with the configured model/effort.
type ModelState string

// ModelStatus is a descriptive alias for ModelState.
type ModelStatus = ModelState

const (
	ModelUnknown      ModelState = "unknown"
	ModelCompatible   ModelState = "compatible"
	ModelIncompatible ModelState = "incompatible"
	ModelUnavailable  ModelState = "unavailable"
)

type ModelObservation struct {
	Status        ModelState
	LastSuccessAt time.Time
	LastFailureAt time.Time
}

// HealthState is shared by reconciliation and persistence observations.
type HealthState string

// HealthStatus is a descriptive alias for HealthState.
type HealthStatus = HealthState

const (
	HealthUnknown   HealthState = "unknown"
	HealthHealthy   HealthState = "healthy"
	HealthAttention HealthState = "attention"
	HealthFailed    HealthState = "failed"
	HealthOK        HealthState = HealthHealthy
)

type HealthObservation struct {
	Status        HealthState
	LastSuccessAt time.Time
	LastFailureAt time.Time
}

// Input is the complete observation set for one evaluation. ObservedAt must
// be supplied by the caller; Evaluate never reads the wall clock. All other
// timestamps are optional evidence and are copied only after UTC
// normalization.
type Input struct {
	ObservedAt time.Time

	Database       DatabaseObservation
	Codex          CodexObservation
	Supervisor     SupervisorObservation
	Account        AccountObservation
	Model          ModelObservation
	Reconciliation HealthObservation
	Persistence    HealthObservation

	// LastSuccessAt and LastFailureAt allow a caller to report a bounded
	// service-wide operation result in addition to component-local evidence.
	LastSuccessAt time.Time
	LastFailureAt time.Time
}

// EvaluationInput is a descriptive alias for callers that prefer the
// evaluator-oriented name.
type EvaluationInput = Input

// RuntimeHealthInput is a descriptive alias for Input.
type RuntimeHealthInput = Input

// Snapshot is the complete immutable result for one Input. It contains no
// handles, callbacks, errors, secrets, request data, maps, or slices.
type Snapshot struct {
	State             State        `json:"state"`
	Status            Status       `json:"status"`
	Ready             bool         `json:"ready"`
	Live              bool         `json:"live"`
	Liveness          bool         `json:"liveness"`
	WatchdogEligible  bool         `json:"watchdog_eligible"`
	SchedulerEligible bool         `json:"scheduler_eligible"`
	Generation        uint64       `json:"generation"`
	Reason            ReasonCode   `json:"reason"`
	ObservedAt        time.Time    `json:"observed_at"`
	LastSuccessAt     time.Time    `json:"last_success_at,omitempty"`
	LastFailureAt     time.Time    `json:"last_failure_at,omitempty"`
	Components        ComponentSet `json:"components"`
}

// Evaluate deterministically evaluates one observation set. The evaluator
// never performs I/O, starts work, calls time.Now, or propagates caller
// supplied strings/errors into the result.
func Evaluate(input Input) Snapshot {
	observedAt := normalizeTime(input.ObservedAt)

	database := databaseComponent(input.Database, observedAt)
	persistence := healthComponent(input.Persistence, observedAt, ReasonPersistenceUnknown, ReasonPersistenceAttention, ReasonPersistenceFailed)
	reconciliation := healthComponent(input.Reconciliation, observedAt, ReasonReconciliationUnknown, ReasonReconciliationAttention, ReasonReconciliationFailed)
	supervisor := supervisorComponent(input.Supervisor, observedAt)

	codexUsable := input.Codex.Configured && input.Codex.Available &&
		accountReady(input.Account.Status) && input.Model.Status == ModelCompatible
	codex := codexComponent(input, codexUsable, observedAt)
	account := accountComponent(input, observedAt)
	model := modelComponent(input, observedAt)

	components := ComponentSet{
		Database:       database,
		Codex:          codex,
		Supervisor:     supervisor,
		Account:        account,
		Model:          model,
		Reconciliation: reconciliation,
		Persistence:    persistence,
	}

	lastSuccessAt := normalizeEvidenceTime(input.LastSuccessAt, observedAt)
	lastFailureAt := normalizeEvidenceTime(input.LastFailureAt, observedAt)
	for _, component := range []ComponentStatus{database, codex, supervisor, account, model, reconciliation, persistence} {
		lastSuccessAt = latest(lastSuccessAt, component.LastSuccessAt)
		lastFailureAt = latest(lastFailureAt, component.LastFailureAt)
	}

	generation := input.Supervisor.Generation
	stopping := input.Supervisor.Status == SupervisorStopping || input.Supervisor.Status == SupervisorStopped || input.Supervisor.Status == SupervisorClosed
	exhausted := input.Supervisor.Status == SupervisorExhausted

	databaseHealthy := input.Database.Status == DatabaseHealthy
	databaseUnknown := input.Database.Status != DatabaseHealthy &&
		input.Database.Status != DatabaseFailed && input.Database.Status != DatabaseUnavailable
	persistenceHealthy := input.Persistence.Status == HealthHealthy
	persistenceUnknown := !knownHealthStatus(input.Persistence.Status)
	reconciliationUnknown := !knownHealthStatus(input.Reconciliation.Status)
	reconciliationFailed := input.Reconciliation.Status == HealthFailed
	persistenceFailed := input.Persistence.Status == HealthFailed

	// Live is intentionally narrower than Ready. A healthy database and a
	// non-stopping process are enough to keep the process liveness signal true;
	// Codex being optional or temporarily unusable does not make the service
	// dead. Database failure always makes both Live and Ready false.
	live := databaseHealthy && !stopping && !persistenceFailed

	// Core readiness is independent of optional Codex. Reconciliation
	// attention is a durable warning (for example unresolved uncertainty), not
	// a reason to take the HTTP service out of readiness; an unknown or failed
	// persistence/reconciliation observation is not safe to call ready.
	coreReady := databaseHealthy && persistenceHealthy && !persistenceUnknown &&
		!reconciliationUnknown && !reconciliationFailed && !stopping

	// Required Codex makes all of its usability gates readiness-critical.
	// Optional Codex never removes core service readiness, but it still marks
	// the snapshot degraded and keeps scheduler work disabled.
	ready := coreReady
	if input.Codex.Required {
		ready = ready && codexUsable && supervisorRunning(input.Supervisor.Status)
	}

	// A supervisor that is still starting is a startup state for required and
	// optional capability alike. Recovery/exhaustion are a degraded capability
	// state; optional mode remains service-ready while the database/core is
	// healthy. Stopping is always not-ready.
	if input.Codex.Required && (input.Supervisor.Status == SupervisorStarting || input.Supervisor.Status == SupervisorRecovering || exhausted) {
		ready = false
	}
	if stopping || databaseUnknown || input.Database.Status == DatabaseFailed || input.Database.Status == DatabaseUnavailable || persistenceUnknown || persistenceFailed || reconciliationUnknown || reconciliationFailed {
		ready = false
	}

	schedulerEligible := ready && live && input.Codex.Configured && codexUsable &&
		supervisorRunning(input.Supervisor.Status) &&
		input.Reconciliation.Status == HealthHealthy && input.Persistence.Status == HealthHealthy

	// Optional Codex is not part of the service liveness contract: the core
	// watchdog remains eligible while that capability is absent, starting, or
	// recovering. Required Codex applies the stricter supervisor policy so a
	// required recovery/exhaustion state cannot be treated as indefinitely
	// healthy. Stopping, database failure, and failed/unknown core persistence
	// are already excluded by live/coreReady.
	watchdogEligible := live && coreReady
	if input.Codex.Required {
		watchdogEligible = watchdogEligible && supervisorRunning(input.Supervisor.Status)
	}

	state, reason := overallState(input, components, databaseHealthy, databaseUnknown, coreReady, ready, codexUsable, stopping, exhausted)
	return Snapshot{
		State:             state,
		Status:            state,
		Ready:             ready,
		Live:              live,
		Liveness:          live,
		WatchdogEligible:  watchdogEligible,
		SchedulerEligible: schedulerEligible,
		Generation:        generation,
		Reason:            reason,
		ObservedAt:        observedAt,
		LastSuccessAt:     lastSuccessAt,
		LastFailureAt:     lastFailureAt,
		Components:        components,
	}
}

// EvaluateSnapshot is an explicit alias for callers that want the result
// name in the function call site.
func EvaluateSnapshot(input Input) Snapshot { return Evaluate(input) }

func overallState(input Input, components ComponentSet, databaseHealthy, databaseUnknown, coreReady, ready, codexUsable, stopping, exhausted bool) (State, ReasonCode) {
	if stopping {
		if input.Supervisor.Status == SupervisorStopped || input.Supervisor.Status == SupervisorClosed {
			return StateStopping, ReasonSupervisorStopped
		}
		return StateStopping, ReasonSupervisorStopping
	}
	if exhausted && input.Codex.Required {
		return StateExhausted, ReasonSupervisorExhausted
	}
	if input.Database.Status == DatabaseFailed {
		return StateDegraded, ReasonDatabaseFailed
	}
	if input.Database.Status == DatabaseUnavailable {
		return StateDegraded, ReasonDatabaseUnavailable
	}
	if databaseUnknown {
		return StateStarting, ReasonDatabaseUnknown
	}
	if input.Persistence.Status == HealthFailed {
		return StateDegraded, ReasonPersistenceFailed
	}
	if !knownHealthStatus(input.Persistence.Status) {
		return StateStarting, ReasonPersistenceUnknown
	}
	if input.Reconciliation.Status == HealthFailed {
		return StateDegraded, ReasonReconciliationFailed
	}
	if !knownHealthStatus(input.Reconciliation.Status) {
		return StateStarting, ReasonReconciliationUnknown
	}
	// A supervisor only matters to the overall service state when Codex is
	// configured. Optional/unconfigured Codex must not leave a healthy core in
	// "starting" merely because no managed process exists.
	if input.Codex.Configured && (!knownSupervisorStatus(input.Supervisor.Status) || input.Supervisor.Status == SupervisorStarting) {
		return StateStarting, supervisorReason(input.Supervisor.Status)
	}
	if input.Codex.Configured && input.Supervisor.Status == SupervisorDegraded {
		return StateDegraded, ReasonSupervisorDegraded
	}
	if input.Codex.Configured && input.Supervisor.Status == SupervisorRecovering {
		return StateDegraded, ReasonSupervisorRecovering
	}
	if input.Codex.Configured && exhausted {
		// Optional Codex exhaustion is a capability degradation, not a service
		// outage. Required mode returned StateExhausted above.
		return StateDegraded, ReasonSupervisorExhausted
	}
	if input.Reconciliation.Status == HealthAttention {
		return StateDegraded, ReasonReconciliationAttention
	}
	if input.Persistence.Status == HealthAttention {
		return StateDegraded, ReasonPersistenceAttention
	}
	if !codexUsable {
		return StateDegraded, codexReason(input)
	}
	if !coreReady || !ready || !databaseHealthy {
		return StateDegraded, firstDegradedReason(components)
	}
	return StateReady, ReasonHealthy
}

func databaseComponent(input DatabaseObservation, observedAt time.Time) ComponentStatus {
	switch input.Status {
	case DatabaseHealthy:
		return component(ComponentHealthy, ReasonHealthy, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case DatabaseFailed:
		return component(ComponentFailed, ReasonDatabaseFailed, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case DatabaseUnavailable:
		return component(ComponentFailed, ReasonDatabaseUnavailable, observedAt, input.LastSuccessAt, input.LastFailureAt)
	default:
		return component(ComponentUnknown, ReasonDatabaseUnknown, observedAt, input.LastSuccessAt, input.LastFailureAt)
	}
}

func codexComponent(input Input, usable bool, observedAt time.Time) ComponentStatus {
	if !input.Codex.Configured {
		if input.Codex.Required {
			return component(ComponentFailed, ReasonCodexRequiredNotConfigured, observedAt, input.Codex.LastSuccessAt, input.Codex.LastFailureAt)
		}
		return component(ComponentDegraded, ReasonCodexOptionalNotConfigured, observedAt, input.Codex.LastSuccessAt, input.Codex.LastFailureAt)
	}
	if !input.Codex.Available {
		reason := ReasonCodexOptionalUnavailable
		status := ComponentState(ComponentDegraded)
		if input.Codex.Required {
			reason, status = ReasonCodexRequiredUnavailable, ComponentFailed
		}
		return component(status, reason, observedAt, input.Codex.LastSuccessAt, input.Codex.LastFailureAt)
	}
	if !usable {
		reason := codexReason(input)
		if input.Codex.Required {
			return component(ComponentFailed, reason, observedAt, input.Codex.LastSuccessAt, input.Codex.LastFailureAt)
		}
		return component(ComponentDegraded, reason, observedAt, input.Codex.LastSuccessAt, input.Codex.LastFailureAt)
	}
	return component(ComponentHealthy, ReasonHealthy, observedAt, input.Codex.LastSuccessAt, input.Codex.LastFailureAt)
}

func accountComponent(input Input, observedAt time.Time) ComponentStatus {
	if !input.Codex.Configured {
		return component(ComponentDisabled, codexNotConfiguredReason(input.Codex.Required), observedAt, input.Account.LastSuccessAt, input.Account.LastFailureAt)
	}
	switch input.Account.Status {
	case AccountReady, AccountSignedIn:
		return component(ComponentHealthy, ReasonHealthy, observedAt, input.Account.LastSuccessAt, input.Account.LastFailureAt)
	case AccountNotReady, AccountSignedOut, AccountFailed:
		reason := ReasonAccountNotReady
		if input.Account.Status == AccountFailed {
			reason = ReasonAccountNotReady
		}
		if input.Codex.Required {
			return component(ComponentFailed, reason, observedAt, input.Account.LastSuccessAt, input.Account.LastFailureAt)
		}
		return component(ComponentDegraded, reason, observedAt, input.Account.LastSuccessAt, input.Account.LastFailureAt)
	default:
		return component(ComponentUnknown, ReasonAccountUnknown, observedAt, input.Account.LastSuccessAt, input.Account.LastFailureAt)
	}
}

func modelComponent(input Input, observedAt time.Time) ComponentStatus {
	if !input.Codex.Configured {
		return component(ComponentDisabled, codexNotConfiguredReason(input.Codex.Required), observedAt, input.Model.LastSuccessAt, input.Model.LastFailureAt)
	}
	switch input.Model.Status {
	case ModelCompatible:
		return component(ComponentHealthy, ReasonHealthy, observedAt, input.Model.LastSuccessAt, input.Model.LastFailureAt)
	case ModelIncompatible:
		reason := ReasonModelIncompatible
		if input.Codex.Required {
			return component(ComponentFailed, reason, observedAt, input.Model.LastSuccessAt, input.Model.LastFailureAt)
		}
		return component(ComponentDegraded, reason, observedAt, input.Model.LastSuccessAt, input.Model.LastFailureAt)
	case ModelUnavailable:
		reason := ReasonModelUnavailable
		if input.Codex.Required {
			return component(ComponentFailed, reason, observedAt, input.Model.LastSuccessAt, input.Model.LastFailureAt)
		}
		return component(ComponentDegraded, reason, observedAt, input.Model.LastSuccessAt, input.Model.LastFailureAt)
	default:
		return component(ComponentUnknown, ReasonModelUnknown, observedAt, input.Model.LastSuccessAt, input.Model.LastFailureAt)
	}
}

func supervisorComponent(input SupervisorObservation, observedAt time.Time) ComponentStatus {
	switch input.Status {
	case SupervisorRunning, SupervisorAvailable:
		return component(ComponentHealthy, ReasonHealthy, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case SupervisorStarting:
		return component(ComponentStarting, ReasonSupervisorStarting, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case SupervisorDegraded:
		return component(ComponentDegraded, ReasonSupervisorDegraded, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case SupervisorRecovering:
		return component(ComponentRecovering, ReasonSupervisorRecovering, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case SupervisorExhausted:
		return component(ComponentExhausted, ReasonSupervisorExhausted, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case SupervisorStopping:
		return component(ComponentStopping, ReasonSupervisorStopping, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case SupervisorStopped, SupervisorClosed:
		return component(ComponentStopping, ReasonSupervisorStopped, observedAt, input.LastSuccessAt, input.LastFailureAt)
	default:
		return component(ComponentUnknown, ReasonSupervisorUnknown, observedAt, input.LastSuccessAt, input.LastFailureAt)
	}
}

func healthComponent(input HealthObservation, observedAt time.Time, unknown, attention, failed ReasonCode) ComponentStatus {
	switch input.Status {
	case HealthHealthy:
		return component(ComponentHealthy, ReasonHealthy, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case HealthAttention:
		return component(ComponentDegraded, attention, observedAt, input.LastSuccessAt, input.LastFailureAt)
	case HealthFailed:
		return component(ComponentFailed, failed, observedAt, input.LastSuccessAt, input.LastFailureAt)
	default:
		return component(ComponentUnknown, unknown, observedAt, input.LastSuccessAt, input.LastFailureAt)
	}
}

func knownHealthStatus(status HealthState) bool {
	switch status {
	case HealthHealthy, HealthAttention, HealthFailed:
		return true
	default:
		return false
	}
}

func component(status ComponentState, reason ReasonCode, observedAt, lastSuccessAt, lastFailureAt time.Time) ComponentStatus {
	return ComponentStatus{
		Status:        status,
		Reason:        reason,
		ObservedAt:    observedAt,
		LastSuccessAt: normalizeEvidenceTime(lastSuccessAt, observedAt),
		LastFailureAt: normalizeEvidenceTime(lastFailureAt, observedAt),
	}
}

func codexReason(input Input) ReasonCode {
	if !input.Codex.Configured {
		return codexNotConfiguredReason(input.Codex.Required)
	}
	if !input.Codex.Available {
		if input.Codex.Required {
			return ReasonCodexRequiredUnavailable
		}
		return ReasonCodexOptionalUnavailable
	}
	if !accountReady(input.Account.Status) {
		if input.Account.Status == AccountUnknown || input.Account.Status == "" {
			return ReasonAccountUnknown
		}
		if input.Codex.Required {
			return ReasonCodexRequiredNotReady
		}
		return ReasonCodexOptionalNotReady
	}
	if input.Model.Status != ModelCompatible {
		if input.Model.Status == ModelUnknown || input.Model.Status == "" {
			return ReasonModelUnknown
		}
		if input.Model.Status == ModelUnavailable {
			return ReasonModelUnavailable
		}
		if input.Codex.Required {
			return ReasonCodexRequiredIncompatible
		}
		return ReasonCodexOptionalIncompatible
	}
	return ReasonHealthy
}

func codexNotConfiguredReason(required bool) ReasonCode {
	if required {
		return ReasonCodexRequiredNotConfigured
	}
	return ReasonCodexOptionalNotConfigured
}

func accountReady(status AccountState) bool {
	return status == AccountReady || status == AccountSignedIn
}

func supervisorRunning(status SupervisorState) bool {
	return status == SupervisorRunning || status == SupervisorAvailable
}

func supervisorReason(status SupervisorState) ReasonCode {
	switch status {
	case SupervisorStarting:
		return ReasonSupervisorStarting
	case SupervisorDegraded:
		return ReasonSupervisorDegraded
	case SupervisorRecovering:
		return ReasonSupervisorRecovering
	case SupervisorExhausted:
		return ReasonSupervisorExhausted
	case SupervisorStopping:
		return ReasonSupervisorStopping
	case SupervisorStopped, SupervisorClosed:
		return ReasonSupervisorStopped
	default:
		return ReasonSupervisorUnknown
	}
}

func knownSupervisorStatus(status SupervisorState) bool {
	switch status {
	case SupervisorStarting, SupervisorRunning, SupervisorAvailable, SupervisorDegraded, SupervisorRecovering, SupervisorExhausted, SupervisorStopping, SupervisorStopped, SupervisorClosed:
		return true
	default:
		return false
	}
}

func firstDegradedReason(components ComponentSet) ReasonCode {
	for _, status := range []ComponentStatus{components.Database, components.Persistence, components.Reconciliation, components.Supervisor, components.Codex, components.Account, components.Model} {
		if status.Reason != ReasonHealthy && status.Reason != ReasonStarting {
			return status.Reason
		}
	}
	return ReasonStarting
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

// normalizeEvidenceTime rejects evidence from the future relative to the
// observation it is attached to. A future timestamp cannot be reconciled
// safely by a pure evaluator, so it is omitted rather than clamped or
// presented as if it were already observed.
func normalizeEvidenceTime(value, observedAt time.Time) time.Time {
	value = normalizeTime(value)
	if value.IsZero() || observedAt.IsZero() || value.Before(observedAt) || value.Equal(observedAt) {
		return value
	}
	return time.Time{}
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// SnapshotStore publishes whole immutable snapshots with atomic pointer
// replacement. Store is safe for concurrent Load/Publish calls and performs
// no work beyond the caller's explicit operation.
type SnapshotStore struct {
	value atomic.Pointer[Snapshot]
}

// Store is a short alias for SnapshotStore.
type Store = SnapshotStore

// NewSnapshotStore creates a store initialized with initial.
func NewSnapshotStore(initial Snapshot) *SnapshotStore {
	store := &SnapshotStore{}
	store.Store(initial)
	return store
}

// NewStore is a short alias for NewSnapshotStore.
func NewStore(initial Snapshot) *SnapshotStore { return NewSnapshotStore(initial) }

// Load returns the most recently published snapshot, or the zero Snapshot for
// a nil/uninitialized store.
func (s *SnapshotStore) Load() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	value := s.value.Load()
	if value == nil {
		return Snapshot{}
	}
	return *value
}

// Store atomically publishes snapshot. The snapshot is value-only, so later
// caller changes to its local variable cannot mutate the published result.
func (s *SnapshotStore) Store(snapshot Snapshot) {
	if s == nil {
		return
	}
	copy := snapshot
	s.value.Store(&copy)
}

// Publish is a descriptive alias for Store.
func (s *SnapshotStore) Publish(snapshot Snapshot) { s.Store(snapshot) }
