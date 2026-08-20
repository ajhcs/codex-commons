package application

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

// restartAcceptanceStore keeps the database and all repository behavior real,
// injecting only the one pre-restart failure needed to exercise the scheduler's
// durable terminal-write boundary. The retry failure keeps the pending row in
// place long enough to prove that the claim gate does not launch new work.
type restartAcceptanceStore struct {
	*commonsstore.Store

	mu            sync.Mutex
	completeApply int
	loseApply     int
	retryFailures int
	applyFailed   chan struct{}
	claimCalls    int
}

func (s *restartAcceptanceStore) ApplyArchaeologyNativePersistenceIntent(ctx context.Context, id string) error {
	record, err := s.Store.ArchaeologyNativePersistenceIntent(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	shouldFail := false
	failureMessage := "disposable terminal apply failure"
	switch record.Operation {
	case domain.ArchaeologyNativePersistenceCompleteTurn:
		shouldFail = s.completeApply > 0
		if shouldFail {
			s.completeApply--
		}
	case domain.ArchaeologyNativePersistenceLoseTurn:
		shouldFail = s.loseApply > 0
		if shouldFail {
			s.loseApply--
			failureMessage = "disposable lose apply failure"
		}
	}
	if shouldFail {
		if s.applyFailed != nil {
			select {
			case s.applyFailed <- struct{}{}:
			default:
			}
		}
	}
	s.mu.Unlock()
	if shouldFail {
		return errors.New(failureMessage)
	}
	return s.Store.ApplyArchaeologyNativePersistenceIntent(ctx, id)
}

func (s *restartAcceptanceStore) RetryDueArchaeologyNativePersistence(ctx context.Context, limits ...int) (domain.ArchaeologyNativePersistenceRetryReport, error) {
	s.mu.Lock()
	if s.retryFailures > 0 {
		s.retryFailures--
		s.mu.Unlock()
		return domain.ArchaeologyNativePersistenceRetryReport{}, errors.New("disposable retry unavailable")
	}
	s.mu.Unlock()
	return s.Store.RetryDueArchaeologyNativePersistence(ctx, limits...)
}

func (s *restartAcceptanceStore) ClaimArchaeologyNativeJob(ctx context.Context) (domain.ArchaeologyNativeJob, error) {
	s.mu.Lock()
	s.claimCalls++
	s.mu.Unlock()
	return s.Store.ClaimArchaeologyNativeJob(ctx)
}

func (s *restartAcceptanceStore) claims() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimCalls
}

type restartAcceptanceLauncher struct {
	mu         sync.Mutex
	result     domain.ArchaeologyLaunchResult
	terminal   domain.ArchaeologyNativeTerminal
	launches   int
	finalizes  int
	interrupts int
}

func (l *restartAcceptanceLauncher) Available(context.Context) error { return nil }

func (l *restartAcceptanceLauncher) LaunchNative(_ context.Context, _ domain.ArchaeologyNativeJob, _ domain.ArchaeologySession, _ domain.ArchaeologyCandidate, _ func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, onTerminal func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	l.mu.Lock()
	l.launches++
	result, terminal := l.result, l.terminal
	l.mu.Unlock()
	onTerminal(terminal)
	return result, nil
}

func (l *restartAcceptanceLauncher) InterruptNative(context.Context, domain.ArchaeologyNativeJob) error {
	l.mu.Lock()
	l.interrupts++
	l.mu.Unlock()
	return nil
}

func (l *restartAcceptanceLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	l.mu.Lock()
	l.finalizes++
	l.mu.Unlock()
	return nil
}

func (l *restartAcceptanceLauncher) counts() (launches, finalizes, interrupts int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.launches, l.finalizes, l.interrupts
}

