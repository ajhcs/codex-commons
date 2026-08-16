package store

import (
	"context"
	"errors"
	"testing"

	"codex-commons/internal/domain"
)

func seedForeignArchaeologyOutcome(t *testing.T, s *Store) {
	t.Helper()
	at := stamp(testNow)
	_, err := s.DB().ExecContext(context.Background(), `
INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at,policy_attested)
VALUES('other-b','other-s','other-request',zeroblob(32),'app_server_dynamic_tools','completed',1,?,?,1);
INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,project_name,mode,state,report_digest,created_at,updated_at)
VALUES('other-j','other-b','other-s','other-c','alpha','Alpha','app_server_dynamic_tools','completed',zeroblob(32),?,?);
INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at)
VALUES('foreign-o','other-j','alpha','Foreign','',1,'{"batch_id":"other-b"}',?)`, at, at, at)
	must(t, err)
}

func TestArchaeologySelectedOutcomesEligibilityMatrix(t *testing.T) {
	tests := []struct {
		name          string
		principal     string
		batchID       string
		ids           []string
		options       archaeologyEligibilityFixtureOptions
		foreignOutput bool
		wantErr       error
	}{
		{name: "running batch", options: archaeologyEligibilityFixtureOptions{batchState: "running", policyAttested: true, reportBearing: true, withOutcome: true}, wantErr: domain.ErrConflict},
		{name: "completed unattested", options: archaeologyEligibilityFixtureOptions{policyAttested: false, reportBearing: true, withOutcome: true}, wantErr: domain.ErrConflict},
		{name: "active job", options: archaeologyEligibilityFixtureOptions{policyAttested: true, jobState: "active", reportBearing: true, withOutcome: true}, wantErr: domain.ErrConflict},
		{name: "missing report", options: archaeologyEligibilityFixtureOptions{policyAttested: true, withOutcome: true}, wantErr: domain.ErrConflict},
		{name: "foreign principal", principal: "human:other", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}, wantErr: domain.ErrNotFound},
		{name: "foreign batch", batchID: "other-b", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}, foreignOutput: true, ids: []string{"foreign-o"}, wantErr: domain.ErrNotFound},
		{name: "foreign outcome", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}, foreignOutput: true, ids: []string{"foreign-o"}, wantErr: domain.ErrNotFound},
		{name: "missing outcome", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}, ids: []string{"missing-o"}, wantErr: domain.ErrNotFound},
		{name: "eligible success", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}, ids: []string{"elig-o"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTest(t)
			defer s.Close()
			seedArchaeologyEligibilityFixture(t, s, test.options)
			if test.foreignOutput {
				seedForeignArchaeologyOutcome(t, s)
			}
			principal := test.principal
			if principal == "" {
				principal = domain.HumanLocalPrincipal
			}
			batchID := test.batchID
			if batchID == "" {
				batchID = "elig-b"
			}
			ids := test.ids
			if len(ids) == 0 {
				ids = []string{"elig-o"}
			}
			outcomes, err := s.ArchaeologySelectedOutcomes(context.Background(), principal, batchID, ids)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("err=%v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil || len(outcomes) != 1 || outcomes[0].ID != "elig-o" {
				t.Fatalf("outcomes=%+v err=%v", outcomes, err)
			}
		})
	}
}

func TestArchaeologySelectedOutcomesRequiresCanonicalIDs(t *testing.T) {
	s, _ := openTest(t)
	defer s.Close()
	seedArchaeologyEligibilityFixture(t, s, archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true})
	for _, ids := range [][]string{{"elig-o", "elig-o"}, {"missing", "elig-o"}, nil} {
		if _, err := s.ArchaeologySelectedOutcomes(context.Background(), domain.HumanLocalPrincipal, "elig-b", ids); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("ids=%v err=%v, want invalid", ids, err)
		}
	}
}

func TestPreviewArchaeologySelectedImportsRejectsIneligibleWithoutMutation(t *testing.T) {
	tests := []struct {
		name    string
		options archaeologyEligibilityFixtureOptions
	}{
		{name: "running", options: archaeologyEligibilityFixtureOptions{batchState: "running", policyAttested: true, reportBearing: true, withOutcome: true}},
		{name: "unattested", options: archaeologyEligibilityFixtureOptions{policyAttested: false, reportBearing: true, withOutcome: true}},
		{name: "active job", options: archaeologyEligibilityFixtureOptions{policyAttested: true, jobState: "active", reportBearing: true, withOutcome: true}},
		{name: "missing report", options: archaeologyEligibilityFixtureOptions{policyAttested: true, withOutcome: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTest(t)
			defer s.Close()
			seedArchaeologyEligibilityFixture(t, s, test.options)
			command := domain.ArchaeologySelectedPreviewCommand{
				Principal:  domain.HumanLocalPrincipal,
				BatchID:    "elig-b",
				RequestID:  "preview-eligibility",
				OutcomeIDs: []string{"elig-o"},
				Imports:    []domain.HistoricalImportCommand{historicalCommand("alpha", "elig-b", "preview-eligibility")},
			}
			var beforeTasks, beforeBatches int
			must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks`).Scan(&beforeTasks))
			must(t, s.DB().QueryRow(`SELECT count(*) FROM historical_import_batches`).Scan(&beforeBatches))
			if _, err := s.PreviewArchaeologySelectedImports(context.Background(), command); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("err=%v, want conflict", err)
			}
			var afterTasks, afterBatches int
			must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks`).Scan(&afterTasks))
			must(t, s.DB().QueryRow(`SELECT count(*) FROM historical_import_batches`).Scan(&afterBatches))
			if beforeTasks != afterTasks || beforeBatches != afterBatches {
				t.Fatalf("preview rejection mutated tasks/batches: before=%d/%d after=%d/%d", beforeTasks, beforeBatches, afterTasks, afterBatches)
			}
		})
	}
}

