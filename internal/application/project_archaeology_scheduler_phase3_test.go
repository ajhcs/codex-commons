package application

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

// phase3LedgerRepository is intentionally small and in-memory. It records the
// application-facing capability boundary rather than reproducing Store SQL;
// Store owns ledger durability and has its own focused tests.
type phase3LedgerRepository struct {
	ArchaeologyNativeRepository

	mu sync.Mutex

	jobs        []domain.ArchaeologyNativeJob
	claims      int
	rawBind     int
	rawActivate int
	rawFail     int
	rawComplete int
	rawLose     int

	events           []string
	intents          []domain.ArchaeologyNativePersistenceIntent
	applyIDs         []string
	nextIntentID     int
	ensureErr        map[domain.ArchaeologyNativePersistenceOperation]error
	ensureFailures   map[domain.ArchaeologyNativePersistenceOperation][]error
	ensureState      map[domain.ArchaeologyNativePersistenceOperation]string
	applyErr         map[domain.ArchaeologyNativePersistenceOperation]error
	postApplyState   map[domain.ArchaeologyNativePersistenceOperation]string
	intentRecords    map[string]domain.ArchaeologyNativePersistenceIntentRecord
	retryCalls       int
	readbackCalls    int
	retryErr         error
	recordRetryEvent bool
	status           domain.ArchaeologyNativePersistenceStatus
	completeStarted  chan struct{}
	completeRelease  chan struct{}
	ensureSignal     chan struct{}
	ensureBlockOp    domain.ArchaeologyNativePersistenceOperation
	ensureStarted    chan struct{}
	ensureRelease    chan struct{}
	cancelJobs       []domain.ArchaeologyNativeJob
	cancelValue      domain.ArchaeologySession
	cancelErr        error
	sessionErr       error
	claimStarted     chan struct{}
	claimWaitContext bool
	claimContext     context.Context
	lastFailContext  context.Context
}

func (r *phase3LedgerRepository) ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionErr != nil {
		return domain.ArchaeologySession{}, r.sessionErr
	}
	candidate := "candidate"
	if len(r.jobs) != 0 && r.jobs[0].CandidateID != "" {
		candidate = r.jobs[0].CandidateID
	}
	return domain.ArchaeologySession{Candidates: []domain.ArchaeologyCandidate{{ID: candidate, Name: "Candidate"}}}, nil
}

func (r *phase3LedgerRepository) ClaimArchaeologyNativeJob(ctx context.Context) (domain.ArchaeologyNativeJob, error) {
	r.mu.Lock()
	r.claims++
	if r.claimStarted != nil {
		nonBlockingPhase3Signal(r.claimStarted)
	}
	r.claimContext = ctx
	if r.claimWaitContext {
		r.mu.Unlock()
		<-ctx.Done()
		return domain.ArchaeologyNativeJob{}, ctx.Err()
	}
	defer r.mu.Unlock()
	if len(r.jobs) == 0 {
		return domain.ArchaeologyNativeJob{}, domain.ErrConflict
	}
	job := r.jobs[0]
	r.jobs = r.jobs[1:]
	return job, nil
}

func (r *phase3LedgerRepository) BindArchaeologyNativeIdentity(context.Context, string, string, string, string) error {
	r.mu.Lock()
	r.rawBind++
	r.mu.Unlock()
	return nil
}

func (r *phase3LedgerRepository) ActivateArchaeologyNativeJob(context.Context, string, string, string) error {
	r.mu.Lock()
	r.rawActivate++
	r.mu.Unlock()
	return nil
}

func (r *phase3LedgerRepository) FailArchaeologyNativeStart(context.Context, string, domain.ArchaeologyLaunchResult, bool) error {
	r.mu.Lock()
	r.rawFail++
	r.mu.Unlock()
	return nil
}

func (r *phase3LedgerRepository) UpdateArchaeologyNativeProgress(context.Context, domain.ArchaeologyNativeProgress) error {
	return nil
}

func (r *phase3LedgerRepository) ReportArchaeologyNativeJob(context.Context, domain.ArchaeologyNativeReport) error {
	return nil
}

func (r *phase3LedgerRepository) CompleteArchaeologyNativeTurn(_ context.Context, _ domain.ArchaeologyNativeTerminal) error {
	r.mu.Lock()
	r.rawComplete++
	r.mu.Unlock()
	return nil
}

func (r *phase3LedgerRepository) LoseArchaeologyNativeTurn(context.Context, string, string, string) error {
	r.mu.Lock()
	r.rawLose++
	r.mu.Unlock()
	return nil
}

func (r *phase3LedgerRepository) CancelArchaeologyNativeBatch(context.Context, string, string, int64) ([]domain.ArchaeologyNativeJob, domain.ArchaeologySession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelErr != nil {
		return nil, r.cancelValue, r.cancelErr
	}
	return append([]domain.ArchaeologyNativeJob(nil), r.cancelJobs...), r.cancelValue, nil
}

func (r *phase3LedgerRepository) EnsureArchaeologyNativePersistenceIntent(_ context.Context, intent domain.ArchaeologyNativePersistenceIntent) (domain.ArchaeologyNativePersistenceIntentRecord, error) {
	r.mu.Lock()
	r.events = append(r.events, "ensure:"+string(intent.Operation))
	if r.ensureSignal != nil {
		nonBlockingPhase3Signal(r.ensureSignal)
	}
	if intent.Operation == r.ensureBlockOp && r.ensureRelease != nil {
		if r.ensureStarted != nil {
			nonBlockingPhase3Signal(r.ensureStarted)
		}
		release := r.ensureRelease
		r.mu.Unlock()
		<-release
		r.mu.Lock()
	}
	defer r.mu.Unlock()
	if failures := r.ensureFailures[intent.Operation]; len(failures) != 0 {
		err := failures[0]
		r.ensureFailures[intent.Operation] = failures[1:]
		return domain.ArchaeologyNativePersistenceIntentRecord{}, err
	}
	if err := r.ensureErr[intent.Operation]; err != nil {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, err
	}
	r.nextIntentID++
	id := fmt.Sprintf("intent-%d", r.nextIntentID)
	state := "pending"
	if configured := r.ensureState[intent.Operation]; configured != "" {
		state = configured
	}
	record := domain.ArchaeologyNativePersistenceIntentRecord{ID: id, JobID: intent.JobID, Operation: intent.Operation, State: state}
	r.intents = append(r.intents, intent)
	if r.intentRecords == nil {
		r.intentRecords = make(map[string]domain.ArchaeologyNativePersistenceIntentRecord)
	}
	r.intentRecords[id] = record
	return record, nil
}

