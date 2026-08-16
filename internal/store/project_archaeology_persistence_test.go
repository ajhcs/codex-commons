package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func persistenceTestClock() (*time.Time, func() time.Time) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	return &now, func() time.Time { return now }
}

func TestArchaeologyNativePersistenceEnsureReplayAndClaimGate(t *testing.T) {
	ctx := context.Background()
	now, clock := persistenceTestClock()
	s, _ := nativeTestSession(t, 2, 1)
	s.now = clock
	first, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: first.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	var payloadJSON string
	if err = s.DB().QueryRowContext(ctx, `SELECT payload_json FROM archaeology_native_persistence_intents WHERE id=?`, record.ID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if record.State != "pending" || record.ID == "" || record.NextAttemptAt == nil || !record.NextAttemptAt.Equal(*now) || record.PayloadDigest == [32]byte{} || payloadJSON == "" {
		t.Fatalf("record=%+v", record)
	}
	if _, err = s.EnsureArchaeologyNativePersistenceIntent(ctx, intent); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	changed := intent
	changed.Uncertain = true
	if _, err = s.EnsureArchaeologyNativePersistenceIntent(ctx, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed payload err=%v", err)
	}
	if _, err = s.ClaimArchaeologyNativeJob(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("pending intent must close global claim gate: %v", err)
	}
	if err = s.ApplyArchaeologyNativePersistenceIntent(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	status, err := s.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil || !status.Healthy() || status.Applied != 1 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	second, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil || second.ID == first.ID {
		t.Fatalf("claim after applied intent=%+v err=%v", second, err)
	}
}

func TestArchaeologyNativePersistenceRejectsExternalOperations(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []domain.ArchaeologyNativePersistenceOperation{
		"launch_native", "LaunchNative", "launch", "finalize_native", "FinalizeNative", "finalize", "interrupt_native", "InterruptNative", "interrupt", "unknown",
	} {
		intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: operation}
		if _, err = s.EnsureArchaeologyNativePersistenceIntent(ctx, intent); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("operation %q err=%v", operation, err)
		}
	}
	var count int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_persistence_intents`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("external operation rows=%d", count)
	}
}

func TestArchaeologyNativePersistenceRetryCASDoesNotOverwriteAttempts(t *testing.T) {
	ctx := context.Background()
	now, clock := persistenceTestClock()
	s, _ := nativeTestSession(t, 1, 1)
	s.now = clock
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	leaseExpiry := archaeologyPersistenceStamp(now.Add(time.Minute))
	if _, err = s.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET state='leased',lease_owner='worker',lease_expires_at=? WHERE id=?`, leaseExpiry, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET attempts=1 WHERE id=?`, record.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.markArchaeologyPersistenceRetry(ctx, record.ID, "worker", "unavailable", 1, *now); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale retry CAS err=%v", err)
	}
	read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil || read.State != "leased" || read.Attempts != 1 || read.LeaseOwner != "worker" {
		t.Fatalf("CAS read=%+v err=%v", read, err)
	}
}

func TestArchaeologyNativePersistenceAllRepositoryOperationsAndReplay(t *testing.T) {
	ctx := context.Background()
	now, clock := persistenceTestClock()
	s, _ := nativeTestSession(t, 1, 1)
	s.now = clock
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceBindIdentity, ThreadID: "thread-p", CodexSessionID: "session-p", TurnID: "turn-p"}
	activate := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceActivate, ThreadID: "thread-p", TurnID: "turn-p"}
	lose := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceLoseTurn, ThreadID: "thread-p", TurnID: "turn-p"}
	complete := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceCompleteTurn, ThreadID: "thread-p", TurnID: "turn-p", Status: "interrupted"}
	fail := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart, Launch: domain.ArchaeologyLaunchResult{ThreadID: "thread-p", CodexSessionID: "session-p", TurnID: "turn-p"}, Uncertain: true}
	for _, intent := range []domain.ArchaeologyNativePersistenceIntent{identity, activate, lose, complete} {
		if _, err = s.EnsureAndApplyArchaeologyNativePersistence(ctx, intent); err != nil {
			t.Fatalf("operation %s: %v", intent.Operation, err)
		}
	}
	if _, err = s.EnsureAndApplyArchaeologyNativePersistence(ctx, complete); err != nil {
		t.Fatalf("complete replay: %v", err)
	}
	if _, err = s.EnsureAndApplyArchaeologyNativePersistence(ctx, fail); err != nil {
		t.Fatalf("stale fail should be durably superseded: %v", err)
	}
	status, err := s.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil || status.Applied != 4 || status.Superseded != 1 || !status.Healthy() || status.NextAttemptAt != nil {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	value, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	jobView := value.NativeBatches[0].Jobs[0]
	if jobView.State != "interrupted" || jobView.ThreadID != identity.ThreadID || jobView.TurnID != identity.TurnID {
		t.Fatalf("job=%+v", jobView)
	}
	_ = now
}

func TestArchaeologyNativePersistenceLoseTurnSurvivesStartupReconcile(t *testing.T) {
	ctx := context.Background()
	_, clock := persistenceTestClock()
	s, _ := nativeTestSession(t, 1, 1)
	s.now = clock
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "lose-thread", "lose-session", "lose-turn"); err != nil {
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{
		JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceLoseTurn,
		ThreadID: "lose-thread", TurnID: "lose-turn",
	}
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}

	// Startup reconciliation runs before the persistence ledger. It must not
	// discard the stronger exact unavailable evidence merely because the generic
	// restart pass has latched this active job as uncertain.
	if err = s.ReconcileArchaeology(ctx); err != nil {
		t.Fatal(err)
	}
	var state, errorCode string
	if err = s.DB().QueryRowContext(ctx, `SELECT state,error_code FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&state, &errorCode); err != nil {
		t.Fatal(err)
	}
	if state != "uncertain" || errorCode != "server_restarted_during_active_task" {
		t.Fatalf("generic restart state=%q error=%q", state, errorCode)
	}
	outcome, err := s.classifyArchaeologyPersistenceReadback(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != persistenceReadbackRetry {
		t.Fatalf("generic restart lose readback outcome=%d, want retry", outcome)
	}
	if err = s.ReconcileArchaeologyNativePersistence(ctx); err != nil {
		t.Fatalf("native persistence startup reconcile: %v", err)
	}

	read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.State != "applied" {
		t.Fatalf("lose intent after startup reconcile=%+v", read)
	}
	if err = s.DB().QueryRowContext(ctx, `SELECT state,error_code FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&state, &errorCode); err != nil {
		t.Fatal(err)
	}
	if state != "uncertain" || errorCode != "codex_process_unavailable" {
		t.Fatalf("normalized loss state=%q error=%q", state, errorCode)
	}
	status, err := s.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Healthy() || status.Pending != 0 || status.Leased != 0 || status.Blocked != 0 || status.Applied != 1 {
		t.Fatalf("lose startup status=%+v", status)
	}
}

func TestArchaeologyNativePersistenceLoseTurnIdentityMismatchBlocks(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "expected-thread", "expected-session", "expected-turn"); err != nil {
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{
		JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceLoseTurn,
		ThreadID: "expected-thread", TurnID: "expected-turn",
	}
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().ExecContext(ctx, `UPDATE archaeology_native_jobs SET thread_id='other-thread',turn_id='other-turn' WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
		t.Fatal(err)
	}
	read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.State != "blocked" {
		t.Fatalf("mismatched lose intent=%+v, want blocked", read)
	}
}

