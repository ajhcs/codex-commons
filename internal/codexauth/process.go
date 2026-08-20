package codexauth

import (
	"context"
	"errors"
	"io"
	"log/slog"
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

// ProcessState is the externally observable lifecycle state of a managed App
// Server. Values are intentionally stable strings because they are suitable
// for bounded health/status output.
type ProcessState string

const (
	ProcessStateStarting   ProcessState = "starting"
	ProcessStateAvailable  ProcessState = "available"
	ProcessStateDegraded   ProcessState = "degraded"
	ProcessStateRecovering ProcessState = "recovering"
	ProcessStateExhausted  ProcessState = "exhausted"
	ProcessStateClosed     ProcessState = "closed"

	// Short aliases keep the state vocabulary convenient for package callers.
	StateStarting   = ProcessStateStarting
	StateAvailable  = ProcessStateAvailable
	StateDegraded   = ProcessStateDegraded
	StateRecovering = ProcessStateRecovering
	StateExhausted  = ProcessStateExhausted
	StateClosed     = ProcessStateClosed
)

// ManagedProcessState is retained as a descriptive alias for callers that
// prefer the managed-client terminology.
type ManagedProcessState = ProcessState
type SupervisorState = ProcessState

const (
	ManagedProcessStateStarting   = ProcessStateStarting
	ManagedProcessStateAvailable  = ProcessStateAvailable
	ManagedProcessStateDegraded   = ProcessStateDegraded
	ManagedProcessStateRecovering = ProcessStateRecovering
	ManagedProcessStateExhausted  = ProcessStateExhausted
	ManagedProcessStateClosed     = ProcessStateClosed
	SupervisorStateStarting       = ProcessStateStarting
	SupervisorStateAvailable      = ProcessStateAvailable
	SupervisorStateDegraded       = ProcessStateDegraded
	SupervisorStateRecovering     = ProcessStateRecovering
	SupervisorStateExhausted      = ProcessStateExhausted
	SupervisorStateClosed         = ProcessStateClosed
)

// Snapshot is an immutable, metadata-only view of the supervisor. Every call
// returns values copied while the manager lock is held; it contains no child
// pointers, protocol payloads, prompts, environment values, or secrets.
type Snapshot struct {
	State      ProcessState `json:"state"`
	Generation uint64       `json:"generation"`
	PID        int          `json:"pid,omitempty"`

	// Attempts is the number of restart factory attempts consumed in the
	// current health episode. Attempt and RestartAttempts are compatibility
	// aliases for status consumers that use either vocabulary.
	Attempts               int `json:"attempts"`
	Attempt                int `json:"attempt"`
	RestartAttempts        int `json:"restart_attempts"`
	MaxAttempts            int `json:"max_attempts"`
	RestartBudget          int `json:"restart_budget"`
	MaxRestartBudget       int `json:"max_restart_budget"`
	RestartBudgetRemaining int `json:"restart_budget_remaining"`

	LastExitReason    string `json:"last_exit_reason,omitempty"`
	LastFailureReason string `json:"last_failure_reason,omitempty"`
	LastExit          string `json:"last_exit,omitempty"`
	LastFailure       string `json:"last_failure,omitempty"`
	Reason            string `json:"reason,omitempty"`

	LastExitAt    time.Time `json:"last_exit_at,omitempty"`
	LastFailureAt time.Time `json:"last_failure_at,omitempty"`
	LastHealthyAt time.Time `json:"last_healthy_at,omitempty"`
	HealthySince  time.Time `json:"healthy_since,omitempty"`
	NextRetryAt   time.Time `json:"next_retry_at,omitempty"`
	NextRetry     time.Time `json:"next_retry,omitempty"`
}

// ManagedProcessSnapshot and ProcessSnapshot are descriptive aliases for
// integrations that want to make the source of the snapshot explicit.
type ManagedProcessSnapshot = Snapshot
type ProcessSnapshot = Snapshot

// Reporter is the read-only reporting capability of a managed client. The
// returned Snapshot is detached from the manager and safe to retain.
type Reporter interface {
	Snapshot() Snapshot
}

// StateChangeReporter is the optional wake-up extension for Reporter. A
// returned channel closes on the next state/snapshot transition; consumers
// must re-read Snapshot and then subscribe again. It carries no process or
// protocol payload.
type StateChangeReporter interface {
	Reporter
	StateChanges() <-chan struct{}
}

// ManagedProcessFactory is an optional test/integration seam. The factory
// must honor the supplied parent context and stop promptly when it is
// canceled; production NewProcess bounds its handshake through that context.
// The returned Client must also implement the optional archaeology
// capabilities when those capabilities are used by the caller; otherwise the
// managed constructor reports ErrUnavailable rather than creating a partially
// capable child.
type ManagedProcessFactory func(context.Context, ProcessConfig) (Client, error)

// ManagedProcessOptions controls bounded restart behaviour. A zero value is
// production-safe and uses the package defaults. Now, Backoff, and Factory
// are intentionally injectable so supervisor tests can be deterministic.
type ManagedProcessOptions struct {
	Factory ManagedProcessFactory
	// Logger receives metadata-only lifecycle records. If nil, slog.Default is
	// used. The supervisor never logs command lines, environment values,
	// prompts, or raw factory/process errors.
	Logger             *slog.Logger
	MaxRestartAttempts int
	// MaxRecoveryAttempts is an older spelling retained for compatibility.
	MaxRecoveryAttempts      int
	MaxRestarts              int
	RestartBudget            int
	Backoff                  func(attempt int) time.Duration
	HealthyInterval          time.Duration
	HealthInterval           time.Duration
	SustainedHealthyInterval time.Duration
	Now                      func() time.Time
	Clock                    func() time.Time
	StartupTimeout           time.Duration
}

type supervisorEvent struct {
	generation uint64
	client     managedProcess
	err        error
	pid        int
	lifecycle  bool
}

const (
	lifecycleChildExit        = "managed process child exit"
	lifecycleRestartAttempt   = "managed process restart attempt"
	lifecycleRestartResult    = "managed process restart result"
	lifecycleRestartExhausted = "managed process restart exhausted"
)

// ManagedProcessClient supervises one App Server process generation. Real
// instances created by NewManagedProcess have one autonomous supervisor loop;
// request paths only signal that loop and never call the factory. A small
// legacy mode remains for package tests and source-compatible hand-built
// managers from older releases.
type ManagedProcessClient struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	config  ProcessConfig
	client  managedProcess
	handler func(Event)
	closed  bool

	state            ProcessState
	supervise        bool
	supervisorEvents chan supervisorEvent
	supervisorWake   chan struct{}
	supervisorDone   chan struct{}
	initialReady     chan error
	initialReported  bool
	initialAttempted bool
	stateChanged     chan struct{}

	generation          uint64
	recoveryAttempts    int
	nextRecoveryAt      time.Time
	maxRecoveryAttempts int
	backoff             func(attempt int) time.Duration
	now                 func() time.Time
	factory             processFactory
	logger              *slog.Logger
	healthyInterval     time.Duration
	healthySince        time.Time

	lastExitReason    string
	lastFailureReason string
	lastExitAt        time.Time
	lastFailureAt     time.Time
	lastHealthyAt     time.Time
	pid               int

	// recovery is used both by the autonomous supervisor and by the legacy
	// request-recovery seam. Only the autonomous loop creates replacements
	// when supervise is true.
	recovery *managedRecovery
	closeErr error
}

