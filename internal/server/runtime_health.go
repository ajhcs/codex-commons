package server

// The runtime monitor is the one owner of slow health observations. It
// probes in a context-owned goroutine, evaluates the storage-neutral
// internal/runtimehealth model, and atomically publishes one immutable value.
// Request handlers and the notifier only read the last publication.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/runtimehealth"
	commonsstore "codex-commons/internal/store"
)

const (
	defaultRuntimeHealthInterval = 5 * time.Second
	defaultRuntimeProbeTimeout   = 2 * time.Second
	defaultRuntimeRecoveryGrace  = 30 * time.Second
	defaultStateChangeBackoff    = 10 * time.Millisecond
	maxStateChangeBackoff        = time.Second
)

// RuntimeHealthSnapshot is an alias for the authoritative immutable model.
// It is kept in the server package so package-local integration tests can use
// the same vocabulary without introducing another health shape.
type RuntimeHealthSnapshot = runtimehealth.Snapshot

type runtimeHealthProbe struct {
	DB            func(context.Context) error
	Persistence   func(context.Context) (runtimePersistenceResult, error)
	Supervisor    func(context.Context) (runtimeSupervisorResult, error)
	StateChanges  func() <-chan struct{}
	Account       func(context.Context) (codexauth.AccountState, error)
	Compatibility func(context.Context) (bool, error)
}

type runtimePersistenceResult struct {
	Healthy          bool
	Reconciliation   string
	Uncertain        int
	PersistenceFault bool
}

type runtimeSupervisorResult struct {
	Configured        bool
	Available         bool
	Generation        uint64
	State             codexauth.ProcessState
	Attempts          int
	MaxAttempts       int
	NextRetryAt       time.Time
	LastHealthyAt     time.Time
	RecoveryActive    bool
	RecoverySince     time.Time
	RecoveryExhausted bool
}

type runtimeHealthMeta struct {
	readyLatched      bool
	recoverySince     time.Time
	recoveryActive    bool
	recoveryExhausted bool
}

type runtimeHealthOptions struct {
	Parent          context.Context
	RequiredCodex   bool
	CodexConfigured bool
	Probe           runtimeHealthProbe
	// StateChangeBackoffWait is a narrow test seam for the defensive wait used
	// when a custom StateChanges reporter violates the one-shot contract by
	// returning an already-closed channel forever. Production uses a cancellable
	// timer; a valid open channel still wakes immediately on every transition.
	StateChangeBackoffWait func(context.Context, time.Duration) bool
	Interval               time.Duration
	ProbeTimeout           time.Duration
	RecoveryGrace          time.Duration
	Now                    func() time.Time
	OnSnapshot             func(runtimehealth.Snapshot)
	OnSchedulerReady       func()
}

func waitStateChangeBackoff(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextStateChangeBackoff(delay time.Duration) time.Duration {
	if delay <= 0 {
		return defaultStateChangeBackoff
	}
	if delay >= maxStateChangeBackoff/2 {
		return maxStateChangeBackoff
	}
	return delay * 2
}

type runtimeHealthMonitor struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	trigger chan struct{}

	options     runtimeHealthOptions
	publication atomic.Pointer[runtimePublication]

	startOnce sync.Once
	closeOnce sync.Once
	readyOnce sync.Once
	readyCh   chan struct{}

	// These fields are owned by the monitor goroutine. They are deliberately
	// not protected by a mutex, so no monitor lock can span a probe call.
	cacheGeneration uint64
	cacheValid      bool
	cachedAccount   codexauth.AccountState
	cachedModel     string
}

// runtimePublication is the one atomic unit exposed to all consumers. A
// health snapshot and its recovery/supervisor metadata must never be read
// from separate atomics, or a request could observe two different probe
// generations.
type runtimePublication struct {
	snapshot   runtimehealth.Snapshot
	meta       runtimeHealthMeta
	supervisor runtimeSupervisorResult
}

