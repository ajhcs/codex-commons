export const AUTHENTICATED_SESSION_DTO = Object.freeze({
  authenticated: true,
  principal: {
    kind: "human",
    principal: "human:release-gate",
    handle: "release-gate",
    display_name: "Release Gate",
  },
  csrf_token: "qa-csrf",
  auth_method: "codex",
  profile_revision: 1,
});

function candidate(index) {
  const primary = index === 0;
  return {
    id: primary ? "codex-commons" : `codex-project-${String(index + 1).padStart(2, "0")}`,
    name: primary ? "Codex Commons" : `Codex Project ${String(index + 1).padStart(2, "0")}`,
    repository_label: primary ? "codex-commons" : `codex-project-${String(index + 1).padStart(2, "0")}`,
    last_activity_at: new Date(Date.UTC(2026, 7, 12, 23, 59 - index)).toISOString(),
    signals: { git: true, docs: index % 3 !== 0, codex_history: true },
    estimate: { duration_seconds_min: 240, duration_seconds_max: 480, relative_cost: "medium" },
    privacy_note: "Only selected sources are read after start.",
    selected_by_default: false,
    sources: ["codex_metadata"],
    codex_thread_count: primary ? 179 : 26 - index,
  };
}

export function archaeologyDTO({
  revision = 11,
  discoveryState = "ready",
  stage = "ready",
  selectedProjectIds = [],
  taskLaunchAvailable = true,
  canStart = selectedProjectIds.length > 0,
  state = "draft",
  handoff = null,
} = {}) {
  return {
    id: "ARCH-LIVE",
    state,
    discovery: {
      state: discoveryState,
      stage,
      metadata_only: true,
      source_roots_scanned: 0,
      discovered_at: "2026-08-13T00:40:28Z",
      updated_at: "2026-08-13T00:40:28Z",
      codex_threads_examined: 2431,
      workspaces_grouped: 26,
      candidates: Array.from({ length: 26 }, (_, index) => candidate(index)),
    },
    config: {
      selected_project_ids: selectedProjectIds,
      depth: "deep",
      sources: { git: true, docs: true, codex_history: true },
      max_concurrency: 2,
    },
    runs: [],
    review: null,
    capabilities: {
      project_catalog: { configured: true, available: true, mode: "codex_metadata" },
      task_launch: {
        configured: true,
        available: taskLaunchAvailable,
        mode: "app_server_stdio",
        reason: taskLaunchAvailable ? "" : "Refresh the project catalog before starting Codex tasks.",
      },
      discovery: { configured: true, available: true, mode: "allowlisted_metadata" },
      historian_handoff: { configured: true, available: true, mode: "exact_task_claim_report" },
      review: { configured: true, available: true, mode: "validated_manifest" },
      canonical_apply: { configured: true, available: true, mode: "preview_digest_confirm" },
    },
    handoff,
    controls: { can_start: canStart, can_pause: false, can_resume: false, can_cancel: state === "running" },
    revision,
    updated_at: "2026-08-13T00:40:28Z",
  };
}

export function startedArchaeologyDTO({ revision = 14 } = {}) {
  return archaeologyDTO({
    revision,
    selectedProjectIds: ["codex-commons"],
    canStart: false,
    state: "running",
    handoff: {
      id: "HANDOFF-QA",
      batch_id: "BATCH-QA",
      state: "running",
      created_at: "2026-08-13T01:00:00Z",
      updated_at: "2026-08-13T01:00:00Z",
      depth: "deep",
      sources: { git: true, docs: true, codex_history: true },
      concurrency: 2,
      candidate_ids: ["codex-commons"],
      tasks: [{
        job_id: "JOB-QA-1",
        batch_id: "BATCH-QA",
        candidate_id: "codex-commons",
        project_id: "project-codex-commons",
        state: "queued",
        mode: "app_server_dynamic_tools",
        phase_label: "Queued",
        sources_examined: 0,
        available_actions: [],
      }],
      allowed_actions: ["cancel"],
      progress: {
        queued_count: 1,
        active_count: 0,
        attention_count: 0,
        selected_total: 1,
        preparing_count: 0,
        starting_count: 0,
        task_created_count: 0,
        claimed_count: 0,
        running_count: 0,
        report_ready_count: 0,
        completed_count: 0,
        failed_count: 0,
        uncertain_count: 0,
        updated_at: "2026-08-13T01:00:00Z",
      },
    },
  });
}

export function apiResponse(data, requestID = "project-archaeology-release-gate") {
  return new Response(JSON.stringify({
    ok: true,
    data,
    meta: { request_id: requestID, untrusted: false },
  }), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

export function apiError(status, code, requestID = "project-archaeology-release-gate-error") {
  return new Response(JSON.stringify({
    ok: false,
    error: { code, message: code },
    meta: { request_id: requestID, untrusted: false },
  }), {
    status,
    headers: { "content-type": "application/json" },
  });
}

export function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, resolve, reject };
}