type managedRecovery struct {
	generation uint64
	done       chan struct{}
	client     managedProcess
	err        error
	doneClosed bool
}

const (
	defaultMaxRecoveryAttempts = 3
	defaultRecoveryBackoff     = 250 * time.Millisecond
	maxRecoveryBackoff         = 2 * time.Second
	defaultHealthyInterval     = 30 * time.Second
	defaultStartupTimeout      = 10 * time.Second
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

func safeLifecycleState(state ProcessState) string {
	switch state {
	case ProcessStateStarting, ProcessStateAvailable, ProcessStateDegraded,
		ProcessStateRecovering, ProcessStateExhausted, ProcessStateClosed:
		return string(state)
	default:
		return "unknown"
	}
}

func safeLifecycleReason(reason string) string {
	switch reason {
	case "unknown", "process_exited", "protocol_failure", "line_too_large",
		"unavailable", "canceled", "factory_failure":
		return reason
	default:
		return "unknown"
	}
}

func lifecyclePhase(initial bool) string {
	if initial {
		return "initial"
	}
	return "restart"
}

// logLifecycle emits only bounded correlation metadata. Keep this helper free
// of manager locking: slog handlers are callbacks and must never run while a
// supervisor mutex is held.
func (m *ManagedProcessClient) logLifecycle(message string, state ProcessState, generation uint64, pid, attempt, maxAttempts int, reason, phase string) {
	if m == nil {
		return
	}
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	if attempt < 0 {
		attempt = 0
	}
	if maxAttempts < 0 {
		maxAttempts = 0
	}
	if phase != "initial" && phase != "restart" {
		phase = "unknown"
	}
	logger.LogAttrs(context.Background(), slog.LevelInfo, message,
		slog.String("state", safeLifecycleState(state)),
		slog.Uint64("generation", generation),
		slog.Int("pid", pid),
		slog.Int("attempt", attempt),
		slog.Int("max_attempts", maxAttempts),
		slog.String("reason", safeLifecycleReason(reason)),
		slog.String("phase", phase),
	)
}

// NewManagedProcess starts an autonomous supervisor and waits for its first
// child handshake. Once it returns successfully, later child exits are
// observed through the child lifecycle signal and recovered without request
// traffic.
func NewManagedProcess(ctx context.Context, config ProcessConfig) (*ManagedProcessClient, error) {
	return NewManagedProcessWithOptions(ctx, config, ManagedProcessOptions{})
}

// NewManagedProcessAsync returns the supervisor immediately in starting state.
// It is useful when service topology must be installed even if the optional
// Codex executable is cold or temporarily unavailable. Callers can inspect
// Snapshot/Available and retain the returned client; the supervisor will keep
// boundedly retrying initial creation until it becomes available or exhausts
// its restart policy.
func NewManagedProcessAsync(ctx context.Context, config ProcessConfig) *ManagedProcessClient {
	return newAutonomousManagedProcess(ctx, config, defaultProcessFactory, ManagedProcessOptions{})
}

// NewManagedProcessSupervisor is a descriptive alias for the nonblocking
// constructor.
func NewManagedProcessSupervisor(ctx context.Context, config ProcessConfig) *ManagedProcessClient {
	return NewManagedProcessAsync(ctx, config)
}

// NewManagedProcessWithFactory is a deterministic/integration-friendly
// constructor. It has the same startup contract as NewManagedProcess while
// allowing a caller to provide an already controlled Client factory.
func NewManagedProcessWithFactory(ctx context.Context, config ProcessConfig, factory ManagedProcessFactory) (*ManagedProcessClient, error) {
	return NewManagedProcessWithOptions(ctx, config, ManagedProcessOptions{Factory: factory})
}

// NewManagedProcessAsyncWithOptions is the nonblocking counterpart to
// NewManagedProcessWithOptions. It never reports a point-in-time factory
// error to the caller; the error is captured as a safe Snapshot reason while
// the supervisor continues its bounded policy.
func NewManagedProcessAsyncWithOptions(ctx context.Context, config ProcessConfig, options ManagedProcessOptions) *ManagedProcessClient {
	if ctx == nil {
		ctx = context.Background()
	}
	factory := defaultProcessFactory
	if options.Factory != nil {
		factory = func(factoryCtx context.Context, factoryConfig ProcessConfig) (managedProcess, error) {
			client, err := options.Factory(factoryCtx, factoryConfig)
			if err != nil {
				return nil, err
			}
			if client == nil {
				return nil, ErrUnavailable
			}
			managed, ok := client.(managedProcess)
			if !ok {
				_ = client.Close()
				return nil, ErrUnavailable
			}
			return managed, nil
		}
	}
	return newAutonomousManagedProcess(ctx, config, factory, options)
}

// NewManagedProcessWithOptions is the configurable constructor used by tests
// and by future embedding applications. It does not expose process handles or
// payloads through the resulting status API.
func NewManagedProcessWithOptions(ctx context.Context, config ProcessConfig, options ManagedProcessOptions) (*ManagedProcessClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	factory := defaultProcessFactory
	if options.Factory != nil {
		factory = func(factoryCtx context.Context, factoryConfig ProcessConfig) (managedProcess, error) {
			client, err := options.Factory(factoryCtx, factoryConfig)
			if err != nil {
				return nil, err
			}
			if client == nil {
				return nil, ErrUnavailable
			}
			managed, ok := client.(managedProcess)
			if !ok {
				_ = client.Close()
				return nil, ErrUnavailable
			}
			return managed, nil
		}
	}
	manager := newAutonomousManagedProcess(ctx, config, factory, options)
	startupTimeout := options.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = defaultStartupTimeout
	}
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	select {
	case err := <-manager.initialReady:
		if err != nil {
			_ = manager.Close()
			return nil, err
		}
		return manager, nil
	case <-startupCtx.Done():
		_ = manager.Close()
		return nil, startupCtx.Err()
	}
}