func (r *phase3LedgerRepository) ApplyArchaeologyNativePersistenceIntent(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "apply")
	r.applyIDs = append(r.applyIDs, id)
	record, ok := r.intentRecords[id]
	if !ok {
		return domain.ErrNotFound
	}
	operation := record.Operation
	if operation == domain.ArchaeologyNativePersistenceFailStart {
		r.lastFailContext = ctx
	}
	if operation == domain.ArchaeologyNativePersistenceCompleteTurn && r.completeStarted != nil {
		nonBlockingPhase3Signal(r.completeStarted)
		release := r.completeRelease
		r.mu.Unlock()
		if release != nil {
			<-release
		}
		r.mu.Lock()
	}
	if err := r.applyErr[operation]; err != nil {
		return err
	}
	record.State = "applied"
	if configured := r.postApplyState[operation]; configured != "" {
		record.State = configured
	}
	r.intentRecords[id] = record
	return nil
}

func (r *phase3LedgerRepository) ArchaeologyNativePersistenceIntent(_ context.Context, id string) (domain.ArchaeologyNativePersistenceIntentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readbackCalls++
	record, ok := r.intentRecords[id]
	if !ok {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, domain.ErrNotFound
	}
	return record, nil
}

func (r *phase3LedgerRepository) RetryDueArchaeologyNativePersistence(context.Context, ...int) (domain.ArchaeologyNativePersistenceRetryReport, error) {
	r.mu.Lock()
	r.retryCalls++
	if r.recordRetryEvent {
		r.events = append(r.events, "retry")
	}
	err := r.retryErr
	r.mu.Unlock()
	return domain.ArchaeologyNativePersistenceRetryReport{}, err
}

func (r *phase3LedgerRepository) ArchaeologyNativePersistenceStatus(context.Context) (domain.ArchaeologyNativePersistenceStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, nil
}

func (r *phase3LedgerRepository) snapshot() (events []string, intents []domain.ArchaeologyNativePersistenceIntent, rawBind, rawActivate, rawFail, rawComplete, rawLose, retries int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...), append([]domain.ArchaeologyNativePersistenceIntent(nil), r.intents...), r.rawBind, r.rawActivate, r.rawFail, r.rawComplete, r.rawLose, r.retryCalls
}

type phase3SchedulerLauncher struct {
	mu               sync.Mutex
	availableErr     error
	result           domain.ArchaeologyLaunchResult
	launchErr        error
	terminal         *domain.ArchaeologyNativeTerminal
	terminalOnce     bool
	finalizeErr      error
	finalizeStarted  chan struct{}
	finalizeRelease  chan struct{}
	launches         int
	finalizes        int
	interrupts       int
	interruptErr     error
	interruptContext context.Context
	interruptStarted chan struct{}
	interruptRelease chan struct{}
}

func (l *phase3SchedulerLauncher) Available(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.availableErr
}

func (l *phase3SchedulerLauncher) LaunchNative(_ context.Context, _ domain.ArchaeologyNativeJob, _ domain.ArchaeologySession, _ domain.ArchaeologyCandidate, _ func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, onTerminal func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	l.mu.Lock()
	l.launches++
	result := l.result
	terminal := l.terminal
	if l.terminalOnce {
		l.terminal = nil
	}
	l.mu.Unlock()
	if terminal != nil {
		onTerminal(*terminal)
	}
	return result, l.launchErr
}

func (l *phase3SchedulerLauncher) InterruptNative(ctx context.Context, _ domain.ArchaeologyNativeJob) error {
	l.mu.Lock()
	l.interrupts++
	l.interruptContext = ctx
	started, release := l.interruptStarted, l.interruptRelease
	l.mu.Unlock()
	if started != nil {
		nonBlockingPhase3Signal(started)
	}
	if release != nil {
		<-release
	}
	return l.interruptErr
}

func (l *phase3SchedulerLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	l.mu.Lock()
	l.finalizes++
	started, release, err := l.finalizeStarted, l.finalizeRelease, l.finalizeErr
	l.mu.Unlock()
	if started != nil {
		nonBlockingPhase3Signal(started)
	}
	if release != nil {
		<-release
	}
	return err
}

func (l *phase3SchedulerLauncher) counts() (launches, finalizes, interrupts int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.launches, l.finalizes, l.interrupts
}

func nonBlockingPhase3Signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func TestPhase3SchedulerDurableOperationsEnsureBeforeApply(t *testing.T) {
	repository := &phase3LedgerRepository{
		jobs:      []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}},
		ensureErr: map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:  map[domain.ArchaeologyNativePersistenceOperation]error{},
	}
	launcher := &phase3SchedulerLauncher{result: domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.drain()

	events, intents, rawBind, rawActivate, rawFail, _, _, retries := repository.snapshot()
	if fmt.Sprint(events) != "[ensure:bind_identity apply ensure:activate apply]" {
		t.Fatalf("ledger event order = %v", events)
	}
	if len(intents) != 2 || intents[0].Operation != domain.ArchaeologyNativePersistenceBindIdentity || intents[1].Operation != domain.ArchaeologyNativePersistenceActivate {
		t.Fatalf("durable intents = %+v", intents)
	}
	if intents[0].ThreadID != "thread" || intents[0].CodexSessionID != "session" || intents[0].TurnID != "turn" || intents[1].ThreadID != "thread" || intents[1].TurnID != "turn" {
		t.Fatalf("intent identities = %+v", intents)
	}
	if rawBind != 0 || rawActivate != 0 || rawFail != 0 {
		t.Fatalf("raw replay-safe calls bind=%d activate=%d fail=%d", rawBind, rawActivate, rawFail)
	}
	if retries != 1 {
		t.Fatalf("retry calls = %d, want one pre-claim drain", retries)
	}
	if launches, finalizes, interrupts := launcher.counts(); launches != 1 || finalizes != 1 || interrupts != 0 {
		t.Fatalf("external calls launch=%d finalize=%d interrupt=%d", launches, finalizes, interrupts)
	}
}