func newRuntimeHealthMonitor(options runtimeHealthOptions) *runtimeHealthMonitor {
	if options.Parent == nil {
		options.Parent = context.Background()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Interval <= 0 {
		options.Interval = defaultRuntimeHealthInterval
	}
	if options.ProbeTimeout <= 0 {
		options.ProbeTimeout = defaultRuntimeProbeTimeout
	}
	if options.RecoveryGrace <= 0 {
		options.RecoveryGrace = defaultRuntimeRecoveryGrace
	}
	ctx, cancel := context.WithCancel(options.Parent)
	initial := runtimehealth.Evaluate(runtimehealth.Input{
		Codex: runtimehealth.CodexObservation{Configured: options.CodexConfigured, Required: options.RequiredCodex},
	})
	m := &runtimeHealthMonitor{
		ctx: ctx, cancel: cancel, done: make(chan struct{}), trigger: make(chan struct{}, 1), options: options,
		readyCh: make(chan struct{}),
	}
	m.publication.Store(&runtimePublication{snapshot: initial})
	return m
}

func newRuntimeHealthMonitorForTest(options runtimeHealthOptions) *runtimeHealthMonitor {
	return newRuntimeHealthMonitor(options)
}

func (m *runtimeHealthMonitor) Start() {
	if m == nil {
		return
	}
	m.startOnce.Do(func() {
		go func() {
			watchDone := make(chan struct{})
			go m.watchStateChanges(watchDone)
			defer func() {
				<-watchDone
				close(m.done)
			}()
			m.loop()
		}()
	})
}

// watchStateChanges converts the supervisor's one-shot close/re-subscribe
// channel into a coalescing trigger owned by the monitor loop. A conforming
// reporter returns an open channel and closes it once for each transition;
// production transitions therefore remain immediate. A misbehaving custom
// reporter that returns an already-closed channel forever is defensively
// capped by exponential backoff so it cannot spin the monitor or probe loop.
// All probes stay serialized in loop; a state change can never race
// account/model cache mutation or scheduler wake publication.
func (m *runtimeHealthMonitor) watchStateChanges(done chan<- struct{}) {
	defer close(done)
	if m.options.Probe.StateChanges == nil {
		return
	}
	backoff := defaultStateChangeBackoff
	waitBackoff := m.options.StateChangeBackoffWait
	if waitBackoff == nil {
		waitBackoff = waitStateChangeBackoff
	}
	for {
		changes := m.options.Probe.StateChanges()
		if changes == nil {
			return
		}
		// Check once without waiting so a permanently closed channel is
		// distinguished from the normal open-until-transition contract. If the
		// channel is open, immediately block on it; a real transition is never
		// delayed by the defensive backoff.
		select {
		case _, ok := <-changes:
			if !ok {
				select {
				case m.trigger <- struct{}{}:
				default:
				}
				if !waitBackoff(m.ctx, backoff) {
					return
				}
				backoff = nextStateChangeBackoff(backoff)
				continue
			}
			// A value is outside the documented close-only contract, but it is
			// still a transition signal. Re-subscribe immediately and reset the
			// defensive backoff because the channel was observably open.
			select {
			case m.trigger <- struct{}{}:
			default:
			}
			backoff = defaultStateChangeBackoff
			continue
		default:
		}
		select {
		case <-m.ctx.Done():
			return
		case _, ok := <-changes:
			if !ok {
				select {
				case m.trigger <- struct{}{}:
				default:
				}
				// StateChanges returns a fresh channel after each transition.
				// ManagedProcessClient supplies an open replacement. If a custom
				// reporter closes the channel between the nonblocking check above
				// and this receive, treat the observed wait as a valid transition
				// and reset the defensive backoff.
				backoff = defaultStateChangeBackoff
			} else {
				select {
				case m.trigger <- struct{}{}:
				default:
				}
				backoff = defaultStateChangeBackoff
			}
		}
	}
}

func (m *runtimeHealthMonitor) loop() {
	// New() may perform the first probe before returning so a request cannot
	// observe an unprobed readiness state. Direct monitor users retain the
	// conservative zero snapshot and get their first probe here.
	if m.current().ObservedAt.IsZero() {
		m.probeAndPublish(m.ctx)
	}
	ticker := time.NewTicker(m.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.probeAndPublish(m.ctx)
		case <-m.trigger:
			m.probeAndPublish(m.ctx)
		}
	}
}