// newAutonomousManagedProcess creates a manager without waiting for initial
// availability. It is deliberately unexported so package tests can exercise
// starting/degraded states without depending on process startup timing.
func newAutonomousManagedProcess(ctx context.Context, config ProcessConfig, factory processFactory, options ManagedProcessOptions) *ManagedProcessClient {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	maxAttempts := options.MaxRestartAttempts
	if maxAttempts == 0 {
		maxAttempts = options.MaxRecoveryAttempts
	}
	if maxAttempts == 0 {
		maxAttempts = options.MaxRestarts
	}
	if maxAttempts == 0 {
		maxAttempts = options.RestartBudget
	}
	if maxAttempts == 0 {
		maxAttempts = defaultMaxRecoveryAttempts
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	manager := &ManagedProcessClient{
		ctx:                 runCtx,
		cancel:              cancel,
		config:              config,
		state:               ProcessStateStarting,
		supervise:           true,
		supervisorEvents:    make(chan supervisorEvent, 32),
		supervisorWake:      make(chan struct{}, 1),
		supervisorDone:      make(chan struct{}),
		initialReady:        make(chan error, 1),
		stateChanged:        make(chan struct{}),
		maxRecoveryAttempts: maxAttempts,
		backoff:             options.Backoff,
		now:                 options.Now,
		factory:             factory,
		logger:              logger,
		healthyInterval:     options.HealthyInterval,
	}
	if manager.now == nil {
		manager.now = options.Clock
	}
	if manager.backoff == nil {
		manager.backoff = defaultRecoveryBackoffFor
	}
	if manager.now == nil {
		manager.now = time.Now
	}
	if manager.healthyInterval == 0 {
		manager.healthyInterval = options.HealthInterval
	}
	if manager.healthyInterval == 0 {
		manager.healthyInterval = options.SustainedHealthyInterval
	}
	if manager.healthyInterval == 0 {
		manager.healthyInterval = defaultHealthyInterval
	}
	go manager.supervisorLoop()
	manager.signalSupervisor()
	return manager
}

// These unexported constructors intentionally accept the private factory
// type, making deterministic package tests concise without widening public
// capability surfaces.
func newManagedProcessWithFactory(ctx context.Context, config ProcessConfig, factory processFactory, options ...ManagedProcessOptions) *ManagedProcessClient {
	var opts ManagedProcessOptions
	if len(options) != 0 {
		opts = options[0]
	}
	return newAutonomousManagedProcess(ctx, config, factory, opts)
}

func newManagedProcessSupervisor(ctx context.Context, config ProcessConfig, factory processFactory) *ManagedProcessClient {
	return newManagedProcessWithFactory(ctx, config, factory)
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
	if m.supervise && m.state != ProcessStateAvailable {
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
		return boundedRecoveryBackoff(delay)
	}
	return defaultRecoveryBackoffFor(attempt)
}

func boundedRecoveryBackoff(delay time.Duration) time.Duration {
	if delay < 0 {
		return 0
	}
	if delay > maxRecoveryBackoff {
		return maxRecoveryBackoff
	}
	return delay
}

// Snapshot returns an immutable metadata-only status view.
func (m *ManagedProcessClient) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{State: ProcessStateClosed}
	}
	m.mu.Lock()
	state := m.state
	closed := m.closed
	client := m.client
	generation := m.generation
	pid := m.pid
	attempts := m.recoveryAttempts
	maxAttempts := m.maxRecoveryAttempts
	lastExitReason := m.lastExitReason
	lastFailureReason := m.lastFailureReason
	lastExitAt := m.lastExitAt
	lastFailureAt := m.lastFailureAt
	lastHealthyAt := m.lastHealthyAt
	healthySince := m.healthySince
	nextRecoveryAt := m.nextRecoveryAt
	m.mu.Unlock()
	if state == "" {
		if closed {
			state = ProcessStateClosed
		} else if client != nil && client.Available() {
			state = ProcessStateAvailable
		} else {
			state = ProcessStateDegraded
		}
	}
	remaining := maxAttempts - attempts
	if remaining < 0 {
		remaining = 0
	}
	return Snapshot{
		State:                  state,
		Generation:             generation,
		PID:                    pid,
		Attempts:               attempts,
		Attempt:                attempts,
		RestartAttempts:        attempts,
		MaxAttempts:            maxAttempts,
		RestartBudget:          remaining,
		MaxRestartBudget:       maxAttempts,
		RestartBudgetRemaining: remaining,
		LastExitReason:         lastExitReason,
		LastFailureReason:      lastFailureReason,
		LastExit:               lastExitReason,
		LastFailure:            lastFailureReason,
		Reason:                 snapshotReason(lastFailureReason, lastExitReason),
		LastExitAt:             lastExitAt,
		LastFailureAt:          lastFailureAt,
		LastHealthyAt:          lastHealthyAt,
		HealthySince:           healthySince,
		NextRetryAt:            nextRecoveryAt,
		NextRetry:              nextRecoveryAt,
	}
}