func TestPhase3SchedulerPersistenceStateGateAndReadback(t *testing.T) {
	validBind := domain.ArchaeologyNativePersistenceIntent{
		JobID:          "job",
		Operation:      domain.ArchaeologyNativePersistenceBindIdentity,
		ThreadID:       "thread",
		CodexSessionID: "session",
		TurnID:         "turn",
	}
	for _, state := range []string{"blocked", "superseded"} {
		t.Run(state+" ensure", func(t *testing.T) {
			repository := &phase3LedgerRepository{
				ensureState: map[domain.ArchaeologyNativePersistenceOperation]string{domain.ArchaeologyNativePersistenceBindIdentity: state},
				ensureErr:   map[domain.ArchaeologyNativePersistenceOperation]error{},
				applyErr:    map[domain.ArchaeologyNativePersistenceOperation]error{},
			}
			scheduler := &ArchaeologyScheduler{repository: repository, ctx: context.Background(), wake: make(chan struct{}, 1)}
			if err := scheduler.applyPersistenceIntent(context.Background(), validBind); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("state=%s Ensure error=%v, want conflict", state, err)
			}
			events, _, _, _, _, _, _, _ := repository.snapshot()
			if fmt.Sprint(events) != "[ensure:bind_identity]" {
				t.Fatalf("state=%s external Apply was reached: events=%v", state, events)
			}

			lifecycleRepository := &phase3LedgerRepository{
				ensureState: map[domain.ArchaeologyNativePersistenceOperation]string{domain.ArchaeologyNativePersistenceBindIdentity: state},
				ensureErr:   map[domain.ArchaeologyNativePersistenceOperation]error{},
				applyErr:    map[domain.ArchaeologyNativePersistenceOperation]error{},
			}
			lifecycleLauncher := &phase3SchedulerLauncher{result: domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}}
			lifecycleScheduler := &ArchaeologyScheduler{repository: lifecycleRepository, launcher: lifecycleLauncher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
			lifecycleScheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
			_, _, _, rawActivate, _, _, _, _ := lifecycleRepository.snapshot()
			if _, finalizes, _ := lifecycleLauncher.counts(); finalizes != 0 || rawActivate != 0 {
				t.Fatalf("state=%s advanced lifecycle: finalizes=%d activates=%d", state, finalizes, rawActivate)
			}
		})
	}

	t.Run("applied replay skips mutation", func(t *testing.T) {
		repository := &phase3LedgerRepository{
			ensureState: map[domain.ArchaeologyNativePersistenceOperation]string{domain.ArchaeologyNativePersistenceBindIdentity: "applied"},
			ensureErr:   map[domain.ArchaeologyNativePersistenceOperation]error{},
			applyErr:    map[domain.ArchaeologyNativePersistenceOperation]error{},
		}
		scheduler := &ArchaeologyScheduler{repository: repository, ctx: context.Background(), wake: make(chan struct{}, 1)}
		if err := scheduler.applyPersistenceIntent(context.Background(), validBind); err != nil {
			t.Fatalf("exact applied replay=%v", err)
		}
		events, _, _, _, _, _, _, _ := repository.snapshot()
		if fmt.Sprint(events) != "[ensure:bind_identity]" {
			t.Fatalf("applied replay duplicated mutation: events=%v", events)
		}
	})

	for _, state := range []string{"superseded", "blocked", "pending", "leased"} {
		t.Run("successful Apply requires "+state+" readback", func(t *testing.T) {
			repository := &phase3LedgerRepository{
				postApplyState: map[domain.ArchaeologyNativePersistenceOperation]string{domain.ArchaeologyNativePersistenceBindIdentity: state},
				ensureErr:      map[domain.ArchaeologyNativePersistenceOperation]error{},
				applyErr:       map[domain.ArchaeologyNativePersistenceOperation]error{},
			}
			scheduler := &ArchaeologyScheduler{repository: repository, ctx: context.Background(), wake: make(chan struct{}, 1)}
			err := scheduler.applyPersistenceIntent(context.Background(), validBind)
			if state == "superseded" || state == "blocked" {
				if !errors.Is(err, domain.ErrConflict) {
					t.Fatalf("post-Apply state=%s error=%v, want conflict", state, err)
				}
			} else if !errors.Is(err, domain.ErrUnavailable) {
				t.Fatalf("post-Apply state=%s error=%v, want unavailable", state, err)
			}
			repository.mu.Lock()
			readbacks := repository.readbackCalls
			repository.mu.Unlock()
			if readbacks != 1 {
				t.Fatalf("state=%s readback calls=%d, want one", state, readbacks)
			}
		})

		t.Run("lifecycle stops after "+state+" readback", func(t *testing.T) {
			repository := &phase3LedgerRepository{
				postApplyState: map[domain.ArchaeologyNativePersistenceOperation]string{domain.ArchaeologyNativePersistenceBindIdentity: state},
				ensureErr:      map[domain.ArchaeologyNativePersistenceOperation]error{},
				applyErr:       map[domain.ArchaeologyNativePersistenceOperation]error{},
			}
			launcher := &phase3SchedulerLauncher{result: domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}}
			scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
			scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
			events, _, _, rawActivate, _, _, _, _ := repository.snapshot()
			_, finalizes, _ := launcher.counts()
			if finalizes != 0 || rawActivate != 0 {
				t.Fatalf("post-Apply state=%s advanced lifecycle: events=%v finalizes=%d activates=%d", state, events, finalizes, rawActivate)
			}
			if !strings.Contains(fmt.Sprint(events), "ensure:fail_start") {
				t.Fatalf("post-Apply state=%s did not fail-start after readback: events=%v", state, events)
			}
		})
	}
}

func TestPhase3SchedulerTerminalUsesDurableCompleteWithExactDuration(t *testing.T) {
	duration := int64(1234)
	terminalStarted := make(chan struct{}, 1)
	repository := &phase3LedgerRepository{
		jobs:            []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}},
		ensureErr:       map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:        map[domain.ArchaeologyNativePersistenceOperation]error{},
		completeStarted: terminalStarted,
	}
	launcher := &phase3SchedulerLauncher{
		result:   domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"},
		terminal: &domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "completed", DurationMS: &duration},
	}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.launch(repository.jobs[0])
	select {
	case <-terminalStarted:
	case <-time.After(time.Second):
		t.Fatal("complete intent was not applied")
	}
	_, intents, _, _, _, rawComplete, _, _ := repository.snapshot()
	var complete *domain.ArchaeologyNativePersistenceIntent
	for index := range intents {
		if intents[index].Operation == domain.ArchaeologyNativePersistenceCompleteTurn {
			complete = &intents[index]
		}
	}
	if complete == nil || complete.Status != "completed" || complete.ThreadID != "thread" || complete.TurnID != "turn" || complete.DurationMS == nil || *complete.DurationMS != duration {
		t.Fatalf("complete intent = %+v", complete)
	}
	if rawComplete != 0 {
		t.Fatalf("raw complete calls = %d", rawComplete)
	}
}