func TestArchaeologyNativePersistenceAppliedBeforeAcknowledgementAndExpiredLease(t *testing.T) {
	ctx := context.Background()
	now, clock := persistenceTestClock()
	databasePath := filepath.Join(t.TempDir(), "persistence.sqlite")
	s, err := Open(ctx, databasePath, WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	value, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "persistence-discovery"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "p", Name: "Project p", PathLabel: "Project p", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low", PrivacyNote: "Metadata only."}}})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "persistence-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"p"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	value, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "persistence-start", BaseRevision: value.Revision})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	// Simulate a committed repository mutation whose acknowledgement was lost.
	if err = s.FailArchaeologyNativeStart(ctx, job.ID, domain.ArchaeologyLaunchResult{}, false); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
		s.Close()
		t.Fatal(err)
	}
	read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil || read.State != "applied" || read.Attempts != 0 {
		s.Close()
		t.Fatalf("read=%+v err=%v", read, err)
	}
	// Keep the durable lease check independent of batch/session transitions.
	recoveryIntent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceBindIdentity, ThreadID: "recovery-thread", CodexSessionID: "recovery-session", TurnID: "recovery-turn"}
	recoveryRecord, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, recoveryIntent)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	var leaseID string
	if err = s.DB().QueryRowContext(ctx, `SELECT id FROM archaeology_native_persistence_intents WHERE id=?`, recoveryRecord.ID).Scan(&leaseID); err != nil {
		s.Close()
		t.Fatal(err)
	}
	expired := archaeologyPersistenceStamp(now.Add(-time.Second))
	if _, err = s.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET state='leased',lease_owner='test',lease_expires_at=?,updated_at=? WHERE id=?`, expired, expired, recoveryRecord.ID); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, databasePath, WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
		t.Fatal(err)
	}
	read, err = s.ArchaeologyNativePersistenceIntent(ctx, recoveryRecord.ID)
	if err != nil || read.State != "pending" || read.Attempts != 1 || read.NextAttemptAt == nil {
		t.Fatalf("recovered read=%+v err=%v", read, err)
	}
	*now = read.NextAttemptAt.Add(time.Millisecond)
	if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
		t.Fatal(err)
	}
	read, err = s.ArchaeologyNativePersistenceIntent(ctx, recoveryRecord.ID)
	if err != nil || read.State != "superseded" || read.Attempts != 1 {
		t.Fatalf("recovered terminal read=%+v err=%v", read, err)
	}
}

func TestArchaeologyNativePersistenceReadbackAllFiveOperations(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		setup func(*Store, domain.ArchaeologyNativeJob) (domain.ArchaeologyNativePersistenceIntent, error)
	}{
		{
			name: "fail_start",
			setup: func(s *Store, job domain.ArchaeologyNativeJob) (domain.ArchaeologyNativePersistenceIntent, error) {
				err := s.FailArchaeologyNativeStart(ctx, job.ID, domain.ArchaeologyLaunchResult{}, false)
				return domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}, err
			},
		},
		{
			name: "bind_identity",
			setup: func(s *Store, job domain.ArchaeologyNativeJob) (domain.ArchaeologyNativePersistenceIntent, error) {
				err := s.BindArchaeologyNativeIdentity(ctx, job.ID, "thread-r", "session-r", "turn-r")
				return domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceBindIdentity, ThreadID: "thread-r", CodexSessionID: "session-r", TurnID: "turn-r"}, err
			},
		},
		{
			name: "activate",
			setup: func(s *Store, job domain.ArchaeologyNativeJob) (domain.ArchaeologyNativePersistenceIntent, error) {
				if err := s.BindArchaeologyNativeJob(ctx, job.ID, "thread-r", "session-r", "turn-r"); err != nil {
					return domain.ArchaeologyNativePersistenceIntent{}, err
				}
				err := s.ActivateArchaeologyNativeJob(ctx, job.ID, "thread-r", "turn-r")
				return domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceActivate, ThreadID: "thread-r", TurnID: "turn-r"}, err
			},
		},
		{
			name: "lose_turn",
			setup: func(s *Store, job domain.ArchaeologyNativeJob) (domain.ArchaeologyNativePersistenceIntent, error) {
				if err := s.BindArchaeologyNativeJob(ctx, job.ID, "thread-r", "session-r", "turn-r"); err != nil {
					return domain.ArchaeologyNativePersistenceIntent{}, err
				}
				err := s.LoseArchaeologyNativeTurn(ctx, job.ID, "thread-r", "turn-r")
				return domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceLoseTurn, ThreadID: "thread-r", TurnID: "turn-r"}, err
			},
		},
		{
			name: "complete_turn",
			setup: func(s *Store, job domain.ArchaeologyNativeJob) (domain.ArchaeologyNativePersistenceIntent, error) {
				if err := s.BindArchaeologyNativeJob(ctx, job.ID, "thread-r", "session-r", "turn-r"); err != nil {
					return domain.ArchaeologyNativePersistenceIntent{}, err
				}
				if err := s.LoseArchaeologyNativeTurn(ctx, job.ID, "thread-r", "turn-r"); err != nil {
					return domain.ArchaeologyNativePersistenceIntent{}, err
				}
				terminal := domain.ArchaeologyNativeTerminal{JobID: job.ID, ThreadID: "thread-r", TurnID: "turn-r", Status: "interrupted"}
				if err := s.CompleteArchaeologyNativeTurn(ctx, terminal); err != nil {
					return domain.ArchaeologyNativePersistenceIntent{}, err
				}
				return domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceCompleteTurn, ThreadID: "thread-r", TurnID: "turn-r", Status: "interrupted"}, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now, clock := persistenceTestClock()
			s, _ := nativeTestSession(t, 1, 1)
			s.now = clock
			job, err := s.ClaimArchaeologyNativeJob(ctx)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := test.setup(s, job)
			if err != nil {
				t.Fatal(err)
			}
			record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
				t.Fatal(err)
			}
			read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
			if err != nil || read.State != "applied" {
				t.Fatalf("read=%+v err=%v", read, err)
			}
			_ = now
		})
	}
}

func TestArchaeologyNativePersistenceCASRefusesAnotherLiveLease(t *testing.T) {
	ctx := context.Background()
	now, clock := persistenceTestClock()
	s, _ := nativeTestSession(t, 1, 1)
	s.now = clock
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	leaseUntil := archaeologyPersistenceStamp(now.Add(time.Minute))
	if _, err = s.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET state='leased',lease_owner='other-worker',lease_expires_at=?,updated_at=? WHERE id=?`, leaseUntil, leaseUntil, record.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyArchaeologyNativePersistenceIntent(ctx, record.ID); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("live lease must not be stolen: %v", err)
	}
	var state string
	if err = s.DB().QueryRowContext(ctx, `SELECT state FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "starting" {
		t.Fatalf("live lease mutation state=%s", state)
	}
	*now = now.Add(2 * time.Minute)
	if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
		t.Fatal(err)
	}
	read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil || read.State != "pending" || read.Attempts != 1 || read.NextAttemptAt == nil {
		t.Fatalf("expired lease read=%+v err=%v", read, err)
	}
	*now = read.NextAttemptAt.Add(time.Millisecond)
	if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
		t.Fatal(err)
	}
	read, err = s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil || read.State != "applied" {
		t.Fatalf("expired lease applied read=%+v err=%v", read, err)
	}
}

func TestArchaeologyNativePersistenceApplyHonorsFutureBackoff(t *testing.T) {
	ctx := context.Background()
	now, clock := persistenceTestClock()
	s, _ := nativeTestSession(t, 1, 1)
	s.now = clock
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	future := now.Add(time.Minute)
	if _, err = s.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET attempts=1,next_attempt_at=?,last_error_code='unavailable',updated_at=? WHERE id=?`, archaeologyPersistenceStamp(future), archaeologyPersistenceStamp(*now), record.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyArchaeologyNativePersistenceIntent(ctx, record.ID); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("future backoff must not be bypassed: %v", err)
	}
	read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil || read.State != "pending" || read.Attempts != 1 {
		t.Fatalf("future read=%+v err=%v", read, err)
	}
	*now = future.Add(time.Millisecond)
	if err = s.ApplyArchaeologyNativePersistenceIntent(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	read, err = s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil || read.State != "applied" || read.Attempts != 1 {
		t.Fatalf("due read=%+v err=%v", read, err)
	}
}