func (m *ManagedProcessClient) Report() Snapshot { return m.Snapshot() }

// Status, HealthSnapshot, and Diagnostics are descriptive read-only aliases
// for integrations that already use one of those status vocabularies.
func (m *ManagedProcessClient) Status() Snapshot           { return m.Snapshot() }
func (m *ManagedProcessClient) HealthSnapshot() Snapshot   { return m.Snapshot() }
func (m *ManagedProcessClient) Diagnostics() Snapshot      { return m.Snapshot() }
func (m *ManagedProcessClient) CurrentState() ProcessState { return m.Snapshot().State }

// Done closes when the autonomous supervisor loop has stopped. It is useful
// to lifecycle owners that need to await shutdown without exposing any child
// handle. A nil manager has no loop and returns nil.
func (m *ManagedProcessClient) Done() <-chan struct{} {
	if m == nil {
		return nil
	}
	return m.supervisorDone
}

func (m *ManagedProcessClient) SupervisorDone() <-chan struct{} { return m.Done() }

// StateChanges returns the current one-shot state-change channel. The channel
// is replaced under the manager lock whenever a transition is published, so
// callers can safely wait without polling and Close always unblocks a waiter.
func (m *ManagedProcessClient) StateChanges() <-chan struct{} {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.supervise || m.stateChanged == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return m.stateChanged
}

// Reporter returns the read-only reporting capability. The manager itself is
// safe to retain as the reporter because Snapshot always returns a detached
// value and never exposes internal handles.
func (m *ManagedProcessClient) Reporter() Reporter { return m }

func snapshotReason(failure, exit string) string {
	if failure != "" {
		return failure
	}
	return exit
}

func (m *ManagedProcessClient) signalSupervisor() {
	if m == nil || !m.supervise {
		return
	}
	select {
	case m.supervisorWake <- struct{}{}:
	default:
	}
}

func (m *ManagedProcessClient) broadcastStateLocked() {
	if !m.supervise {
		return
	}
	if m.stateChanged != nil {
		close(m.stateChanged)
	}
	m.stateChanged = make(chan struct{})
}

func (m *ManagedProcessClient) finishInitial(err error) {
	if m == nil || !m.supervise {
		return
	}
	m.mu.Lock()
	if m.initialReported {
		m.mu.Unlock()
		return
	}
	m.initialReported = true
	ready := m.initialReady
	m.mu.Unlock()
	if ready != nil {
		ready <- err
	}
}

func (m *ManagedProcessClient) supervisorLoop() {
	defer close(m.supervisorDone)
	for {
		delay := m.supervisorDelay()
		timer := time.NewTimer(delay)
		select {
		case <-m.ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case event := <-m.supervisorEvents:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			m.handleSupervisorEvent(event)
		case <-m.supervisorWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			m.supervisorStep()
		case <-timer.C:
			m.supervisorStep()
		}
	}
}

func (m *ManagedProcessClient) supervisorDelay() time.Duration {
	if m == nil {
		return time.Hour
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return time.Hour
	}
	if !m.initialAttempted && m.client == nil {
		m.mu.Unlock()
		return 0
	}
	nowFn := m.now
	recovery := m.recovery != nil
	nextRecoveryAt := m.nextRecoveryAt
	state := m.state
	healthySince := m.healthySince
	healthyInterval := m.healthyInterval
	m.mu.Unlock()
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	if recovery {
		if nextRecoveryAt.IsZero() || !now.Before(nextRecoveryAt) {
			return 0
		}
		return nextRecoveryAt.Sub(now)
	}
	if state == ProcessStateAvailable && !healthySince.IsZero() && healthyInterval > 0 {
		until := healthySince.Add(healthyInterval).Sub(now)
		if until <= 0 {
			return 0
		}
		return until
	}
	return time.Hour
}