func TestPhase3SchedulerPersistenceFailureLeavesOneShotUncertainPath(t *testing.T) {
	repository := &phase3LedgerRepository{
		ensureErr: map[domain.ArchaeologyNativePersistenceOperation]error{
			domain.ArchaeologyNativePersistenceBindIdentity: errors.New("bind unavailable"),
		},
		applyErr: map[domain.ArchaeologyNativePersistenceOperation]error{},
	}
	launcher := &phase3SchedulerLauncher{result: domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	events, intents, rawBind, rawActivate, rawFail, _, _, _ := repository.snapshot()
	if len(intents) != 1 || intents[0].Operation != domain.ArchaeologyNativePersistenceFailStart || !intents[0].Uncertain {
		t.Fatalf("uncertain ledger intents=%+v events=%v", intents, events)
	}
	if rawBind != 0 || rawActivate != 0 || rawFail != 0 {
		t.Fatalf("raw calls bind=%d activate=%d fail=%d", rawBind, rawActivate, rawFail)
	}
	if _, _, interrupts := launcher.counts(); interrupts != 1 {
		t.Fatalf("interrupts = %d, want one", interrupts)
	}
	if !scheduler.Status().PersistenceFault {
		t.Fatal("persistence failure did not latch")
	}
}

func TestPhase3SchedulerPreLedgerTerminalEnsureRetriesUntilCommitted(t *testing.T) {
	repository := &phase3LedgerRepository{
		ensureFailures: map[domain.ArchaeologyNativePersistenceOperation][]error{
			domain.ArchaeologyNativePersistenceCompleteTurn: {errors.New("first ensure unavailable")},
		},
		ensureSignal: make(chan struct{}, 4),
		ensureErr:    map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:     map[domain.ArchaeologyNativePersistenceOperation]error{},
	}
	launcher := &phase3SchedulerLauncher{
		result:   domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"},
		terminal: &domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "completed"},
	}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	select {
	case <-repository.ensureSignal:
	case <-time.After(time.Second):
		t.Fatal("initial terminal Ensure was not attempted")
	}
	waitPhase3PendingIntents(t, scheduler, 1)
	scheduler.drain()
	_, intents, _, _, _, rawComplete, _, retries := repository.snapshot()
	complete := false
	for _, intent := range intents {
		if intent.Operation == domain.ArchaeologyNativePersistenceCompleteTurn {
			complete = true
		}
	}
	if !complete || rawComplete != 0 {
		t.Fatalf("terminal retry intents=%+v rawComplete=%d", intents, rawComplete)
	}
	if scheduler.Status().PersistenceFault || scheduler.Status().PersistenceAttention {
		t.Fatalf("healthy post-retry status=%+v", scheduler.Status())
	}
	if retries != 1 {
		t.Fatalf("RetryDue calls=%d, want one after pre-ledger commit", retries)
	}
}

func TestPhase3SchedulerRepeatedPreLedgerEnsureFailureBlocksClaims(t *testing.T) {
	repository := &phase3LedgerRepository{
		ensureFailures: map[domain.ArchaeologyNativePersistenceOperation][]error{
			domain.ArchaeologyNativePersistenceCompleteTurn: {errors.New("first"), errors.New("second")},
		},
		ensureSignal: make(chan struct{}, 4),
		ensureErr:    map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:     map[domain.ArchaeologyNativePersistenceOperation]error{},
		jobs:         []domain.ArchaeologyNativeJob{{ID: "later", CandidateID: "candidate"}},
	}
	launcher := &phase3SchedulerLauncher{
		result:   domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"},
		terminal: &domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "completed"},
	}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	select {
	case <-repository.ensureSignal:
	case <-time.After(time.Second):
		t.Fatal("initial terminal Ensure was not attempted")
	}
	waitPhase3PendingIntents(t, scheduler, 1)
	scheduler.drain()
	repository.mu.Lock()
	claims := repository.claims
	repository.mu.Unlock()
	if claims != 0 {
		t.Fatalf("claims with repeated pre-ledger failure=%d", claims)
	}
	if !scheduler.Status().PersistenceFault {
		t.Fatalf("repeated Ensure failure status=%+v", scheduler.Status())
	}
}

func TestPhase3SchedulerPreLedgerQueueCoversOnlyFiveReplaySafeOperations(t *testing.T) {
	operations := []domain.ArchaeologyNativePersistenceOperation{
		domain.ArchaeologyNativePersistenceFailStart,
		domain.ArchaeologyNativePersistenceBindIdentity,
		domain.ArchaeologyNativePersistenceActivate,
		domain.ArchaeologyNativePersistenceLoseTurn,
		domain.ArchaeologyNativePersistenceCompleteTurn,
	}
	repository := &phase3LedgerRepository{
		ensureErr:      map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:       map[domain.ArchaeologyNativePersistenceOperation]error{},
		ensureFailures: map[domain.ArchaeologyNativePersistenceOperation][]error{},
	}
	for _, operation := range operations {
		repository.ensureFailures[operation] = []error{errors.New("ensure unavailable")}
	}
	scheduler := &ArchaeologyScheduler{repository: repository, ctx: context.Background(), wake: make(chan struct{}, 1)}
	for index, operation := range operations {
		intent := domain.ArchaeologyNativePersistenceIntent{JobID: fmt.Sprintf("job-%d", index), Operation: operation}
		switch operation {
		case domain.ArchaeologyNativePersistenceFailStart:
			intent.Launch = domain.ArchaeologyLaunchResult{State: "failed"}
		case domain.ArchaeologyNativePersistenceBindIdentity:
			intent.ThreadID, intent.CodexSessionID, intent.TurnID = "thread", "session", "turn"
		case domain.ArchaeologyNativePersistenceActivate, domain.ArchaeologyNativePersistenceLoseTurn:
			intent.ThreadID, intent.TurnID = "thread", "turn"
		case domain.ArchaeologyNativePersistenceCompleteTurn:
			intent.ThreadID, intent.TurnID, intent.Status = "thread", "turn", "completed"
		}
		if err := scheduler.applyPersistenceIntent(context.Background(), intent); err == nil {
			t.Fatalf("Ensure unexpectedly succeeded for %s", operation)
		}
	}
	scheduler.persistenceMu.Lock()
	pending := len(scheduler.pendingIntents)
	scheduler.persistenceMu.Unlock()
	if pending != len(operations) {
		t.Fatalf("pre-ledger queue size=%d, want %d", pending, len(operations))
	}
	scheduler.drain()
	_, intents, _, _, _, _, _, _ := repository.snapshot()
	if len(intents) != len(operations) {
		t.Fatalf("committed intents=%d, want %d", len(intents), len(operations))
	}
	for index, intent := range intents {
		if intent.Operation != operations[index] {
			t.Fatalf("intent[%d]=%s, want insertion order %s", index, intent.Operation, operations[index])
		}
	}
	if status := scheduler.Status(); status.PersistenceFault || status.PersistenceAttention {
		t.Fatalf("post-queue status=%+v", status)
	}
}

