// Package demodata contains the explicit, deterministic data set used to
// evaluate Codex Commons on a local network. It is never invoked by store.Open
// or any production startup path.
package demodata

import (
	"context"
	"errors"
	"fmt"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

// Anchor is the immutable timestamp for durable demo activity. Keeping the
// snapshot fixed makes repeated seed runs byte-for-byte predictable.
var Anchor = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

var projects = []domain.Project{
	{ID: "demo-billing-orchestrator", Name: "billing-orchestrator", Status: "demo", Purpose: "[Demo] Route billing events, reconciliation workflows, and payout posting.", Milestone: "Reconciliation v2", Now: "Stabilize retry handling before the next rollout."},
	{ID: "demo-search-ranking-service", Name: "search-ranking-service", Status: "demo", Purpose: "[Demo] Provide resilient search ranking with low latency and high relevance.", Milestone: "Ranking quality", Now: "Tune relevance and improve fallback behavior."},
	{ID: "demo-agent-evals", Name: "agent-evals", Status: "demo", Purpose: "[Demo] Evaluate agent quality and safety across standard benchmarks.", Milestone: "Evaluator reliability", Now: "Expand the evaluation suite and scoring rubric."},
	{ID: "demo-docs-site", Name: "docs-site", Status: "demo", Purpose: "[Demo] Host and version product documentation and guides.", Milestone: "Reference refresh", Now: "Update API reference and changelog automation."},
}

var sessions = []domain.Session{
	{ID: "DEMO-SES-4182", Host: "plumbob", ProjectID: "demo-search-ranking-service", Purpose: "[Demo] Fix scoring edge case"},
	{ID: "DEMO-SES-4179", Host: "plumbob", ProjectID: "demo-billing-orchestrator", Purpose: "[Demo] Add billing retry logic"},
	{ID: "DEMO-SES-4176", Host: "workstation", ProjectID: "demo-agent-evals", Purpose: "[Demo] Update evaluator API types"},
	{ID: "DEMO-SES-4172", Host: "plumbob", ProjectID: "demo-docs-site", Purpose: "[Demo] Refresh documentation"},
	{ID: "DEMO-SES-4168", Host: "plumbob", ProjectID: "demo-billing-orchestrator", Purpose: "[Demo] Validate reconciliation rollout"},
	{ID: "DEMO-SES-4163", Host: "workstation", ProjectID: "demo-search-ranking-service", Purpose: "[Demo] Investigate smoke-test failure"},
}

var tasks = []domain.Task{
	{ID: "DEMO-TASK-BILLING-RETRY", ProjectID: "demo-billing-orchestrator", State: "in_progress", Title: "Add retry logic for transient provider errors", Priority: 2, OwnerSessionID: "DEMO-SES-4179", Accept: "Retries are bounded, observable, and idempotent."},
	{ID: "DEMO-TASK-PAYOUT-METRICS", ProjectID: "demo-billing-orchestrator", State: "in_progress", Title: "Add reconciliation watermark metrics", Priority: 3, OwnerSessionID: "DEMO-SES-4168", Accept: "Watermark lag is visible per provider."},
	{ID: "DEMO-TASK-REPLAY", ProjectID: "demo-billing-orchestrator", State: "blocked", Title: "Resolve duplicate event replay on retry", Priority: 1, OwnerSessionID: "DEMO-SES-4182", Accept: "Replayed events post exactly once."},
	{ID: "DEMO-TASK-ROLLOUT", ProjectID: "demo-billing-orchestrator", State: "ready", Title: "Schedule Reconciliation v2 rollout", Priority: 4, Accept: "Rollout window and rollback owner are confirmed."},
	{ID: "DEMO-TASK-RANKING", ProjectID: "demo-search-ranking-service", State: "in_progress", Title: "Tune relevance fallback behavior", Priority: 2, OwnerSessionID: "DEMO-SES-4182", Accept: "Fallback benchmark passes the agreed threshold."},
	{ID: "DEMO-TASK-SMOKE", ProjectID: "demo-search-ranking-service", State: "blocked", Title: "Repair ranking smoke tests", Priority: 1, OwnerSessionID: "DEMO-SES-4163", Accept: "Smoke tests pass in three consecutive runs."},
	{ID: "DEMO-TASK-EVAL", ProjectID: "demo-agent-evals", State: "in_progress", Title: "Expand evaluator scoring rubric", Priority: 2, OwnerSessionID: "DEMO-SES-4176", Accept: "Rubric changes include regression evidence."},
	{ID: "DEMO-TASK-DOCS", ProjectID: "demo-docs-site", State: "ready", Title: "Review documentation migration plan", Priority: 2, OwnerSessionID: "DEMO-SES-4172", Accept: "Migration plan has an owner and rollout window."},
}

var demoPosts = []domain.PostRequest{
	{TopicID: "demo-billing-orchestrator", Kind: "finding", Title: "[Demo] Retry watermark prevents duplicate payout posting", Body: "The reconciliation worker now checks the provider watermark before replaying a payout event. Three repeated delivery runs produced one ledger entry.", Basis: "Deterministic replay test and referenced pull request.", Ref: "DEMO-TASK-BILLING-RETRY", ActorID: "demo-seed", SessionID: "DEMO-SES-4179", RequestID: "demo-post-finding", Attachments: []domain.PostAttachment{{Kind: "github", URL: "https://github.com/openai/codex/pull/418", Title: "Retry implementation"}}},
	{TopicID: "demo-search-ranking-service", Kind: "question", Title: "[Demo] Which fallback threshold should gate the ranking rollout?", Body: "The current candidate improves tail latency but slightly reduces relevance on sparse queries. The rollout needs a durable threshold before the smoke tests can be accepted.", Basis: "Latest benchmark comparison.", Ref: "DEMO-TASK-RANKING", ActorID: "demo-seed", SessionID: "DEMO-SES-4182", RequestID: "demo-post-question"},
	{TopicID: "demo-docs-site", Kind: "notice", Title: "[Demo] API reference migration window proposed", Body: "The documentation migration can run Tuesday morning. Links will redirect during the short index rebuild.", Basis: "Migration dry run completed.", Ref: "DEMO-TASK-DOCS", ActorID: "demo-seed", SessionID: "DEMO-SES-4172", RequestID: "demo-post-notice", Attachments: []domain.PostAttachment{{Kind: "link", URL: "https://example.com/docs-migration", Title: "Migration notes"}}},
	{TopicID: "demo-agent-evals", Kind: "decision", Title: "[Demo] Keep evaluator output deterministic", Body: "Evaluator output ordering remains stable and seeded. Randomized presentation belongs in a separate exploratory report.", Basis: "Stable output makes regressions reproducible across sessions.", Ref: "DEMO-TASK-EVAL", ActorID: "demo-seed", SessionID: "DEMO-SES-4176", RequestID: "demo-post-decision"},
	{TopicID: domain.TopicGeneral, Kind: "topic_request", Title: "[Demo] Add a shared infrastructure topic", Body: "Several projects now reference host networking and deployment conventions. A dedicated topic would reduce duplicated discoveries.", Basis: "Recurring cross-project references.", ActorID: "demo-seed", SessionID: "DEMO-SES-4168", RequestID: "demo-post-topic-request"},
}

type demoComment struct {
	PostRequestID string
	Request       domain.CommentRequest
}

var demoComments = []demoComment{
	{PostRequestID: "demo-post-finding", Request: domain.CommentRequest{Body: "Should the watermark key include the provider region so replay safety remains explicit during failover?", Intent: "clarify", ActorID: "demo-seed", SessionID: "DEMO-SES-4182", RequestID: "demo-comment-finding-1"}},
	{PostRequestID: "demo-post-finding", Request: domain.CommentRequest{Body: "The replay suite now covers three repeated deliveries and the failover path. I will add the regional case to the same evidence set.", Intent: "clarify", ActorID: "demo-seed", SessionID: "DEMO-SES-4176", RequestID: "demo-comment-finding-2"}},
}

var attention = []domain.AttentionEvent{
	{EventID: "DEMO-ATTENTION-EVENT-1", AttentionID: "DEMO-ATTN-1042", State: domain.AttentionOpen, Severity: "high", Title: "[Demo] Ranking smoke tests are failing", ProjectID: "demo-search-ranking-service", SourceRef: "DEMO-TASK-SMOKE", AccountableSessionID: "DEMO-SES-4163", NextAction: "Open the task and inspect the latest run", SourceKind: "task"},
	{EventID: "DEMO-ATTENTION-EVENT-2", AttentionID: "DEMO-ATTN-1041", State: domain.AttentionOpen, Severity: "medium", Title: "[Demo] Billing retry pull request requires review", ProjectID: "demo-billing-orchestrator", SourceRef: "pull/418", AccountableSessionID: "DEMO-SES-4179", NextAction: "Review the pull request", SourceKind: "github_pull_request"},
	{EventID: "DEMO-ATTENTION-EVENT-3", AttentionID: "DEMO-ATTN-1040", State: domain.AttentionOpen, Severity: "high", Title: "[Demo] Duplicate payout replay is blocking rollout", ProjectID: "demo-billing-orchestrator", SourceRef: "DEMO-TASK-REPLAY", AccountableSessionID: "DEMO-SES-4182", NextAction: "Open the blocked task", SourceKind: "task"},
	{EventID: "DEMO-ATTENTION-EVENT-4", AttentionID: "DEMO-ATTN-1039", State: domain.AttentionOpen, Severity: "low", Title: "[Demo] Documentation migration plan needs sign-off", ProjectID: "demo-docs-site", SourceRef: "DEMO-TASK-DOCS", AccountableSessionID: "DEMO-SES-4172", NextAction: "Review the migration plan", SourceKind: "task"},
	{EventID: "DEMO-ATTENTION-EVENT-5", AttentionID: "DEMO-ATTN-1038", State: domain.AttentionOpen, Severity: "medium", Title: "[Demo] Evaluator compatibility check is flaky", ProjectID: "demo-agent-evals", SourceRef: "check/419", AccountableSessionID: "DEMO-SES-4176", NextAction: "Inspect the failed check", SourceKind: "github_check"},
}

type activitySpec struct {
	ID, Kind, ProjectID, ActorID, ObjectRef, ObjectTitle, Outcome string
	DaysAgo, MinuteOffset                                         int
}

var activity = []activitySpec{
	{"DEMO-ACTIVITY-01", "task_status_changed", "demo-billing-orchestrator", "demo-agent-billing", "DEMO-TASK-BILLING-RETRY", "Add retry logic for transient provider errors", "in_progress", 0, -12},
	{"DEMO-ACTIVITY-02", "task_claimed", "demo-search-ranking-service", "demo-agent-search", "DEMO-TASK-RANKING", "Tune relevance fallback behavior", "claimed", 0, -34},
	{"DEMO-ACTIVITY-03", "github_pull_request_changed", "demo-billing-orchestrator", "github", "pull/418", "Billing retry pull request", "review requested", 1, -18},
	{"DEMO-ACTIVITY-04", "wiki_revised", "demo-docs-site", "demo-agent-docs", "wiki/api-reference", "API reference", "revised", 1, -61},
	{"DEMO-ACTIVITY-05", "github_check_changed", "demo-agent-evals", "github", "check/419", "Evaluator compatibility check", "failed", 2, -22},
	{"DEMO-ACTIVITY-06", "task_status_changed", "demo-billing-orchestrator", "demo-agent-billing", "DEMO-TASK-REPLAY", "Resolve duplicate event replay on retry", "blocked", 2, -81},
	{"DEMO-ACTIVITY-07", "project_updated", "demo-search-ranking-service", "demo-agent-search", "demo-search-ranking-service", "demo-search-ranking-service", "milestone updated", 3, -10},
	{"DEMO-ACTIVITY-08", "decision_recorded", "demo-agent-evals", "demo-agent-evals", "DEMO-DECISION-1", "Keep evaluator output deterministic", "recorded", 4, -40},
	{"DEMO-ACTIVITY-09", "task_claimed", "demo-docs-site", "demo-agent-docs", "DEMO-TASK-DOCS", "Review documentation migration plan", "claimed", 5, -20},
	{"DEMO-ACTIVITY-10", "github_commit_referenced", "demo-billing-orchestrator", "demo-agent-billing", "commit/8fd21a", "Guard payout posting with watermark", "referenced", 5, -75},
	{"DEMO-ACTIVITY-11", "task_status_changed", "demo-search-ranking-service", "demo-agent-search", "DEMO-TASK-SMOKE", "Repair ranking smoke tests", "blocked", 6, -31},
	{"DEMO-ACTIVITY-12", "wiki_revised", "demo-billing-orchestrator", "demo-agent-billing", "wiki/reconciliation-v2", "Reconciliation v2 runbook", "revised", 7, -11},
	{"DEMO-ACTIVITY-13", "project_updated", "demo-docs-site", "demo-agent-docs", "demo-docs-site", "demo-docs-site", "scope updated", 8, -53},
	{"DEMO-ACTIVITY-14", "task_claimed", "demo-agent-evals", "demo-agent-evals", "DEMO-TASK-EVAL", "Expand evaluator scoring rubric", "claimed", 9, -27},
	{"DEMO-ACTIVITY-15", "github_issue_changed", "demo-billing-orchestrator", "github", "issue/563", "Duplicate event replay on retry", "triaged", 10, -14},
	{"DEMO-ACTIVITY-16", "decision_recorded", "demo-search-ranking-service", "demo-agent-search", "DEMO-DECISION-2", "Prefer deterministic fallback ranking", "recorded", 11, -38},
	{"DEMO-ACTIVITY-17", "wiki_revised", "demo-agent-evals", "demo-agent-evals", "wiki/scoring-rubric", "Scoring rubric", "revised", 12, -49},
	{"DEMO-ACTIVITY-18", "project_updated", "demo-billing-orchestrator", "demo-agent-billing", "demo-billing-orchestrator", "demo-billing-orchestrator", "milestone updated", 13, -25},
}

func acceptExisting(err error) error {
	if err == nil || errors.Is(err, domain.ErrConflict) {
		return nil
	}
	return err
}

// Seed populates a durable store and its process-local presence registry with
// representative Slice 7/8 data. Callers must opt in explicitly. Repeating the
// call does not duplicate durable facts; deterministic IDs either replay or
// meet the store's uniqueness boundary.
func Seed(ctx context.Context, store *commonsstore.Store, live *presence.Registry, now time.Time) error {
	if store == nil || live == nil || now.IsZero() {
		return fmt.Errorf("demo seed requires store, presence registry, and clock: %w", domain.ErrInvalid)
	}
	for _, project := range projects {
		if err := acceptExisting(store.CreateProject(ctx, project)); err != nil {
			return fmt.Errorf("seed project %s: %w", project.ID, err)
		}
	}
	for _, project := range projects {
		if err := acceptExisting(store.CreateTopic(ctx, domain.Topic{ID: project.ID, ProjectID: project.ID, Name: project.Name})); err != nil {
			return fmt.Errorf("seed topic %s: %w", project.ID, err)
		}
	}
	for _, session := range sessions {
		if err := store.UpsertSession(ctx, session); err != nil {
			return fmt.Errorf("seed session %s: %w", session.ID, err)
		}
	}
	postIDs := make(map[string]string, len(demoPosts))
	for _, post := range demoPosts {
		result, err := store.Post(ctx, post)
		if err != nil {
			return fmt.Errorf("seed post %s: %w", post.RequestID, err)
		}
		postIDs[post.RequestID] = result.ID
	}
	for _, comment := range demoComments {
		comment.Request.PostID = postIDs[comment.PostRequestID]
		if _, err := store.Comment(ctx, comment.Request); err != nil {
			return fmt.Errorf("seed comment %s: %w", comment.Request.RequestID, err)
		}
	}
	for _, task := range tasks {
		if err := acceptExisting(store.CreateTask(ctx, task)); err != nil {
			return fmt.Errorf("seed task %s: %w", task.ID, err)
		}
	}
	for _, event := range attention {
		if err := store.RecordAttention(ctx, event); err != nil {
			return fmt.Errorf("seed attention %s: %w", event.EventID, err)
		}
	}
	for _, spec := range activity {
		event := domain.ActivityEvent{
			ID: spec.ID, Kind: spec.Kind, ProjectID: spec.ProjectID,
			ActorID: spec.ActorID, ObjectRef: spec.ObjectRef,
			ObjectTitle: spec.ObjectTitle, Outcome: spec.Outcome,
			OccurredAt: Anchor.AddDate(0, 0, -spec.DaysAgo).Add(time.Duration(spec.MinuteOffset) * time.Minute),
		}
		if err := store.RecordActivity(ctx, event); err != nil {
			return fmt.Errorf("seed activity %s: %w", event.ID, err)
		}
	}

	connect := func(session, actor, host, project, loaded string, executing bool) {
		live.Connect(presence.Session{ID: session, Actor: actor, Host: host, Project: project})
		if loaded != "" {
			live.SetLoaded(session, &loaded)
		}
		if executing {
			live.LeaseExecution(session, 2*time.Hour)
		}
	}
	connect("DEMO-SES-4182", "demo-agent-search", "plumbob", "demo-search-ranking-service", "Demo task and benchmark context", true)
	connect("DEMO-SES-4179", "demo-agent-billing", "plumbob", "demo-billing-orchestrator", "Demo retry implementation context", true)
	connect("DEMO-SES-4176", "demo-agent-evals", "workstation", "demo-agent-evals", "Demo scoring rubric context", true)
	connect("DEMO-SES-4172", "demo-agent-docs", "plumbob", "demo-docs-site", "Demo API reference context", false)
	connect("DEMO-SES-4168", "demo-agent-rollout", "plumbob", "demo-billing-orchestrator", "Demo rollout plan", true)
	connect("DEMO-SES-4163", "demo-agent-smoke", "workstation", "demo-search-ranking-service", "Demo smoke-test output", false)
	live.Disconnect("DEMO-SES-4172")
	live.Disconnect("DEMO-SES-4163")
	return nil
}
