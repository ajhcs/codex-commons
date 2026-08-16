package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/internal/server"
	commonsstore "codex-commons/internal/store"
)

func TestServerStartupDrainsDueNativePersistenceBeforeReturning(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	repository, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	discovery := domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{
		{ID: "startup-due-a", Name: "Startup Due A", PathLabel: "Startup Due A", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"},
		{ID: "startup-due-b", Name: "Startup Due B", PathLabel: "Startup Due B", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"},
	}}
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "startup-due-discover"}, discovery)
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{
		Principal: domain.HumanLocalPrincipal, RequestID: "startup-due-configure", BaseRevision: value.Revision,
		Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"startup-due-a", "startup-due-b"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "startup-due-queue", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := repository.EnsureArchaeologyNativePersistenceIntent(ctx, domain.ArchaeologyNativePersistenceIntent{
		JobID: claimed.ID, Operation: domain.ArchaeologyNativePersistenceFailStart, Uncertain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if intent.State != "pending" {
		t.Fatalf("durable startup intent=%+v", intent)
	}
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}

	client := &startupRecoveryCodexClient{continuationCodexClient: continuationCodexClient{loginID: "startup-due", email: "startup-due@example.com"}}
	config := server.DefaultConfig()
	config.DatabasePath = database
	config.WebDir = testWeb(t)
	config.CodexAuth = true
	config.CodexBin = "/usr/bin/codex"
	config.CodexBindingKeySet = true
	config.CodexBindingKey[0] = 1
	config.CodexClient = client
	config.EnableExperimentalHistorian = true
	app, err := server.New(ctx, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	verifier, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer verifier.Close()
	status, err := verifier.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending != 0 || status.Leased != 0 || status.Blocked != 0 || status.Applied != 1 {
		t.Fatalf("startup did not drain due ledger before scheduler wake: %+v", status)
	}
	// This is deliberately an immediate startup-return assertion. The Store
	// claim gate and the deterministic application scheduler drain tests own
	// the longer-lived no-claim guarantee.
	if client.launches.Load() != 0 {
		t.Fatalf("startup returned after an unexpected launch: launches=%d", client.launches.Load())
	}
	session, err := verifier.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]int{}
	for _, batch := range session.NativeBatches {
		for _, job := range batch.Jobs {
			states[job.State]++
		}
	}
	if states["uncertain"] != 1 || states["queued"] != 1 {
		t.Fatalf("startup returned with unexpected native states: states=%v", states)
	}
}

func TestServerAllowsSafeFutureNativePersistenceBacklogButStaysNonGreen(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	repository, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	discovery := domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{
		{ID: "startup-future", Name: "Startup Future", PathLabel: "Startup Future", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"},
	}}
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "startup-future-discover"}, discovery)
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{
		Principal: domain.HumanLocalPrincipal, RequestID: "startup-future-configure", BaseRevision: value.Revision,
		Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"startup-future"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "startup-future-queue", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := repository.EnsureArchaeologyNativePersistenceIntent(ctx, domain.ArchaeologyNativePersistenceIntent{
		JobID: claimed.ID, Operation: domain.ArchaeologyNativePersistenceFailStart, Uncertain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour).Format("2006-01-02T15:04:05.000000000Z07:00")
	if _, err = repository.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET next_attempt_at=? WHERE id=?`, future, intent.ID); err != nil {
		t.Fatal(err)
	}
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}

	config := server.DefaultConfig()
	config.DatabasePath = database
	config.WebDir = testWeb(t)
	app, err := server.New(ctx, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	readinessRequest := httptest.NewRequest(http.MethodGet, "http://"+config.Listen+"/v1/internal/readiness", nil)
	readinessRequest.Host = config.Listen
	readinessRequest.RemoteAddr = "127.0.0.1:43127"
	readyRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(readyRecorder, readinessRequest)
	ready := readyRecorder
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("safe future backlog became ready: code=%d body=%s", ready.Code, ready.Body.String())
	}

	verifier, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer verifier.Close()
	var reconciliation string
	if err = verifier.DB().QueryRowContext(ctx, `SELECT reconciliation_status FROM installation_status WHERE id=1`).Scan(&reconciliation); err != nil {
		t.Fatal(err)
	}
	if reconciliation != "attention" {
		t.Fatalf("safe native backlog status=%q, want attention", reconciliation)
	}
	status, err := verifier.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending != 1 || status.Leased != 0 || status.Blocked != 0 {
		t.Fatalf("future ledger unexpectedly drained: %+v", status)
	}
	pending, err := verifier.ArchaeologyNativePersistenceIntent(ctx, intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != "pending" {
		t.Fatalf("future ledger state=%+v", pending)
	}
}

func prepareConfiguredSchedulerBacklog(t *testing.T, backlogState string) string {
	t.Helper()
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	repository, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	discovery := domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{
		{ID: "configured-backlog-a", Name: "Configured Backlog A", PathLabel: "Configured Backlog A", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"},
		{ID: "configured-backlog-b", Name: "Configured Backlog B", PathLabel: "Configured Backlog B", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"},
	}}
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configured-backlog-discover"}, discovery)
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{
		Principal: domain.HumanLocalPrincipal, RequestID: "configured-backlog-configure", BaseRevision: value.Revision,
		Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"configured-backlog-a", "configured-backlog-b"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "configured-backlog-queue", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := repository.EnsureArchaeologyNativePersistenceIntent(ctx, domain.ArchaeologyNativePersistenceIntent{
		JobID: claimed.ID, Operation: domain.ArchaeologyNativePersistenceFailStart, Uncertain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	stamp := func(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") }
	switch backlogState {
	case "future":
		_, err = repository.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET next_attempt_at=? WHERE id=?`, stamp(now.Add(time.Hour)), intent.ID)
	case "live":
		_, err = repository.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET state='leased',lease_owner=?,lease_expires_at=?,next_attempt_at=? WHERE id=?`, "configured-live-worker", stamp(now.Add(time.Hour)), stamp(now), intent.ID)
	case "blocked":
		_, err = repository.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET state='blocked',attempts=8,next_attempt_at=NULL,last_error_code='max_attempts' WHERE id=?`, intent.ID)
	default:
		t.Fatalf("unknown backlog state %q", backlogState)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestConfiguredSchedulerStartsWithNativePersistenceBacklogBlocked(t *testing.T) {
	for _, backlogState := range []string{"future", "live", "blocked"} {
		t.Run(backlogState, func(t *testing.T) {
			database := prepareConfiguredSchedulerBacklog(t, backlogState)
			client := &startupRecoveryCodexClient{continuationCodexClient: continuationCodexClient{loginID: "configured-" + backlogState, email: "configured-" + backlogState + "@example.com"}}
			config := server.DefaultConfig()
			config.DatabasePath = database
			config.WebDir = testWeb(t)
			config.CodexAuth = true
			config.CodexBin = "/usr/bin/codex"
			config.CodexBindingKeySet = true
			config.CodexBindingKey[0] = 1
			config.CodexClient = client
			config.EnableExperimentalHistorian = true
			app, err := server.New(context.Background(), config, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()

			// Keep this scoped to the startup-return boundary; the deterministic
			// application drain test and Store claim predicate cover persistence
			// blocking after the scheduler is running.
			if client.launches.Load() != 0 {
				t.Fatalf("configured scheduler launched during %s startup: %d", backlogState, client.launches.Load())
			}
			readinessRequest := httptest.NewRequest(http.MethodGet, "http://"+config.Listen+"/v1/internal/readiness", nil)
			readinessRequest.Host = config.Listen
			readinessRequest.RemoteAddr = "127.0.0.1:43129"
			readyRecorder := httptest.NewRecorder()
			app.Handler().ServeHTTP(readyRecorder, readinessRequest)
			if readyRecorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("configured %s backlog became ready: code=%d body=%s", backlogState, readyRecorder.Code, readyRecorder.Body.String())
			}

			verifier, err := commonsstore.Open(context.Background(), database)
			if err != nil {
				t.Fatal(err)
			}
			defer verifier.Close()
			status, err := verifier.ArchaeologyNativePersistenceStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status.Healthy() {
				t.Fatalf("configured %s backlog became healthy: %+v", backlogState, status)
			}
			session, err := verifier.ArchaeologySession(context.Background(), domain.HumanLocalPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			states := map[string]int{}
			for _, batch := range session.NativeBatches {
				for _, job := range batch.Jobs {
					states[job.State]++
				}
			}
			if states["uncertain"] != 1 || states["queued"] != 1 {
				t.Fatalf("configured %s startup returned with unexpected states: states=%v", backlogState, states)
			}
		})
	}
}

func TestServerCleanReopenPublishesHealthyNativePersistence(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	config := server.DefaultConfig()
	config.DatabasePath = database
	config.WebDir = testWeb(t)
	first, err := server.New(ctx, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := server.New(ctx, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	readinessRequest := httptest.NewRequest(http.MethodGet, "http://"+config.Listen+"/v1/internal/readiness", nil)
	readinessRequest.Host = config.Listen
	readinessRequest.RemoteAddr = "127.0.0.1:43128"
	readyRecorder := httptest.NewRecorder()
	second.Handler().ServeHTTP(readyRecorder, readinessRequest)
	if readyRecorder.Code != http.StatusOK {
		t.Fatalf("clean reopen was not ready: code=%d body=%s", readyRecorder.Code, readyRecorder.Body.String())
	}
	verifier, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer verifier.Close()
	status, err := verifier.ArchaeologyNativePersistenceStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Healthy() {
		t.Fatalf("clean reopen persistence status=%+v", status)
	}
	var reconciliation string
	if err = verifier.DB().QueryRowContext(ctx, `SELECT reconciliation_status FROM installation_status WHERE id=1`).Scan(&reconciliation); err != nil {
		t.Fatal(err)
	}
	if reconciliation != "healthy" {
		t.Fatalf("clean reopen reconciliation status=%q", reconciliation)
	}
}