func (m *ManagedProcessClient) supervisorStep() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	nowFn := m.now
	if nowFn == nil {
		nowFn = time.Now
	}
	m.mu.Unlock()
	now := nowFn()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if m.state == ProcessStateAvailable && !m.healthySince.IsZero() &&
		(m.healthyInterval <= 0 || !now.Before(m.healthySince.Add(m.healthyInterval))) {
		m.resetHealthyLocked(now)
		m.mu.Unlock()
		return
	}
	initial := !m.initialAttempted && m.client == nil
	if initial {
		attempt := m.recoveryAttempts + 1
		generation := m.generation
		pid := m.pid
		maxAttempts := m.maxRecoveryAttempts
		m.initialAttempted = true
		m.state = ProcessStateStarting
		m.broadcastStateLocked()
		factory := m.factoryFunc()
		parentCtx, config := m.ctx, m.config
		m.mu.Unlock()
		m.logLifecycle(lifecycleRestartAttempt, ProcessStateStarting, generation, pid, attempt, maxAttempts, "unknown", "initial")
		m.spawnCandidate(true, nil, nil, 0, factory, parentCtx, config)
		return
	}
	if m.recovery == nil || m.state == ProcessStateAvailable {
		m.mu.Unlock()
		return
	}
	if m.maxRecoveryAttempts <= 0 || m.recoveryAttempts >= m.maxRecoveryAttempts {
		pending := m.recovery
		m.state = ProcessStateExhausted
		m.nextRecoveryAt = time.Time{}
		m.broadcastStateLocked()
		m.recovery = nil
		generation := m.generation
		pid := m.pid
		attempts := m.recoveryAttempts
		maxAttempts := m.maxRecoveryAttempts
		reason := m.lastFailureReason
		if reason == "" {
			reason = "unavailable"
		}
		m.mu.Unlock()
		m.logLifecycle(lifecycleRestartExhausted, ProcessStateExhausted, generation, pid, attempts, maxAttempts, reason, "restart")
		m.finishRecovery(pending, nil, ErrUnavailable)
		return
	}
	if !m.nextRecoveryAt.IsZero() && now.Before(m.nextRecoveryAt) {
		m.mu.Unlock()
		return
	}
	attempt := m.recoveryAttempts + 1
	old := m.client
	oldGeneration := m.generation
	oldPID := m.pid
	maxAttempts := m.maxRecoveryAttempts
	reason := m.lastFailureReason
	factory := m.factoryFunc()
	parentCtx, config := m.ctx, m.config
	m.mu.Unlock()
	m.mu.Lock()
	if m.closed || m.recovery == nil || m.client != old || m.generation != oldGeneration ||
		(!m.nextRecoveryAt.IsZero() && now.Before(m.nextRecoveryAt)) {
		m.mu.Unlock()
		return
	}
	m.recoveryAttempts = attempt
	m.state = ProcessStateRecovering
	m.nextRecoveryAt = time.Time{}
	pending := m.recovery
	m.broadcastStateLocked()
	m.mu.Unlock()
	m.logLifecycle(lifecycleRestartAttempt, ProcessStateRecovering, oldGeneration, oldPID, attempt, maxAttempts, reason, "restart")
	m.spawnCandidate(false, pending, old, oldGeneration, factory, parentCtx, config)
}