func TestArchaeologyNativePersistenceMismatchAndStaleStates(t *testing.T) {
	ctx := context.Background()
	t.Run("identity_bearing_fail_requires_exact_readback", func(t *testing.T) {
		s, _ := nativeTestSession(t, 1, 1)
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.FailArchaeologyNativeStart(ctx, job.ID, domain.ArchaeologyLaunchResult{}, true); err != nil {
			t.Fatal(err)
		}
		launch := domain.ArchaeologyLaunchResult{ThreadID: "late-thread", CodexSessionID: "late-session", TurnID: "late-turn"}
		intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart, Launch: launch, Uncertain: true}
		record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
			t.Fatal(err)
		}
		read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
		if err != nil || read.State != "applied" {
			t.Fatalf("read=%+v err=%v", read, err)
		}
		var threadID, sessionID, turnID string
		if err = s.DB().QueryRowContext(ctx, `SELECT thread_id,codex_session_id,turn_id FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&threadID, &sessionID, &turnID); err != nil {
			t.Fatal(err)
		}
		if threadID != launch.ThreadID || sessionID != launch.CodexSessionID || turnID != launch.TurnID {
			t.Fatalf("identity=(%s,%s,%s)", threadID, sessionID, turnID)
		}
	})
	t.Run("identity_mismatch_blocks", func(t *testing.T) {
		s, _ := nativeTestSession(t, 1, 1)
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceBindIdentity, ThreadID: "intent-thread", CodexSessionID: "intent-session", TurnID: "intent-turn"}
		record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.BindArchaeologyNativeIdentity(ctx, job.ID, "other-thread", "other-session", "other-turn"); err != nil {
			t.Fatal(err)
		}
		if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
			t.Fatal(err)
		}
		read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
		if err != nil || read.State != "blocked" {
			t.Fatalf("read=%+v err=%v", read, err)
		}
	})
	t.Run("stale_activation_supersedes", func(t *testing.T) {
		s, _ := nativeTestSession(t, 1, 1)
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-stale", "session-stale", "turn-stale"); err != nil {
			t.Fatal(err)
		}
		intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceActivate, ThreadID: "thread-stale", TurnID: "turn-stale"}
		record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.CompleteArchaeologyNativeTurn(ctx, domain.ArchaeologyNativeTerminal{JobID: job.ID, ThreadID: "thread-stale", TurnID: "turn-stale", Status: "failed"}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
			t.Fatal(err)
		}
		read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
		if err != nil || read.State != "superseded" {
			t.Fatalf("read=%+v err=%v", read, err)
		}
	})
	t.Run("terminal_duration_is_part_of_replay_identity", func(t *testing.T) {
		s, _ := nativeTestSession(t, 1, 1)
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-duration", "session-duration", "turn-duration"); err != nil {
			t.Fatal(err)
		}
		duration := int64(10)
		terminal := domain.ArchaeologyNativeTerminal{JobID: job.ID, ThreadID: "thread-duration", TurnID: "turn-duration", Status: "failed", DurationMS: &duration}
		if err = s.CompleteArchaeologyNativeTurn(ctx, terminal); err != nil {
			t.Fatal(err)
		}
		changedDuration := int64(11)
		terminal.DurationMS = &changedDuration
		if err = s.CompleteArchaeologyNativeTurn(ctx, terminal); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("changed duration replay err=%v", err)
		}
		durableIntent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceCompleteTurn, ThreadID: "thread-duration", TurnID: "turn-duration", Status: "failed", DurationMS: &changedDuration}
		record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, durableIntent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
			t.Fatal(err)
		}
		read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
		if err != nil || read.State != "superseded" {
			t.Fatalf("duration mismatch read=%+v err=%v", read, err)
		}
	})
}

func TestArchaeologyNativePersistenceReconcileAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
	if _, err = s.EnsureArchaeologyNativePersistenceIntent(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileArchaeologyNativePersistence(ctx); err != nil {
		t.Fatalf("due reconcile should drain pending work: %v", err)
	}
	if err = s.ApplyArchaeologyNativePersistenceIntent(ctx, mustPersistenceIntentID(t, s, ctx, intent)); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileArchaeologyNativePersistence(ctx); err != nil {
		t.Fatalf("healthy reconcile err=%v", err)
	}
	rows, err := s.DB().QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
}

func TestArchaeologyNativePersistenceRejectsMalformedTimestamps(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET next_attempt_at='not-a-timestamp' WHERE id=?`, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArchaeologyNativePersistenceIntent(ctx, record.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("malformed intent timestamp err=%v", err)
	}
	if _, err = s.ArchaeologyNativePersistenceStatus(ctx); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("malformed status timestamp err=%v", err)
	}
}

