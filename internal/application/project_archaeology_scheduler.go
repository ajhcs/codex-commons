package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"codex-commons/internal/domain"
)

type ArchaeologyNativeRepository interface {
	ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error)
	QueueArchaeologyNativeBatch(context.Context, domain.ArchaeologyNativeBatchRequest) (domain.ArchaeologySession, error)
	ClaimArchaeologyNativeJob(context.Context) (domain.ArchaeologyNativeJob, error)
	BindArchaeologyNativeJob(context.Context, string, string, string, string) error
	BindArchaeologyNativeIdentity(context.Context, string, string, string, string) error
	ActivateArchaeologyNativeJob(context.Context, string, string, string) error
	FailArchaeologyNativeStart(context.Context, string, domain.ArchaeologyLaunchResult, bool) error
	UpdateArchaeologyNativeProgress(context.Context, domain.ArchaeologyNativeProgress) error
	ReportArchaeologyNativeJob(context.Context, domain.ArchaeologyNativeReport) error
	CompleteArchaeologyNativeTurn(context.Context, domain.ArchaeologyNativeTerminal) error
	CancelArchaeologyNativeBatch(context.Context, string, string, int64) ([]domain.ArchaeologyNativeJob, domain.ArchaeologySession, error)
	LoseArchaeologyNativeTurn(context.Context, string, string, string) error
}

type ArchaeologyNativeToolCall struct {
	ThreadID, TurnID, Tool string
	Arguments              []byte
}
type ArchaeologyNativeToolResponse struct {
	Success bool
	Message string
}
type ArchaeologyNativeLauncher interface {
	Available(context.Context) error
	// LaunchNative is the single non-idempotent acceptance boundary. A
	// response with State=uncertain or any partial Codex identity means the
	// request may have been accepted and must be fail-started as uncertain;
	// pre-call failures may return an empty, certain result.
	LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error)
	InterruptNative(context.Context, domain.ArchaeologyNativeJob) error
	FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error
}
type ArchaeologyNativeResolutionRepository interface {
	ResolveArchaeologyNativeUncertainty(context.Context, domain.ArchaeologyNativeResolution) (domain.ArchaeologySession, error)
}
type ArchaeologyNativeIdentityRepository interface {
	BindArchaeologyNativeUncertainty(context.Context, string, domain.ArchaeologyLaunchResult) error
}
type ArchaeologyNativeIdentityReconciler interface {
	RecoverNativeIdentity(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate) (domain.ArchaeologyLaunchResult, bool, error)
}

// ArchaeologyNativePersistenceReconciler is the explicit, verified recovery
// seam for a scheduler persistence fault. A successful call must establish
// that the repository is readable and that any post-claim state is safe to
// resume. It intentionally does not prescribe a durable retry queue.
type ArchaeologyNativePersistenceReconciler interface {
	ReconcileArchaeologyNativePersistence(context.Context) error
}

// ArchaeologyNativePersistenceDriver is the optional durable write capability
// used by the scheduler when the repository owns the native persistence
// ledger.  Repositories that do not implement it retain the historical direct
// mutation path.  RetryDue is variadic to match the concrete Store API while
// still allowing the scheduler to pass one bounded row limit.
type ArchaeologyNativePersistenceDriver interface {
	EnsureArchaeologyNativePersistenceIntent(context.Context, domain.ArchaeologyNativePersistenceIntent) (domain.ArchaeologyNativePersistenceIntentRecord, error)
	ApplyArchaeologyNativePersistenceIntent(context.Context, string) error
	ArchaeologyNativePersistenceIntent(context.Context, string) (domain.ArchaeologyNativePersistenceIntentRecord, error)
	RetryDueArchaeologyNativePersistence(context.Context, ...int) (domain.ArchaeologyNativePersistenceRetryReport, error)
	ArchaeologyNativePersistenceStatus(context.Context) (domain.ArchaeologyNativePersistenceStatus, error)
}

// A few application fakes use the natural fixed-limit method shape. Keep the
// Store-compatible variadic interface above as the public capability while
// accepting that equivalent test/integration shape at the boundary.
type archaeologyNativeFixedLimitPersistenceDriver interface {
	EnsureArchaeologyNativePersistenceIntent(context.Context, domain.ArchaeologyNativePersistenceIntent) (domain.ArchaeologyNativePersistenceIntentRecord, error)
	ApplyArchaeologyNativePersistenceIntent(context.Context, string) error
	ArchaeologyNativePersistenceIntent(context.Context, string) (domain.ArchaeologyNativePersistenceIntentRecord, error)
	RetryDueArchaeologyNativePersistence(context.Context, int) (domain.ArchaeologyNativePersistenceRetryReport, error)
	ArchaeologyNativePersistenceStatus(context.Context) (domain.ArchaeologyNativePersistenceStatus, error)
}

type archaeologyNativeFixedLimitPersistenceAdapter struct {
	driver archaeologyNativeFixedLimitPersistenceDriver
}

func (a archaeologyNativeFixedLimitPersistenceAdapter) EnsureArchaeologyNativePersistenceIntent(ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent) (domain.ArchaeologyNativePersistenceIntentRecord, error) {
	return a.driver.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
}

func (a archaeologyNativeFixedLimitPersistenceAdapter) ApplyArchaeologyNativePersistenceIntent(ctx context.Context, id string) error {
	return a.driver.ApplyArchaeologyNativePersistenceIntent(ctx, id)
}

func (a archaeologyNativeFixedLimitPersistenceAdapter) ArchaeologyNativePersistenceIntent(ctx context.Context, id string) (domain.ArchaeologyNativePersistenceIntentRecord, error) {
	return a.driver.ArchaeologyNativePersistenceIntent(ctx, id)
}

func (a archaeologyNativeFixedLimitPersistenceAdapter) RetryDueArchaeologyNativePersistence(ctx context.Context, limits ...int) (domain.ArchaeologyNativePersistenceRetryReport, error) {
	limit := archaeologySchedulerPersistenceRetryLimit
	if len(limits) != 0 && limits[0] > 0 {
		limit = limits[0]
	}
	return a.driver.RetryDueArchaeologyNativePersistence(ctx, limit)
}

func (a archaeologyNativeFixedLimitPersistenceAdapter) ArchaeologyNativePersistenceStatus(ctx context.Context) (domain.ArchaeologyNativePersistenceStatus, error) {
	return a.driver.ArchaeologyNativePersistenceStatus(ctx)
}

// ErrArchaeologySchedulerPersistenceFault is deliberately generic. The
// scheduler retains the underlying error for process-local diagnostics but
// never exposes database or transport details as a public status value.
var ErrArchaeologySchedulerPersistenceFault = errors.New("archaeology scheduler persistence fault")

type ArchaeologySchedulerStatus struct {
	PersistenceFault     bool   `json:"persistence_fault"`
	PersistenceAttention bool   `json:"persistence_attention,omitempty"`
	Error                string `json:"error,omitempty"`
}