func (m *ManagedProcessClient) handleSupervisorEvent(event supervisorEvent) {
	if m == nil || event.client == nil {
		return
	}
	m.mu.Lock()
	if m.closed || !m.supervise || m.client != event.client || m.generation != event.generation || m.state != ProcessStateAvailable {
		state := m.state
		attempt := m.recoveryAttempts + 1
		maxAttempts := m.maxRecoveryAttempts
		m.mu.Unlock()
		if event.lifecycle {
			m.logLifecycle(lifecycleChildExit, state, event.generation, event.pid, attempt, maxAttempts, safeProcessReason(event.err), "restart")
		}
		return
	}
	nowFn := m.now
	backoffFn := m.backoff
	attempt := m.recoveryAttempts + 1
	maxAttempts := m.maxRecoveryAttempts
	pid := event.pid
	if pid == 0 {
		pid = m.pid
	}
	reason := safeProcessReason(event.err)
	if m.recovery == nil {
		m.recovery = &managedRecovery{generation: event.generation, done: make(chan struct{})}
	}
	pending := m.recovery
	m.state = ProcessStateDegraded
	m.nextRecoveryAt = time.Time{}
	m.healthySince = time.Time{}
	m.lastExitReason = reason
	m.lastFailureReason = reason
	m.broadcastStateLocked()
	m.mu.Unlock()
	if event.lifecycle {
		m.logLifecycle(lifecycleChildExit, ProcessStateDegraded, event.generation, pid, attempt, maxAttempts, reason, "restart")
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	if backoffFn == nil {
		backoffFn = defaultRecoveryBackoffFor
	}
	now := nowFn()
	delay := backoffFn(attempt)
	delay = boundedRecoveryBackoff(delay)
	m.mu.Lock()
	if m.closed || !m.supervise || m.client != event.client || m.generation != event.generation {
		m.mu.Unlock()
		return
	}
	m.lastExitAt = now
	m.lastFailureAt = now
	if m.maxRecoveryAttempts <= 0 || m.recoveryAttempts >= m.maxRecoveryAttempts {
		m.recovery = nil
		m.state = ProcessStateExhausted
		m.nextRecoveryAt = time.Time{}
		m.broadcastStateLocked()
		exhaustedGeneration := m.generation
		exhaustedPID := m.pid
		exhaustedAttempts := m.recoveryAttempts
		m.mu.Unlock()
		m.logLifecycle(lifecycleRestartExhausted, ProcessStateExhausted, exhaustedGeneration, exhaustedPID, exhaustedAttempts, maxAttempts, reason, "restart")
		m.finishRecovery(pending, nil, event.err)
		return
	}
	m.nextRecoveryAt = now.Add(delay)
	m.broadcastStateLocked()
	m.mu.Unlock()
	// Do not close the child under the manager lock. The actual close happens
	// in the single supervisor owner immediately before replacement.
	m.signalSupervisor()
}

func (m *ManagedProcessClient) spawnCandidate(initial bool, pending *managedRecovery, old managedProcess, oldGeneration uint64, factory processFactory, parentCtx context.Context, config ProcessConfig) {
	if old != nil {
		_ = old.Close()
	}
	candidate, err := factory(parentCtx, config)
	if err == nil && candidate == nil {
		err = ErrUnavailable
	}
	if err == nil && candidate != nil {
		if !candidate.Available() || processDone(candidate) == nil {
			_ = candidate.Close()
			candidate = nil
			err = ErrUnavailable
		}
	}
	if err != nil && candidate != nil {
		_ = candidate.Close()
		candidate = nil
	}

	m.mu.Lock()
	if m.closed || !m.supervise || (!initial && (m.client != old || m.generation != oldGeneration)) || (initial && m.client != nil) {
		state := m.state
		generation := m.generation
		pid := m.pid
		attempt := m.recoveryAttempts
		if attempt == 0 {
			attempt = 1
		}
		maxAttempts := m.maxRecoveryAttempts
		m.mu.Unlock()
		m.logLifecycle(lifecycleRestartResult, state, generation, pid, attempt, maxAttempts, safeProcessReason(ErrUnavailable), lifecyclePhase(initial))
		if candidate != nil {
			_ = candidate.Close()
		}
		if pending != nil {
			m.finishRecovery(pending, nil, ErrUnavailable)
		}
		return
	}
	if err != nil || candidate == nil {
		reason := safeProcessReason(err)
		nowFn := m.now
		backoffFn := m.backoff
		attempt := m.recoveryAttempts + 1
		logAttempt := m.recoveryAttempts
		if logAttempt == 0 {
			logAttempt = 1
		}
		maxAttempts := m.maxRecoveryAttempts
		m.mu.Unlock()
		if nowFn == nil {
			nowFn = time.Now
		}
		if backoffFn == nil {
			backoffFn = defaultRecoveryBackoffFor
		}
		now := nowFn()
		delay := backoffFn(attempt)
		delay = boundedRecoveryBackoff(delay)
		m.mu.Lock()
		if m.closed || !m.supervise || (!initial && (m.client != old || m.generation != oldGeneration)) || (initial && m.client != nil) {
			state := m.state
			generation := m.generation
			pid := m.pid
			m.mu.Unlock()
			m.logLifecycle(lifecycleRestartResult, state, generation, pid, logAttempt, maxAttempts, reason, lifecyclePhase(initial))
			return
		}
		m.lastFailureReason = reason
		m.lastFailureAt = now
		if initial {
			m.state = ProcessStateDegraded
			m.nextRecoveryAt = now.Add(delay)
			m.recoveryAttempts = 1
			m.recovery = &managedRecovery{generation: m.generation, done: make(chan struct{})}
			m.broadcastStateLocked()
			generation := m.generation
			pid := m.pid
			m.mu.Unlock()
			m.logLifecycle(lifecycleRestartResult, ProcessStateDegraded, generation, pid, logAttempt, maxAttempts, reason, lifecyclePhase(initial))
			m.finishInitial(err)
			m.signalSupervisor()
			return
		}
		if m.recoveryAttempts >= m.maxRecoveryAttempts {
			m.state = ProcessStateExhausted
			m.nextRecoveryAt = time.Time{}
			m.broadcastStateLocked()
			m.recovery = nil
			generation := m.generation
			pid := m.pid
			exhaustedAttempts := m.recoveryAttempts
			m.mu.Unlock()
			m.logLifecycle(lifecycleRestartResult, ProcessStateExhausted, generation, pid, logAttempt, maxAttempts, reason, lifecyclePhase(initial))
			m.logLifecycle(lifecycleRestartExhausted, ProcessStateExhausted, generation, pid, exhaustedAttempts, maxAttempts, reason, lifecyclePhase(initial))
			m.finishRecovery(pending, nil, err)
			return
		}
		m.state = ProcessStateDegraded
		m.nextRecoveryAt = now.Add(delay)
		m.broadcastStateLocked()
		generation := m.generation
		pid := m.pid
		m.mu.Unlock()
		m.logLifecycle(lifecycleRestartResult, ProcessStateDegraded, generation, pid, logAttempt, maxAttempts, reason, lifecyclePhase(initial))
		m.signalSupervisor()
		return
	}

	// Install the child before invoking SetEventHandler, then invoke all child
	// callbacks outside the manager lock. A concurrent SetEventHandler call can
	// safely update this same candidate after installation.
	handler := m.handler
	nowFn := m.now
	oldClient := m.client
	oldGenerationNow := m.generation
	logAttempt := m.recoveryAttempts
	if logAttempt == 0 {
		logAttempt = 1
	}
	maxAttempts := m.maxRecoveryAttempts
	m.mu.Unlock()
	if nowFn == nil {
		nowFn = time.Now
	}
	lastHealthyAt := nowFn()
	candidatePID := processPID(candidate)
	m.mu.Lock()
	if m.closed || !m.supervise || (!initial && (m.client != oldClient || m.generation != oldGenerationNow)) || (initial && m.client != nil) {
		state := m.state
		generation := m.generation
		m.mu.Unlock()
		m.logLifecycle(lifecycleRestartResult, state, generation, candidatePID, logAttempt, maxAttempts, safeProcessReason(ErrUnavailable), lifecyclePhase(initial))
		_ = candidate.Close()
		if pending != nil {
			m.finishRecovery(pending, nil, ErrUnavailable)
		}
		return
	}
	m.client = candidate
	m.generation++
	m.pid = candidatePID
	m.state = ProcessStateAvailable
	m.nextRecoveryAt = time.Time{}
	m.lastHealthyAt = lastHealthyAt
	m.healthySince = m.lastHealthyAt
	m.broadcastStateLocked()
	newGeneration := m.generation
	m.mu.Unlock()
	m.logLifecycle(lifecycleRestartResult, ProcessStateAvailable, newGeneration, candidatePID, logAttempt, maxAttempts, "unknown", lifecyclePhase(initial))
	candidate.SetEventHandler(handler)
	m.watchChild(candidate, newGeneration)
	if initial {
		m.finishInitial(nil)
	}
	if pending != nil {
		m.finishRecovery(pending, candidate, nil)
	}
}

func (m *ManagedProcessClient) watchChild(client managedProcess, generation uint64) {
	done := processDone(client)
	if done == nil {
		return
	}
	go func() {
		select {
		case <-done:
			event := supervisorEvent{generation: generation, client: client, err: processExitReason(client), pid: processPID(client), lifecycle: true}
			select {
			case m.supervisorEvents <- event:
			case <-m.ctx.Done():
			}
		case <-m.ctx.Done():
		}
	}()
}

func processDone(client managedProcess) <-chan struct{} {
	if client == nil {
		return nil
	}
	if lifecycle, ok := client.(interface{ Done() <-chan struct{} }); ok {
		return lifecycle.Done()
	}
	if lifecycle, ok := client.(interface{ ExitSignal() <-chan struct{} }); ok {
		return lifecycle.ExitSignal()
	}
	if lifecycle, ok := client.(interface{ LifecycleDone() <-chan struct{} }); ok {
		return lifecycle.LifecycleDone()
	}
	return nil
}

func processExitReason(client managedProcess) error {
	if client == nil {
		return ErrUnavailable
	}
	if lifecycle, ok := client.(interface{ ExitReason() error }); ok {
		if err := lifecycle.ExitReason(); err != nil {
			return err
		}
	}
	if lifecycle, ok := client.(interface{ Failure() error }); ok {
		if err := lifecycle.Failure(); err != nil {
			return err
		}
	}
	return ErrProcessExited
}

func processPID(client managedProcess) int {
	if client == nil {
		return 0
	}
	if lifecycle, ok := client.(interface{ PID() int }); ok {
		return lifecycle.PID()
	}
	if lifecycle, ok := client.(interface{ ProcessID() int }); ok {
		return lifecycle.ProcessID()
	}
	return 0
}

func safeProcessReason(err error) string {
	switch {
	case err == nil:
		return "unknown"
	case errors.Is(err, ErrProcessExited), errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe):
		return "process_exited"
	case errors.Is(err, ErrProtocol):
		return "protocol_failure"
	case errors.Is(err, ErrLineTooLarge):
		return "line_too_large"
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "factory_failure"
	}
}