func TestPhase3SchedulerPreLedgerQueueDoesNotStarveLaterIntent(t *testing.T) {
	const poisonCount = archaeologySchedulerPendingTerminalDrainLimit + 1
	repository := &phase3LedgerRepository{
		ensureErr: map[domain.ArchaeologyNativePersistenceOperation]error{
			domain.ArchaeologyNativePersistenceFailStart: errors.New("poison"),
		},
		ensureFailures: map[domain.ArchaeologyNativePersistenceOperation][]error{
			domain.ArchaeologyNativePersistenceCompleteTurn: {errors.New("first attempt only")},
		},
		applyErr: map[domain.ArchaeologyNativePersistenceOperation]error{},
	}
	scheduler := &ArchaeologyScheduler{repository: repository, ctx: context.Background(), wake: make(chan struct{}, 1)}
	for index := 0; index < poisonCount; index++ {
		intent := domain.ArchaeologyNativePersistenceIntent{
			JobID:     fmt.Sprintf("poison-%d", index),
			Operation: domain.ArchaeologyNativePersistenceFailStart,
			Launch:    domain.ArchaeologyLaunchResult{State: "failed"},
		}
		if err := scheduler.applyPersistenceIntent(context.Background(), intent); err == nil {
			t.Fatalf("poison Ensure unexpectedly succeeded for %s", intent.JobID)
		}
	}
	good := domain.ArchaeologyNativePersistenceIntent{
		JobID:     "good",
		Operation: domain.ArchaeologyNativePersistenceCompleteTurn,
		ThreadID:  "thread",
		TurnID:    "turn",
		Status:    "completed",
	}
	if err := scheduler.applyPersistenceIntent(context.Background(), good); err == nil {
		t.Fatal("good Ensure unexpectedly succeeded on its initial attempt")
	}

	// The first bounded pass examines only the poison entries. Rotation then
	// lets the next pass reach the previously starved good callback.
	scheduler.drain()
	_, intents, _, _, _, _, _, _ := repository.snapshot()
	if len(intents) != 0 {
		t.Fatalf("first bounded pass committed intents=%+v", intents)
	}
	scheduler.drain()
	_, intents, _, _, _, _, _, _ = repository.snapshot()
	if len(intents) != 1 || intents[0].Operation != domain.ArchaeologyNativePersistenceCompleteTurn || intents[0].JobID != good.JobID {
		t.Fatalf("later intent was starved, committed=%+v", intents)
	}
}

func waitPhase3PendingIntents(t *testing.T, scheduler *ArchaeologyScheduler, want int) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		scheduler.persistenceMu.Lock()
		got := len(scheduler.pendingIntents)
		scheduler.persistenceMu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("pending intents=%d, want at least %d", got, want)
		default:
			runtime.Gosched()
		}
	}
}

func TestPhase3SchedulerRetryDueFailureDoesNotSelfWake(t *testing.T) {
	repository := &phase3LedgerRepository{retryErr: errors.New("retry transport")}
	scheduler := &ArchaeologyScheduler{repository: repository, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.drain()
	select {
	case <-scheduler.wake:
		t.Fatal("RetryDue failure enqueued a self-wake")
	default:
	}
	repository.mu.Lock()
	first := repository.retryCalls
	repository.mu.Unlock()
	scheduler.drain()
	repository.mu.Lock()
	second := repository.retryCalls
	repository.mu.Unlock()
	if first != 1 || second != 2 {
		t.Fatalf("RetryDue calls without wake/tick = %d then %d", first, second)
	}
}

func TestPhase3SchedulerPendingLedgerBlocksClaimsBeforeAvailability(t *testing.T) {
	repository := &phase3LedgerRepository{
		jobs:      []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}},
		status:    domain.ArchaeologyNativePersistenceStatus{Pending: 1},
		ensureErr: map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:  map[domain.ArchaeologyNativePersistenceOperation]error{},
	}
	launcher := &phase3SchedulerLauncher{availableErr: domain.ErrUnavailable}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.drain()
	_, _, _, _, _, _, _, retries := repository.snapshot()
	repository.mu.Lock()
	claims := repository.claims
	repository.mu.Unlock()
	if claims != 0 {
		t.Fatalf("claims with pending ledger = %d", claims)
	}
	if retries != 1 {
		t.Fatalf("retry calls = %d, want one before status gate", retries)
	}
	status := scheduler.Status()
	if status.PersistenceFault || !status.PersistenceAttention {
		t.Fatalf("pending ledger status = %+v, want attention without fatal fault", status)
	}
}

func TestPhase3SchedulerDurableDrainPrecedesAndSharesProgressWithPreLedgerQueue(t *testing.T) {
	repository := &phase3LedgerRepository{
		ensureFailures: map[domain.ArchaeologyNativePersistenceOperation][]error{
			domain.ArchaeologyNativePersistenceBindIdentity: {errors.New("pre-ledger first attempt")},
		},
		ensureErr:        map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:         map[domain.ArchaeologyNativePersistenceOperation]error{},
		status:           domain.ArchaeologyNativePersistenceStatus{Pending: 1},
		recordRetryEvent: true,
	}
	scheduler := &ArchaeologyScheduler{repository: repository, ctx: context.Background(), wake: make(chan struct{}, 1)}
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: "job", Operation: domain.ArchaeologyNativePersistenceBindIdentity, ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}
	if err := scheduler.applyPersistenceIntent(context.Background(), intent); err == nil {
		t.Fatal("initial pre-ledger Ensure unexpectedly succeeded")
	}
	scheduler.drain()
	events, intents, _, _, _, _, _, retries := repository.snapshot()
	if retries != 1 {
		t.Fatalf("RetryDue calls=%d, want one before pre-ledger work", retries)
	}
	if len(intents) != 1 || intents[0].Operation != domain.ArchaeologyNativePersistenceBindIdentity {
		t.Fatalf("pre-ledger queue did not make bounded progress: intents=%+v", intents)
	}
	if len(events) < 4 || events[1] != "retry" || events[2] != "ensure:bind_identity" || events[3] != "apply" {
		t.Fatalf("durable/pre-ledger order=%v, want retry before retryable Ensure/Apply", events)
	}
	repository.mu.Lock()
	claims := repository.claims
	repository.mu.Unlock()
	if claims != 0 {
		t.Fatalf("claim proceeded with durable backlog: %d", claims)
	}
}

