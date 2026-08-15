package historicalimport

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func loadFixture(t *testing.T) (Manifest, CurrentSnapshot) {
	t.Helper()
	manifestFile, err := os.Open("manifests/codex-commons.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := Decode(manifestFile)
	if closeErr := manifestFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	snapshotFile, err := os.Open("snapshots/codex-commons-current.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := DecodeSnapshot(snapshotFile)
	if closeErr := snapshotFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return manifest, snapshot
}

func cloneManifest(t *testing.T, manifest Manifest) Manifest {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var clone Manifest
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func issueCodes(issues []ValidationIssue) map[string]bool {
	out := map[string]bool{}
	for _, issue := range issues {
		out[issue.Code] = true
	}
	return out
}

func TestCodexCommonsPreviewCorpus(t *testing.T) {
	manifest, snapshot := loadFixture(t)
	sourceIssues := VerifySourceFiles(manifest, "..")
	if len(sourceIssues) != 0 {
		t.Fatalf("source verification issues: %+v", sourceIssues)
	}
	report, err := BuildPreview(manifest, snapshot, sourceIssues...)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ApplyEligible || report.Status != "ready_for_review" || report.NetworkCalls != 0 {
		t.Fatalf("unexpected preview state: %+v", report)
	}
	want := PreviewCounts{
		Tasks: 20, ProjectThreadAliases: 4, TaskSessions: 37,
		AccountedSessions: 41, AttributionLinks: 37, Events: 13,
		Planned: 20, SkippedCurrent: 0, Ambiguous: 0,
	}
	if report.Counts != want {
		t.Fatalf("counts=%+v want=%+v", report.Counts, want)
	}
	if len(report.Collisions) != 0 || len(report.RedactionFindings) != 0 || len(report.Blockers) != 0 {
		t.Fatalf("unexpected findings: collisions=%+v redactions=%+v blockers=%+v", report.Collisions, report.RedactionFindings, report.Blockers)
	}

	rootSessions := map[string]bool{}
	taskSessions := map[string]bool{}
	for _, session := range manifest.Sessions {
		switch session.Kind {
		case "root_alias":
			rootSessions[session.ID] = true
		case "subagent":
			taskSessions[session.ID] = true
		default:
			t.Fatalf("unexpected session kind %q", session.Kind)
		}
	}
	if len(rootSessions) != 4 || len(taskSessions) != 37 {
		t.Fatalf("root=%d task=%d", len(rootSessions), len(taskSessions))
	}
	linked := map[string]int{}
	for _, task := range manifest.Tasks {
		if task.State != "done" || sourceByRef(manifest.Sources, task.CompletionSourceRef).OccurredAt == "" {
			t.Fatalf("task lacks immutable completion source: %+v", task)
		}
		for _, attribution := range task.Attributions {
			if rootSessions[attribution.SessionID] {
				t.Fatalf("root alias became task contributor: %s", attribution.SessionID)
			}
			linked[attribution.SessionID]++
		}
		for _, event := range task.Events {
			if rootSessions[event.SessionID] {
				t.Fatalf("root alias became event author: %s", event.SessionID)
			}
		}
	}
	if len(linked) != 37 {
		t.Fatalf("linked task sessions=%d", len(linked))
	}
	for sessionID := range taskSessions {
		if linked[sessionID] != 1 {
			t.Fatalf("task session %s linked %d times", sessionID, linked[sessionID])
		}
	}
}

func TestCurrentWinsAndIdempotencyAreStable(t *testing.T) {
	manifest, snapshot := loadFixture(t)
	snapshot.Tasks = append(snapshot.Tasks, CurrentTask{
		Key: "history-product-model", ID: "T-current",
		Title: "Current canonical product model", State: "done",
	})
	first, err := BuildPreview(manifest, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPreview(manifest, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceDigest != second.SourceDigest || first.ManifestDigest != second.ManifestDigest {
		t.Fatal("preview digests changed without input change")
	}
	if first.Counts.SkippedCurrent != 1 || first.Counts.Planned != 19 || len(first.Collisions) != 1 {
		t.Fatalf("collision report=%+v counts=%+v", first.Collisions, first.Counts)
	}
	if first.Collisions[0].Resolution != "current_wins" || first.Collisions[0].MatchedBy != "stable_key" {
		t.Fatalf("collision=%+v", first.Collisions[0])
	}
	if first.Tasks[0].IdempotencyKey != second.Tasks[0].IdempotencyKey {
		t.Fatal("idempotency key is not stable")
	}
	request, err := BuildApplyRequest(manifest, first, first.SourceDigest, first.ManifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Tasks) != 20 {
		t.Fatalf("canonical apply must receive all 20 tasks, got %d", len(request.Tasks))
	}
	changed := cloneManifest(t, manifest)
	changed.Tasks[0].Title += " changed"
	changedDigest, err := SourceDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedDigest == first.SourceDigest {
		t.Fatal("source digest did not attest a changed reconstructed task")
	}
}

func TestValidationProtectsLineageAndSourceDedupe(t *testing.T) {
	manifest, _ := loadFixture(t)
	rootID := manifest.ProjectThreadAliases[0].SessionID
	bad := cloneManifest(t, manifest)
	bad.Tasks[0].Attributions = append(bad.Tasks[0].Attributions, Attribution{
		SessionID: rootID, Role: "originator", Confidence: "verified", SourceRef: "root-01",
	})
	bad.Sources = append(bad.Sources, bad.Sources[0])
	codes := issueCodes(Validate(bad))
	if !codes["root_alias_task_link"] || !codes["duplicate"] {
		t.Fatalf("expected lineage and source dedupe errors, got %+v", codes)
	}
}

func TestTaskEventSessionMustBeAttributedToSameTask(t *testing.T) {
	manifest, _ := loadFixture(t)
	bad := cloneManifest(t, manifest)
	taskIndex := -1
	for i := range bad.Tasks {
		if len(bad.Tasks[i].Events) > 0 {
			taskIndex = i
			break
		}
	}
	if taskIndex < 0 {
		t.Fatal("fixture requires a task event")
	}
	attributed := map[string]bool{}
	for _, attribution := range bad.Tasks[taskIndex].Attributions {
		attributed[attribution.SessionID] = true
	}
	otherSession := ""
	for _, session := range bad.Sessions {
		if session.Kind == "subagent" && !attributed[session.ID] {
			otherSession = session.ID
			break
		}
	}
	if otherSession == "" {
		t.Fatal("fixture requires a subagent attributed to a different task")
	}
	bad.Tasks[taskIndex].Events[0].SessionID = otherSession
	wantPath := fmt.Sprintf("tasks[%d].events[0].session_id", taskIndex)
	for _, issue := range Validate(bad) {
		if issue.Path == wantPath && issue.Code == "event_session_not_attributed" && issue.Blocker {
			return
		}
	}
	t.Fatalf("missing event_session_not_attributed blocker at %s", wantPath)
}

func TestAmbiguousNormalizedTitleBlocksWithoutStableKey(t *testing.T) {
	manifest, snapshot := loadFixture(t)
	title := manifest.Tasks[0].Title
	snapshot.Tasks = append(snapshot.Tasks,
		CurrentTask{Key: "legacy-title-a", ID: "T-ambiguous-a", Title: title, State: "done"},
		CurrentTask{Key: "legacy-title-b", ID: "T-ambiguous-b", Title: "  " + strings.ToUpper(title) + "  ", State: "done"},
	)
	report, err := BuildPreview(manifest, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if report.ApplyEligible || report.Counts.Ambiguous != 1 || report.Counts.Planned != 19 || report.Counts.SkippedCurrent != 0 {
		t.Fatalf("unexpected ambiguous preview: eligible=%v counts=%+v", report.ApplyEligible, report.Counts)
	}
	if report.Tasks[0].Disposition != "blocked_ambiguous" {
		t.Fatalf("disposition=%q", report.Tasks[0].Disposition)
	}
	found := false
	for _, blocker := range report.Blockers {
		if blocker.Path == "tasks[0].title" && blocker.Code == "ambiguous_normalized_title" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing ambiguous_normalized_title blocker: %+v", report.Blockers)
	}
	for _, collision := range report.Collisions {
		if collision.TaskKey == manifest.Tasks[0].Key {
			t.Fatalf("ambiguous title was resolved arbitrarily: %+v", collision)
		}
	}
}

func TestExactStableKeyDisambiguatesDuplicateNormalizedTitles(t *testing.T) {
	manifest, snapshot := loadFixture(t)
	title := manifest.Tasks[0].Title
	snapshot.Tasks = append(snapshot.Tasks,
		CurrentTask{Key: manifest.Tasks[0].Key, ID: "T-exact", Title: title, State: "done"},
		CurrentTask{Key: "legacy-title", ID: "T-same-title", Title: strings.ToUpper(title), State: "done"},
	)
	report, err := BuildPreview(manifest, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ApplyEligible || report.Counts.Ambiguous != 0 || report.Counts.SkippedCurrent != 1 || report.Counts.Planned != 19 {
		t.Fatalf("unexpected exact-key preview: eligible=%v counts=%+v blockers=%+v", report.ApplyEligible, report.Counts, report.Blockers)
	}
	if len(report.Collisions) != 1 || report.Collisions[0].MatchedBy != "stable_key" || report.Collisions[0].CurrentID != "T-exact" {
		t.Fatalf("stable key did not disambiguate: %+v", report.Collisions)
	}
}

func TestCurrentSnapshotRejectsDuplicateStableKeys(t *testing.T) {
	input := `{"schema_version":1,"project_id":"codex-commons","tasks":[` +
		`{"key":"same","id":"T-1","title":"One","state":"done"},` +
		`{"key":"same","id":"T-2","title":"Two","state":"done"}]}`
	if _, err := DecodeSnapshot(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "repeats task key") {
		t.Fatalf("duplicate current stable key must be rejected, got %v", err)
	}
}
func TestValidationReportsForbiddenMaterial(t *testing.T) {
	manifest, _ := loadFixture(t)
	bad := cloneManifest(t, manifest)
	bad.Tasks[0].Description = "Contact person@example.invalid"
	bad.Tasks[1].Description = "api_key=REDACTED"
	bad.Sources[0].Locator = "/safe/.codex/generated_images/example.png"
	codes := issueCodes(Validate(bad))
	for _, code := range []string{"possible_email", "possible_secret", "generated_image_path"} {
		if !codes[code] {
			t.Fatalf("missing redaction code %s in %+v", code, codes)
		}
	}
	if _, _, err := Decode(strings.NewReader(`{"schema_version":1,"prompt":"do not import me"}`)); err == nil {
		t.Fatal("unknown transcript-like fields must be rejected")
	}
}
func TestSourceIdentityPrivacyScanningDoesNotDependOnObservedAt(t *testing.T) {
	manifest, _ := loadFixture(t)
	if manifest.Sources[0].ObservedAt != "" {
		t.Fatal("fixture must exercise the empty observed_at path")
	}
	tests := []struct {
		name, field, value, code string
	}{
		{name: "kind secret", field: "kind", value: "api_key=REDACTED", code: "possible_secret"},
		{name: "kind email", field: "kind", value: "person@example.invalid", code: "possible_email"},
		{name: "kind private path", field: "kind", value: "/home/example/kind", code: "private_path"},
		{name: "stable id secret", field: "stable_id", value: "private_key=REDACTED", code: "possible_secret"},
		{name: "stable id email", field: "stable_id", value: "person@example.invalid", code: "possible_email"},
		{name: "stable id private path", field: "stable_id", value: "/home/example/source", code: "private_path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bad := cloneManifest(t, manifest)
			switch test.field {
			case "kind":
				bad.Sources[0].Kind = test.value
			case "stable_id":
				bad.Sources[0].StableID = test.value
			default:
				t.Fatalf("unknown field %q", test.field)
			}
			wantPath := "sources[0]." + test.field
			found := false
			for _, issue := range Validate(bad) {
				if issue.Path == wantPath && issue.Code == test.code {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing %s at %s", test.code, wantPath)
			}
		})
	}
}

func TestSourceFileVerificationDetectsMismatchAndEscape(t *testing.T) {
	manifest, _ := loadFixture(t)
	badDigest := cloneManifest(t, manifest)
	badDigest.Sources[0].Digest = "sha256:" + strings.Repeat("0", 64)
	if !issueCodes(VerifySourceFiles(badDigest, ".."))["source_digest_mismatch"] {
		t.Fatal("source digest mismatch was not reported")
	}
	escape := cloneManifest(t, manifest)
	escape.Sources[0].Locator = "../private-source"
	if !issueCodes(VerifySourceFiles(escape, ".."))["unsafe_source_locator"] {
		t.Fatal("escaping source locator was not rejected")
	}
}

func TestApplyContractRequiresReviewedDigestAndIsAtomic(t *testing.T) {
	manifest, snapshot := loadFixture(t)
	report, err := BuildPreview(manifest, snapshot, VerifySourceFiles(manifest, "..")...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildApplyRequest(manifest, report, "", report.ManifestDigest); err == nil {
		t.Fatal("missing digest confirmation must fail")
	}
	if _, err := BuildApplyRequest(manifest, report, report.SourceDigest, ""); err == nil {
		t.Fatal("missing manifest confirmation must fail")
	}
	request, err := BuildApplyRequest(manifest, report, report.SourceDigest, report.ManifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Tasks) != 20 || len(request.ProjectThreadAliases) != 4 {
		t.Fatalf("apply request counts tasks=%d aliases=%d", len(request.Tasks), len(request.ProjectThreadAliases))
	}
	rootSessions := map[string]bool{}
	for _, alias := range request.ProjectThreadAliases {
		rootSessions[alias.Session] = true
		if alias.Source.OccurredAt == "" {
			t.Fatal("alias source time is missing")
		}
	}
	for _, task := range request.Tasks {
		if task.State != "done" || task.Source.OccurredAt == "" {
			t.Fatalf("invalid apply task %+v", task)
		}
		for _, attribution := range task.Attributions {
			if rootSessions[attribution.Session] {
				t.Fatalf("root alias in apply task %s", task.Key)
			}
		}
	}
	if ReplayDisposition("", "", request.BatchID, request.SourceDigest) != "new" ||
		ReplayDisposition(request.BatchID, request.SourceDigest, request.BatchID, request.SourceDigest) != "replay" ||
		ReplayDisposition(request.BatchID, request.SourceDigest, request.BatchID, "sha256:"+strings.Repeat("f", 64)) != "conflict" {
		t.Fatal("unexpected replay semantics")
	}
	partial := ApplyReceipt{
		BatchID: request.BatchID, SourceDigest: request.SourceDigest, Applied: false,
		Tasks: []TaskReceipt{{Key: "one", ID: "T-partial", Disposition: "created"}},
	}
	if ValidateAtomicReceipt(partial) == nil {
		t.Fatal("partial non-applied receipt must fail")
	}
	complete := ApplyReceipt{
		BatchID: request.BatchID, SourceDigest: request.SourceDigest, Applied: true,
		RecordedAt: "2026-08-10T21:00:00Z",
		Tasks:      []TaskReceipt{{Key: "one", ID: "T-1", Disposition: "created"}},
		Counts:     ReceiptCounts{Created: 1},
	}
	if err := ValidateAtomicReceipt(complete); err != nil {
		t.Fatalf("valid atomic receipt: %v", err)
	}
}
func TestCheckedInPreviewIsFresh(t *testing.T) {
	manifest, snapshot := loadFixture(t)
	report, err := BuildPreview(manifest, snapshot, VerifySourceFiles(manifest, "..")...)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	got, err := os.ReadFile("previews/codex-commons.v1.preview.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("checked-in preview is stale; regenerate it with the offline CLI")
	}
}