type ArchaeologyScheduler struct {
	service       *Service
	repository    ArchaeologyNativeRepository
	launcher      ArchaeologyNativeLauncher
	principal     string
	ctx           context.Context
	cancel        context.CancelFunc
	wake          chan struct{}
	wg            sync.WaitGroup
	callbackMu    sync.Mutex
	callbackWG    sync.WaitGroup
	drainMu       sync.Mutex
	stateMu       sync.RWMutex
	persistenceMu sync.Mutex
	// persistenceFault is process-local by design. A restart must run the
	// existing durable reconciliation before constructing a new scheduler.
	persistenceFault        error
	persistenceSeq          uint64
	persistenceBacklog      bool
	persistenceManualFault  bool
	closing                 bool
	tickerFactory           archaeologySchedulerTickerFactory
	terminalMu              sync.Mutex
	terminalCallbacks       int
	persistenceInFlight     int
	callbackTimeout         time.Duration
	callbackContextFactory  func() (context.Context, context.CancelFunc)
	claimContextFactory     func() (context.Context, context.CancelFunc)
	terminalAdmissionHook   func()
	terminalBoundHook       func()
	interruptMu             sync.Mutex
	interruptedJobs         map[string]struct{}
	interruptContextFactory func() (context.Context, context.CancelFunc)
	pendingIntents          []domain.ArchaeologyNativePersistenceIntent
	pendingIntentKeys       map[string]struct{}
}

func newArchaeologyScheduler(parent context.Context, service *Service, repository ArchaeologyNativeRepository, launcher ArchaeologyNativeLauncher, principal string) *ArchaeologyScheduler {
	return newArchaeologySchedulerWithTicker(parent, service, repository, launcher, principal, defaultArchaeologySchedulerTicker)
}

type archaeologySchedulerTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type archaeologySchedulerTickerFactory func(time.Duration) archaeologySchedulerTicker

type wallClockArchaeologySchedulerTicker struct {
	ticker *time.Ticker
}

func (t *wallClockArchaeologySchedulerTicker) Chan() <-chan time.Time { return t.ticker.C }
func (t *wallClockArchaeologySchedulerTicker) Stop()                  { t.ticker.Stop() }

const archaeologySchedulerTickInterval = time.Second

func defaultArchaeologySchedulerTicker(interval time.Duration) archaeologySchedulerTicker {
	return &wallClockArchaeologySchedulerTicker{ticker: time.NewTicker(interval)}
}

// newArchaeologySchedulerWithTicker keeps the production constructor small
// while giving focused tests a deterministic wake seam.  The ticker is
// process-local; durable retry timing remains owned by the Store ledger.
func newArchaeologySchedulerWithTicker(parent context.Context, service *Service, repository ArchaeologyNativeRepository, launcher ArchaeologyNativeLauncher, principal string, tickerFactory archaeologySchedulerTickerFactory) *ArchaeologyScheduler {
	ctx, cancel := context.WithCancel(parent)
	if tickerFactory == nil {
		tickerFactory = defaultArchaeologySchedulerTicker
	}
	s := &ArchaeologyScheduler{service: service, repository: repository, launcher: launcher, principal: principal, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), tickerFactory: tickerFactory}
	s.wg.Add(1)
	go s.loop()
	return s
}

func (s *ArchaeologyScheduler) persistenceBlocked() bool {
	if s == nil {
		return true
	}
	s.stateMu.RLock()
	blocked := s.persistenceFault != nil
	s.stateMu.RUnlock()
	return blocked
}

// recordPersistenceFailure latches the first post-claim repository failure.
// The underlying error is retained only in memory for diagnostics; callers
// observe ErrArchaeologySchedulerPersistenceFault instead.
func (s *ArchaeologyScheduler) recordPersistenceFailure(err error) error {
	return s.recordPersistenceFailureSource(err, true, false)
}

func (s *ArchaeologyScheduler) recordPersistenceFailureMode(err error, wake bool) error {
	return s.recordPersistenceFailureSource(err, wake, true)
}

func (s *ArchaeologyScheduler) recordPersistenceFailureSource(err error, wake, durable bool) error {
	if s == nil || err == nil {
		return err
	}
	s.stateMu.Lock()
	if s.persistenceFault == nil {
		s.persistenceFault = err
	}
	if !durable {
		s.persistenceManualFault = true
	}
	s.persistenceSeq++
	s.stateMu.Unlock()
	// A durable failure must prompt a retry even if no caller happens to issue
	// another availability wake. Wake is coalescing and harmless after Close.
	if wake {
		s.Wake()
	}
	return err
}

func (s *ArchaeologyScheduler) PersistenceError() error {
	if s == nil {
		return ErrArchaeologySchedulerPersistenceFault
	}
	s.stateMu.RLock()
	faulted := s.persistenceFault != nil
	s.stateMu.RUnlock()
	if faulted {
		return ErrArchaeologySchedulerPersistenceFault
	}
	return nil
}

// PersistenceFault is an alias-shaped status accessor for callers that want
// to treat the latch as an error rather than inspect Status.
func (s *ArchaeologyScheduler) PersistenceFault() error {
	return s.PersistenceError()
}

func (s *ArchaeologyScheduler) Status() ArchaeologySchedulerStatus {
	if s == nil {
		return ArchaeologySchedulerStatus{PersistenceFault: true, Error: ErrArchaeologySchedulerPersistenceFault.Error()}
	}
	s.stateMu.RLock()
	fault := s.persistenceFault != nil
	attention := s.persistenceBacklog
	s.stateMu.RUnlock()
	status := ArchaeologySchedulerStatus{PersistenceAttention: attention}
	if fault {
		status.PersistenceFault = true
		status.Error = ErrArchaeologySchedulerPersistenceFault.Error()
	}
	return status
}