func TestPhase3SchedulerAppliedLedgerFailureIsAttentionNotFatalFault(t *testing.T) {
	repository := &phase3LedgerRepository{
		ensureErr: map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr: map[domain.ArchaeologyNativePersistenceOperation]error{
			domain.ArchaeologyNativePersistenceBindIdentity: errors.New("ack unavailable"),
		},
	}
	launcher := &phase3SchedulerLauncher{result: domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	status := scheduler.Status()
	if status.PersistenceFault || !status.PersistenceAttention {
		t.Fatalf("Apply failure status=%+v, want attention without fatal fault", status)
	}
}

type phase3ManualTicker struct {
	ch      chan time.Time
	stopped chan struct{}
}

func (t *phase3ManualTicker) Chan() <-chan time.Time { return t.ch }
func (t *phase3ManualTicker) Stop()                  { nonBlockingPhase3Signal(t.stopped) }

func TestPhase3SchedulerTickerRetriesTransientClaimAndStopsAfterClose(t *testing.T) {
	repository := &phase3LedgerRepository{jobs: []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}}}
	var claimMu sync.Mutex
	claimCalls := 0
	// Replace the embedded claim behavior through a small wrapper so the first
	// wake returns a transient transport error and the next tick can proceed.
	claiming := &phase3TransientClaimRepository{phase3LedgerRepository: repository, claimCalls: &claimCalls, claimMu: &claimMu, claimSignal: make(chan struct{}, 4)}
	launcher := &phase3SchedulerLauncher{result: domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}}
	ticker := &phase3ManualTicker{ch: make(chan time.Time, 2), stopped: make(chan struct{}, 1)}
	scheduler := newArchaeologySchedulerWithTicker(context.Background(), nil, claiming, launcher, domain.HumanLocalPrincipal, func(time.Duration) archaeologySchedulerTicker { return ticker })
	scheduler.Wake()
	waitPhase3ClaimCount(t, claiming, 1)
	if scheduler.Status().PersistenceFault {
		t.Fatal("transient claim error latched persistence fault")
	}
	ticker.ch <- time.Now()
	waitPhase3ClaimCount(t, claiming, 2)
	if launches, _, _ := launcher.counts(); launches != 1 {
		t.Fatalf("launches after transient retry = %d", launches)
	}
	claimMu.Lock()
	beforeClose := claimCalls
	claimMu.Unlock()
	scheduler.Close()
	select {
	case <-ticker.stopped:
	default:
		t.Fatal("ticker was not stopped on Close")
	}
	ticker.ch <- time.Now()
	select {
	case <-time.After(20 * time.Millisecond):
	}
	claimMu.Lock()
	got := claimCalls
	claimMu.Unlock()
	if got != beforeClose {
		t.Fatalf("claims after Close = %d, want unchanged at %d", got, beforeClose)
	}
}

func TestPhase3SchedulerCloseCancelsBoundedClaimAndTerminalAdmission(t *testing.T) {
	ctx := context.Background()
	repository := &phase3LedgerRepository{claimStarted: make(chan struct{}, 1), claimWaitContext: true}
	ticker := &phase3ManualTicker{ch: make(chan time.Time, 1), stopped: make(chan struct{}, 1)}
	scheduler := newArchaeologySchedulerWithTicker(ctx, nil, repository, &phase3SchedulerLauncher{}, domain.HumanLocalPrincipal, func(time.Duration) archaeologySchedulerTicker { return ticker })
	scheduler.Wake()
	select {
	case <-repository.claimStarted:
	case <-time.After(time.Second):
		t.Fatal("Claim did not reach the context-aware boundary")
	}
	repository.mu.Lock()
	claimCtx := repository.claimContext
	repository.mu.Unlock()
	if claimCtx == nil {
		t.Fatal("Claim did not receive a context")
	}
	if _, ok := claimCtx.Deadline(); !ok {
		t.Fatal("Claim context is not independently bounded")
	}

	admissionDone := make(chan struct{})
	go func() {
		_ = scheduler.beginTerminalCallback()
		close(admissionDone)
	}()
	closed := make(chan struct{})
	go func() {
		scheduler.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close deadlocked behind context-aware Claim")
	}
	select {
	case <-admissionDone:
	case <-time.After(time.Second):
		t.Fatal("terminal admission remained blocked after Close")
	}
	select {
	case <-ticker.stopped:
	default:
		t.Fatal("ticker was not stopped by Close")
	}
	if err := claimCtx.Err(); err == nil {
		t.Fatal("Close did not cancel Claim context")
	}
}

type phase3TransientClaimRepository struct {
	*phase3LedgerRepository
	claimCalls  *int
	claimMu     *sync.Mutex
	claimSignal chan struct{}
}

func (r *phase3TransientClaimRepository) ClaimArchaeologyNativeJob(ctx context.Context) (domain.ArchaeologyNativeJob, error) {
	r.claimMu.Lock()
	*r.claimCalls++
	call := *r.claimCalls
	r.claimMu.Unlock()
	nonBlockingPhase3Signal(r.claimSignal)
	if call == 1 {
		return domain.ArchaeologyNativeJob{}, errors.New("temporary claim transport")
	}
	return r.phase3LedgerRepository.ClaimArchaeologyNativeJob(ctx)
}

func waitPhase3ClaimCount(t *testing.T, repository *phase3TransientClaimRepository, want int) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	seen := 0
	for {
		if seen >= want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("claim count=%d, want at least %d", seen, want)
		case <-repository.claimSignal:
			seen++
		default:
			runtime.Gosched()
		}
	}
}

func TestPhase3SchedulerCloseWaitsIndependentTerminalPersistence(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	duration := int64(7)
	repository := &phase3LedgerRepository{
		ensureErr:       map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:        map[domain.ArchaeologyNativePersistenceOperation]error{},
		completeStarted: started,
		completeRelease: release,
	}
	launcher := &phase3SchedulerLauncher{
		result:   domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"},
		terminal: &domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "completed", DurationMS: &duration},
	}
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("terminal persistence did not start")
	}
	closed := make(chan struct{})
	go func() {
		scheduler.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before accepted terminal persistence")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not await terminal persistence")
	}
}

func TestPhase3SchedulerTerminalAdmissionClosesClaimGate(t *testing.T) {
	admitted := make(chan struct{})
	releaseAdmission := make(chan struct{})
	completeStarted := make(chan struct{}, 1)
	completeRelease := make(chan struct{})
	repository := &phase3LedgerRepository{
		jobs:            []domain.ArchaeologyNativeJob{{ID: "later", CandidateID: "candidate"}},
		ensureErr:       map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:        map[domain.ArchaeologyNativePersistenceOperation]error{},
		completeStarted: completeStarted,
		completeRelease: completeRelease,
	}
	launcher := &phase3SchedulerLauncher{
		result:       domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"},
		terminal:     &domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "completed"},
		terminalOnce: true,
	}
	scheduler := &ArchaeologyScheduler{
		repository: repository,
		launcher:   launcher,
		principal:  domain.HumanLocalPrincipal,
		ctx:        context.Background(),
		wake:       make(chan struct{}, 1),
		terminalAdmissionHook: func() {
			close(admitted)
			<-releaseAdmission
		},
	}
	launchDone := make(chan struct{})
	go func() {
		scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
		close(launchDone)
	}()
	select {
	case <-admitted:
	case <-time.After(time.Second):
		t.Fatal("terminal callback was not admitted")
	}

	drainDone := make(chan struct{})
	go func() {
		scheduler.drain()
		close(drainDone)
	}()
	select {
	case <-drainDone:
		t.Fatal("drain passed the claim gate during terminal admission")
	case <-time.After(20 * time.Millisecond):
	}
	repository.mu.Lock()
	claims := repository.claims
	repository.mu.Unlock()
	if claims != 0 {
		t.Fatalf("claims during terminal admission=%d, want zero", claims)
	}

	close(releaseAdmission)
	select {
	case <-completeStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal persistence did not reach the controlled boundary")
	}
	repository.mu.Lock()
	claims = repository.claims
	repository.mu.Unlock()
	if claims != 0 {
		t.Fatalf("claims while terminal persistence was pending=%d, want zero", claims)
	}
	close(completeRelease)
	select {
	case <-drainDone:
	case <-time.After(time.Second):
		t.Fatal("drain did not finish after terminal persistence release")
	}
	select {
	case <-launchDone:
	case <-time.After(time.Second):
		t.Fatal("launch did not finish after terminal persistence release")
	}
	scheduler.Close()
}