func (m *runtimeHealthMonitor) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		m.cancel()
		// If Start was never called, consume startOnce and close done so Close
		// remains deterministic. Otherwise the monitor goroutine owns done.
		m.startOnce.Do(func() { close(m.done) })
		<-m.done
	})
}

func (m *runtimeHealthMonitor) WaitReady(ctx context.Context) error {
	if m == nil {
		return errors.New("runtime health monitor unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.current().Ready {
		return nil
	}
	select {
	case <-m.readyCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

// RuntimeSnapshot is the pure, storage-neutral internal read.
func (m *runtimeHealthMonitor) RuntimeSnapshot() runtimehealth.Snapshot {
	if m == nil {
		return runtimehealth.Snapshot{}
	}
	publication := m.publication.Load()
	if publication == nil {
		return runtimehealth.Snapshot{}
	}
	return publication.snapshot
}

// Snapshot implements httpapi.RuntimeHealthProvider. It only converts the
// already-published value and never performs I/O.
func (m *runtimeHealthMonitor) Snapshot() httpapi.RuntimeHealthSnapshot {
	if m == nil {
		return httpapi.RuntimeHealthSnapshot{}
	}
	publication := m.publication.Load()
	if publication == nil {
		return httpapi.RuntimeHealthSnapshot{}
	}
	return runtimeHTTPSnapshot(publication.snapshot, publication.meta, publication.supervisor, m.options.RequiredCodex)
}

func (m *runtimeHealthMonitor) current() runtimehealth.Snapshot { return m.RuntimeSnapshot() }

func (m *runtimeHealthMonitor) metaSnapshot() runtimeHealthMeta {
	if m == nil {
		return runtimeHealthMeta{}
	}
	publication := m.publication.Load()
	if publication == nil {
		return runtimeHealthMeta{}
	}
	return publication.meta
}

func (m *runtimeHealthMonitor) supervisorSnapshot() runtimeSupervisorResult {
	if m == nil {
		return runtimeSupervisorResult{}
	}
	publication := m.publication.Load()
	if publication == nil {
		return runtimeSupervisorResult{}
	}
	return publication.supervisor
}

func (m *runtimeHealthMonitor) watchdogSnapshot() NotifierHealthSnapshot {
	if m == nil {
		return NotifierHealthSnapshot{}
	}
	publication := m.publication.Load()
	if publication == nil {
		return NotifierHealthSnapshot{}
	}
	snapshot := publication.snapshot
	meta := publication.meta
	coreHealthy := snapshot.Live &&
		snapshot.Components.Persistence.Status == runtimehealth.ComponentHealthy &&
		snapshot.Components.Reconciliation.Status != runtimehealth.ComponentUnknown &&
		snapshot.Components.Reconciliation.Status != runtimehealth.ComponentFailed
	terminal := !snapshot.ObservedAt.IsZero() &&
		(snapshot.Components.Database.Status == runtimehealth.ComponentFailed ||
			snapshot.Components.Persistence.Status == runtimehealth.ComponentFailed ||
			snapshot.Components.Reconciliation.Status == runtimehealth.ComponentFailed)
	exhausted := meta.recoveryExhausted || snapshot.State == runtimehealth.StateExhausted || snapshot.Components.Supervisor.Status == runtimehealth.ComponentExhausted
	ready := meta.readyLatched || snapshot.Ready || terminal
	// A latched READY state remains useful during bounded recovery, but an
	// already-exhausted required supervisor must never emit a fresh READY edge
	// when a notifier starts against that publication.
	if exhausted {
		ready = false
	}
	return NotifierHealthSnapshot{
		// A terminal post-probe core failure is marked as observed-ready for
		// notifier classification only; DatabaseHealthy/CoreHealthy remain false,
		// so no READY or watchdog signal is emitted and the notifier can return a
		// static fatal error instead of waiting forever in Starting.
		Probed:                 !snapshot.ObservedAt.IsZero(),
		Ready:                  ready,
		DatabaseHealthy:        snapshot.Components.Database.Status == runtimehealth.ComponentHealthy,
		CoreHealthy:            coreHealthy,
		CodexHealthy:           snapshot.Components.Codex.Status == runtimehealth.ComponentHealthy,
		RequiredCodex:          m.options.RequiredCodex,
		WatchdogEligible:       snapshot.WatchdogEligible,
		RecoverySince:          meta.recoverySince,
		RequiredRecoveryActive: meta.recoveryActive,
		RecoveryExhausted:      exhausted,
	}
}

// ProbeOnce executes one observation/evaluation synchronously. It does not
// publish, making it safe for deterministic tests to inspect before Start.
func (m *runtimeHealthMonitor) ProbeOnce(ctx context.Context) runtimehealth.Snapshot {
	if m == nil {
		return runtimehealth.Snapshot{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := m.options.Now().UTC()
	snapshot, _ := m.probe(ctx, m.current(), now)
	return snapshot
}

func (m *runtimeHealthMonitor) probeAndPublish(parent context.Context) {
	if m == nil {
		return
	}
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	if m.options.ProbeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.options.ProbeTimeout)
		defer cancel()
	}
	previous := m.current()
	now := m.options.Now().UTC()
	next, supervisor := m.probe(ctx, previous, now)
	if next.Ready {
		m.readyOnce.Do(func() { close(m.readyCh) })
	}

	meta := m.metaSnapshot()
	if next.Ready {
		meta.readyLatched = true
	}
	if next.Components.Codex.Status == runtimehealth.ComponentHealthy {
		meta.recoverySince = time.Time{}
		meta.recoveryActive = false
		meta.recoveryExhausted = false
	} else if next.State == runtimehealth.StateExhausted || next.Components.Supervisor.Status == runtimehealth.ComponentExhausted {
		// Exhaustion is meaningful even before the first READY edge. The
		// notifier must fail deterministically rather than waiting for a startup
		// timeout that is unrelated to the shared supervisor state.
		meta.recoveryActive = true
		meta.recoveryExhausted = true
		if meta.recoverySince.IsZero() {
			meta.recoverySince = supervisor.RecoverySince
			if meta.recoverySince.IsZero() {
				meta.recoverySince = now
			}
		}
	} else if meta.readyLatched && m.options.CodexConfigured {
		if meta.recoverySince.IsZero() {
			meta.recoverySince = supervisor.RecoverySince
			if meta.recoverySince.IsZero() {
				meta.recoverySince = now
			}
		}
		meta.recoveryActive = true
		meta.recoveryExhausted = now.Sub(meta.recoverySince) >= m.options.RecoveryGrace
	}
	m.publication.Store(&runtimePublication{snapshot: next, meta: meta, supervisor: supervisor})
	if m.options.OnSnapshot != nil {
		m.options.OnSnapshot(next)
	}
	if !previous.SchedulerEligible && next.SchedulerEligible && m.options.OnSchedulerReady != nil {
		m.options.OnSchedulerReady()
	}
}

func (m *runtimeHealthMonitor) probe(ctx context.Context, previous runtimehealth.Snapshot, now time.Time) (runtimehealth.Snapshot, runtimeSupervisorResult) {
	database := runtimehealth.DatabaseObservation{Status: runtimehealth.DatabaseUnknown}
	if m.options.Probe.DB != nil {
		if err := m.options.Probe.DB(ctx); err == nil {
			database.Status = runtimehealth.DatabaseHealthy
		} else {
			database.Status = runtimehealth.DatabaseUnavailable
		}
	}
	persistence := runtimehealth.HealthObservation{Status: runtimehealth.HealthUnknown}
	reconciliation := runtimehealth.HealthObservation{Status: runtimehealth.HealthUnknown}
	if m.options.Probe.Persistence != nil {
		value, err := m.options.Probe.Persistence(ctx)
		if err != nil {
			persistence.Status = runtimehealth.HealthFailed
			reconciliation.Status = runtimehealth.HealthFailed
		} else {
			if value.Healthy && !value.PersistenceFault {
				persistence.Status = runtimehealth.HealthHealthy
			} else {
				persistence.Status = runtimehealth.HealthFailed
			}
			switch value.Reconciliation {
			case "healthy":
				reconciliation.Status = runtimehealth.HealthHealthy
			case "attention":
				reconciliation.Status = runtimehealth.HealthAttention
			case "failed":
				reconciliation.Status = runtimehealth.HealthFailed
			default:
				reconciliation.Status = runtimehealth.HealthUnknown
			}
			// Uncertain durable jobs are a reconciliation attention state, even
			// if an old installation_status row still says healthy. Never erase
			// the startup/reconciliation safety signal from the runtime snapshot.
			if value.Uncertain > 0 && reconciliation.Status == runtimehealth.HealthHealthy {
				reconciliation.Status = runtimehealth.HealthAttention
			}
		}
	}

	codex := runtimehealth.CodexObservation{Configured: m.options.CodexConfigured, Required: m.options.RequiredCodex}
	supervisor := runtimehealth.SupervisorObservation{Status: runtimehealth.SupervisorUnknown}
	supervisorResult := runtimeSupervisorResult{Configured: m.options.CodexConfigured}
	account := runtimehealth.AccountObservation{Status: runtimehealth.AccountUnknown}
	model := runtimehealth.ModelObservation{Status: runtimehealth.ModelUnknown}
	if !m.options.CodexConfigured {
		// There is no supervisor component when Codex is not configured. Treat
		// the disabled capability as a running core for optional readiness.
		supervisor.Status = runtimehealth.SupervisorRunning
		supervisorResult.State = codexauth.ProcessStateAvailable
	} else if m.options.Probe.Supervisor != nil {
		value, err := m.options.Probe.Supervisor(ctx)
		supervisorResult = value
		supervisor = runtimeSupervisorObservation(value, err, now)
		supervisorResult.RecoverySince = supervisor.LastFailureAt
		supervisorResult.LastHealthyAt = supervisor.LastSuccessAt
		codex.Available = supervisorRunningObservation(supervisor.Status) && value.Available
		codex.LastSuccessAt = supervisor.LastSuccessAt
		codex.LastFailureAt = supervisor.LastFailureAt
		if supervisorRunningObservation(supervisor.Status) {
			m.probeCodexForGeneration(ctx, &account, &model, supervisor.Generation, previous)
		} else {
			// A generation that is no longer running must not retain its cached
			// compatibility decision when it becomes usable again.
			m.cacheValid = false
		}
	} else {
		supervisor.Status = runtimehealth.SupervisorStarting
		m.cacheValid = false
	}
	return runtimehealth.Evaluate(runtimehealth.Input{
		ObservedAt: now, Database: database, Codex: codex, Supervisor: supervisor,
		Account: account, Model: model, Reconciliation: reconciliation, Persistence: persistence,
	}), supervisorResult
}

func supervisorRunningObservation(status runtimehealth.SupervisorState) bool {
	return status == runtimehealth.SupervisorRunning || status == runtimehealth.SupervisorAvailable
}

// runtimeSupervisorObservation preserves the managed process lifecycle in the
// runtimehealth vocabulary. Boolean availability is only a compatibility
// fallback for clients that predate Reporter; a reported process state always
// wins, including degraded and closed states.
func runtimeSupervisorObservation(value runtimeSupervisorResult, probeErr error, observedAt time.Time) runtimehealth.SupervisorObservation {
	status := runtimehealth.SupervisorUnknown
	switch value.State {
	case codexauth.ProcessStateStarting:
		status = runtimehealth.SupervisorStarting
	case codexauth.ProcessStateAvailable:
		status = runtimehealth.SupervisorAvailable
	case codexauth.ProcessStateDegraded:
		status = runtimehealth.SupervisorDegraded
	case codexauth.ProcessStateRecovering:
		status = runtimehealth.SupervisorRecovering
	case codexauth.ProcessStateExhausted:
		status = runtimehealth.SupervisorExhausted
	case codexauth.ProcessStateClosed:
		status = runtimehealth.SupervisorClosed
	default:
		switch {
		case value.RecoveryExhausted:
			status = runtimehealth.SupervisorExhausted
		case value.RecoveryActive:
			status = runtimehealth.SupervisorRecovering
		case value.Available:
			status = runtimehealth.SupervisorAvailable
		case probeErr != nil:
			status = runtimehealth.SupervisorRecovering
		default:
			status = runtimehealth.SupervisorStarting
		}
	}
	lastHealthyAt := clampRuntimeTimestamp(value.LastHealthyAt, observedAt)
	recoveryAt := clampRecoveryTimestamp(value.RecoverySince, observedAt, status)
	return runtimehealth.SupervisorObservation{
		Status:        status,
		Generation:    value.Generation,
		LastSuccessAt: lastHealthyAt,
		LastFailureAt: recoveryAt,
	}
}

func clampRuntimeTimestamp(value, observedAt time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	value = value.UTC()
	if !observedAt.IsZero() && value.After(observedAt) {
		return observedAt.UTC()
	}
	return value
}

func clampRecoveryTimestamp(value, observedAt time.Time, status runtimehealth.SupervisorState) time.Time {
	value = clampRuntimeTimestamp(value, observedAt)
	if !value.IsZero() {
		return value
	}
	if !observedAt.IsZero() && (status == runtimehealth.SupervisorDegraded || status == runtimehealth.SupervisorRecovering || status == runtimehealth.SupervisorExhausted) {
		return observedAt.UTC()
	}
	return time.Time{}
}

// Account/model checks are expensive and cached by supervisor generation. A
// generation change or a transition away from running invalidates the cache.
func (m *runtimeHealthMonitor) probeCodexForGeneration(ctx context.Context, account *runtimehealth.AccountObservation, model *runtimehealth.ModelObservation, generation uint64, previous runtimehealth.Snapshot) {
	modelCached := m.cacheValid && m.cacheGeneration == generation && previous.Generation == generation
	if !modelCached {
		m.cacheGeneration, m.cacheValid = generation, true
		m.cachedModel = "unknown"
	}
	// Account state can change within one process generation (for example an
	// account_updated event), so refresh it every monitor cycle. Model
	// compatibility remains generation-cached because it is the expensive,
	// stable capability probe.
	m.cachedAccount = codexauth.AccountUnknown
	if m.options.Probe.Account != nil {
		state, err := m.options.Probe.Account(ctx)
		if err == nil {
			m.cachedAccount = state
		}
	}
	if !modelCached && m.options.Probe.Compatibility != nil {
		supported, err := m.options.Probe.Compatibility(ctx)
		switch {
		case err != nil:
			m.cachedModel = "unavailable"
		case supported:
			m.cachedModel = "compatible"
		default:
			m.cachedModel = "incompatible"
		}
	}
	account.Status = accountObservation(m.cachedAccount)
	model.Status = modelObservation(m.cachedModel)
}

func accountObservation(state codexauth.AccountState) runtimehealth.AccountState {
	switch state {
	case codexauth.AccountSignedIn:
		return runtimehealth.AccountReady
	case codexauth.AccountSignedOut:
		return runtimehealth.AccountNotReady
	default:
		return runtimehealth.AccountUnknown
	}
}

func modelObservation(value string) runtimehealth.ModelState {
	switch value {
	case "compatible":
		return runtimehealth.ModelCompatible
	case "incompatible":
		return runtimehealth.ModelIncompatible
	case "unavailable":
		return runtimehealth.ModelUnavailable
	default:
		return runtimehealth.ModelUnknown
	}
}

func runtimeHealthProbeForStore(store *commonsstore.Store) runtimeHealthProbe {
	probe := runtimeHealthProbe{}
	if store == nil {
		return probe
	}
	probe.DB = func(ctx context.Context) error { return store.DB().PingContext(ctx) }
	probe.Persistence = func(ctx context.Context) (runtimePersistenceResult, error) {
		var reconciliation string
		if err := store.DB().QueryRowContext(ctx, `SELECT reconciliation_status FROM installation_status WHERE id=1`).Scan(&reconciliation); err != nil {
			return runtimePersistenceResult{}, err
		}
		var uncertain int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_jobs WHERE state='uncertain'`).Scan(&uncertain); err != nil {
			return runtimePersistenceResult{}, err
		}
		if reconciliation == "" || reconciliation == "unknown" {
			return runtimePersistenceResult{Healthy: false, Reconciliation: reconciliation, Uncertain: uncertain}, nil
		}
		return runtimePersistenceResult{Healthy: true, Reconciliation: reconciliation, Uncertain: uncertain}, nil
	}
	return probe
}

func runtimeHealthProbeForClient(client codexauth.Client) runtimeHealthProbe {
	probe := runtimeHealthProbe{}
	if client == nil {
		return probe
	}
	probe.Supervisor = func(context.Context) (runtimeSupervisorResult, error) {
		result := runtimeSupervisorResult{Configured: true, Available: client.Available()}
		if reporter, ok := client.(codexauth.Reporter); ok {
			snapshot := reporter.Snapshot()
			result.State = snapshot.State
			result.Attempts = snapshot.Attempts
			result.MaxAttempts = snapshot.MaxAttempts
			result.NextRetryAt = snapshot.NextRetryAt
			result.LastHealthyAt = snapshot.LastHealthyAt
			result.Generation = snapshot.Generation
			result.Available = snapshot.State == codexauth.ProcessStateAvailable && client.Available()
			switch snapshot.State {
			case codexauth.ProcessStateRecovering, codexauth.ProcessStateDegraded:
				result.RecoveryActive = true
			case codexauth.ProcessStateExhausted:
				result.RecoveryActive, result.RecoveryExhausted = true, true
			}
			result.RecoverySince = snapshot.LastFailureAt
		}
		return result, nil
	}
	if reporter, ok := client.(codexauth.StateChangeReporter); ok {
		probe.StateChanges = reporter.StateChanges
	}
	probe.Account = client.AccountState
	if archaeology, ok := client.(codexauth.ArchaeologyClient); ok {
		probe.Compatibility = func(ctx context.Context) (bool, error) {
			return archaeology.SupportsModel(ctx, "gpt-5.6-luna", "max")
		}
	}
	return probe
}

func runtimeHTTPSnapshot(snapshot runtimehealth.Snapshot, meta runtimeHealthMeta, supervisor runtimeSupervisorResult, required bool) httpapi.RuntimeHealthSnapshot {
	out := httpapi.ProjectRuntimeHealth(snapshot, required)
	recoverySince := timePointer(supervisor.RecoverySince)
	if recoverySince == nil && !meta.recoverySince.IsZero() {
		value := meta.recoverySince.UTC()
		recoverySince = &value
	}
	out.Supervisor.Generation = snapshot.Generation
	if supervisor.Generation != 0 || snapshot.Generation == 0 {
		out.Supervisor.Generation = supervisor.Generation
	}
	if supervisor.State != "" {
		out.Supervisor.State = string(supervisor.State)
	}
	out.Supervisor.RetryCount = supervisor.Attempts
	out.Supervisor.RetryAt = timePointer(supervisor.NextRetryAt)
	out.Supervisor.LastHealthy = timePointer(supervisor.LastHealthyAt)
	out.Supervisor.RecoveryActive = meta.recoveryActive || supervisor.RecoveryActive
	out.Supervisor.RecoveryExhausted = meta.recoveryExhausted || supervisor.RecoveryExhausted || snapshot.State == runtimehealth.StateExhausted
	if out.Supervisor.LastHealthy == nil {
		out.Supervisor.LastHealthy = timePointer(snapshot.Components.Supervisor.LastSuccessAt)
	}
	out.Supervisor.RecoverySince = recoverySince
	return out
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}
