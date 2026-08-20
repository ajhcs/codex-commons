package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

type phase2SchedulerRepository struct {
	ArchaeologyNativeRepository

	mu             sync.Mutex
	jobs           []domain.ArchaeologyNativeJob
	claims         int
	launches       []string
	bindErr        error
	activateErr    error
	failErr        error
	completeErr    error
	loseErr        error
	completeCalls  int
	loseCalls      int
	reconcileErr   error
	reconcileCalls int
	claimSignal    chan struct{}
	emptySignal    chan struct{}
	terminalSignal chan struct{}
}

func (r *phase2SchedulerRepository) ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.jobs) == 0 {
		return domain.ArchaeologySession{Candidates: []domain.ArchaeologyCandidate{{ID: "candidate", Name: "Candidate"}}}, nil
	}
	return domain.ArchaeologySession{Candidates: []domain.ArchaeologyCandidate{{ID: r.jobs[0].CandidateID, Name: "Candidate"}}}, nil
}

func (r *phase2SchedulerRepository) ClaimArchaeologyNativeJob(ctx context.Context) (domain.ArchaeologyNativeJob, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return domain.ArchaeologyNativeJob{}, err
		}
	}
	r.mu.Lock()
	r.claims++
	if len(r.jobs) == 0 {
		r.mu.Unlock()
		if r.emptySignal != nil {
			nonBlockingPhase2Signal(r.emptySignal)
		}
		return domain.ArchaeologyNativeJob{}, domain.ErrConflict
	}
	job := r.jobs[0]
	r.jobs = r.jobs[1:]
	r.mu.Unlock()
	if r.claimSignal != nil {
		nonBlockingPhase2Signal(r.claimSignal)
	}
	return job, nil
}

func (r *phase2SchedulerRepository) BindArchaeologyNativeIdentity(context.Context, string, string, string, string) error {
	r.mu.Lock()
	err := r.bindErr
	r.mu.Unlock()
	return err
}

func (r *phase2SchedulerRepository) ActivateArchaeologyNativeJob(context.Context, string, string, string) error {
	r.mu.Lock()
	err := r.activateErr
	r.mu.Unlock()
	return err
}

func (r *phase2SchedulerRepository) FailArchaeologyNativeStart(context.Context, string, domain.ArchaeologyLaunchResult, bool) error {
	r.mu.Lock()
	err := r.failErr
	r.mu.Unlock()
	return err
}

func (r *phase2SchedulerRepository) UpdateArchaeologyNativeProgress(context.Context, domain.ArchaeologyNativeProgress) error {
	return nil
}

func (r *phase2SchedulerRepository) ReportArchaeologyNativeJob(context.Context, domain.ArchaeologyNativeReport) error {
	return nil
}

func (r *phase2SchedulerRepository) CompleteArchaeologyNativeTurn(context.Context, domain.ArchaeologyNativeTerminal) error {
	r.mu.Lock()
	r.completeCalls++
	err := r.completeErr
	r.mu.Unlock()
	if r.terminalSignal != nil {
		nonBlockingPhase2Signal(r.terminalSignal)
	}
	return err
}

func (r *phase2SchedulerRepository) LoseArchaeologyNativeTurn(context.Context, string, string, string) error {
	r.mu.Lock()
	r.loseCalls++
	err := r.loseErr
	r.mu.Unlock()
	if r.terminalSignal != nil {
		nonBlockingPhase2Signal(r.terminalSignal)
	}
	return err
}

func (r *phase2SchedulerRepository) ReconcileArchaeologyNativePersistence(context.Context) error {
	r.mu.Lock()
	r.reconcileCalls++
	err := r.reconcileErr
	r.mu.Unlock()
	return err
}

func (r *phase2SchedulerRepository) claimCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.claims
}

func (r *phase2SchedulerRepository) terminalCounts() (complete, lose int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completeCalls, r.loseCalls
}

type phase2SchedulerLauncher struct {
	mu sync.Mutex

	availabilityErr         error
	availabilityCalls       int
	availabilityLog         chan struct{}
	availabilityGateEntered chan struct{}
	availabilityGateRelease chan struct{}
	launchErr               error
	finalizeErr             error
	launchResult            domain.ArchaeologyLaunchResult
	terminal                *domain.ArchaeologyNativeTerminal
	launchSignal            chan struct{}
	launches                int
}

