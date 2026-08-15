package appbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	commonsstore "codex-commons/internal/store"
)

func TestHistoricalImportHTTPIsHumanOnlyAndAttestsRecorder(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	clock := integrationClock{now: now}
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "historical-import.sqlite"), commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.CreateCanonicalProject(ctx, domain.CreateProjectCommand{
		ID: "commons", Name: "Codex Commons", Purpose: "Dogfood continuity",
		Meta: domain.CoreWriteMeta{ActorID: "admin", SessionID: "human", RequestID: "create-commons"},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(store, nil, clock)
	backend, err := New(legacyStub{}, service)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(backend, httpapi.Config{
		Credentials: []httpapi.Credential{{BearerToken: "agent-secret", Actor: "agent-1", Session: "S-agent", Host: "plumbob"}},
		HumanAuth: &httpapi.HumanAuthConfig{AdminSecret: projectCoreAdminSecret, DisplayName: "Admin", Actor: "local-admin",
			Session: "human-local-admin", Host: "browser", SessionTTL: time.Hour, RecoveryEnabled: true},
	})
	completed := now.Add(-2 * time.Hour)
	digest := func(value string) string { return "sha256:" + strings.Repeat(value, 64) }
	request := application.HistoricalImportRequest{
		SchemaVersion: 1, BatchID: "http-history", SourceDigest: digest("a"), CollisionPolicy: "current_wins",
		ProjectThreadAliases: []application.HistoricalProjectThreadAliasRequest{{
			Alias: "root", Session: "S-root",
			Source: application.HistoricalSourceRequest{Kind: "codex_session_uuidv7", StableID: "root-thread", Digest: digest("b"), OccurredAt: completed.Add(-time.Hour)},
		}},
		Tasks: []application.HistoricalTaskRequest{{
			Key: "outcome", Title: "Historical outcome", Description: "Verified durable result", Acceptance: "Receipt preserved", State: "done",
			Source: application.HistoricalSourceRequest{Kind: "codex_turn_audit", StableID: "completion-turn", Digest: digest("c"), OccurredAt: completed},
			Attributions: []application.HistoricalAttributionRequest{{
				Session: "S-worker", Role: "implementer", Confidence: "verified",
				Source: application.HistoricalSourceRequest{Kind: "codex_session_uuidv7", StableID: "worker-thread", Digest: digest("d"), OccurredAt: completed},
			}},
			Events: []application.HistoricalTaskEventRequest{{
				Key: "review", Kind: "reviewed", Summary: "Review completed", Session: "S-worker", Confidence: "verified",
				Source: application.HistoricalSourceRequest{Kind: "repository_document", StableID: "review-receipt", Digest: digest("e"), OccurredAt: completed},
			}},
		}},
	}
	previewBody, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	agentPreview := projectCoreRequest(handler, http.MethodPost, "/v1/projects/commons/historical-imports/preview", string(previewBody), "agent-secret", nil, "", "agent-preview")
	if agentPreview.Code != http.StatusForbidden {
		t.Fatalf("agent preview code=%d body=%s", agentPreview.Code, agentPreview.Body.String())
	}
	cookie, csrf := loginProjectCoreHuman(t, handler)
	missingCSRF := projectCoreRequest(handler, http.MethodPost, "/v1/projects/commons/historical-imports/preview", string(previewBody), "", cookie, "", "missing-csrf")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf code=%d body=%s", missingCSRF.Code, missingCSRF.Body.String())
	}
	spoofedBody := strings.TrimSuffix(string(previewBody), "}") + `,"recorded_by_actor":"spoof"}`
	spoofed := projectCoreRequest(handler, http.MethodPost, "/v1/projects/commons/historical-imports/preview", spoofedBody, "", cookie, csrf, "spoofed-recorder")
	if spoofed.Code != http.StatusBadRequest {
		t.Fatalf("caller asserted recorder code=%d body=%s", spoofed.Code, spoofed.Body.String())
	}
	preview := projectCoreRequest(handler, http.MethodPost, "/v1/projects/commons/historical-imports/preview", string(previewBody), "", cookie, csrf, "history-preview")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"state":"preview"`) || !strings.Contains(preview.Body.String(), `"project_thread_aliases":1`) {
		t.Fatalf("preview code=%d body=%s", preview.Code, preview.Body.String())
	}
	var batches int
	if err = store.DB().QueryRowContext(ctx, "SELECT count(*) FROM historical_import_batches").Scan(&batches); err != nil || batches != 0 {
		t.Fatalf("preview mutated batches=%d err=%v", batches, err)
	}
	request.ConfirmSourceDigest = request.SourceDigest
	var previewEnvelope struct {
		Data httpapi.HistoricalImportResult `json:"data"`
	}
	if err = json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil || previewEnvelope.Data.ManifestDigest == "" {
		t.Fatalf("preview manifest decode err=%v data=%+v", err, previewEnvelope.Data)
	}
	request.ConfirmManifestDigest = previewEnvelope.Data.ManifestDigest
	applyBody, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	agentApply := projectCoreRequest(handler, http.MethodPost, "/v1/projects/commons/historical-imports/apply", string(applyBody), "agent-secret", nil, "", "agent-apply")
	if agentApply.Code != http.StatusForbidden {
		t.Fatalf("agent apply code=%d body=%s", agentApply.Code, agentApply.Body.String())
	}
	apply := projectCoreRequest(handler, http.MethodPost, "/v1/projects/commons/historical-imports/apply", string(applyBody), "", cookie, csrf, "history-apply")
	if apply.Code != http.StatusOK {
		t.Fatalf("apply code=%d body=%s", apply.Code, apply.Body.String())
	}
	var envelope struct {
		Data httpapi.HistoricalImportResult `json:"data"`
	}
	if err = json.Unmarshal(apply.Body.Bytes(), &envelope); err != nil || !envelope.Data.Applied || envelope.Data.Counts.Created != 1 || len(envelope.Data.Tasks) != 1 {
		t.Fatalf("apply decode err=%v data=%+v body=%s", err, envelope.Data, apply.Body.String())
	}
	replay := projectCoreRequest(handler, http.MethodPost, "/v1/projects/commons/historical-imports/apply", string(applyBody), "", cookie, csrf, "history-apply")
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), envelope.Data.ManifestDigest) || !strings.Contains(replay.Body.String(), envelope.Data.Tasks[0].ID) {
		t.Fatalf("replay code=%d body=%s", replay.Code, replay.Body.String())
	}
	open := projectCoreRequest(handler, http.MethodGet, "/v1/tasks/"+envelope.Data.Tasks[0].ID+"?events_limit=50", "", "agent-secret", nil, "", "")
	if open.Code != http.StatusOK || !strings.Contains(open.Body.String(), `"kind":"historical"`) ||
		!strings.Contains(open.Body.String(), `"session":"S-worker"`) ||
		!strings.Contains(open.Body.String(), `"recorded_by":{"actor":"local-admin","session":"human-local-admin"}`) ||
		strings.Contains(open.Body.String(), `"session":"S-root"`) {
		t.Fatalf("historical provenance open code=%d body=%s", open.Code, open.Body.String())
	}
	var fabricated int
	if err = store.DB().QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE id IN ('S-root','S-worker')").Scan(&fabricated); err != nil || fabricated != 0 {
		t.Fatalf("fabricated historical sessions=%d err=%v", fabricated, err)
	}
}