func seedRestartAcceptanceJob(t *testing.T, repository *commonsstore.Store, now time.Time) domain.ArchaeologyNativeJob {
	t.Helper()
	ctx := context.Background()
	discovered, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{
		Principal: domain.HumanLocalPrincipal,
		RequestID: "phase3-restart-discovery",
	}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{
		ID: "phase3-restart-candidate", Name: "Phase 3 restart", PathLabel: "Phase 3 restart",
		HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	configured, err := repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{
		Principal:    domain.HumanLocalPrincipal,
		RequestID:    "phase3-restart-configure",
		BaseRevision: discovered.Revision,
		Config: domain.ArchaeologyConfig{
			SelectedProjectIDs: []string{"phase3-restart-candidate"},
			Depth:              "quick",
			Sources:            domain.ArchaeologySources{Git: true},
			MaxConcurrency:     1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{
		Principal:    domain.HumanLocalPrincipal,
		RequestID:    "phase3-restart-queue",
		BaseRevision: configured.Revision,
	}); err != nil {
		t.Fatal(err)
	}
	job, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "starting" || !job.StartedAt.Equal(now) {
		t.Fatalf("seeded job=%+v", job)
	}
	return job
}

func waitRestartAcceptancePending(t *testing.T, repository *commonsstore.Store, jobID string, operation domain.ArchaeologyNativePersistenceOperation) domain.ArchaeologyNativePersistenceIntentRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := repository.ArchaeologyNativePersistenceStatus(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status.Pending == 1 {
			var id string
			if err = repository.DB().QueryRowContext(context.Background(), `SELECT id FROM archaeology_native_persistence_intents WHERE job_id=? AND operation=?`, jobID, string(operation)).Scan(&id); err != nil {
				t.Fatal(err)
			}
			record, err := repository.ArchaeologyNativePersistenceIntent(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			return record
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("terminal intent did not remain pending")
	return domain.ArchaeologyNativePersistenceIntentRecord{}
}

func TestPhase3StoreSchedulerTerminalCompleteSurvivesRestartWithoutExternalReplay(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	database := filepath.Join(t.TempDir(), "phase3-restart.sqlite")
	repository, err := commonsstore.Open(ctx, database, commonsstore.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	job := seedRestartAcceptanceJob(t, repository, now)

	storeWrapper := &restartAcceptanceStore{Store: repository, completeApply: 1, retryFailures: 1, applyFailed: make(chan struct{}, 1)}
	duration := int64(4242)
	launcher := &restartAcceptanceLauncher{
		result:   domain.ArchaeologyLaunchResult{ThreadID: "phase3-thread", CodexSessionID: "phase3-session", TurnID: "phase3-turn"},
		terminal: domain.ArchaeologyNativeTerminal{ThreadID: "phase3-thread", TurnID: "phase3-turn", Status: "failed", DurationMS: &duration},
	}
	scheduler := &ArchaeologyScheduler{
		repository: storeWrapper,
		launcher:   launcher,
		principal:  domain.HumanLocalPrincipal,
		ctx:        ctx,
		wake:       make(chan struct{}, 1),
	}
	scheduler.launch(job)
	select {
	case <-storeWrapper.applyFailed:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal persistence did not reach the injected failure")
	}
	record := waitRestartAcceptancePending(t, repository, job.ID, domain.ArchaeologyNativePersistenceCompleteTurn)
	if record.State != "pending" || record.Operation != domain.ArchaeologyNativePersistenceCompleteTurn {
		t.Fatalf("pending terminal intent=%+v", record)
	}
	var state string
	if err = repository.DB().QueryRowContext(ctx, `SELECT state FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "active" {
		t.Fatalf("job state after terminal persistence failure=%q, want active", state)
	}
	attentionDeadline := time.Now().Add(2 * time.Second)
	for !scheduler.Status().PersistenceAttention && time.Now().Before(attentionDeadline) {
		time.Sleep(time.Millisecond)
	}
	if !scheduler.Status().PersistenceAttention {
		t.Fatalf("scheduler did not expose persistence attention: %+v", scheduler.Status())
	}

	// The scheduler retry path is deliberately held at the injected failure. A
	// drain must therefore stop before Claim and cannot cross into a new external
	// Launch while the durable complete intent is unresolved.
	scheduler.drain()
	if calls := storeWrapper.claims(); calls != 0 {
		t.Fatalf("claim calls with pending terminal intent=%d", calls)
	}
	if launches, finalizes, interrupts := launcher.counts(); launches != 1 || finalizes != 1 || interrupts != 0 {
		t.Fatalf("external calls before restart launch=%d finalize=%d interrupt=%d", launches, finalizes, interrupts)
	}
	scheduler.Close()
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := commonsstore.Open(ctx, database, commonsstore.WithClock(func() time.Time { return now.Add(time.Minute) }))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.ReconcileArchaeology(ctx); err != nil {
		t.Fatal(err)
	}
	var startupState string
	if err = reopened.DB().QueryRowContext(ctx, `SELECT state FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&startupState); err != nil {
		t.Fatal(err)
	}
	if startupState != "uncertain" {
		t.Fatalf("generic startup reconciliation state=%q, want uncertain", startupState)
	}
	if err = reopened.ReconcileArchaeologyNativePersistence(ctx); err != nil {
		t.Fatalf("startup persistence reconciliation: %v", err)
	}

	read, err := reopened.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.State != "applied" {
		t.Fatalf("reconciled terminal intent=%+v", read)
	}
	var terminalState, errorCode string
	var storedDuration int64
	if err = reopened.DB().QueryRowContext(ctx, `SELECT state,error_code,duration_ms FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&terminalState, &errorCode, &storedDuration); err != nil {
		t.Fatal(err)
	}
	if terminalState != "failed" || errorCode != "codex_turn_failed" || storedDuration != duration {
		t.Fatalf("exact terminal outcome state=%q error=%q duration=%d", terminalState, errorCode, storedDuration)
	}
	status, err := reopened.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Healthy() || status.Pending != 0 || status.Leased != 0 || status.Blocked != 0 || status.Applied != 3 {
		t.Fatalf("reconciled persistence status=%+v", status)
	}

	restartedWrapper := &restartAcceptanceStore{Store: reopened}
	restartedLauncher := &restartAcceptanceLauncher{}
	restartedScheduler := &ArchaeologyScheduler{
		repository: restartedWrapper,
		launcher:   restartedLauncher,
		principal:  domain.HumanLocalPrincipal,
		ctx:        ctx,
		wake:       make(chan struct{}, 1),
	}
	restartedScheduler.drain()
	if calls := restartedWrapper.claims(); calls != 1 {
		t.Fatalf("restart claim calls=%d, want one bounded empty claim", calls)
	}
	if launches, finalizes, interrupts := restartedLauncher.counts(); launches != 0 || finalizes != 0 || interrupts != 0 {
		t.Fatalf("external calls replayed after restart launch=%d finalize=%d interrupt=%d", launches, finalizes, interrupts)
	}
	restartedScheduler.Close()
}

func TestPhase3StoreSchedulerLoseTurnSurvivesRestartWithoutExternalReplay(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 13, 0, 0, 0, time.UTC)
	database := filepath.Join(t.TempDir(), "phase3-lose-restart.sqlite")
	repository, err := commonsstore.Open(ctx, database, commonsstore.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	job := seedRestartAcceptanceJob(t, repository, now)

	storeWrapper := &restartAcceptanceStore{Store: repository, loseApply: 1, retryFailures: 1, applyFailed: make(chan struct{}, 1)}
	launcher := &restartAcceptanceLauncher{
		result:   domain.ArchaeologyLaunchResult{ThreadID: "lose-thread", CodexSessionID: "lose-session", TurnID: "lose-turn"},
		terminal: domain.ArchaeologyNativeTerminal{ThreadID: "lose-thread", TurnID: "lose-turn", Status: "unavailable"},
	}
	scheduler := &ArchaeologyScheduler{
		repository: storeWrapper,
		launcher:   launcher,
		principal:  domain.HumanLocalPrincipal,
		ctx:        ctx,
		wake:       make(chan struct{}, 1),
	}
	scheduler.launch(job)
	select {
	case <-storeWrapper.applyFailed:
	case <-time.After(2 * time.Second):
		t.Fatal("lose-turn persistence did not reach the injected failure")
	}
	record := waitRestartAcceptancePending(t, repository, job.ID, domain.ArchaeologyNativePersistenceLoseTurn)
	if record.State != "pending" || record.Operation != domain.ArchaeologyNativePersistenceLoseTurn {
		t.Fatalf("pending lose intent=%+v", record)
	}
	var state, errorCode string
	if err = repository.DB().QueryRowContext(ctx, `SELECT state,error_code FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&state, &errorCode); err != nil {
		t.Fatal(err)
	}
	if state != "active" || errorCode != "" {
		t.Fatalf("job state after lose persistence failure=%q error=%q", state, errorCode)
	}
	attentionDeadline := time.Now().Add(2 * time.Second)
	for !scheduler.Status().PersistenceAttention && time.Now().Before(attentionDeadline) {
		time.Sleep(time.Millisecond)
	}
	if !scheduler.Status().PersistenceAttention {
		t.Fatalf("scheduler did not expose lose persistence attention: %+v", scheduler.Status())
	}
	scheduler.drain()
	if calls := storeWrapper.claims(); calls != 0 {
		t.Fatalf("claim calls with pending lose intent=%d", calls)
	}
	if launches, finalizes, interrupts := launcher.counts(); launches != 1 || finalizes != 1 || interrupts != 0 {
		t.Fatalf("external calls before lose restart launch=%d finalize=%d interrupt=%d", launches, finalizes, interrupts)
	}
	scheduler.Close()
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := commonsstore.Open(ctx, database, commonsstore.WithClock(func() time.Time { return now.Add(time.Minute) }))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.ReconcileArchaeology(ctx); err != nil {
		t.Fatal(err)
	}
	if err = reopened.DB().QueryRowContext(ctx, `SELECT state,error_code FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&state, &errorCode); err != nil {
		t.Fatal(err)
	}
	if state != "uncertain" || errorCode != "server_restarted_during_active_task" {
		t.Fatalf("generic lose restart state=%q error=%q", state, errorCode)
	}
	if err = reopened.ReconcileArchaeologyNativePersistence(ctx); err != nil {
		t.Fatalf("lose startup persistence reconcile: %v", err)
	}

	read, err := reopened.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.State != "applied" {
		t.Fatalf("reconciled lose intent=%+v", read)
	}
	if err = reopened.DB().QueryRowContext(ctx, `SELECT state,error_code FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&state, &errorCode); err != nil {
		t.Fatal(err)
	}
	if state != "uncertain" || errorCode != "codex_process_unavailable" {
		t.Fatalf("normalized lose state=%q error=%q", state, errorCode)
	}
	status, err := reopened.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Healthy() || status.Pending != 0 || status.Leased != 0 || status.Blocked != 0 || status.Applied != 3 {
		t.Fatalf("lose restart persistence status=%+v", status)
	}

	restartedWrapper := &restartAcceptanceStore{Store: reopened}
	restartedLauncher := &restartAcceptanceLauncher{}
	restartedScheduler := &ArchaeologyScheduler{
		repository: restartedWrapper,
		launcher:   restartedLauncher,
		principal:  domain.HumanLocalPrincipal,
		ctx:        ctx,
		wake:       make(chan struct{}, 1),
	}
	restartedScheduler.drain()
	if calls := restartedWrapper.claims(); calls != 1 {
		t.Fatalf("lose restart claim calls=%d, want one bounded empty claim", calls)
	}
	if launches, finalizes, interrupts := restartedLauncher.counts(); launches != 0 || finalizes != 0 || interrupts != 0 {
		t.Fatalf("lose external calls replayed after restart launch=%d finalize=%d interrupt=%d", launches, finalizes, interrupts)
	}
	restartedScheduler.Close()
}