func (l *phase2SchedulerLauncher) Available(ctx context.Context) error {
	l.mu.Lock()
	l.availabilityCalls++
	log := l.availabilityLog
	entered := l.availabilityGateEntered
	release := l.availabilityGateRelease
	l.mu.Unlock()
	if entered != nil {
		nonBlockingPhase2Signal(entered)
	}
	if release != nil {
		if ctx == nil {
			<-release
		} else {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}
	l.mu.Lock()
	err := l.availabilityErr
	l.mu.Unlock()
	if log != nil {
		nonBlockingPhase2Signal(log)
	}
	return err
}

func (l *phase2SchedulerLauncher) LaunchNative(_ context.Context, job domain.ArchaeologyNativeJob, _ domain.ArchaeologySession, _ domain.ArchaeologyCandidate, _ func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, onTerminal func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	l.mu.Lock()
	l.launches++
	result := l.launchResult
	if result.ThreadID == "" {
		result.ThreadID = "thread-" + job.ID
	}
	if result.CodexSessionID == "" {
		result.CodexSessionID = "session-" + job.ID
	}
	if result.TurnID == "" {
		result.TurnID = "turn-" + job.ID
	}
	err := l.launchErr
	terminal := l.terminal
	signal := l.launchSignal
	l.mu.Unlock()
	if signal != nil {
		nonBlockingPhase2Signal(signal)
	}
	if terminal != nil {
		onTerminal(*terminal)
	}
	return result, err
}

func (*phase2SchedulerLauncher) InterruptNative(context.Context, domain.ArchaeologyNativeJob) error {
	return nil
}

func (l *phase2SchedulerLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.finalizeErr
}

func (l *phase2SchedulerLauncher) launchCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.launches
}

func (l *phase2SchedulerLauncher) availabilityCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.availabilityCalls
}

func (l *phase2SchedulerLauncher) setAvailability(err error) {
	l.mu.Lock()
	l.availabilityErr = err
	l.mu.Unlock()
}

func nonBlockingPhase2Signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func phase2SchedulerForTest(ctx context.Context, repository *phase2SchedulerRepository, launcher *phase2SchedulerLauncher) *ArchaeologyScheduler {
	return &ArchaeologyScheduler{
		repository: repository,
		launcher:   launcher,
		principal:  domain.HumanLocalPrincipal,
		ctx:        ctx,
		wake:       make(chan struct{}, 1),
	}
}

func TestPhase2SchedulerClaimsZeroWhileCodexUnavailable(t *testing.T) {
	ctx := context.Background()
	repository := &phase2SchedulerRepository{jobs: []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}}}
	launcher := &phase2SchedulerLauncher{availabilityErr: domain.ErrUnavailable}
	scheduler := phase2SchedulerForTest(ctx, repository, launcher)

	scheduler.drain()

	if got := repository.claimCount(); got != 0 {
		t.Fatalf("claims while unavailable = %d, want zero", got)
	}
	if got := launcher.launchCount(); got != 0 {
		t.Fatalf("launches while unavailable = %d, want zero", got)
	}
}

func TestPhase2SchedulerServiceWakeForwardsWithoutBlocking(t *testing.T) {
	var nilService *Service
	nilService.WakeNativeProjectArchaeologyScheduler()

	scheduler := &ArchaeologyScheduler{ctx: context.Background(), wake: make(chan struct{}, 1)}
	service := &Service{archaeologyScheduler: scheduler}
	service.WakeNativeProjectArchaeologyScheduler()
	select {
	case <-scheduler.wake:
	default:
		t.Fatal("service wake did not reach scheduler")
	}

	// A recovery callback must remain nonblocking when a wake is already
	// pending; the scheduler's one-slot signal is deliberately coalescing.
	scheduler.wake <- struct{}{}
	done := make(chan struct{})
	go func() {
		service.WakeNativeProjectArchaeologyScheduler()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("service wake blocked on a pending scheduler signal")
	}
}

