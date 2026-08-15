package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NotifierHealthSnapshot is the small, server-facing projection of the
// runtime-health snapshot used by the systemd notifier.  The runtime-health
// package owns the authoritative snapshot; server wiring should copy only
// these bounded facts into this projection.  In particular, do not pass
// errors, payloads, identifiers, or credentials to the notifier.
//
// Ready is the shared service-readiness result.  It must only become true
// after startup reconciliation and the database/core checks represented by
// the runtime-health snapshot have completed.
type NotifierHealthSnapshot struct {
	// Probed distinguishes the conservative pre-probe zero value from a
	// published observation whose database/core state is unhealthy.
	Probed          bool
	Ready           bool
	DatabaseHealthy bool
	CoreHealthy     bool
	CodexHealthy    bool
	RequiredCodex   bool
	// WatchdogEligible is the authoritative runtime-health decision for the
	// current snapshot. The notifier may override it only during an explicit,
	// bounded required-Codex recovery (see RequiredRecoveryActive).
	WatchdogEligible       bool
	RecoverySince          time.Time
	RequiredRecoveryActive bool
	RecoveryExhausted      bool
}

// ServiceHealthSnapshot is kept as an alias so server integration can use a
// descriptive name without coupling runtimehealth's concrete type to this
// package.
type ServiceHealthSnapshot = NotifierHealthSnapshot

var (
	// ErrNotifierDatabaseFailure indicates that the database/core health model
	// has become unsafe for watchdog supervision.  It is deliberately static;
	// callers should not add database error text to systemd status messages.
	ErrNotifierDatabaseFailure = errors.New("database health failed")
	// ErrNotifierCoreFailure indicates that the required Commons core is no
	// longer healthy enough for watchdog supervision.
	ErrNotifierCoreFailure = errors.New("core health failed")
	// ErrNotifierExhausted indicates that required Codex recovery exceeded its
	// bounded grace period.
	ErrNotifierExhausted = errors.New("required Codex recovery exhausted")
)

const (
	notifierStatusStarting   = "Starting"
	notifierStatusReady      = "Ready"
	notifierStatusRecovering = "Recovering"
	notifierStatusDegraded   = "Degraded"
	notifierStatusExhausted  = "Exhausted"
	notifierStatusStopping   = "Stopping"
)

type notifierState uint8

const (
	notifierStateStarting notifierState = iota
	notifierStateReady
	notifierStateRecovering
	notifierStateDegraded
	notifierStateExhausted
)

// notifierTicker is intentionally tiny so tests can inject a deterministic
// ticker without sleeping.  The production implementation wraps
// time.Ticker, and Run always stops it before returning.
type notifierTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type realNotifierTicker struct{ ticker *time.Ticker }

func (t *realNotifierTicker) Chan() <-chan time.Time { return t.ticker.C }
func (t *realNotifierTicker) Stop()                  { t.ticker.Stop() }

// serviceNotifierOptions controls notifier construction.  A notifier made
// with this constructor is not started until Run is called, which lets server
// integration own context cancellation and observe a fatal return.  Sender,
// Snapshot, NewTicker, and Now are all injectable for deterministic tests.
type serviceNotifierOptions struct {
	Context context.Context

	Sender    func(string) error
	Snapshot  func() NotifierHealthSnapshot
	NewTicker func(time.Duration) notifierTicker
	Now       func() time.Time

	WatchdogInterval time.Duration
	RecoveryGrace    time.Duration
	OnFatal          func(error)
}

type notifierFatalState struct {
	once   sync.Once
	mu     sync.RWMutex
	err    error
	closed bool
}

type serviceNotifier struct {
	conn    *net.UnixConn
	logger  *slog.Logger
	context context.Context

	// sender is nil only when systemd notification is unavailable.  Keeping a
	// sender separate from conn makes every state transition testable without a
	// real NOTIFY_SOCKET.
	sender    func(string) error
	snapshot  func() NotifierHealthSnapshot
	newTicker func(time.Duration) notifierTicker
	now       func() time.Time

	watchdogInterval time.Duration
	recoveryGrace    time.Duration
	onFatal          func(error)

	stop chan struct{}
	once sync.Once
	wg   sync.WaitGroup

	readyMu   sync.Mutex
	readySent bool
	statusMu  sync.Mutex
	lastState notifierState
	haveState bool

	// recoverySince is only used when the authoritative snapshot does not
	// provide RecoverySince.  It is set on the first ready snapshot that shows
	// a required Codex outage and reset after recovery.
	recoveryMu    sync.Mutex
	recoverySince time.Time

	fatal   notifierFatalState
	fatalCh chan error
}

