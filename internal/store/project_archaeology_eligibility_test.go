package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"codex-commons/internal/domain"
)

type archaeologyEligibilityFixtureOptions struct {
	batchState               string
	policyAttested           bool
	jobState                 string
	jobSession               string
	reportBearing            bool
	withOutcome              bool
	outcomeProject           string
	additionalJobState       string
	additionalOutcomeProject string
}

func seedArchaeologyEligibilityFixture(t *testing.T, s *Store, options archaeologyEligibilityFixtureOptions) {
	t.Helper()
	ctx := context.Background()
	at := stamp(testNow)
	if options.batchState == "" {
		options.batchState = "completed"
	}
	if options.jobState == "" {
		options.jobState = "completed"
	}
	if options.jobSession == "" {
		options.jobSession = "elig-s"
	}
	if options.outcomeProject == "" {
		options.outcomeProject = "alpha"
	}
	mustCoreProject(t, s, "alpha", "Alpha")
	mustCoreProject(t, s, "beta", "Beta")
	mustExecEligibility(t, s, ctx, `INSERT INTO archaeology_sessions(id,principal,state,discovery_state,created_at,updated_at) VALUES
('elig-s','human:local-admin','completed','ready',?,?),
('other-s','human:other','completed','ready',?,?)`, at, at, at, at)
	mustExecEligibility(t, s, ctx, `INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at,policy_attested) VALUES('elig-b','elig-s','elig-request',zeroblob(32),'app_server_dynamic_tools',?,?,?, ?,?)`, options.batchState, 1, at, at, boolInt(options.policyAttested))
	mustExecEligibility(t, s, ctx, `INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,canonical_project_id) VALUES
('elig-s','elig-c','Alpha','alpha',1,0,0,1,2,'low','','alpha'),
('other-s','other-c','Alpha','alpha',1,0,0,1,2,'low','','alpha')`)
	reportDigest := any(nil)
	if options.reportBearing {
		reportDigest = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	}
	candidateID := "elig-c"
	if options.jobSession == "other-s" {
		candidateID = "other-c"
	}
	mustExecEligibility(t, s, ctx, `INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,project_name,mode,state,report_digest,created_at,updated_at) VALUES('elig-j','elig-b',?,?,?,?,?,?, ?,?,?)`, options.jobSession, candidateID, "alpha", "Alpha", "app_server_dynamic_tools", options.jobState, reportDigest, at, at)
	if options.withOutcome {
		mustExecEligibility(t, s, ctx, `INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES('elig-o','elig-j',?,'Alpha','',1,'{"batch_id":"elig-b"}',?)`, options.outcomeProject, at)
	}
	if options.additionalJobState != "" {
		mustExecEligibility(t, s, ctx, `INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,canonical_project_id) VALUES('elig-s','elig-c2','Beta 2','beta-2',1,0,0,1,2,'low','','beta')`)
		mustExecEligibility(t, s, ctx, `INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,project_name,mode,state,report_digest,created_at,updated_at) VALUES('elig-j2','elig-b','elig-s','elig-c2','beta','Beta','app_server_dynamic_tools',?,?,?,?)`, options.additionalJobState, reportDigest, at, at)
	}
	if options.additionalOutcomeProject != "" {
		mustExecEligibility(t, s, ctx, `INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES('elig-o2','elig-j',?,'Alpha 2','',1,'{"batch_id":"elig-b"}',?)`, options.additionalOutcomeProject, at)
	}
}

func mustExecEligibility(t *testing.T, s *Store, ctx context.Context, query string, args ...any) {
	t.Helper()
	_, err := s.DB().ExecContext(ctx, query, args...)
	must(t, err)
}