func TestSelectedReviewRejectsStaleEligibilityBeforeAnyRowMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate string
	}{
		{name: "policy attestation", mutate: `UPDATE archaeology_native_batches SET policy_attested=0 WHERE id='elig-b'`},
		{name: "job state", mutate: `UPDATE archaeology_native_jobs SET state='active' WHERE id='elig-j'`},
		{name: "missing report", mutate: `UPDATE archaeology_native_jobs SET report_digest=NULL WHERE id='elig-j'`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTest(t)
			defer s.Close()
			seedArchaeologyEligibilityFixture(t, s, archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true})
			ctx := context.Background()
			selection, manifest := historicalDigest("c"), historicalDigest("d")
			firstCommand := domain.ArchaeologySelectedReviewCommand{Principal: domain.HumanLocalPrincipal, BatchID: "elig-b", SelectionDigest: selection, ManifestDigest: manifest, RequestID: "stale-page-0", OutcomeIDs: []string{"elig-o"}, Page: 0, PageCount: 2}
			first, err := s.AdvanceArchaeologySelectedReview(ctx, firstCommand)
			must(t, err)
			if _, err = s.DB().ExecContext(ctx, test.mutate); err != nil {
				t.Fatal(err)
			}
			secondCommand := firstCommand
			secondCommand.SessionToken = first.SessionToken
			secondCommand.RequestID = "stale-page-1"
			secondCommand.Page = 1
			if _, err = s.AdvanceArchaeologySelectedReview(ctx, secondCommand); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("stale page err=%v, want conflict", err)
			}
			if _, err = s.AdvanceArchaeologySelectedReview(ctx, firstCommand); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("stale replay err=%v, want conflict", err)
			}
			var reviews, pages, nextPage int
			must(t, s.DB().QueryRowContext(ctx, `SELECT count(*),coalesce(max(next_page),-1) FROM archaeology_selected_reviews`).Scan(&reviews, &nextPage))
			must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_review_pages`).Scan(&pages))
			if reviews != 1 || pages != 1 || nextPage != 1 {
				t.Fatalf("stale rejection mutated review state: reviews=%d pages=%d next=%d", reviews, pages, nextPage)
			}
		})
	}
}

func TestSelectedReviewRejectsIneligibleWithoutCreatingRows(t *testing.T) {
	tests := []struct {
		name      string
		principal string
		batchID   string
		ids       []string
		options   archaeologyEligibilityFixtureOptions
	}{
		{name: "running", options: archaeologyEligibilityFixtureOptions{batchState: "running", policyAttested: true, reportBearing: true, withOutcome: true}},
		{name: "unattested", options: archaeologyEligibilityFixtureOptions{policyAttested: false, reportBearing: true, withOutcome: true}},
		{name: "active job", options: archaeologyEligibilityFixtureOptions{policyAttested: true, jobState: "active", reportBearing: true, withOutcome: true}},
		{name: "missing report", options: archaeologyEligibilityFixtureOptions{policyAttested: true, withOutcome: true}},
		{name: "foreign principal", principal: "human:other", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}},
		{name: "missing batch", batchID: "missing-batch", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}},
		{name: "missing outcome", ids: []string{"missing-outcome"}, options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTest(t)
			defer s.Close()
			seedArchaeologyEligibilityFixture(t, s, test.options)
			principal := test.principal
			if principal == "" {
				principal = domain.HumanLocalPrincipal
			}
			batchID := test.batchID
			if batchID == "" {
				batchID = "elig-b"
			}
			ids := test.ids
			if len(ids) == 0 {
				ids = []string{"elig-o"}
			}
			_, err := s.AdvanceArchaeologySelectedReview(context.Background(), domain.ArchaeologySelectedReviewCommand{Principal: principal, BatchID: batchID, SelectionDigest: historicalDigest("c"), ManifestDigest: historicalDigest("d"), RequestID: "rejected-page-0", OutcomeIDs: ids, Page: 0, PageCount: 1})
			want := domain.ErrConflict
			if test.name == "foreign principal" || test.name == "missing batch" || test.name == "missing outcome" {
				want = domain.ErrNotFound
			}
			if !errors.Is(err, want) {
				t.Fatalf("err=%v, want %v", err, want)
			}
			var reviews, pages int
			must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews`).Scan(&reviews))
			must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_review_pages`).Scan(&pages))
			if reviews != 0 || pages != 0 {
				t.Fatalf("rejected review mutated rows: reviews=%d pages=%d", reviews, pages)
			}
		})
	}
}