// newServiceNotifier retains the old constructor's callback shape while
// accepting the new snapshot callback as well.  New server code should use
// newServiceNotifierWithOptions so context, sender, ticker, and clock are
// explicit.  The any parameter is intentional: it lets callers migrate from
// func() bool to func() NotifierHealthSnapshot without a flag day.
func newServiceNotifier(logger *slog.Logger, health any) *serviceNotifier {
	var snapshot func() NotifierHealthSnapshot
	switch value := health.(type) {
	case nil:
		snapshot = func() NotifierHealthSnapshot { return NotifierHealthSnapshot{} }
	case func() bool:
		snapshot = func() NotifierHealthSnapshot {
			healthy := value == nil || value()
			return NotifierHealthSnapshot{Probed: true, Ready: true, DatabaseHealthy: healthy, CoreHealthy: healthy, CodexHealthy: true, WatchdogEligible: healthy}
		}
	case func() NotifierHealthSnapshot:
		if value == nil {
			snapshot = func() NotifierHealthSnapshot { return NotifierHealthSnapshot{} }
		} else {
			snapshot = value
		}
	case NotifierHealthSnapshot:
		snapshot = func() NotifierHealthSnapshot { return value }
	default:
		// Keep the failure safe: an unknown callback cannot authorize READY or
		// watchdog pings.  Integration should use the typed snapshot callback.
		snapshot = func() NotifierHealthSnapshot { return NotifierHealthSnapshot{} }
	}

	options := serviceNotifierOptions{Snapshot: snapshot}
	if interval := legacyWatchdogInterval(); interval > 0 {
		options.WatchdogInterval = interval
	}
	n := newServiceNotifierWithOptions(logger, options)
	// Preserve the legacy constructor's behavior for deployments that have not
	// yet moved their server loop to Run.  close waits for this goroutine.
	n.start(context.Background())
	return n
}

func newServiceNotifierWithOptions(logger *slog.Logger, options serviceNotifierOptions) *serviceNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Snapshot == nil {
		options.Snapshot = func() NotifierHealthSnapshot { return NotifierHealthSnapshot{} }
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewTicker == nil {
		options.NewTicker = func(interval time.Duration) notifierTicker {
			return &realNotifierTicker{ticker: time.NewTicker(interval)}
		}
	}
	if options.WatchdogInterval <= 0 {
		options.WatchdogInterval = defaultNotifierWatchdogInterval
	}
	if options.RecoveryGrace <= 0 {
		options.RecoveryGrace = defaultNotifierRecoveryGrace
	}

	n := &serviceNotifier{
		logger:           logger,
		context:          options.Context,
		sender:           options.Sender,
		snapshot:         options.Snapshot,
		newTicker:        options.NewTicker,
		now:              options.Now,
		watchdogInterval: options.WatchdogInterval,
		recoveryGrace:    options.RecoveryGrace,
		onFatal:          options.OnFatal,
		stop:             make(chan struct{}),
		fatalCh:          make(chan error, 1),
		// A fresh notifier is in a startup state even before its first
		// snapshot.  This lets the first observed status be safe and stable.
		lastState: notifierStateStarting,
	}

	if n.sender == nil {
		n.sender = n.socketSender()
	}
	return n
}

const (
	defaultNotifierWatchdogInterval = 30 * time.Second
	defaultNotifierRecoveryGrace    = 30 * time.Second
)

// newServiceNotifierForTest is a readable alias for package-local tests and
// future server tests.  It deliberately has the same options contract as the
// production constructor.
func newServiceNotifierForTest(logger *slog.Logger, options serviceNotifierOptions) *serviceNotifier {
	return newServiceNotifierWithOptions(logger, options)
}

func (n *serviceNotifier) socketSender() func(string) error {
	name := os.Getenv("NOTIFY_SOCKET")
	if name == "" {
		return nil
	}
	if strings.HasPrefix(name, "@") {
		name = "\x00" + name[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		n.logger.Warn("systemd notify unavailable", "error", err)
		return nil
	}
	n.conn = conn
	return func(state string) error {
		_, err := conn.Write([]byte(state))
		return err
	}
}

func (n *serviceNotifier) send(state string) {
	if n.sender == nil {
		return
	}
	if err := n.sender(state); err != nil {
		n.logger.Warn("systemd notify failed", "state", sanitizeNotifyState(state), "error", err)
	}
}

// sanitizeNotifyState is only used as a bounded log field.  Dynamic status
// text is never generated by this package, but the guard prevents accidental
// future payloads from putting line breaks or arbitrary bytes in logs.
func sanitizeNotifyState(state string) string {
	const maximum = 128
	state = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\x00' {
			return ' '
		}
		return r
	}, state)
	if len(state) > maximum {
		return state[:maximum]
	}
	return state
}

// ready observes the current shared snapshot.  It intentionally does not
// force readiness: callers cannot turn a post-Listen invocation into READY=1
// unless the snapshot has already declared the service ready.
func (n *serviceNotifier) ready() {
	_ = n.observeAt(n.now(), false)
}