func mustPersistenceIntentID(t *testing.T, s *Store, ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent) string {
	t.Helper()
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	return record.ID
}

func TestArchaeologyNativePersistenceBackoffAndBoundedBlocking(t *testing.T) {
	ctx := context.Background()
	t.Run("fail_once_then_success", func(t *testing.T) {
		now, clock := persistenceTestClock()
		s, _ := nativeTestSession(t, 1, 1)
		s.now = clock
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
		record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		trigger := "persistence_fail_once"
		if _, err = s.DB().ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER %s BEFORE UPDATE ON archaeology_native_jobs WHEN OLD.id='%s' BEGIN SELECT RAISE(ABORT, 'database is locked'); END", trigger, job.ID)); err != nil {
			t.Fatal(err)
		}
		if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
			t.Fatal(err)
		}
		read, err := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
		if err != nil || read.State != "pending" || read.Attempts != 1 || read.NextAttemptAt == nil || !read.NextAttemptAt.After(*now) {
			t.Fatalf("retry read=%+v err=%v", read, err)
		}
		if _, err = s.DB().ExecContext(ctx, "DROP TRIGGER "+trigger); err != nil {
			t.Fatal(err)
		}
		*now = read.NextAttemptAt.Add(time.Millisecond)
		if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
			t.Fatal(err)
		}
		read, err = s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
		if err != nil || read.State != "applied" {
			t.Fatalf("success read=%+v err=%v", read, err)
		}
	})
	t.Run("repeated_failure_blocks", func(t *testing.T) {
		now, clock := persistenceTestClock()
		s, _ := nativeTestSession(t, 1, 1)
		s.now = clock
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		intent := domain.ArchaeologyNativePersistenceIntent{JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart}
		record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		trigger := "persistence_fail_always"
		if _, err = s.DB().ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER %s BEFORE UPDATE ON archaeology_native_jobs WHEN OLD.id='%s' BEGIN SELECT RAISE(ABORT, 'database is locked'); END", trigger, job.ID)); err != nil {
			t.Fatal(err)
		}
		defer s.DB().ExecContext(ctx, "DROP TRIGGER "+trigger)
		for attempt := 1; attempt <= archaeologyPersistenceMaxAttempts; attempt++ {
			if _, err = s.RetryArchaeologyNativePersistence(ctx, 1); err != nil {
				t.Fatal(err)
			}
			read, readErr := s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if attempt < archaeologyPersistenceMaxAttempts {
				if read.State != "pending" || read.Attempts != attempt || read.NextAttemptAt == nil {
					t.Fatalf("attempt %d read=%+v", attempt, read)
				}
				*now = read.NextAttemptAt.Add(time.Millisecond)
			} else if read.State != "blocked" || read.Attempts != archaeologyPersistenceMaxAttempts {
				t.Fatalf("blocked read=%+v", read)
			}
		}
		if _, err = s.ClaimArchaeologyNativeJob(ctx); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("blocked intent must close claims: %v", err)
		}
	})
}