// ClearPersistenceFault clears the in-memory latch only after the caller's
// verified reconciliation function succeeds. A bare reset is intentionally
// unavailable: the scheduler must not resume claiming against an unknown
// repository state.
func (s *ArchaeologyScheduler) ClearPersistenceFault(ctx context.Context, verify func(context.Context) error) error {
	if s == nil || ctx == nil || verify == nil {
		return domain.ErrInvalid
	}
	s.stateMu.RLock()
	sequence := s.persistenceSeq
	s.stateMu.RUnlock()
	if err := verify(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if s.terminalCallbacks != 0 || s.persistenceInFlight != 0 {
		return ErrArchaeologySchedulerPersistenceFault
	}
	s.persistenceMu.Lock()
	defer s.persistenceMu.Unlock()
	if len(s.pendingIntents) != 0 {
		return ErrArchaeologySchedulerPersistenceFault
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.persistenceSeq != sequence {
		return ErrArchaeologySchedulerPersistenceFault
	}
	s.persistenceFault = nil
	s.persistenceBacklog = false
	s.persistenceManualFault = false
	s.Wake()
	return nil
}

// ReconcilePersistence is the repository-backed clearing seam. Repositories
// that can perform a verified, read-only reconciliation may opt into
// ArchaeologyNativePersistenceReconciler; absent that capability, the safe
// result is ErrUnavailable and the latch remains set.
func (s *ArchaeologyScheduler) ReconcilePersistence(ctx context.Context) error {
	if s == nil || s.repository == nil {
		return domain.ErrUnavailable
	}
	reconciler, ok := s.repository.(ArchaeologyNativePersistenceReconciler)
	if !ok {
		return domain.ErrUnavailable
	}
	return s.ClearPersistenceFault(ctx, reconciler.ReconcileArchaeologyNativePersistence)
}

func (s *ArchaeologyScheduler) Available(ctx context.Context) error {
	if s == nil || s.launcher == nil {
		return domain.ErrUnavailable
	}
	if s.persistenceBlocked() {
		return ErrArchaeologySchedulerPersistenceFault
	}
	return s.launcher.Available(ctx)
}
func (s *ArchaeologyScheduler) Launch(context.Context, domain.ArchaeologySession) error {
	return domain.ErrUnavailable
}

// Wake is the bounded availability/recovery signal. Callers may invoke it
// from a Codex process-recovery callback or after queueing a batch; it never
// blocks and never performs a claim itself.
func (s *ArchaeologyScheduler) Wake() {
	if s == nil || s.wake == nil {
		return
	}
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *ArchaeologyScheduler) Close() {
	if s == nil {
		return
	}
	// Close wins the callback registration race before cancelling the main
	// scheduler. Accepted callbacks are counted before this gate is closed and
	// are therefore included in the wait below.
	s.callbackMu.Lock()
	s.closing = true
	s.callbackMu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.callbackWG.Wait()
}
func (s *ArchaeologyScheduler) beginCallback() bool {
	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	if s.closing {
		return false
	}
	s.callbackWG.Add(1)
	return true
}

// beginTerminalCallback admits a terminal callback and publishes its
// outstanding count while holding the same gate used immediately before a
// repository claim. Close's callbackMu ordering still guarantees Add happens
// before callbackWG.Wait can begin.
func (s *ArchaeologyScheduler) beginTerminalCallback() bool {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	s.callbackMu.Lock()
	if s.closing {
		s.callbackMu.Unlock()
		return false
	}
	s.callbackWG.Add(1)
	s.callbackMu.Unlock()
	// Test-only scheduling seam: keep the terminal gate held while admission is
	// observable, so a concurrent drain cannot pass the claim check before the
	// count is published.
	if s.terminalAdmissionHook != nil {
		s.terminalAdmissionHook()
	}
	s.terminalCallbacks++
	return true
}

// beginPersistenceFlight publishes an in-process persistence operation to the
// claim gate without holding terminalMu during driver I/O.  Claim takes
// terminalMu before deciding whether it may call the repository, so a flight
// that starts before that decision always wins the race.
func (s *ArchaeologyScheduler) beginPersistenceFlight() {
	s.terminalMu.Lock()
	s.persistenceInFlight++
	s.terminalMu.Unlock()
}

func (s *ArchaeologyScheduler) endPersistenceFlight() {
	s.terminalMu.Lock()
	if s.persistenceInFlight > 0 {
		s.persistenceInFlight--
	}
	s.terminalMu.Unlock()
}

func (s *ArchaeologyScheduler) isClosing() bool {
	if s == nil {
		return true
	}
	s.callbackMu.Lock()
	closing := s.closing
	s.callbackMu.Unlock()
	return closing
}

func (s *ArchaeologyScheduler) loop() {
	defer s.wg.Done()
	tickerFactory := s.tickerFactory
	if tickerFactory == nil {
		tickerFactory = defaultArchaeologySchedulerTicker
	}
	ticker := tickerFactory(archaeologySchedulerTickInterval)
	if ticker != nil {
		defer ticker.Stop()
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
			s.drain()
		case <-tickerChan(ticker):
			s.drain()
		}
	}
}

func tickerChan(ticker archaeologySchedulerTicker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.Chan()
}

const archaeologySchedulerPersistenceRetryLimit = 32
const archaeologySchedulerPendingTerminalDrainLimit = 8
const archaeologySchedulerCallbackTimeout = 5 * time.Second
const archaeologySchedulerClaimTimeout = 5 * time.Second

func (s *ArchaeologyScheduler) persistenceDriver() (ArchaeologyNativePersistenceDriver, bool) {
	if s == nil || s.repository == nil {
		return nil, false
	}
	driver, ok := s.repository.(ArchaeologyNativePersistenceDriver)
	if ok && driver != nil {
		return driver, true
	}
	fixed, fixedOK := s.repository.(archaeologyNativeFixedLimitPersistenceDriver)
	if fixedOK && fixed != nil {
		return archaeologyNativeFixedLimitPersistenceAdapter{driver: fixed}, true
	}
	return nil, false
}

func (s *ArchaeologyScheduler) schedulerContext() context.Context {
	if s == nil || s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *ArchaeologyScheduler) claimContext() (context.Context, context.CancelFunc) {
	if s != nil && s.claimContextFactory != nil {
		return s.claimContextFactory()
	}
	return context.WithTimeout(s.schedulerContext(), archaeologySchedulerClaimTimeout)
}

func (s *ArchaeologyScheduler) terminalPersistenceContext() (context.Context, context.CancelFunc) {
	if s != nil && s.callbackContextFactory != nil {
		return s.callbackContextFactory()
	}
	timeout := archaeologySchedulerCallbackTimeout
	if s != nil && s.callbackTimeout > 0 {
		timeout = s.callbackTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func persistenceIntentKey(intent domain.ArchaeologyNativePersistenceIntent) string {
	duration := "<nil>"
	if intent.DurationMS != nil {
		duration = fmt.Sprintf("%d", *intent.DurationMS)
	}
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		intent.JobID, intent.Operation, intent.ThreadID, intent.CodexSessionID,
		intent.TurnID, intent.Status, duration, intent.Uncertain,
		intent.Launch.LaunchID, intent.Launch.ProjectID, intent.Launch.State,
		intent.Launch.ThreadID, intent.Launch.CodexSessionID, intent.Launch.TurnID,
		intent.Launch.Error)
}

func (s *ArchaeologyScheduler) enqueuePendingIntentLocked(intent domain.ArchaeologyNativePersistenceIntent) {
	if s.pendingIntentKeys == nil {
		s.pendingIntentKeys = make(map[string]struct{})
	}
	key := persistenceIntentKey(intent)
	if _, exists := s.pendingIntentKeys[key]; !exists {
		s.pendingIntentKeys[key] = struct{}{}
		s.pendingIntents = append(s.pendingIntents, intent)
	}
}

func (s *ArchaeologyScheduler) applyQueuedPersistenceIntent(ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent) {
	s.beginPersistenceFlight()
	defer s.endPersistenceFlight()
	_, ensured, err := s.applyPersistenceIntentDetailed(ctx, intent, true)
	if err != nil && !ensured {
		// No durable row exists yet. Keep the exact callback/mutation in process
		// memory so a healthy-looking empty ledger cannot clear the fault and drop
		// it. The same queue covers all five replay-safe operations.
		s.persistenceMu.Lock()
		s.enqueuePendingIntentLocked(intent)
		s.persistenceMu.Unlock()
	}
}

func (s *ArchaeologyScheduler) drainPendingIntents() bool {
	s.persistenceMu.Lock()
	budget := len(s.pendingIntents)
	s.persistenceMu.Unlock()
	if budget == 0 {
		return true
	}
	if _, durable := s.persistenceDriver(); !durable {
		return false
	}
	if budget > archaeologySchedulerPendingTerminalDrainLimit {
		budget = archaeologySchedulerPendingTerminalDrainLimit
	}
	processed := 0
	for processed < budget {
		s.beginPersistenceFlight()
		s.persistenceMu.Lock()
		if len(s.pendingIntents) == 0 {
			s.persistenceMu.Unlock()
			s.endPersistenceFlight()
			break
		}
		intent := s.pendingIntents[0]
		copy(s.pendingIntents, s.pendingIntents[1:])
		s.pendingIntents = s.pendingIntents[:len(s.pendingIntents)-1]
		s.persistenceMu.Unlock()
		processed++
		_, ensured, err := s.applyPersistenceIntentDetailed(s.schedulerContext(), intent, false)
		s.persistenceMu.Lock()
		if err == nil || ensured {
			key := persistenceIntentKey(intent)
			delete(s.pendingIntentKeys, key)
		} else {
			// A repeatedly failing entry must not monopolize the bounded drain.
			// Keep it queued, but rotate it behind entries that have not been
			// attempted in this pass so a later durable callback can progress.
			s.pendingIntents = append(s.pendingIntents, intent)
		}
		s.persistenceMu.Unlock()
		s.endPersistenceFlight()
	}
	s.persistenceMu.Lock()
	ready := len(s.pendingIntents) == 0
	s.persistenceMu.Unlock()
	return ready
}

func (s *ArchaeologyScheduler) terminalWorkPending() bool {
	s.terminalMu.Lock()
	pending := s.terminalCallbacks != 0 || s.persistenceInFlight != 0
	s.terminalMu.Unlock()
	s.persistenceMu.Lock()
	pending = pending || len(s.pendingIntents) != 0
	s.persistenceMu.Unlock()
	return pending
}

func (s *ArchaeologyScheduler) interruptNativeOnce(job domain.ArchaeologyNativeJob) error {
	if s == nil || s.launcher == nil || job.ThreadID == "" || job.TurnID == "" {
		return nil
	}
	key := job.ID
	if key == "" {
		key = job.ThreadID + "\x00" + job.TurnID
	}
	s.interruptMu.Lock()
	if s.interruptedJobs == nil {
		s.interruptedJobs = make(map[string]struct{})
	}
	if _, attempted := s.interruptedJobs[key]; attempted {
		s.interruptMu.Unlock()
		return nil
	}
	// Mark before the external call. An error is still an attempted interrupt;
	// replaying it could duplicate a non-idempotent cancellation.
	s.interruptedJobs[key] = struct{}{}
	s.interruptMu.Unlock()
	var interruptCtx context.Context
	var cancel context.CancelFunc
	if s.interruptContextFactory != nil {
		interruptCtx, cancel = s.interruptContextFactory()
	} else {
		interruptCtx, cancel = context.WithTimeout(context.Background(), archaeologySchedulerCallbackTimeout)
	}
	defer cancel()
	return s.launcher.InterruptNative(interruptCtx, job)
}

// applyPersistenceIntent is the only scheduler path for the five
// replay-safe native repository mutations when a durable driver is present.
// Ensure commits the ledger row before Apply is allowed to mutate the
// repository. An acknowledgement failure deliberately leaves the durable row
// for RetryDue and marks persistence attention.
func (s *ArchaeologyScheduler) applyPersistenceIntent(ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent) error {
	// Keep the in-process durable boundary closed across Ensure and the
	// pre-ledger enqueue.  Cancel can invoke this path outside drainMu; if the
	// lock were released after Ensure failed, a concurrent drain could observe
	// an empty queue and healthy ledger, clear the latch, and claim before the
	// failed intent became retryable.
	s.beginPersistenceFlight()
	defer s.endPersistenceFlight()
	_, ensured, err := s.applyPersistenceIntentDetailed(ctx, intent, true)
	if err != nil && !ensured {
		s.persistenceMu.Lock()
		s.enqueuePendingIntentLocked(intent)
		s.persistenceMu.Unlock()
	}
	return err
}

func (s *ArchaeologyScheduler) applyPersistenceIntentDetailed(ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent, wake bool) (domain.ArchaeologyNativePersistenceIntentRecord, bool, error) {
	driver, ok := s.persistenceDriver()
	if !ok {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, false, domain.ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record, err := driver.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, false, s.recordPersistenceFailureMode(err, wake)
	}
	if record.ID == "" {
		return record, false, s.recordPersistenceFailureMode(domain.ErrInvalid, wake)
	}
	switch record.State {
	case "applied":
		// Ensure verified the exact deterministic payload. Replaying an already
		// applied row is therefore an acknowledgement readback, not another
		// repository mutation.
		return record, true, nil
	case "pending":
		// Continue to the single Apply attempt below.
	case "blocked", "superseded":
		// A durable terminal state is evidence that this callback must not
		// advance the native lifecycle (especially Finalize/Activate callers).
		s.markPersistenceBacklog(wake)
		return record, true, domain.ErrConflict
	case "leased":
		// Another worker owns the row. Never steal or duplicate its mutation.
		s.markPersistenceBacklog(wake)
		return record, true, domain.ErrUnavailable
	default:
		s.markPersistenceBacklog(wake)
		return record, true, domain.ErrInvalid
	}

	applyErr := driver.ApplyArchaeologyNativePersistenceIntent(ctx, record.ID)
	readback, readErr := driver.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if readErr != nil {
		s.markPersistenceBacklog(wake)
		if applyErr != nil {
			return record, true, applyErr
		}
		return record, true, readErr
	}
	switch readback.State {
	case "applied":
		// This covers mutation-committed/ack-lost and exact idempotent replay.
		return readback, true, nil
	case "superseded", "blocked":
		// Store Apply may return nil after classifying a stale callback. Do not
		// let that nil acknowledgement authorize the next external action.
		s.markPersistenceBacklog(wake)
		return readback, true, domain.ErrConflict
	case "pending", "leased":
		s.markPersistenceBacklog(wake)
		if applyErr != nil {
			return readback, true, applyErr
		}
		return readback, true, domain.ErrUnavailable
	default:
		s.markPersistenceBacklog(wake)
		return readback, true, domain.ErrInvalid
	}
}

func (s *ArchaeologyScheduler) markPersistenceBacklog(wake bool) {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	s.persistenceBacklog = true
	s.persistenceSeq++
	s.stateMu.Unlock()
	if wake {
		s.Wake()
	}
}

func (s *ArchaeologyScheduler) drainPersistence() bool {
	driver, ok := s.persistenceDriver()
	if !ok {
		return !s.persistenceBlocked()
	}
	s.beginPersistenceFlight()
	defer s.endPersistenceFlight()
	if s.ctx != nil {
		if err := s.ctx.Err(); err != nil {
			return false
		}
	}
	ctx := s.schedulerContext()
	if _, err := driver.RetryDueArchaeologyNativePersistence(ctx, archaeologySchedulerPersistenceRetryLimit); err != nil {
		s.recordPersistenceFailureMode(err, false)
		return false
	}
	s.stateMu.RLock()
	sequence := s.persistenceSeq
	s.stateMu.RUnlock()
	status, err := driver.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil {
		s.recordPersistenceFailureMode(err, false)
		return false
	}
	if !status.Healthy() {
		// Existing pending, leased, or blocked work is readiness attention, not
		// an opaque process fault. Store's claim gate and this return prevent a
		// claim until the retry queue reports healthy.
		s.stateMu.Lock()
		s.persistenceBacklog = true
		s.persistenceSeq++
		s.stateMu.Unlock()
		return false
	}
	// A healthy durable ledger is the verified condition for clearing an
	// automatically latched fault. Explicit ReconcilePersistence retains its
	// stronger sequence-checked semantics for non-ledger repositories.
	s.stateMu.Lock()
	if s.persistenceSeq != sequence {
		s.stateMu.Unlock()
		return false
	}
	if s.persistenceManualFault {
		s.stateMu.Unlock()
		return false
	}
	if s.persistenceBacklog {
		s.persistenceBacklog = false
	}
	s.persistenceFault = nil
	s.stateMu.Unlock()
	return true
}

func (s *ArchaeologyScheduler) drain() {
	if s == nil || s.isClosing() {
		return
	}
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	if s.isClosing() {
		return
	}
	// Reconcile already-durable rows first so an in-process pre-ledger retry
	// cannot hide a blocked/leased durable row. Both queues still get one
	// bounded pass per wake; claims require both passes to be ready.
	durableReady := s.drainPersistence()
	pendingReady := s.drainPendingIntents()
	if !durableReady || !pendingReady {
		return
	}
	for s.ctx == nil || s.ctx.Err() == nil {
		if _, durable := s.persistenceDriver(); !durable && s.persistenceBlocked() {
			return
		}
		// Availability is deliberately checked for every individual claim. A
		// recovery attempt may be in progress or exhausted even when a prior
		// wake observed a usable launcher; queued rows must remain queued until
		// the next explicit availability wake.
		if err := s.Available(s.schedulerContext()); err != nil {
			return
		}
		s.terminalMu.Lock()
		if s.terminalCallbacks != 0 || s.persistenceInFlight != 0 {
			s.terminalMu.Unlock()
			return
		}
		s.persistenceMu.Lock()
		pending := len(s.pendingIntents) != 0
		s.persistenceMu.Unlock()
		if pending {
			s.terminalMu.Unlock()
			return
		}
		s.stateMu.RLock()
		persistenceBlocked := s.persistenceFault != nil || s.persistenceBacklog
		s.stateMu.RUnlock()
		if persistenceBlocked {
			s.terminalMu.Unlock()
			return
		}
		// This is the sole scheduler gate intentionally held across repository
		// I/O. Claim must linearize with terminal admission: without terminalMu,
		// an admitted terminal callback could race a new claim after the checks
		// above. Claim receives a bounded context derived from the scheduler
		// context, and Close cancels that parent so this lock cannot hang
		// shutdown indefinitely. Persistence queue writers cannot start while
		// terminalMu is held because every writer publishes persistenceInFlight
		// first, so releasing persistenceMu before Claim is safe.
		claimCtx, cancelClaim := s.claimContext()
		job, err := s.repository.ClaimArchaeologyNativeJob(claimCtx)
		cancelClaim()
		s.terminalMu.Unlock()
		if err != nil {
			// ErrConflict is the store's bounded "nothing claimable" result,
			// including the durable uncertainty gate. Other claim failures are
			// transient process-local wake/tick work: stop this drain, but do not
			// latch a durable write fault or permanently stop the scheduler.
			return
		}
		s.launch(job)
	}
}

func (s *ArchaeologyScheduler) failStart(ctx context.Context, jobID string, result domain.ArchaeologyLaunchResult, uncertain bool) error {
	if s == nil || s.repository == nil {
		return s.recordPersistenceFailure(domain.ErrUnavailable)
	}
	if _, ok := s.persistenceDriver(); ok {
		return s.applyPersistenceIntent(ctx, domain.ArchaeologyNativePersistenceIntent{
			JobID:     jobID,
			Operation: domain.ArchaeologyNativePersistenceFailStart,
			Launch:    result,
			Uncertain: uncertain,
		})
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.recordPersistenceFailure(s.repository.FailArchaeologyNativeStart(ctx, jobID, result, uncertain))
}

func (s *ArchaeologyScheduler) launch(job domain.ArchaeologyNativeJob) {
	launchCtx := s.schedulerContext()
	session, err := s.repository.ArchaeologySession(launchCtx, s.principal)
	if err != nil {
		_ = s.failStart(launchCtx, job.ID, domain.ArchaeologyLaunchResult{}, false)
		return
	}
	var candidate domain.ArchaeologyCandidate
	found := false
	for _, item := range session.Candidates {
		if item.ID == job.CandidateID {
			candidate = item
			found = true
			break
		}
	}
	if !found {
		_ = s.failStart(launchCtx, job.ID, domain.ArchaeologyLaunchResult{}, false)
		return
	}
	bound := make(chan struct{})
	var boundOnce sync.Once
	closeBound := func() { boundOnce.Do(func() { close(bound) }) }
	var bindMu sync.RWMutex
	var bindErr error
	setBindErr := func(err error) {
		bindMu.Lock()
		bindErr = err
		bindMu.Unlock()
	}
	getBindErr := func() error {
		bindMu.RLock()
		err := bindErr
		bindMu.RUnlock()
		return err
	}
	onTool := func(ctx context.Context, call ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse {
		select {
		case <-ctx.Done():
			return ArchaeologyNativeToolResponse{}
		case <-bound:
		}
		if getBindErr() != nil || !s.beginCallback() {
			return ArchaeologyNativeToolResponse{}
		}
		defer s.callbackWG.Done()
		return s.handleTool(ctx, job, call)
	}
	onTerminal := func(terminal domain.ArchaeologyNativeTerminal) {
		if !s.beginTerminalCallback() {
			return
		}
		go func() {
			defer func() {
				s.terminalMu.Lock()
				s.terminalCallbacks--
				s.terminalMu.Unlock()
				s.callbackWG.Done()
				s.Wake()
			}()
			// Binding is part of the launch ordering, not terminal persistence.
			// An early callback may wait here while Finalize/Activate completes;
			// only then should the bounded persistence context begin. The launcher
			// contract requires LaunchNative to observe scheduler-context
			// cancellation and return through one of the closeBound paths. There is
			// intentionally no independent timeout here: applying before bound can
			// race Bind/Finalize/Activate, while abandoning the accepted callback
			// would lose terminal evidence. Close therefore waits for the launcher
			// cancellation contract rather than silently dropping this event.
			<-bound
			if s.terminalBoundHook != nil {
				s.terminalBoundHook()
			}
			callbackCtx, cancel := s.terminalPersistenceContext()
			defer cancel()
			// A prior persistence fault blocks new claims, but it must not
			// suppress a best-effort terminal write for this already-claimed job.
			// Attempt it and keep the fault latched if the repository still fails.
			if getBindErr() == nil {
				terminal.JobID = job.ID
				if terminal.Status == "unavailable" {
					if _, durable := s.persistenceDriver(); durable {
						s.applyQueuedPersistenceIntent(callbackCtx, domain.ArchaeologyNativePersistenceIntent{
							JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceLoseTurn,
							ThreadID: terminal.ThreadID, TurnID: terminal.TurnID,
						})
					} else {
						_ = s.recordPersistenceFailure(s.repository.LoseArchaeologyNativeTurn(callbackCtx, job.ID, terminal.ThreadID, terminal.TurnID))
					}
				} else {
					if _, durable := s.persistenceDriver(); durable {
						s.applyQueuedPersistenceIntent(callbackCtx, domain.ArchaeologyNativePersistenceIntent{
							JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceCompleteTurn,
							ThreadID: terminal.ThreadID, TurnID: terminal.TurnID,
							Status: terminal.Status, DurationMS: terminal.DurationMS,
						})
					} else {
						_ = s.recordPersistenceFailure(s.repository.CompleteArchaeologyNativeTurn(callbackCtx, terminal))
					}
				}
				s.Wake()
			}
		}()
	}
	result, launchErr := s.launcher.LaunchNative(launchCtx, job, session, candidate, onTool, onTerminal)
	if launchErr != nil {
		setBindErr(launchErr)
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, result.State == "uncertain" || result.ThreadID != "" || result.CodexSessionID != "" || result.TurnID != "")
		closeBound()
		// Once an exact turn exists, any later protocol/visibility failure is
		// post-acceptance. Persist uncertainty first, then interrupt that exact
		// turn once. Never retry the non-idempotent launch boundary.
		if result.ThreadID != "" && result.TurnID != "" {
			_ = s.interruptNativeOnce(domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		}
		return
	}
	if result.State == "uncertain" || result.ThreadID == "" || result.CodexSessionID == "" || result.TurnID == "" {
		// A nil launcher error is not proof that the external acceptance result
		// is complete. Never bind/finalize/activate an uncertain or partial
		// identity; preserve the one-shot boundary as uncertain and interrupt
		// only when the exact thread+turn pair is known.
		setBindErr(domain.ErrInvalid)
		closeBound()
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, true)
		if result.ThreadID != "" && result.TurnID != "" {
			_ = s.interruptNativeOnce(domain.ArchaeologyNativeJob{
				ID:             job.ID,
				ThreadID:       result.ThreadID,
				CodexSessionID: result.CodexSessionID,
				TurnID:         result.TurnID,
			})
		}
		return
	}
	if _, durable := s.persistenceDriver(); durable {
		setBindErr(s.applyPersistenceIntent(launchCtx, domain.ArchaeologyNativePersistenceIntent{
			JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceBindIdentity,
			ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID,
		}))
	} else {
		setBindErr(s.recordPersistenceFailure(s.repository.BindArchaeologyNativeIdentity(launchCtx, job.ID, result.ThreadID, result.CodexSessionID, result.TurnID)))
	}
	if getBindErr() != nil {
		closeBound()
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, true)
		// A task accepted by Codex but not durably bound must never become an
		// invisible orphan. Persist its exact identity above, then make one
		// best-effort interrupt of the known turn. The durable state remains
		// uncertain until a human reconciles it, regardless of interrupt result.
		if result.ThreadID != "" && result.TurnID != "" {
			_ = s.interruptNativeOnce(domain.ArchaeologyNativeJob{
				ID:             job.ID,
				ThreadID:       result.ThreadID,
				CodexSessionID: result.CodexSessionID,
				TurnID:         result.TurnID,
			})
		}
		return
	}
	setBindErr(s.launcher.FinalizeNative(launchCtx, job, candidate, result))
	if getBindErr() != nil {
		closeBound()
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, true)
		if result.ThreadID != "" && result.TurnID != "" {
			_ = s.interruptNativeOnce(domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		}
		return
	}
	if _, durable := s.persistenceDriver(); durable {
		setBindErr(s.applyPersistenceIntent(launchCtx, domain.ArchaeologyNativePersistenceIntent{
			JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceActivate,
			ThreadID: result.ThreadID, TurnID: result.TurnID,
		}))
	} else {
		setBindErr(s.recordPersistenceFailure(s.repository.ActivateArchaeologyNativeJob(launchCtx, job.ID, result.ThreadID, result.TurnID)))
	}
	if getBindErr() != nil {
		closeBound()
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, true)
		if result.ThreadID != "" && result.TurnID != "" {
			_ = s.interruptNativeOnce(domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		}
		return
	}
	closeBound()
}
func decodeStrictOne(body []byte, target any) error {
	return decodeStrictOneLimit(body, target, 64<<10)
}
func decodeStrictOneLimit(body []byte, target any, limit int) error {
	if len(body) == 0 || len(body) > limit {
		return domain.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return domain.ErrInvalid
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return domain.ErrInvalid
	}
	return nil
}
func (s *ArchaeologyScheduler) handleTool(ctx context.Context, job domain.ArchaeologyNativeJob, call ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse {
	if call.ThreadID == "" || call.TurnID == "" {
		return ArchaeologyNativeToolResponse{}
	}
	switch call.Tool {
	case "commons_project_history_progress":
		var input struct {
			Phase           string `json:"phase"`
			SourcesExamined int    `json:"sources_examined"`
			Note            string `json:"note"`
		}
		limits, validPolicy := job.Policy.Limits()
		if decodeStrictOne(call.Arguments, &input) != nil || len(input.Note) > 500 || !validPolicy || input.SourcesExamined < 0 || input.SourcesExamined > limits.MaxSourcesExamined {
			return ArchaeologyNativeToolResponse{}
		}
		labels := map[string]string{"inspecting_sources": "Inspecting selected sources", "building_proposals": "Building history proposals", "ready_to_report": "Preparing review report"}
		label := labels[input.Phase]
		if label == "" {
			return ArchaeologyNativeToolResponse{}
		}
		if s.recordPersistenceFailure(s.repository.UpdateArchaeologyNativeProgress(ctx, domain.ArchaeologyNativeProgress{JobID: job.ID, ThreadID: call.ThreadID, TurnID: call.TurnID, PhaseLabel: label, SourcesExamined: input.SourcesExamined})) != nil {
			return ArchaeologyNativeToolResponse{}
		}
		return ArchaeologyNativeToolResponse{Success: true, Message: `{"accepted":true}`}
	case "commons_project_history_report":
		var envelope struct {
			Outcomes []json.RawMessage `json:"outcomes"`
		}
		limits, validPolicy := job.Policy.Limits()
		if decodeStrictOneLimit(call.Arguments, &envelope, domain.ArchaeologyNativeReportMaxBytes) != nil || !validPolicy || len(envelope.Outcomes) == 0 || len(envelope.Outcomes) > limits.MaxOutcomes {
			return ArchaeologyNativeToolResponse{}
		}
		for _, rawOutcome := range envelope.Outcomes {
			var fields map[string]json.RawMessage
			if decodeStrictOneLimit(rawOutcome, &fields, domain.ArchaeologyNativeProposalMaxBytes+16<<10) != nil {
				return ArchaeologyNativeToolResponse{}
			}
			if _, supplied := fields["project_id"]; supplied {
				return ArchaeologyNativeToolResponse{}
			}
		}
		var input struct {
			Outcomes []ArchaeologyOutcomeReportRequest `json:"outcomes"`
		}
		if decodeStrictOneLimit(call.Arguments, &input, domain.ArchaeologyNativeReportMaxBytes) != nil {
			return ArchaeologyNativeToolResponse{}
		}
		for index := range input.Outcomes {
			if input.Outcomes[index].HistoricalImport.BatchID != "" {
				return ArchaeologyNativeToolResponse{}
			}
			input.Outcomes[index].ProjectID = job.ProjectID
			input.Outcomes[index].HistoricalImport.BatchID = nativeHistoricalImportBatchID(job.ID, index)
		}
		outcomes, err := s.service.archaeologyReportOutcomes(ctx, input.Outcomes)
		if err != nil {
			return ArchaeologyNativeToolResponse{}
		}
		for _, outcome := range outcomes {
			if outcome.ProjectID != job.ProjectID {
				return ArchaeologyNativeToolResponse{}
			}
		}
		digest := sha256.Sum256(call.Arguments)
		if err = s.recordPersistenceFailure(s.repository.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: call.ThreadID, TurnID: call.TurnID, Digest: digest, Outcomes: outcomes})); err != nil {
			return ArchaeologyNativeToolResponse{}
		}
		return ArchaeologyNativeToolResponse{Success: true, Message: `{"accepted":true,"canonical_apply":false}`}
	default:
		return ArchaeologyNativeToolResponse{}
	}
}

func nativeHistoricalImportBatchID(jobID string, index int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", jobID, index)))
	return fmt.Sprintf("native-%x", digest[:12])
}

func HistorianTitle(name, jobID string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if name == "" {
		name = "Project"
	}
	jobID = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, jobID))
	if jobID == "" {
		return ""
	}
	suffix := " · " + jobID
	prefix := "Project history · " + name
	maximum := 200 - len(suffix)
	if maximum <= 0 {
		return ""
	}
	if len(prefix) > maximum {
		prefix = prefix[:maximum]
		for !utf8.ValidString(prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix + suffix
}

func HistorianVisibleTitle(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if name == "" {
		name = "Project"
	}
	title := "Project history · " + name
	if len(title) > 200 {
		title = title[:200]
		for !utf8.ValidString(title) {
			title = title[:len(title)-1]
		}
	}
	return title
}
func HistorianPrompt(policy domain.ArchaeologyExecutionPolicy) string {
	limits, ok := policy.Limits()
	if !ok {
		return ""
	}
	allowed := make([]string, 0, 3)
	forbidden := make([]string, 0, 3)
	stableRules := make([]string, 0, 3)
	for _, kind := range []string{"git", "docs", "codex_history"} {
		if policy.Allows(kind) {
			allowed = append(allowed, kind)
			switch kind {
			case "git":
				stableRules = append(stableRules, "git objects as commit|tree|blob|tag:<40-or-64 lowercase hex> or refs as ref:refs/<name>, with no traversal, repeated slash, dot/hidden/.lock segments, @{, or trailing dot/slash")
			case "docs":
				stableRules = append(stableRules, "docs as normalized repository-relative paths with no absolute path, traversal, hidden/private credential filename, prompt text, or control characters")
			case "codex_history":
				stableRules = append(stableRules, "Codex history as task:<id> or thread:<id>, never a title, prompt, transcript, or path")
			}
		} else {
			forbidden = append(forbidden, kind)
		}
	}
	forbiddenText := "none"
	if len(forbidden) > 0 {
		forbiddenText = strings.Join(forbidden, ", ")
	}
	return fmt.Sprintf("Review the selected Codex project read-only for source-grounded historical Tasks and provenance. Execution depth is %s. The only admissible evidence kinds are: %s. Forbidden evidence kinds are: %s. Source selections govern evidence you may cite; they do not imply filesystem isolation. Never cite a disabled kind. Stable IDs for allowed evidence must be: %s. Commons binds project_id and historical_import.batch_id; do not supply either field. Hard limits: at most %d outcomes, %d provenance records and %d contributors per outcome, %d aliases, %d historical tasks, two attributions and one event per task, and %d examined sources. The complete report must be below 60 KiB and each historical_import below 32 KiB. Outcome titles, task keys, event keys, aliases, outer provenance records, and contributor session IDs must be unique in their applicable arrays. Every nested source must exactly match one outer provenance record. Alias sessions must be unique and cannot appear in any task attribution or event. An event session, when present, must exactly match an attribution session on that same task. Only report exact contributor session IDs observed in allowed evidence; never invent, rename, or infer a contributor, and each outer contributor must appear in the proposal's aliases, attributions, or events. Use only the two provided Commons project-history tools for progress and the final proposal. Do not write, apply, mutate, publish, expose credentials or prompts, include private data, or reproduce raw transcripts. A human must review every proposal before canonical import.", policy.Depth, strings.Join(allowed, ", "), forbiddenText, strings.Join(stableRules, "; "), limits.MaxOutcomes, limits.MaxProvenancePerOutcome, limits.MaxContributorsPerOutcome, limits.MaxHistoricalAliases, limits.MaxHistoricalTasks, limits.MaxSourcesExamined)
}

func (s *Service) ConfigureNativeProjectArchaeology(ctx context.Context, launcher ArchaeologyNativeLauncher, principal string) error {
	repository, ok := s.repository.(ArchaeologyNativeRepository)
	if !ok || launcher == nil {
		return domain.ErrUnavailable
	}
	if err := reconcileArchaeologyNativeIdentities(ctx, repository, launcher, principal); err != nil {
		return err
	}
	s.archaeologyScheduler = newArchaeologyScheduler(ctx, s, repository, launcher, principal)
	s.archaeologyLauncher = s.archaeologyScheduler
	s.archaeologyScheduler.Wake()
	return nil
}

func reconcileArchaeologyNativeIdentities(ctx context.Context, repository ArchaeologyNativeRepository, launcher ArchaeologyNativeLauncher, principal string) error {
	identityStore, storeOK := repository.(ArchaeologyNativeIdentityRepository)
	reconciler, launcherOK := launcher.(ArchaeologyNativeIdentityReconciler)
	if !storeOK || !launcherOK {
		return nil
	}
	value, err := repository.ArchaeologySession(ctx, principal)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	candidates := make(map[string]domain.ArchaeologyCandidate, len(value.Candidates))
	for _, candidate := range value.Candidates {
		candidates[candidate.ID] = candidate
	}
	checked := 0
	for _, batch := range value.NativeBatches {
		for _, job := range batch.Jobs {
			if checked >= domain.ArchaeologyNativeMaxProjects*2 {
				return nil
			}
			if job.State != "uncertain" || job.ThreadID != "" && job.TurnID != "" {
				continue
			}
			candidate, found := candidates[job.CandidateID]
			if !found {
				continue
			}
			checked++
			lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			launch, exact, lookupErr := reconciler.RecoverNativeIdentity(lookupCtx, job, candidate)
			cancel()
			if lookupErr != nil || !exact {
				// Lookup is best-effort and read-only. Zero, multiple, incomplete, or
				// unavailable matches all leave the global uncertainty gate closed.
				continue
			}
			if bindErr := identityStore.BindArchaeologyNativeUncertainty(ctx, job.ID, launch); bindErr != nil && !errors.Is(bindErr, domain.ErrConflict) {
				return bindErr
			}
		}
	}
	return nil
}
func (s *Service) CloseProjectArchaeology() {
	if s != nil && s.archaeologyScheduler != nil {
		s.archaeologyScheduler.Close()
	}
}

// NativeProjectArchaeologySchedulerStatus exposes only the scheduler's safe
// process-local status. It intentionally omits the underlying persistence
// error text.
func (s *Service) NativeProjectArchaeologySchedulerStatus() ArchaeologySchedulerStatus {
	if s == nil || s.archaeologyScheduler == nil {
		return ArchaeologySchedulerStatus{}
	}
	return s.archaeologyScheduler.Status()
}

// WakeNativeProjectArchaeologyScheduler forwards the bounded, nonblocking
// availability/recovery signal without exposing the scheduler instance.
func (s *Service) WakeNativeProjectArchaeologyScheduler() {
	if s == nil || s.archaeologyScheduler == nil {
		return
	}
	s.archaeologyScheduler.Wake()
}

// ReconcileNativeProjectArchaeologyPersistence delegates to the explicit
// repository reconciliation seam before allowing queued work to resume.
func (s *Service) ReconcileNativeProjectArchaeologyPersistence(ctx context.Context) error {
	if s == nil || s.archaeologyScheduler == nil {
		return domain.ErrUnavailable
	}
	return s.archaeologyScheduler.ReconcilePersistence(ctx)
}

func (s *Service) queueNativeProjectArchaeology(ctx context.Context, principal, requestID string, baseRevision int64, acknowledgeLargeBatch bool) (domain.ArchaeologySession, error) {
	repository, ok := s.repository.(ArchaeologyNativeRepository)
	if !ok || s.archaeologyScheduler == nil {
		return domain.ArchaeologySession{}, domain.ErrUnavailable
	}
	if err := s.archaeologyScheduler.Available(ctx); err != nil {
		return domain.ArchaeologySession{}, domain.ErrUnavailable
	}
	value, err := repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: principal, RequestID: requestID, BaseRevision: baseRevision, AcknowledgeLargeBatch: acknowledgeLargeBatch})
	if err == nil {
		s.archaeologyScheduler.Wake()
	}

	return value, err
}