// requestFailure is the only recovery entry point for autonomous request
// paths. It records an event and never creates a child itself.
func (m *ManagedProcessClient) requestFailure(client managedProcess, err error) {
	if m == nil || !m.supervise || client == nil {
		return
	}
	m.mu.Lock()
	if m.closed || m.client != client || m.state != ProcessStateAvailable {
		m.mu.Unlock()
		return
	}
	generation := m.generation
	m.mu.Unlock()
	// Handle the transition synchronously. This is deliberately not a factory
	// call: the supervisor goroutine remains the sole child creation owner.
	// Coalescing here prevents a full event queue from dropping the first
	// generation failure and lets concurrent callers observe the degraded state.
	event := supervisorEvent{generation: generation, client: client, err: err, pid: processPID(client)}
	m.handleSupervisorEvent(event)
}

func (m *ManagedProcessClient) waitForClient(ctx context.Context, failed managedProcess) managedProcess {
	for {
		if ctx == nil {
			ctx = context.Background()
		}
		m.mu.Lock()
		if m.closed || m.state == ProcessStateExhausted || m.state == ProcessStateClosed {
			m.mu.Unlock()
			return nil
		}
		client := m.client
		state := m.state
		changed := m.stateChanged
		m.mu.Unlock()
		if state == ProcessStateAvailable && client != nil && client != failed && client.Available() {
			return client
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *ManagedProcessClient) waitForAvailable(ctx context.Context) managedProcess {
	for {
		if ctx == nil {
			ctx = context.Background()
		}
		m.mu.Lock()
		if m.closed || m.state == ProcessStateExhausted || m.state == ProcessStateClosed {
			m.mu.Unlock()
			return nil
		}
		client := m.client
		state := m.state
		changed := m.stateChanged
		m.mu.Unlock()
		if state == ProcessStateAvailable && client != nil && client.Available() {
			return client
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return nil
		}
	}
}

// withRetry performs one bounded idempotent retry on a retryable process
// failure. Autonomous instances wait for the supervisor's replacement; they
// never call the factory. Legacy hand-built managers retain the prior seam.
func (m *ManagedProcessClient) withRetry(ctx context.Context, call func(managedProcess) error) error {
	if m == nil {
		return ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.supervise {
		client := m.current()
		if client == nil {
			client = m.waitForAvailable(ctx)
			if client == nil {
				return ErrUnavailable
			}
		}
		err := call(client)
		if err == nil {
			m.markHealthy(client)
			return nil
		}
		if !retryableProcessError(err) || ctx.Err() != nil {
			return err
		}
		m.requestFailure(client, err)
		replacement := m.waitForClient(ctx, client)
		if replacement == nil {
			return err
		}
		err = call(replacement)
		if err == nil {
			m.markHealthy(replacement)
		} else if retryableProcessError(err) {
			m.requestFailure(replacement, err)
		}
		return err
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

// recover is the compatibility path for older hand-built managers. It is
// never used by autonomous instances and remains deliberately bounded.
func (m *ManagedProcessClient) recover(ctx context.Context, failed managedProcess, originalErr error, call func(managedProcess) error) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return originalErr
	}
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
		err := call(current)
		if err == nil {
			m.markHealthy(current)
		}
		return err
	}
	nowFn := m.now
	backoffFn := m.backoff
	if nowFn == nil {
		nowFn = time.Now
	}
	if backoffFn == nil {
		backoffFn = defaultRecoveryBackoffFor
	}
	attemptHint := m.recoveryAttempts + 1
	m.mu.Unlock()
	now := nowFn()
	// Compute policy callbacks outside the manager lock. Re-enter and
	// validate the same generation before publishing the attempt.
	delay := backoffFn(attemptHint)
	delay = boundedRecoveryBackoff(delay)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return originalErr
	}
	if m.client != failed {
		current := m.client
		pending := m.recovery
		m.mu.Unlock()
		if pending != nil {
			return m.waitForRecovery(ctx, pending, originalErr, call)
		}
		if current == nil || ctx.Err() != nil {
			return originalErr
		}
		err := call(current)
		if err == nil {
			m.markHealthy(current)
		}
		return err
	}
	// Another caller may have won the ownership race while this caller was
	// computing policy outside the lock. Join that recovery instead of
	// returning the first-generation error; all idempotent callers must reuse
	// the same pending replacement and observe its retry result.
	if pending := m.recovery; pending != nil {
		m.mu.Unlock()
		return m.waitForRecovery(ctx, pending, originalErr, call)
	}
	if m.recoveryAttempts != attemptHint-1 ||
		m.maxRecoveryAttempts <= 0 || m.recoveryAttempts >= m.maxRecoveryAttempts ||
		(!m.nextRecoveryAt.IsZero() && now.Before(m.nextRecoveryAt)) {
		m.mu.Unlock()
		return originalErr
	}
	m.recoveryAttempts = attemptHint
	m.nextRecoveryAt = now.Add(delay)
	pending := &managedRecovery{generation: m.generation, done: make(chan struct{})}
	m.recovery = pending
	factory := m.factoryFunc()
	parentCtx := m.ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	config := m.config
	old := current
	m.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	candidate, err := factory(parentCtx, config)
	if err == nil && candidate == nil {
		err = ErrUnavailable
	}
	if err != nil && candidate != nil {
		_ = candidate.Close()
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
		m.mu.Unlock()
		m.finishRecovery(pending, nil, err)
		return originalErr
	}
	handler := m.handler
	m.client = candidate
	m.generation++
	m.mu.Unlock()
	candidate.SetEventHandler(handler)
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
	if pending == nil {
		return
	}
	m.mu.Lock()
	if pending.doneClosed {
		m.mu.Unlock()
		return
	}
	pending.client = client
	pending.err = err
	pending.doneClosed = true
	if m.recovery == pending {
		m.recovery = nil
	}
	close(pending.done)
	m.broadcastStateLocked()
	m.mu.Unlock()
}

func (m *ManagedProcessClient) isCurrent(client managedProcess) bool {
	if m == nil || client == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.closed && m.client == client && (!m.supervise || m.state == ProcessStateAvailable)
}

// markHealthy is the explicit health observation point. Legacy managers keep
// their old immediate reset; autonomous managers reset only after a sustained
// healthy interval. A single successful RPC can therefore never replenish a
// restart budget.
func (m *ManagedProcessClient) markHealthy(client managedProcess) {
	if m == nil || client == nil {
		return
	}
	m.mu.Lock()
	if m.closed || m.client != client {
		m.mu.Unlock()
		return
	}
	nowFn := m.now
	supervise := m.supervise
	m.mu.Unlock()
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	m.mu.Lock()
	if m.closed || m.client != client {
		m.mu.Unlock()
		return
	}
	if !supervise {
		m.recoveryAttempts = 0
		m.nextRecoveryAt = time.Time{}
		m.lastHealthyAt = now
		m.mu.Unlock()
		return
	}
	if m.state == ProcessStateAvailable {
		healthyInterval := m.healthyInterval
		healthySince := m.healthySince
		if healthySince.IsZero() {
			healthySince = now
			m.healthySince = now
		}
		m.lastHealthyAt = now
		if healthyInterval <= 0 || !now.Before(healthySince.Add(healthyInterval)) {
			m.resetHealthyLocked(now)
		}
	}
	m.mu.Unlock()
}

func (m *ManagedProcessClient) resetHealthyLocked(now time.Time) {
	m.recoveryAttempts = 0
	m.nextRecoveryAt = time.Time{}
	m.lastHealthyAt = now
	if m.state == ProcessStateDegraded || m.state == ProcessStateRecovering {
		m.state = ProcessStateAvailable
	}
	m.broadcastStateLocked()
}

func (m *ManagedProcessClient) Available() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	if m.closed || (m.supervise && m.state != ProcessStateAvailable) {
		m.mu.Unlock()
		return false
	}
	client := m.client
	m.mu.Unlock()
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

// Close cancels the supervisor and closes the current child without waiting
// on a factory callback that may be blocked in an injected test seam. Any
// candidate returned after cancellation is closed by spawnCandidate and can
// never be installed. This ordering also avoids callback/manager deadlocks.
func (m *ManagedProcessClient) Close() error {
	if m == nil {
		return nil
	}
	if !m.supervise {
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
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return m.closeErr
	}
	m.closed = true
	m.state = ProcessStateClosed
	client := m.client
	m.client = nil
	pending := m.recovery
	m.recovery = nil
	cancel := m.cancel
	m.broadcastStateLocked()
	if pending != nil && !pending.doneClosed {
		pending.err = ErrUnavailable
		pending.doneClosed = true
		close(pending.done)
	}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var closeErr error
	if client != nil {
		closeErr = client.Close()
	}
	m.mu.Lock()
	m.closeErr = closeErr
	m.mu.Unlock()
	m.finishInitial(ErrUnavailable)
	m.signalSupervisor()
	return closeErr
}

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

var _ Client = (*ManagedProcessClient)(nil)
var _ Reporter = (*ManagedProcessClient)(nil)
var _ StateChangeReporter = (*ManagedProcessClient)(nil)
var _ ArchaeologyClient = (*ManagedProcessClient)(nil)
var _ ExperimentalArchaeologyClient = (*ManagedProcessClient)(nil)
var _ HistorianTaskFinder = (*ManagedProcessClient)(nil)
var _ HistorianTaskInventory = (*ManagedProcessClient)(nil)
var _ HistorianTaskRenamer = (*ManagedProcessClient)(nil)