func TestPhase3SchedulerTerminalTimeoutStartsAfterBound(t *testing.T) {
	boundReady := make(chan struct{})
	completeStarted := make(chan struct{}, 1)
	repository := &phase3LedgerRepository{
		ensureErr:       map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:        map[domain.ArchaeologyNativePersistenceOperation]error{},
		completeStarted: completeStarted,
	}
	finalizeStarted := make(chan struct{}, 1)
	finalizeRelease := make(chan struct{})
	launcher := &phase3SchedulerLauncher{
		result:          domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"},
		terminal:        &domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "completed"},
		finalizeStarted: finalizeStarted,
		finalizeRelease: finalizeRelease,
	}
	factoryBeforeBound := false
	scheduler := &ArchaeologyScheduler{
		repository:      repository,
		launcher:        launcher,
		principal:       domain.HumanLocalPrincipal,
		ctx:             context.Background(),
		wake:            make(chan struct{}, 1),
		callbackTimeout: time.Nanosecond,
		terminalBoundHook: func() {
			select {
			case <-boundReady:
			default:
				close(boundReady)
			}
		},
		callbackContextFactory: func() (context.Context, context.CancelFunc) {
			select {
			case <-boundReady:
				return context.WithCancel(context.Background())
			default:
				factoryBeforeBound = true
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			}
		},
	}
	launchDone := make(chan struct{})
	go func() {
		scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
		close(launchDone)
	}()
	select {
	case <-finalizeStarted:
	case <-time.After(time.Second):
		t.Fatal("Finalize did not reach controlled delay")
	}
	close(finalizeRelease)
	select {
	case <-launchDone:
	case <-time.After(time.Second):
		t.Fatal("launch did not finish after controlled delay")
	}
	select {
	case <-completeStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal intent was dropped after delayed binding")
	}
	if factoryBeforeBound {
		t.Fatal("terminal persistence context was created before bound")
	}
	scheduler.Close()
}

func TestPhase3SchedulerInterruptHasIndependentBudget(t *testing.T) {
	repository := &phase3LedgerRepository{
		ensureErr: map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:  map[domain.ArchaeologyNativePersistenceOperation]error{},
	}
	launcher := &phase3SchedulerLauncher{
		result:      domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"},
		finalizeErr: errors.New("finalize readback failed"),
	}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	repository.mu.Lock()
	failContext := repository.lastFailContext
	repository.mu.Unlock()
	launcher.mu.Lock()
	interruptContext := launcher.interruptContext
	interrupts := launcher.interrupts
	launcher.mu.Unlock()
	if failContext == nil || interruptContext == nil || failContext == interruptContext {
		t.Fatalf("fail/interrupt contexts not independent: fail=%v interrupt=%v", failContext, interruptContext)
	}
	if _, ok := failContext.Deadline(); !ok {
		t.Fatal("fail-start context is not bounded")
	}
	if _, ok := interruptContext.Deadline(); !ok {
		t.Fatal("interrupt context is not bounded")
	}
	if interrupts != 1 {
		t.Fatalf("interrupts=%d, want one", interrupts)
	}
}

func TestPhase3SchedulerCancelAndFailureInterruptAtMostOnce(t *testing.T) {
	job := domain.ArchaeologyNativeJob{ID: "job", ThreadID: "thread", TurnID: "turn"}
	repository := &cancelAllNativeRepository{jobs: []domain.ArchaeologyNativeJob{job}, value: domain.ArchaeologySession{ID: "session"}}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	launcher := &phase3SchedulerLauncher{interruptStarted: started, interruptRelease: release}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, ctx: context.Background(), wake: make(chan struct{}, 1)}
	firstDone := make(chan struct{})
	go func() {
		_ = scheduler.interruptNativeOnce(job)
		close(firstDone)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first interrupt did not reach external boundary")
	}
	cancelDone := make(chan struct{})
	go func() {
		_, _ = scheduler.Cancel(context.Background(), domain.HumanLocalPrincipal, "cancel", 1)
		close(cancelDone)
	}()
	select {
	case <-cancelDone:
	case <-time.After(time.Second):
		t.Fatal("Cancel did not deduplicate in-flight interrupt")
	}
	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first interrupt did not finish")
	}
	launcher.mu.Lock()
	interrupts := launcher.interrupts
	launcher.mu.Unlock()
	if interrupts != 1 {
		t.Fatalf("interrupt attempts=%d, want one", interrupts)
	}
}

func TestPhase3SchedulerCancelExpectedErrorsDoNotLatch(t *testing.T) {
	expected := []error{domain.ErrConflict, domain.ErrInvalid, domain.ErrNotFound, context.Canceled, context.DeadlineExceeded}
	for _, want := range expected {
		t.Run(want.Error(), func(t *testing.T) {
			repository := &phase3LedgerRepository{cancelErr: want}
			scheduler := &ArchaeologyScheduler{repository: repository, launcher: &phase3SchedulerLauncher{}, ctx: context.Background(), wake: make(chan struct{}, 1)}
			_, err := scheduler.Cancel(context.Background(), domain.HumanLocalPrincipal, "cancel", 1)
			if !errors.Is(err, want) {
				t.Fatalf("Cancel error=%v, want %v", err, want)
			}
			if scheduler.Status().PersistenceFault {
				t.Fatalf("expected Cancel error latched persistence fault: %v", want)
			}
		})
	}

	t.Run("unexpected storage error latches", func(t *testing.T) {
		repository := &phase3LedgerRepository{cancelErr: errors.New("database unavailable")}
		scheduler := &ArchaeologyScheduler{repository: repository, launcher: &phase3SchedulerLauncher{}, ctx: context.Background(), wake: make(chan struct{}, 1)}
		_, _ = scheduler.Cancel(context.Background(), domain.HumanLocalPrincipal, "cancel", 1)
		if !scheduler.Status().PersistenceFault {
			t.Fatal("unexpected Cancel storage error did not latch")
		}
	})

	t.Run("post-cancel session read error latches", func(t *testing.T) {
		repository := &phase3LedgerRepository{sessionErr: errors.New("session read failed")}
		scheduler := &ArchaeologyScheduler{repository: repository, launcher: &phase3SchedulerLauncher{}, ctx: context.Background(), wake: make(chan struct{}, 1)}
		_, _ = scheduler.Cancel(context.Background(), domain.HumanLocalPrincipal, "cancel", 1)
		if !scheduler.Status().PersistenceFault {
			t.Fatal("unexpected post-cancel storage error did not latch")
		}
	})
}

