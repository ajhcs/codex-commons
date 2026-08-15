package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	historicalimport "codex-commons/historical-import"
	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

func TestReviewedHistoricalApplyRequestMatchesServerDTO(t *testing.T) {
	manifestFile, err := os.Open("../../historical-import/manifests/codex-commons.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	defer manifestFile.Close()
	manifest, _, err := historicalimport.Decode(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	snapshotFile, err := os.Open("../../historical-import/snapshots/codex-commons-current.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotFile.Close()
	snapshot, err := historicalimport.DecodeSnapshot(snapshotFile)
	if err != nil {
		t.Fatal(err)
	}
	report, err := historicalimport.BuildPreview(manifest, snapshot)
	if err != nil || !report.ApplyEligible {
		t.Fatalf("preview eligible=%v err=%v blockers=%+v", report.ApplyEligible, err, report.Blockers)
	}
	request, err := historicalimport.BuildApplyRequest(manifest, report, report.SourceDigest, report.ManifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input HistoricalImportRequest
	if err = decoder.Decode(&input); err != nil {
		t.Fatalf("server DTO rejected reviewed apply request: %v", err)
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("apply request has trailing JSON: %v", err)
	}
	if len(input.ProjectThreadAliases) != 4 || len(input.Tasks) != 20 {
		t.Fatalf("aliases=%d tasks=%d", len(input.ProjectThreadAliases), len(input.Tasks))
	}
	command := historicalImportCommand(manifest.ProjectID, input, ProjectCoreActor{})
	if len(command.ProjectThreadAliases) != 4 || len(command.Tasks) != 20 {
		t.Fatalf("mapped aliases=%d tasks=%d", len(command.ProjectThreadAliases), len(command.Tasks))
	}
	first := command.Tasks[0]
	if first.Source.Kind == "" || first.Source.OccurredAt.IsZero() || len(first.Attributions) == 0 {
		t.Fatalf("task source or attributions lost: %+v", first)
	}
	if len(first.Events) > 0 && first.Events[0].Source.OccurredAt.IsZero() {
		t.Fatalf("event source time lost: %+v", first.Events[0])
	}
	ctx := context.Background()
	clock := func() time.Time { return time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC) }
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "contract.sqlite"), commonsstore.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.CreateProject(ctx, domain.Project{ID: manifest.ProjectID, Name: "Codex Commons", Purpose: "continuity contract test"}); err != nil {
		t.Fatal(err)
	}
	receipt, err := store.PreviewHistoricalImport(ctx, command)
	if err != nil || receipt.Counts.Tasks != 20 || receipt.Counts.ProjectThreadAliases != 4 ||
		receipt.Counts.Attributions != 37 || receipt.Counts.Events != 13 || receipt.Counts.Created != 20 {
		t.Fatalf("server preview receipt=%+v err=%v", receipt, err)
	}
	var batches int
	if err = store.DB().QueryRowContext(ctx, "SELECT count(*) FROM historical_import_batches").Scan(&batches); err != nil || batches != 0 {
		t.Fatalf("preview mutated batch ledger: count=%d err=%v", batches, err)
	}
}