func (s *Service) ResolveProjectArchaeologyUncertainty(ctx context.Context, principal, requestID string, input ArchaeologyResolutionRequest) (ArchaeologySession, error) {
	repository, ok := s.repository.(ArchaeologyNativeResolutionRepository)
	if !ok {
		return ArchaeologySession{}, domain.ErrUnavailable
	}
	value, err := repository.ResolveArchaeologyNativeUncertainty(ctx, domain.ArchaeologyNativeResolution{Principal: principal, RequestID: requestID, BaseRevision: input.BaseRevision, JobID: input.JobID, ThreadID: input.ThreadID, TurnID: input.TurnID, Resolution: input.Resolution})
	return s.archaeologySessionView(value), err
}

func expectedArchaeologyCancelError(err error) bool {
	return errors.Is(err, domain.ErrConflict) ||
		errors.Is(err, domain.ErrInvalid) ||
		errors.Is(err, domain.ErrNotFound) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

func (s *ArchaeologyScheduler) Cancel(ctx context.Context, principal, requestID string, baseRevision int64) (domain.ArchaeologySession, error) {
	if s == nil || s.repository == nil || s.launcher == nil {
		return domain.ArchaeologySession{}, domain.ErrUnavailable
	}
	if s.persistenceBlocked() {
		return domain.ArchaeologySession{}, ErrArchaeologySchedulerPersistenceFault
	}
	jobs, value, err := s.repository.CancelArchaeologyNativeBatch(ctx, principal, requestID, baseRevision)
	if err != nil {
		if !expectedArchaeologyCancelError(err) {
			s.recordPersistenceFailure(err)
		}
		return value, err
	}
	for _, job := range jobs {
		if job.ThreadID == "" || job.TurnID == "" {
			continue
		}
		if interruptErr := s.interruptNativeOnce(job); interruptErr != nil {
			if _, durable := s.persistenceDriver(); durable {
				_ = s.applyPersistenceIntent(ctx, domain.ArchaeologyNativePersistenceIntent{
					JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceLoseTurn,
					ThreadID: job.ThreadID, TurnID: job.TurnID,
				})
			} else {
				_ = s.recordPersistenceFailure(s.repository.LoseArchaeologyNativeTurn(ctx, job.ID, job.ThreadID, job.TurnID))
			}
		}
	}
	s.Wake()
	value, err = s.repository.ArchaeologySession(ctx, principal)
	if err != nil && !expectedArchaeologyCancelError(err) {
		s.recordPersistenceFailure(err)
	}
	return value, err
}