func TestPhase3SchedulerCancelEnsureFailureCannotRaceEmptyLedgerClaim(t *testing.T) {
	ensureStarted := make(chan struct{}, 1)
	ensureRelease := make(chan struct{})
	claimJob := domain.ArchaeologyNativeJob{ID: "later", CandidateID: "candidate"}
	repository := &phase3LedgerRepository{
		jobs:       []domain.ArchaeologyNativeJob{claimJob},
		cancelJobs: []domain.ArchaeologyNativeJob{{ID: "cancelled", ThreadID: "thread", TurnID: "turn"}},
		ensureFailures: map[domain.ArchaeologyNativePersistenceOperation][]error{
			domain.ArchaeologyNativePersistenceLoseTurn: {errors.New("pre-ledger unavailable")},
		},
		ensureErr:     map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:      map[domain.ArchaeologyNativePersistenceOperation]error{},
		ensureBlockOp: domain.ArchaeologyNativePersistenceLoseTurn,
		ensureStarted: ensureStarted,
		ensureRelease: ensureRelease,
	}
	launcher := &phase3SchedulerLauncher{interruptErr: errors.New("interrupt unavailable")}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, ctx: context.Background(), wake: make(chan struct{}, 1)}
	cancelDone := make(chan struct{})
	go func() {
		_, _ = scheduler.Cancel(context.Background(), domain.HumanLocalPrincipal, "cancel", 1)
		close(cancelDone)
	}()
	select {
	case <-ensureStarted:
	case <-time.After(time.Second):
		t.Fatal("Cancel did not reach the blocked durable Ensure")
	}

	// The in-flight persistence counter closes the claim gate here. A
	// concurrent drain may run its read/retry pass, but it must not claim the
	// later job before the failed intent is enqueued.
	drainDone := make(chan struct{})
	go func() {
		scheduler.drain()
		close(drainDone)
	}()
	select {
	case <-repository.ensureSignal:
		// ensureSignal is not configured for this test; keep the select useful if
		// a future fixture adds one.
	case <-time.After(20 * time.Millisecond):
	}
	repository.mu.Lock()
	claimsWhileBlocked := repository.claims
	repository.mu.Unlock()
	if claimsWhileBlocked != 0 {
		t.Fatalf("claim raced blocked pre-ledger Ensure: claims=%d", claimsWhileBlocked)
	}
	select {
	case <-drainDone:
	default:
	}

	close(ensureRelease)
	select {
	case <-cancelDone:
	case <-time.After(time.Second):
		t.Fatal("Cancel did not finish after Ensure release")
	}
	// The first drain may have observed the in-flight operation and returned
	// without waiting. The coalesced wake would drive this in production; issue
	// the deterministic equivalent here so the newly enqueued intent is retried.
	scheduler.drain()
	select {
	case <-drainDone:
	case <-time.After(time.Second):
		t.Fatal("drain did not finish after Ensure release")
	}
	events, intents, _, _, _, _, rawLose, _ := repository.snapshot()
	if len(intents) == 0 || intents[0].Operation != domain.ArchaeologyNativePersistenceLoseTurn || rawLose != 0 {
		t.Fatalf("failed Cancel intent was not durably retried before claim: events=%v intents=%+v rawLose=%d", events, intents, rawLose)
	}
	loseEnsures := 0
	firstOtherEnsure := len(events)
	for index, event := range events {
		if event == "ensure:lose_turn" {
			loseEnsures++
		}
		if event == "ensure:bind_identity" && firstOtherEnsure == len(events) {
			firstOtherEnsure = index
		}
	}
	if loseEnsures < 2 || firstOtherEnsure < 2 {
		t.Fatalf("claim or later mutation preceded lose retry: events=%v", events)
	}
}

func TestPhase3SchedulerPartialIdentityLaunchFailureIsUncertain(t *testing.T) {
	repository := &phase3LedgerRepository{
		ensureErr: map[domain.ArchaeologyNativePersistenceOperation]error{},
		applyErr:  map[domain.ArchaeologyNativePersistenceOperation]error{},
	}
	launcher := &phase3SchedulerLauncher{
		result:    domain.ArchaeologyLaunchResult{CodexSessionID: "session-only"},
		launchErr: errors.New("request response unavailable"),
	}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	_, intents, _, _, _, _, _, _ := repository.snapshot()
	if len(intents) != 1 || intents[0].Operation != domain.ArchaeologyNativePersistenceFailStart || !intents[0].Uncertain {
		t.Fatalf("partial identity fail-start intents=%+v", intents)
	}
}

func TestPhase3SchedulerNilErrorIncompleteLaunchResultFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		result     domain.ArchaeologyLaunchResult
		interrupts int
	}{
		{name: "uncertain state", result: domain.ArchaeologyLaunchResult{State: "uncertain", ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}, interrupts: 1},
		{name: "missing thread", result: domain.ArchaeologyLaunchResult{CodexSessionID: "session", TurnID: "turn"}},
		{name: "missing session", result: domain.ArchaeologyLaunchResult{ThreadID: "thread", TurnID: "turn"}, interrupts: 1},
		{name: "missing turn", result: domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &phase3LedgerRepository{
				ensureErr: map[domain.ArchaeologyNativePersistenceOperation]error{},
				applyErr:  map[domain.ArchaeologyNativePersistenceOperation]error{},
			}
			launcher := &phase3SchedulerLauncher{result: test.result}
			scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background(), wake: make(chan struct{}, 1)}
			scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
			events, intents, _, _, _, _, _, _ := repository.snapshot()
			if len(intents) != 1 || intents[0].Operation != domain.ArchaeologyNativePersistenceFailStart || !intents[0].Uncertain {
				t.Fatalf("invalid nil-error result was not uncertain fail-start: events=%v intents=%+v", events, intents)
			}
			launches, finalizes, interrupts := launcher.counts()
			if launches != 1 || finalizes != 0 || interrupts != test.interrupts {
				t.Fatalf("external calls launches=%d finalizes=%d interrupts=%d, want launches=1 finalizes=0 interrupts=%d", launches, finalizes, interrupts, test.interrupts)
			}
			if strings.Contains(fmt.Sprint(events), "bind_identity") || strings.Contains(fmt.Sprint(events), "activate") {
				t.Fatalf("invalid nil-error result advanced lifecycle: events=%v", events)
			}
		})
	}
}
