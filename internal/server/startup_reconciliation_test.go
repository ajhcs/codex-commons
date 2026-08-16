package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
	"codex-commons/internal/runtimehealth"
	commonsstore "codex-commons/internal/store"
)

const startupPersistenceStampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func openStartupPersistenceStore(t *testing.T) (*commonsstore.Store, context.Context, time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "commons.sqlite"), commonsstore.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, ctx, now
}

func seedStartupNativeJobs(t *testing.T, store *commonsstore.Store, count int) []domain.ArchaeologyNativeJob {
	t.Helper()
	if count < 1 {
		t.Fatal("native job count must be positive")
	}
	candidates := make([]domain.ArchaeologyCandidate, 0, count)
	selected := make([]string, 0, count)
	for i := 0; i < count; i++ {
		id := "startup-project-" + string(rune('a'+i))
		candidates = append(candidates, domain.ArchaeologyCandidate{
			ID: id, Name: "Startup " + id, PathLabel: "Startup " + id, HasGit: true,
			DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low",
			PrivacyNote: "Metadata only.",
		})
		selected = append(selected, id)
	}
	ctx := context.Background()
	value, err := store.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{
		Principal: domain.HumanLocalPrincipal, RequestID: "startup-discovery",
	}, domain.ArchaeologyDiscovery{Candidates: candidates})
	if err != nil {
		t.Fatal(err)
	}
	value, err = store.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{
		Principal: domain.HumanLocalPrincipal, RequestID: "startup-configure", BaseRevision: value.Revision,
		Config: domain.ArchaeologyConfig{
			SelectedProjectIDs: selected, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: count,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{
		Principal: domain.HumanLocalPrincipal, RequestID: "startup-queue", BaseRevision: value.Revision,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatalf("claim startup job: %v", err)
	}
	session, err := store.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	jobs := make([]domain.ArchaeologyNativeJob, 0, count)
	for _, batch := range session.NativeBatches {
		for _, job := range batch.Jobs {
			if job.ID == claimed.ID {
				jobs = append([]domain.ArchaeologyNativeJob{job}, jobs...)
			} else {
				jobs = append(jobs, job)
			}
		}
	}
	if len(jobs) != count || jobs[0].State != "starting" {
		t.Fatalf("seeded native jobs=%+v", jobs)
	}
	return jobs
}

func startupPersistenceStamp(value time.Time) string {
	return value.UTC().Format(startupPersistenceStampLayout)
}

func ensureStartupFailStartIntent(t *testing.T, store *commonsstore.Store, job domain.ArchaeologyNativeJob) domain.ArchaeologyNativePersistenceIntentRecord {
	t.Helper()
	record, err := store.EnsureArchaeologyNativePersistenceIntent(context.Background(), domain.ArchaeologyNativePersistenceIntent{
		JobID: job.ID, Operation: domain.ArchaeologyNativePersistenceFailStart, Uncertain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestRuntimeHealthProbeTracksNativePersistenceLedger(t *testing.T) {
	store, ctx, now := openStartupPersistenceStore(t)
	if err := store.RecordReconciliationStatus(ctx, "healthy", now); err != nil {
		t.Fatal(err)
	}
	probe := runtimeHealthProbeForStore(store)
	clean, err := probe.Persistence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !clean.Healthy || clean.Reconciliation != "healthy" {
		t.Fatalf("clean persistence probe=%+v", clean)
	}

	jobs := seedStartupNativeJobs(t, store, 1)
	if record := ensureStartupFailStartIntent(t, store, jobs[0]); record.State != "pending" {
		t.Fatalf("new persistence intent=%+v", record)
	}
	pending, err := probe.Persistence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Healthy || !pending.PersistenceAttention {
		t.Fatalf("pending ledger was reported healthy: %+v", pending)
	}
	status, err := store.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil || status.Pending != 1 || status.Leased != 0 || status.Blocked != 0 {
		t.Fatalf("pending status=%+v err=%v", status, err)
	}

	if err = store.ReconcileArchaeologyNativePersistence(ctx); err != nil {
		t.Fatal(err)
	}
	applied, err := probe.Persistence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status, err = store.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Healthy || applied.PersistenceAttention || status.Pending != 0 || status.Leased != 0 || status.Blocked != 0 || status.Applied != 1 {
		t.Fatalf("drained persistence probe=%+v status=%+v", applied, status)
	}
	if err = store.RecordReconciliationStatus(ctx, "failed", now); err != nil {
		t.Fatal(err)
	}
	failed, err := probe.Persistence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Reconciliation != "failed" {
		t.Fatalf("failed reconciliation marker was masked: %+v", failed)
	}
}

func TestRuntimeHealthRecoversAttentionAfterDueLedgerDrain(t *testing.T) {
	store, ctx, now := openStartupPersistenceStore(t)
	if err := store.RecordReconciliationStatus(ctx, "attention", now); err != nil {
		t.Fatal(err)
	}
	jobs := seedStartupNativeJobs(t, store, 1)
	record, err := store.EnsureArchaeologyNativePersistenceIntent(ctx, domain.ArchaeologyNativePersistenceIntent{
		JobID: jobs[0].ID, Operation: domain.ArchaeologyNativePersistenceFailStart,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET next_attempt_at=? WHERE id=?`, startupPersistenceStamp(now.Add(time.Hour)), record.ID); err != nil {
		t.Fatal(err)
	}

	probe := runtimeHealthProbeForStore(store)
	probe.Supervisor = func(context.Context) (runtimeSupervisorResult, error) {
		return runtimeSupervisorResult{Configured: true, Available: true, Generation: 1, State: codexauth.ProcessStateAvailable}, nil
	}
	probe.Account = func(context.Context) (codexauth.AccountState, error) { return codexauth.AccountSignedIn, nil }
	probe.Compatibility = func(context.Context) (bool, error) { return true, nil }
	monitor := newRuntimeHealthMonitorForTest(runtimeHealthOptions{CodexConfigured: true, Probe: probe})
	defer monitor.Close()
	monitor.probeAndPublish(ctx)
	before := monitor.RuntimeSnapshot()
	if before.Ready || before.SchedulerEligible || before.Components.Reconciliation.Status != runtimehealth.ComponentDegraded || before.Components.Persistence.Status != runtimehealth.ComponentDegraded {
		t.Fatalf("future attention was not conservative: %+v", before)
	}

	if _, err = store.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET next_attempt_at=? WHERE id=?`, startupPersistenceStamp(now), record.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.ReconcileArchaeologyNativePersistence(ctx); err != nil {
		t.Fatal(err)
	}
	monitor.probeAndPublish(ctx)
	after := monitor.RuntimeSnapshot()
	if !after.Ready || !after.SchedulerEligible || after.Components.Reconciliation.Status != runtimehealth.ComponentHealthy || after.Components.Persistence.Status != runtimehealth.ComponentHealthy {
		t.Fatalf("cleared attention did not recover runtime eligibility: %+v", after)
	}
}

func TestRuntimeHealthProbeRespectsSQLiteWriterBoundary(t *testing.T) {
	store, ctx, now := openStartupPersistenceStore(t)
	if err := store.RecordReconciliationStatus(ctx, "healthy", now); err != nil {
		t.Fatal(err)
	}
	jobs := seedStartupNativeJobs(t, store, 1)
	record := ensureStartupFailStartIntent(t, store, jobs[0])
	if err := store.RecordReconciliationStatus(ctx, "attention", now); err != nil {
		t.Fatal(err)
	}
	probe := runtimeHealthProbeForStore(store)

	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE installation_status SET reconciliation_status='healthy' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET state='applied',next_attempt_at=NULL,lease_owner='',lease_expires_at=NULL,applied_at=?,updated_at=? WHERE id=?`, startupPersistenceStamp(now), startupPersistenceStamp(now), record.ID); err != nil {
		t.Fatal(err)
	}
	// The writer has not committed: one health statement must see the prior
	// committed attention/pending snapshot rather than a mixed half-update.
	beforeCommit, err := probe.Persistence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if beforeCommit.Reconciliation != "attention" || !beforeCommit.PersistenceAttention || beforeCommit.Healthy {
		t.Fatalf("probe crossed uncommitted writer boundary: %+v", beforeCommit)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	afterCommit, err := probe.Persistence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterCommit.Reconciliation != "healthy" || afterCommit.PersistenceAttention || !afterCommit.Healthy {
		t.Fatalf("probe did not observe committed healthy snapshot: %+v", afterCommit)
	}
}

func TestNativePersistenceStartupAcceptsOnlyExactDeferredSentinel(t *testing.T) {
	backlog := domain.ArchaeologyNativePersistenceStatus{Pending: 1}
	if !safeNativePersistenceBacklog(domain.ErrUnavailable, backlog) {
		t.Fatal("exact deferred persistence sentinel was not accepted")
	}
	if safeNativePersistenceBacklog(fmt.Errorf("database busy: %w", domain.ErrUnavailable), backlog) {
		t.Fatal("wrapped database-unavailable error was treated as safe deferred work")
	}
	if safeNativePersistenceBacklog(domain.ErrUnavailable, domain.ArchaeologyNativePersistenceStatus{}) {
		t.Fatal("deferred sentinel with a clean ledger was treated as safe backlog")
	}
}

func TestNativePersistenceBacklogIsAttentionAndCannotClaim(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *commonsstore.Store, domain.ArchaeologyNativePersistenceIntentRecord, time.Time)
	}{
		{
			name: "future pending",
			mutate: func(t *testing.T, store *commonsstore.Store, record domain.ArchaeologyNativePersistenceIntentRecord, now time.Time) {
				t.Helper()
				_, err := store.DB().ExecContext(context.Background(), `UPDATE archaeology_native_persistence_intents SET next_attempt_at=? WHERE id=?`, startupPersistenceStamp(now.Add(time.Hour)), record.ID)
				if err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "live lease",
			mutate: func(t *testing.T, store *commonsstore.Store, record domain.ArchaeologyNativePersistenceIntentRecord, now time.Time) {
				t.Helper()
				_, err := store.DB().ExecContext(context.Background(), `UPDATE archaeology_native_persistence_intents SET state='leased',lease_owner=?,lease_expires_at=?,next_attempt_at=? WHERE id=?`, "live-worker", startupPersistenceStamp(now.Add(time.Hour)), startupPersistenceStamp(now), record.ID)
				if err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "blocked",
			mutate: func(t *testing.T, store *commonsstore.Store, record domain.ArchaeologyNativePersistenceIntentRecord, _ time.Time) {
				t.Helper()
				_, err := store.DB().ExecContext(context.Background(), `UPDATE archaeology_native_persistence_intents SET state='blocked',attempts=8,next_attempt_at=NULL,last_error_code='max_attempts' WHERE id=?`, record.ID)
				if err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, ctx, now := openStartupPersistenceStore(t)
			if err := store.RecordReconciliationStatus(ctx, "healthy", now); err != nil {
				t.Fatal(err)
			}
			jobs := seedStartupNativeJobs(t, store, 2)
			record := ensureStartupFailStartIntent(t, store, jobs[0])
			test.mutate(t, store, record, now)

			if err := store.ReconcileArchaeologyNativePersistence(ctx); !errors.Is(err, domain.ErrUnavailable) {
				t.Fatalf("reconcile error=%v, want durable attention sentinel", err)
			}
			value, err := runtimeHealthProbeForStore(store).Persistence(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if value.Healthy || !value.PersistenceAttention {
				t.Fatalf("backlog probe was green: %+v", value)
			}
			monitor := newRuntimeHealthMonitorForTest(runtimeHealthOptions{Probe: runtimeHealthProbeForStore(store)})
			monitor.probeAndPublish(ctx)
			defer monitor.Close()
			snapshot := monitor.RuntimeSnapshot()
			if snapshot.Ready || snapshot.SchedulerEligible || snapshot.Components.Persistence.Status != runtimehealth.ComponentDegraded || snapshot.Components.Persistence.Reason != runtimehealth.ReasonPersistenceAttention {
				t.Fatalf("backlog runtime snapshot was green/eligible: %+v", snapshot)
			}
			if _, err = store.ClaimArchaeologyNativeJob(ctx); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("claim crossed persistence gate: %v", err)
			}
		})
	}
}

func TestExpiredNativePersistenceLeaseIsRequeuedBeforeClaims(t *testing.T) {
	store, ctx, now := openStartupPersistenceStore(t)
	if err := store.RecordReconciliationStatus(ctx, "healthy", now); err != nil {
		t.Fatal(err)
	}
	jobs := seedStartupNativeJobs(t, store, 2)
	record := ensureStartupFailStartIntent(t, store, jobs[0])
	_, err := store.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET state='leased',lease_owner=?,lease_expires_at=?,next_attempt_at=? WHERE id=?`, "stale-worker", startupPersistenceStamp(now.Add(-time.Minute)), startupPersistenceStamp(now), record.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err = store.ReconcileArchaeologyNativePersistence(ctx); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("expired lease reconciliation error=%v", err)
	}
	recovered, err := store.ArchaeologyNativePersistenceIntent(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != "pending" || recovered.LeaseOwner != "" || recovered.LeaseExpiresAt != nil || recovered.Attempts != 1 || recovered.NextAttemptAt == nil || !recovered.NextAttemptAt.After(now) {
		t.Fatalf("expired lease was not durably requeued: %+v", recovered)
	}
	if _, err = store.ClaimArchaeologyNativeJob(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("claim crossed recovered lease gate: %v", err)
	}
}