func TestArchaeologyBatchEligibilityPredicate(t *testing.T) {
	tests := []struct {
		name         string
		principal    string
		batchID      string
		options      archaeologyEligibilityFixtureOptions
		wantEligible bool
		wantNotFound bool
	}{
		{name: "eligible", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true}, wantEligible: true},
		{name: "missing", batchID: "missing-batch", wantNotFound: true},
		{name: "foreign", principal: "human:other", wantNotFound: true},
		{name: "state", options: archaeologyEligibilityFixtureOptions{batchState: "running", policyAttested: true, reportBearing: true, withOutcome: true}},
		{name: "unattested", options: archaeologyEligibilityFixtureOptions{policyAttested: false, reportBearing: true, withOutcome: true}},
		{name: "incomplete", options: archaeologyEligibilityFixtureOptions{policyAttested: true, jobState: "active", reportBearing: true, withOutcome: true}},
		{name: "no outcome", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true}},
		{name: "missing report", options: archaeologyEligibilityFixtureOptions{policyAttested: true, withOutcome: true}},
		{name: "cross session", options: archaeologyEligibilityFixtureOptions{policyAttested: true, jobSession: "other-s", reportBearing: true, withOutcome: true}},
		{name: "cross project", options: archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true, outcomeProject: "beta"}},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTest(t)
			principal := test.principal
			if principal == "" {
				principal = domain.HumanLocalPrincipal
			}
			batchID := test.batchID
			if batchID == "" {
				batchID = "elig-b"
			}
			seedArchaeologyEligibilityFixture(t, s, test.options)
			got, err := s.ArchaeologyBatchEligibility(context.Background(), principal, batchID)
			if test.wantNotFound {
				if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("err=%v, want not found", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Eligible != test.wantEligible {
				t.Fatalf("eligibility=%+v, want eligible=%t", got, test.wantEligible)
			}
			if got.JobCount != 1 || got.OutcomeCount != boolInt(test.options.withOutcome) {
				t.Fatalf("counts=%+v", got)
			}
		})
	}
}

func TestArchaeologyBatchEligibilityRejectsMalformedInputs(t *testing.T) {
	s, _ := openTest(t)
	for _, test := range []struct {
		name      string
		principal string
		batchID   string
	}{
		{name: "missing principal", principal: "", batchID: "elig-b"},
		{name: "blank principal", principal: "   ", batchID: "elig-b"},
		{name: "oversized principal", principal: strings.Repeat("p", 201), batchID: "elig-b"},
		{name: "missing batch", principal: domain.HumanLocalPrincipal, batchID: ""},
		{name: "oversized batch", principal: domain.HumanLocalPrincipal, batchID: strings.Repeat("b", 121)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := s.ArchaeologyBatchEligibility(context.Background(), test.principal, test.batchID); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("err=%v, want invalid", err)
			}
		})
	}
}

func TestArchaeologyBatchEligibilityCountsMixedChildren(t *testing.T) {
	for _, test := range []struct {
		name              string
		options           archaeologyEligibilityFixtureOptions
		wantJobs          int
		wantCompletedJobs int
		wantOutcomes      int
		wantValidOutcomes int
	}{
		{
			name:              "mixed job states",
			options:           archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true, additionalJobState: "active"},
			wantJobs:          2,
			wantCompletedJobs: 1,
			wantOutcomes:      1,
			wantValidOutcomes: 1,
		},
		{
			name:              "mixed outcome validity",
			options:           archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true, additionalOutcomeProject: "beta"},
			wantJobs:          1,
			wantCompletedJobs: 1,
			wantOutcomes:      2,
			wantValidOutcomes: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTest(t)
			seedArchaeologyEligibilityFixture(t, s, test.options)
			got, err := s.ArchaeologyBatchEligibility(context.Background(), domain.HumanLocalPrincipal, "elig-b")
			if err != nil {
				t.Fatal(err)
			}
			if got.Eligible || got.JobCount != test.wantJobs || got.CompletedJobCount != test.wantCompletedJobs || got.OutcomeCount != test.wantOutcomes || got.ValidOutcomeCount != test.wantValidOutcomes {
				t.Fatalf("eligibility=%+v", got)
			}
		})
	}
}

func TestArchaeologyEligibilitySnapshotAttachedToSessionAndBatchDetail(t *testing.T) {
	s, _ := openTest(t)
	seedArchaeologyEligibilityFixture(t, s, archaeologyEligibilityFixtureOptions{policyAttested: true, reportBearing: true, withOutcome: true})
	ctx := context.Background()
	session, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	must(t, err)
	if len(session.NativeBatches) != 1 || !session.NativeBatches[0].Eligibility.Eligible {
		t.Fatalf("session batches=%+v", session.NativeBatches)
	}
	detail, err := s.ArchaeologyBatch(ctx, domain.HumanLocalPrincipal, "elig-b")
	must(t, err)
	if !detail.Batch.Eligibility.Eligible || detail.Batch.Eligibility.OutcomeCount != 1 {
		t.Fatalf("detail batch=%+v", detail.Batch)
	}
}