// Run owns one cancellable notification loop.  It returns a static fatal
// error when the health model is exhausted, or the context error on
// cancellation.  The caller may also observe Fatal() for integrations that
// need a separate signal channel.
func (n *serviceNotifier) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = n.context
		if ctx == nil {
			ctx = context.Background()
		}
	}
	return n.run(ctx, n.newTicker(n.watchdogInterval))
}

func (n *serviceNotifier) start(ctx context.Context) {
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		if err := n.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			n.logger.Warn("systemd notifier stopped", "error", err)
		}
	}()
}

func (n *serviceNotifier) run(ctx context.Context, ticker notifierTicker) error {
	if ticker == nil {
		return errors.New("notifier ticker is nil")
	}
	defer ticker.Stop()

	// Emit a safe startup status once, before the first tick.  This is not a
	// readiness signal and therefore cannot make systemd consider the service
	// ready early.
	n.setStatus(notifierStateStarting)
	if err := n.observeAt(n.now(), false); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-n.stop:
			return nil
		case tick, ok := <-ticker.Chan():
			if !ok {
				return nil
			}
			now := tick
			if now.IsZero() {
				now = n.now()
			}
			if err := n.observe(now); err != nil {
				return err
			}
		}
	}
}

// watchdog remains as a compatibility entrypoint for old package-local
// callers.  New code should call Run so it can own context cancellation and
// observe the returned exhaustion error.
func (n *serviceNotifier) watchdog(interval time.Duration) {
	if interval <= 0 {
		interval = n.watchdogInterval
	}
	_ = n.run(context.Background(), n.newTicker(interval))
}

func (n *serviceNotifier) observe(now time.Time) error {
	return n.observeAt(now, true)
}

func (n *serviceNotifier) observeAt(now time.Time, ping bool) error {
	snapshot := n.snapshot()
	state, status, fatal := n.classify(snapshot, now)
	readyAuthorized := snapshot.Probed && snapshot.Ready && snapshot.DatabaseHealthy && snapshot.CoreHealthy && n.requiredCodexWithinGrace(snapshot, now)
	// READY is an edge-triggered signal.  Optional Codex degradation and
	// bounded required-Codex recovery still authorize service readiness when
	// the shared snapshot says the core is ready.
	if readyAuthorized {
		n.setState(state, n.readyAlreadySent())
		n.emitReady(status)
	} else {
		n.setState(state, true)
	}
	if fatal != nil {
		n.signalFatal(fatal)
		return fatal
	}
	if ping && (state == notifierStateReady || state == notifierStateRecovering || state == notifierStateDegraded) {
		withinGrace := snapshot.RequiredCodex && !snapshot.CodexHealthy && n.requiredCodexWithinGrace(snapshot, now)
		watchdogAllowed := snapshot.WatchdogEligible || withinGrace
		if snapshot.Probed && snapshot.DatabaseHealthy && snapshot.CoreHealthy && watchdogAllowed {
			// The model's explicit watchdog decision is authoritative. The only
			// allowed override is bounded, active required-Codex recovery.
			if n.lastPingAllowed(now) {
				n.send("WATCHDOG=1")
			}
		}
	}
	return nil
}

func (n *serviceNotifier) readyAlreadySent() bool {
	n.readyMu.Lock()
	defer n.readyMu.Unlock()
	return n.readySent
}

func (n *serviceNotifier) emitReady(status string) {
	n.readyMu.Lock()
	if n.readySent {
		n.readyMu.Unlock()
		return
	}
	n.readySent = true
	n.readyMu.Unlock()
	n.send("READY=1\nSTATUS=" + status)
}

// classify returns a static state/status and, when applicable, an observable
// fatal signal.  It does not include health-provider errors or payloads.
func (n *serviceNotifier) classify(snapshot NotifierHealthSnapshot, now time.Time) (notifierState, string, error) {
	if !snapshot.Probed {
		return notifierStateStarting, notifierStatusStarting, nil
	}
	if snapshot.RequiredCodex && snapshot.RecoveryExhausted {
		return notifierStateExhausted, notifierStatusExhausted, ErrNotifierExhausted
	}
	if !snapshot.DatabaseHealthy {
		return notifierStateDegraded, notifierStatusDegraded, ErrNotifierDatabaseFailure
	}
	if !snapshot.CoreHealthy {
		return notifierStateDegraded, notifierStatusDegraded, ErrNotifierCoreFailure
	}

	if snapshot.RequiredCodex && !snapshot.CodexHealthy {
		// A required supervisor that has not yet reached service readiness is
		// still starting unless the shared snapshot explicitly marks recovery or
		// exhaustion. This prevents a transient initial unavailable state from
		// being mistaken for a post-ready recovery.
		if !snapshot.Ready && !snapshot.RequiredRecoveryActive && snapshot.RecoverySince.IsZero() {
			return notifierStateStarting, notifierStatusStarting, nil
		}
		if !n.requiredCodexWithinGrace(snapshot, now) {
			return notifierStateExhausted, notifierStatusExhausted, ErrNotifierExhausted
		}
		return notifierStateRecovering, notifierStatusRecovering, nil
	}
	if !snapshot.CodexHealthy {
		// Optional Codex degradation does not become fatal; once the shared
		// snapshot is ready, it leaves service readiness and watchdog policy to
		// the database/core/model facts.
		return notifierStateDegraded, notifierStatusDegraded, nil
	}
	if !snapshot.Ready {
		// A healthy probe is not the same as service readiness. Keep the
		// notifier in a safe startup state until the shared model authorizes
		// readiness, even when no component is currently failing.
		return notifierStateStarting, notifierStatusStarting, nil
	}
	return notifierStateReady, notifierStatusReady, nil
}

