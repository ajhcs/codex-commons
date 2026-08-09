const now = "2026-08-09T12:00:00Z";

/** These objects deliberately mirror the backend's snake_case JSON. */
export const contractFixtures = Object.freeze({
  attention: {
    total: 10,
    limit: 10,
    next_cursor: "attention:page-2",
    facets: {
      sources: [
        { value: "github_check", label: "GitHub checks", count: 4 },
        { value: "github_pull_request", label: "Pull requests", count: 3 },
        { value: "task", label: "Tasks", count: 3 },
      ],
      owners: [
        { value: "SES-4182", label: "SES-4182", count: 3 },
        { value: "SES-4179", label: "SES-4179", count: 3 },
        { value: "SES-4176", label: "SES-4176", count: 2 },
        { value: "SES-4168", label: "SES-4168", count: 2 },
      ],
      severities: [
        { value: "high", label: "High", count: 4 },
        { value: "medium", label: "Medium", count: 4 },
        { value: "low", label: "Low", count: 2 },
      ],
      projects: [
        { value: "search-ranking-service", label: "search-ranking-service", count: 3 },
        { value: "billing-orchestrator", label: "billing-orchestrator", count: 2 },
        { value: "agent-evals", label: "agent-evals", count: 2 },
        { value: "docs-site", label: "docs-site", count: 3 },
      ],
      owners_truncated: false,
      projects_truncated: false,
    },
    items: [
      ["ATTN-1042", "high", "Failing smoke tests on search-ranking-service", "search-ranking-service", "check/421", "SES-4182", "Open check", "github_check", "2026-08-09T11:47:00Z"],
      ["ATTN-1041", "medium", "Billing retry pull request requires review", "billing-orchestrator", "pull/418", "SES-4179", "Open PR", "github_pull_request", "2026-08-09T11:24:00Z"],
      ["ATTN-1040", "high", "Host lost connectivity during evaluation", "agent-evals", "task/eval-runner-03", "SES-4176", "Open task", "task", "2026-08-09T10:58:00Z"],
      ["ATTN-1039", "low", "Migration plan is ready for sign-off", "docs-site", "task/docs-migration", "SES-4168", "Review plan", "task", "2026-08-09T10:47:00Z"],
      ["ATTN-1038", "medium", "Search update benchmark is flaky", "search-ranking-service", "check/419", "SES-4176", "Open check", "github_check", "2026-08-09T10:26:00Z"],
      ["ATTN-1037", "low", "Dependency requests 2.28.1 is outdated", "agent-evals", "pull/416", "SES-4182", "Open PR", "github_pull_request", "2026-08-09T09:52:00Z"],
      ["ATTN-1036", "medium", "Documentation build warnings detected", "docs-site", "check/412", "SES-4179", "Open check", "github_check", "2026-08-09T09:18:00Z"],
      ["ATTN-1035", "high", "Deployment failed health checks", "search-ranking-service", "task/rollout-14", "SES-4182", "Open task", "task", "2026-08-09T08:34:00Z"],
      ["ATTN-1034", "medium", "Security alert requires dependency review", "billing-orchestrator", "pull/407", "SES-4179", "Open PR", "github_pull_request", "2026-08-09T07:01:00Z"],
      ["ATTN-1033", "high", "Architecture overview has a broken link", "docs-site", "check/403", "SES-4168", "Open check", "github_check", "2026-08-09T05:45:00Z"],
    ].map(([id, severity, title, project, source_ref, owner, next_action, source_kind, updated_at]) => ({
      id, severity, title, project, project_name: project, source_ref, owner, next_action,
      source_kind, updated_at, untrusted: source_kind.startsWith("github"),
      ...(source_kind === "task" ? { destination: { kind: source_kind, ref: source_ref } } : {}),
    })),
  },
  projects: {
    total: 8,
    limit: 8,
    items: [
      ["search-ranking-service", "Provide resilient search ranking with low latency and high relevance.", "Tune relevance and improve fallback behavior", 2, 5, "2026-08-09T11:47:00Z"],
      ["billing-orchestrator", "Orchestrate billing workflows across providers and accounts.", "Add retry logic for transient provider errors", 2, 4, "2026-08-09T11:24:00Z"],
      ["agent-evals", "Evaluate agent quality and safety across standard benchmarks.", "Expand the evaluation suite and scoring rubric", 1, 7, "2026-08-09T10:58:00Z"],
      ["docs-site", "Host and version product documentation and guides.", "Update API reference and changelog automation", 1, 3, "2026-08-09T10:47:00Z"],
      ["design-system", "Maintain shared components and design tokens.", "Refine component states and accessibility tokens", 0, 2, "2026-08-09T09:52:00Z"],
      ["infra-rollout", "Roll out infrastructure changes safely across environments.", "Roll back canary and update the runbook", 1, 4, "2026-08-09T08:34:00Z"],
      ["auth-gateway", "Secure authentication and session management.", "Implement refresh token rotation", 0, 3, "2026-08-08T22:14:00Z"],
      ["data-pipelines", "Build and maintain reliable data-ingestion pipelines.", "Fix backfill job and add monitoring alerts", 2, 6, "2026-08-08T20:47:00Z"],
    ].map(([id, purpose, work, active_sessions, open_tasks, last_activity], index) => ({
      id, name: id, status: "active", purpose,
      current_work: { id: `TASK-${index + 301}`, title: work, state: "in_progress", priority: 2 },
      open_tasks, active_sessions, last_activity, destination: { kind: "project", ref: id },
    })),
  },
  people: {
    total: 10,
    limit: 10,
    facets: {
      projects: [
        { value: "search-ranking-service", label: "search-ranking-service", count: 3 },
        { value: "billing-orchestrator", label: "billing-orchestrator", count: 2 },
        { value: "agent-evals", label: "agent-evals", count: 2 },
        { value: "docs-site", label: "docs-site", count: 2 },
        { value: "infra-rollout", label: "infra-rollout", count: 1 },
      ],
      execution: [
        { value: "executing", label: "Executing", count: 6 },
        { value: "not_running", label: "Not running", count: 4 },
      ],
      hosts: [
        { value: "plumbob", label: "plumbob", count: 7 },
        { value: "workstation", label: "workstation", count: 3 },
      ],
      connectivity: [
        { value: "connected", label: "Connected", count: 7 },
        { value: "disconnected", label: "Disconnected", count: 3 },
      ],
    },
    items: [
      ["SES-4182", "search-fix-221", "Fix scoring edge case", "search-ranking-service", "executing", "plumbob", true, "2026-08-09T11:58:00Z", "12 files · task context"],
      ["SES-4179", "billing-retry-416", "Add billing retry logic", "billing-orchestrator", "executing", "plumbob", true, "2026-08-09T11:55:00Z", "8 files · review context"],
      ["SES-4176", "api-types-agent", "Update API types", "agent-evals", "executing", "workstation", true, "2026-08-09T11:52:00Z", "12 files · RFC context"],
      ["SES-4172", "docs-refresh", "Refresh documentation", "docs-site", "executing", "plumbob", false, "2026-08-09T11:46:00Z", "8 docs"],
      ["SES-4168", "infra-rollout", "Roll out us-east-1 changes", "infra-rollout", "executing", "plumbob", true, "2026-08-09T11:42:00Z", "Rollout plan v2"],
      ["SES-4163", "eval-runner-check", "Evaluate runner heartbeat", "agent-evals", "not_running", "plumbob", false, "2026-08-09T11:28:00Z", ""],
      ["SES-4161", "docs-indexer", "Update docs index", "docs-site", "not_running", "workstation", true, "2026-08-09T11:14:00Z", "14 docs"],
      ["SES-4157", "dependency-audit", "Investigate dependency warning", "billing-orchestrator", "not_running", "workstation", false, "2026-08-09T10:58:00Z", ""],
      ["SES-4153", "eval-backfill", "Backfill evaluation metrics", "search-ranking-service", "executing", "plumbob", true, "2026-08-09T10:39:00Z", "6 files"],
      ["SES-4149", "docs-navigation", "Refine documentation navigation", "search-ranking-service", "not_running", "plumbob", true, "2026-08-09T10:18:00Z", "9 docs"],
    ].map(([session, actor, purpose, project, execution, host, host_connected, last_activity, loaded]) => ({
      session, actor, purpose, project, project_name: project, execution, host, host_connected,
      last_activity, recency_seconds: Math.max(0, (Date.parse(now) - Date.parse(last_activity)) / 1000),
      ...(loaded ? { loaded } : {}),
    })),
  },
  projectOverview: {
    project: {
      id: "billing-orchestrator",
      name: "billing-orchestrator",
      status: "active",
      purpose: "Service for routing billing events, reconciliation workflows, and payout posting across shared commerce systems.",
      milestone: "Reconciliation v2",
      now: "Stabilize retry handling before the next rollout.",
      revision: 18,
    },
    activity: {
      timezone: "UTC",
      start: "2026-07-27T00:00:00Z",
      end_exclusive: "2026-08-10T00:00:00Z",
      days: [3, 5, 2, 7, 4, 9, 6, 8, 3, 6, 5, 10, 7, 8].map((count, index) => ({
        day: new Date(Date.UTC(2026, 6, 27 + index)).toISOString().slice(0, 10), count,
      })),
    },
    metrics: {
      attention_total: 6,
      attention_high: 3,
      open_work: 12,
      merged_pull_requests: { available: false },
      active_sessions: 5,
    },
    needs_attention: { total: 6, limit: 3, items: [] },
    current_work: {
      total: 12,
      limit: 4,
      items: [
        { id: "RFC-031", title: "Idempotent payout posting", state: "in_progress", priority: 2, owner: "SES-4176", updated_at: "2026-08-09T11:02:00Z", target: { kind: "task", ref: "RFC-031" } },
        { id: "PR-416", title: "Add reconciliation watermark metrics", state: "in_progress", priority: 3, owner: "SES-4176", updated_at: "2026-08-09T10:03:00Z", target: { kind: "task", ref: "PR-416" } },
        { id: "ISS-563", title: "Duplicate event replay on retry", state: "blocked", priority: 1, owner: "SES-4182", updated_at: "2026-08-09T10:47:00Z", target: { kind: "task", ref: "ISS-563" } },
        { id: "ROL-014", title: "Scheduled rollout: Reconciliation v2", state: "ready", priority: 2, owner: "SES-4179", updated_at: "2026-08-09T09:16:00Z", target: { kind: "task", ref: "ROL-014" } },
      ],
    },
    last_action_changing_activity: "2026-08-09T11:02:00Z",
    snapshot_at: now,
  },
});

contractFixtures.projectOverview.needs_attention.items = [
  ...contractFixtures.attention.items.filter((item) => item.project === "billing-orchestrator"),
  {
    ...contractFixtures.attention.items.find((item) => item.id === "ATTN-1040"),
    id: "ATTN-1032",
    title: "Payout mismatch requires triage",
    project: "billing-orchestrator",
    project_name: "billing-orchestrator",
    source_ref: "task/payout-triage",
    source_kind: "task",
    next_action: "Open task",
    destination: { kind: "task", ref: "task/payout-triage" },
  },
].slice(0, 3);