func TestPhase2SchedulerRecoveryWakeDrainsQueuedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repository := &phase2SchedulerRepository{
		jobs:        []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}},
		claimSignal: make(chan struct{}, 1),
	}
	launcher := &phase2SchedulerLauncher{
		availabilityErr: domain.ErrUnavailable,
		availabilityLog: make(chan struct{}, 4),
		launchSignal:    make(chan struct{}, 1),
	}
	scheduler := newArchaeologyScheduler(ctx, nil, repository, launcher, domain.HumanLocalPrincipal)
	defer scheduler.Close()

	scheduler.Wake()
	select {
	case <-launcher.availabilityLog:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not inspect unavailable launcher")
	}
	if got := repository.claimCount(); got != 0 {
		t.Fatalf("claims while unavailable = %d, want zero", got)
	}

	launcher.setAvailability(nil)
	scheduler.Wake()
	select {
	case <-repository.claimSignal:
	case <-time.After(time.Second):
		t.Fatal("recovery wake did not claim queued work")
	}
	select {
	case <-launcher.launchSignal:
	case <-time.After(time.Second):
		t.Fatal("recovery wake did not launch queued work")
	}
	if got := launcher.launchCount(); got != 1 {
		t.Fatalf("launches after recovery = %d, want one", got)
	}
	if got := launcher.availabilityCount(); got < 2 {
		t.Fatalf("availability inspections = %d, want a fresh check before the claim", got)
	}
}

func TestPhase2SchedulerRechecksAvailabilityAfterWakeBeforeClaim(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repository := &phase2SchedulerRepository{jobs: []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}}}
	launcher := &phase2SchedulerLauncher{
		availabilityLog:         make(chan struct{}, 1),
		availabilityGateEntered: make(chan struct{}, 1),
		availabilityGateRelease: make(chan struct{}),
	}
	scheduler := newArchaeologyScheduler(ctx, nil, repository, launcher, domain.HumanLocalPrincipal)
	defer scheduler.Close()

	// Hold the immediate availability inspection after the loop wake. Changing
	// the launcher state before releasing that inspection must still prevent a
	// claim; the scheduler cannot rely on a capability result cached at wake.
	scheduler.Wake()
	select {
	case <-launcher.availabilityGateEntered:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not reach availability gate")
	}
	launcher.setAvailability(domain.ErrUnavailable)
	close(launcher.availabilityGateRelease)
	select {
	case <-launcher.availabilityLog:
	case <-time.After(time.Second):
		t.Fatal("availability inspection did not return")
	}

	if got := repository.claimCount(); got != 0 {
		t.Fatalf("claims after availability changed = %d, want zero", got)
	}
}

func TestPhase2SchedulerPersistenceFaultStopsFurtherClaims(t *testing.T) {
	ctx := context.Background()
	repository := &phase2SchedulerRepository{
		jobs:    []domain.ArchaeologyNativeJob{{ID: "job-1", CandidateID: "candidate"}, {ID: "job-2", CandidateID: "candidate"}},
		bindErr: errors.New("secret persistence transport detail"),
	}
	launcher := &phase2SchedulerLauncher{}
	scheduler := phase2SchedulerForTest(ctx, repository, launcher)

	scheduler.drain()

	if got := repository.claimCount(); got != 1 {
		t.Fatalf("claims after persistence failure = %d, want one", got)
	}
	if got := scheduler.PersistenceError(); !errors.Is(got, ErrArchaeologySchedulerPersistenceFault) {
		t.Fatalf("persistence error = %v, want scheduler fault", got)
	}
	status := scheduler.Status()
	if !status.PersistenceFault || status.Error == "" || status.Error == "secret persistence transport detail" {
		t.Fatalf("unsafe persistence status = %+v", status)
	}
	if err := scheduler.Available(ctx); !errors.Is(err, ErrArchaeologySchedulerPersistenceFault) {
		t.Fatalf("availability after persistence fault = %v", err)
	}

	repository.reconcileErr = errors.New("reconciliation still unavailable")
	if err := scheduler.ReconcilePersistence(ctx); err == nil {
		t.Fatal("failed reconciliation cleared persistence fault")
	}
	if !scheduler.Status().PersistenceFault {
		t.Fatal("failed reconciliation cleared persistence fault")
	}
	repository.reconcileErr = nil
	if err := scheduler.ReconcilePersistence(ctx); err != nil {
		t.Fatalf("successful reconciliation: %v", err)
	}
	if scheduler.Status().PersistenceFault {
		t.Fatal("successful reconciliation left persistence fault latched")
	}
}