func (n *serviceNotifier) requiredCodexWithinGrace(snapshot NotifierHealthSnapshot, now time.Time) bool {
	if !snapshot.RequiredCodex || snapshot.CodexHealthy {
		n.recoveryMu.Lock()
		n.recoverySince = time.Time{}
		n.recoveryMu.Unlock()
		return true
	}
	if !snapshot.RequiredRecoveryActive && snapshot.RecoverySince.IsZero() {
		return false
	}
	since := snapshot.RecoverySince
	if since.IsZero() {
		n.recoveryMu.Lock()
		if n.recoverySince.IsZero() {
			n.recoverySince = now
		}
		since = n.recoverySince
		n.recoveryMu.Unlock()
	}
	if now.Before(since) {
		return true
	}
	return now.Sub(since) < n.recoveryGrace
}

// lastPingAllowed is intentionally a hook point for tests and future
// rate/transition policy.  The snapshot gating in observe is authoritative.
func (n *serviceNotifier) lastPingAllowed(time.Time) bool { return true }

func (n *serviceNotifier) setStatus(state notifierState) {
	n.setState(state, true)
}

func (n *serviceNotifier) setState(state notifierState, emitStatus bool) {
	n.statusMu.Lock()
	defer n.statusMu.Unlock()
	if n.haveState && n.lastState == state {
		return
	}
	n.lastState = state
	n.haveState = true
	if !emitStatus {
		return
	}
	status := notifierStatusStarting
	switch state {
	case notifierStateReady:
		status = notifierStatusReady
	case notifierStateRecovering:
		status = notifierStatusRecovering
	case notifierStateDegraded:
		status = notifierStatusDegraded
	case notifierStateExhausted:
		status = notifierStatusExhausted
	}
	n.send("STATUS=" + status)
}

func (n *serviceNotifier) signalFatal(err error) {
	if err == nil {
		return
	}
	n.fatal.once.Do(func() {
		n.fatal.mu.Lock()
		n.fatal.err = err
		closed := n.fatal.closed
		if !closed {
			select {
			case n.fatalCh <- err:
			default:
			}
		}
		n.fatal.mu.Unlock()
		if n.onFatal != nil {
			n.onFatal(err)
		}
	})
}

// Fatal returns a buffered channel that receives at most one static fatal
// error.  It is closed by close; callers can select it alongside Run.
func (n *serviceNotifier) Fatal() <-chan error { return n.fatalCh }

// FatalError returns the first fatal error observed, if any.
func (n *serviceNotifier) FatalError() error {
	n.fatal.mu.RLock()
	defer n.fatal.mu.RUnlock()
	return n.fatal.err
}

func (n *serviceNotifier) close() {
	n.once.Do(func() {
		close(n.stop)
		n.wg.Wait()
		n.send("STOPPING=1\nSTATUS=" + notifierStatusStopping)
		if n.conn != nil {
			_ = n.conn.Close()
		}
		n.fatal.mu.Lock()
		if !n.fatal.closed {
			close(n.fatalCh)
			n.fatal.closed = true
		}
		n.fatal.mu.Unlock()
	})
}

// String is useful in diagnostics while remaining payload-free.
func (s NotifierHealthSnapshot) String() string {
	return fmt.Sprintf("ready=%t database=%t core=%t codex=%t required=%t", s.Ready, s.DatabaseHealthy, s.CoreHealthy, s.CodexHealthy, s.RequiredCodex)
}

// legacyWatchdogInterval reads the systemd watchdog interval while keeping
// environment parsing bounded and side-effect free.  It is used by the
// server integration when constructing options; the notifier itself never
// reads payloads from the environment.
func legacyWatchdogInterval() time.Duration {
	usec, err := strconv.ParseInt(os.Getenv("WATCHDOG_USEC"), 10, 64)
	if err != nil || usec <= 0 {
		return 0
	}
	return time.Duration(usec) * time.Microsecond / 2
}