func TestPhase2SchedulerTerminalPersistenceFaultsLatchAndBlockClaims(t *testing.T) {
	for _, test := range []struct {
		name        string
		terminal    domain.ArchaeologyNativeTerminal
		completeErr error
		loseErr     error
		failErr     error
		launchErr   error
	}{
		{name: "complete", terminal: domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "completed"}, completeErr: errors.New("complete failed")},
		{name: "lose", terminal: domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "unavailable"}, loseErr: errors.New("lose failed")},
		{name: "fail", failErr: errors.New("fail failed"), launchErr: errors.New("launch response lost")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repository := &phase2SchedulerRepository{
				jobs:           []domain.ArchaeologyNativeJob{{ID: "job-1", CandidateID: "candidate"}, {ID: "job-2", CandidateID: "candidate"}},
				completeErr:    test.completeErr,
				loseErr:        test.loseErr,
				failErr:        test.failErr,
				terminalSignal: make(chan struct{}, 1),
			}
			launcher := &phase2SchedulerLauncher{launchErr: test.launchErr}
			if test.terminal.Status != "" {
				launcher.terminal = &test.terminal
			}
			scheduler := phase2SchedulerForTest(ctx, repository, launcher)

			job, err := repository.ClaimArchaeologyNativeJob(ctx)
			if err != nil {
				t.Fatalf("claim terminal test job: %v", err)
			}
			scheduler.launch(job)
			if test.terminal.Status != "" {
				select {
				case <-repository.terminalSignal:
				case <-time.After(time.Second):
					t.Fatal("terminal persistence was not attempted")
				}
			}
			if !scheduler.Status().PersistenceFault {
				t.Fatal("persistence failure did not latch")
			}
			scheduler.drain()
			if got := repository.claimCount(); got != 1 {
				t.Fatalf("claims after terminal persistence failure = %d, want one", got)
			}
		})
	}
}

func TestPhase2SchedulerFaultDoesNotDropAlreadyClaimedTerminalWrite(t *testing.T) {
	ctx := context.Background()
	repository := &phase2SchedulerRepository{
		jobs:           []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}},
		terminalSignal: make(chan struct{}, 1),
	}
	launcher := &phase2SchedulerLauncher{terminal: &domain.ArchaeologyNativeTerminal{ThreadID: "thread", TurnID: "turn", Status: "completed"}}
	scheduler := phase2SchedulerForTest(ctx, repository, launcher)
	scheduler.recordPersistenceFailure(errors.New("prior persistence fault"))

	job, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatalf("claim terminal job: %v", err)
	}
	scheduler.launch(job)
	select {
	case <-repository.terminalSignal:
	case <-time.After(time.Second):
		t.Fatal("terminal write was silently skipped behind the prior fault")
	}
	complete, lose := repository.terminalCounts()
	if complete != 1 || lose != 0 {
		t.Fatalf("terminal writes complete=%d lose=%d, want one complete", complete, lose)
	}
	if !scheduler.Status().PersistenceFault {
		t.Fatal("prior persistence fault was cleared by terminal success")
	}
}

func TestPhase2SchedulerConcurrentWakesDoNotDuplicateLaunch(t *testing.T) {
	ctx := context.Background()
	repository := &phase2SchedulerRepository{
		jobs:        []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "candidate"}},
		emptySignal: make(chan struct{}, 1),
	}
	launcher := &phase2SchedulerLauncher{}
	scheduler := phase2SchedulerForTest(ctx, repository, launcher)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scheduler.drain()
		}()
	}
	wg.Wait()

	if got := launcher.launchCount(); got != 1 {
		t.Fatalf("duplicate Codex launches = %d, want one", got)
	}
	if got := repository.claimCount(); got < 1 {
		t.Fatalf("claim attempts = %d, want at least one", got)
	}
}
