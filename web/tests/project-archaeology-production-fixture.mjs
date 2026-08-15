export const AUTHENTICATED_SESSION_DTO = Object.freeze({
  authenticated: true,
  principal: {
    kind: "human",
    principal: "human:local-admin",
    handle: "release-gate",
    display_name: "Release Gate",
  },
  csrf_token: "qa-csrf",
  auth_method: "codex",
  profile_revision: 1,
});

export const PRIMARY_CANDIDATE_ID = "codex-000000000000000000000001";
export const ARCHAEOLOGY_SESSION_ID = "AR-000000000000000000000001";
export const NATIVE_BATCH_ID = "ARB-000000000000000000000001";
export const STALE_NATIVE_BATCH_ID = "ARB-000000000000000000000002";
export const CANONICAL_APPLY_REASON = "A signed-in human must review the exact task and evidence diff, then confirm the server-derived manifest digest and source digest.";

export function nativeJobID(index = 0) {
  return `ARJ-${String(index + 1).padStart(24, "0")}`;
}

function candidateID(index) {
  return `codex-${String(index + 1).padStart(24, "0")}`;
}
function candidate(index) {
  const primary = index === 0;
  const id = candidateID(index);
  return {
    id,
    name: primary ? "Codex Commons" : `Codex Project ${String(index + 1).padStart(2, "0")}`,
    path_label: primary ? "Codex Commons" : `Codex Project ${String(index + 1).padStart(2, "0")}`,
    repository_label: primary ? "codex-commons" : `codex-project-${String(index + 1).padStart(2, "0")}`,
    last_activity_at: new Date(Date.UTC(2026, 7, 12, 23, 59 - index)).toISOString(),
    signals: { git: true, docs: index % 3 !== 0, codex_history: true },
    estimate: { duration_seconds_min: 60, duration_seconds_max: 600, relative_cost: "medium" },
    privacy_note: "Cataloged from bounded Codex thread metadata and configured roots; prompts and message bodies are not read.",
    selected_by_default: false,
    sources: ["codex_metadata"],
    codex_thread_count: primary ? 179 : 26 - index,
  };
}

export function archaeologyDTO({
  revision = 11,
  discoveryState = "ready",
  selectedProjectIds = [],
  taskLaunchAvailable = true,
  canStart = selectedProjectIds.length > 0,
  state = "draft",
  handoff = null,
  updatedAt = "2026-08-13T00:40:28Z",
} = {}) {
  return {
    id: ARCHAEOLOGY_SESSION_ID,
    state,
    discovery: {
      state: discoveryState,
      metadata_only: true,
      source_roots_scanned: 0,
      discovered_at: "2026-08-13T00:40:28Z",
      tasks_examined: 179,
      projects_grouped: 26,
      truncated: false,
      completed_at: "2026-08-13T00:40:28Z",
      app_server_identity: "Codex App Server · 0.147.0",
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
      project_catalog: { configured: true, available: true, mode: "codex_metadata", reason: "Projects are grouped from Codex-known workspaces and additive configured roots." },
      task_launch: {
        configured: true,
        available: taskLaunchAvailable,
        mode: "app_server_stdio",
        reason: taskLaunchAvailable ? "Commons can submit one ordinary Codex historian task for every manually confirmed project; Codex governs execution capacity." : "Historian task launch is paused until Commons can reach the paired Codex App Server and reconcile terminal state.",
      },
      discovery: { configured: true, available: true, mode: "codex_known_metadata", reason: "Catalog uses bounded Codex thread metadata plus configured roots; message bodies are not read." },
      historian_handoff: { configured: true, available: true, mode: "exact_task_claim_report", reason: "Historian reports remain bound to exact launched task identity and human review." },
      review: { configured: true, available: false, mode: "durable_manifest" },
      canonical_apply: { configured: true, available: false, mode: "preview_manifest_confirm", reason: CANONICAL_APPLY_REASON },
    },
    handoff,
    controls: { can_start: canStart, can_pause: false, can_resume: false, can_cancel: state === "running" },
    revision,
    updated_at: updatedAt,
  };
}

export function startedArchaeologyDTO({ revision = 14 } = {}) {
  return archaeologyDTO({
    revision,
    selectedProjectIds: [PRIMARY_CANDIDATE_ID],
    canStart: false,
    state: "running",
    updatedAt: "2026-08-13T01:00:00Z",
    handoff: {
      id: "",
      batch_id: NATIVE_BATCH_ID,
      state: "queued",
      created_at: "2026-08-13T01:00:00Z",
      updated_at: "2026-08-13T01:00:00Z",
      depth: "deep",
      sources: { git: true, docs: true, codex_history: true },
      concurrency: 2,
      candidate_ids: [PRIMARY_CANDIDATE_ID],
      policy_attested: true,
      tasks: [{
        job_id: nativeJobID(),
        batch_id: NATIVE_BATCH_ID,
        launch_id: nativeJobID(),
        candidate_id: PRIMARY_CANDIDATE_ID,
        project_id: PRIMARY_CANDIDATE_ID,
        state: "queued",
        mode: "app_server_dynamic_tools",
        phase_label: "",
        sources_examined: 0,
        created_at: "2026-08-13T01:00:00Z",
        updated_at: "2026-08-13T01:00:00Z",
        available_actions: [],
      }],
      allowed_actions: [],
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

export function uncertainArchaeologyDTO({ revision = 21, withIdentity = true } = {}) {
  const base = startedArchaeologyDTO({ revision });
  return {
    ...base,
    state: "draft",
    updated_at: "2026-08-13T01:05:00Z",
    handoff: {
      ...base.handoff,
      state: "attention",
      updated_at: "2026-08-13T01:05:00Z",
      allowed_actions: ["resolve"],
      tasks: base.handoff.tasks.map((task) => ({
        ...task,
        state: "uncertain",
        phase_label: "",
        thread_id: withIdentity ? "019ff-project-history" : "",
        turn_id: withIdentity ? "turn-project-history" : "",
        updated_at: "2026-08-13T01:05:00Z",
        error: "Codex may have accepted this task, but Commons cannot safely retry it.",
        available_actions: ["resolve"],
      })),
      progress: {
        ...base.handoff.progress,
        queued_count: 0,
        uncertain_count: 1,
        updated_at: "2026-08-13T01:05:00Z",
      },
    },
    controls: { can_start: false, can_pause: false, can_resume: false, can_cancel: false },
  };
}

export function terminalAttentionArchaeologyDTO({ revision = 25, count = 6 } = {}) {
  const base = startedArchaeologyDTO({ revision });
  const candidates = base.discovery.candidates.slice(0, count);
  const candidateIDs = candidates.map((candidate) => candidate.id);
  const updatedAt = "2026-08-13T01:18:00Z";
  const tasks = candidates.map((candidate, index) => ({
    ...base.handoff.tasks[0],
    job_id: nativeJobID(index),
    launch_id: nativeJobID(index),
    batch_id: NATIVE_BATCH_ID,
    candidate_id: candidate.id,
    project_id: candidate.id,
    state: "attention",
    phase_label: "",
    thread_id: `019ff-terminal-attention-${index + 1}`,
    turn_id: `turn-terminal-attention-${index + 1}`,
    duration_ms: 420000 + index,
    updated_at: updatedAt,
    error: "This task needs attention before project history can continue.",
    available_actions: [],
  }));
  return archaeologyDTO({
    revision,
    selectedProjectIds: candidateIDs,
    canStart: true,
    state: "draft",
    updatedAt,
    handoff: {
      ...base.handoff,
      state: "canceled",
      updated_at: updatedAt,
      candidate_ids: candidateIDs,
      tasks,
      allowed_actions: [],
      progress: {
        ...base.handoff.progress,
        queued_count: 0,
        attention_count: count,
        selected_total: count,
        uncertain_count: 0,
        updated_at: updatedAt,
      },
    },
  });
}

export function reconciledArchaeologyDTO({ revision = 22 } = {}) {
  const base = uncertainArchaeologyDTO({ revision });
  return {
    ...base,
    updated_at: "2026-08-13T01:06:00Z",
    handoff: {
      ...base.handoff,
      updated_at: "2026-08-13T01:06:00Z",
      allowed_actions: [],
      tasks: base.handoff.tasks.map((task) => ({
        ...task,
        state: "interrupted",
        updated_at: "2026-08-13T01:06:00Z",
        error: "The exact Codex task was confirmed stopped by a signed-in human.",
        available_actions: [],
      })),
      progress: {
        ...base.handoff.progress,
        uncertain_count: 0,
        failed_count: 1,
        updated_at: "2026-08-13T01:06:00Z",
      },
    },
  };
}

function reviewMember() {
  return {
    session_id: "SES-4168",
    display_name: "Integration historian",
    reachability: "historical_or_unknown",
    execution: "not_attested",
    authority: "provenance_only",
    contribution_count: 6,
    source_count: 4,
    collaboration_count: 2,
    demonstrated_strengths: ["Provenance design", "Integration work"],
    uncertainties: ["Current reachability was not observed"],
  };
}

export function completedArchaeologyDTO({ revision = 24, reviewBatchID = NATIVE_BATCH_ID } = {}) {
  const base = startedArchaeologyDTO({ revision });
  const member = reviewMember();
  return {
    ...base,
    state: "draft",
    updated_at: "2026-08-13T01:12:00Z",
    review: {
      batch_id: reviewBatchID,
      proposed_outcomes: [{
        id: "OUT-1",
        title: "Established exact-session provenance",
        summary: "Connected durable contributions to their originating Codex sessions.",
        project_id: PRIMARY_CANDIDATE_ID,
        source_count: 4,
        provenance: [
          { source_kind: "codex_history", source_label: "thread:019ff4168aabbcc", digest: `sha256:${"a".repeat(64)}`, recorded_at: "2026-08-13T01:08:00Z" },
          { source_kind: "git", source_label: `commit:${"b".repeat(40)}`, digest: `sha256:${"b".repeat(64)}`, recorded_at: "2026-08-13T01:09:00Z" },
        ],
        member_sessions: [member],
      }],
      member_sessions: [member],
      provenance_summary: "Four sources were examined; two exact citations are retained in this bounded report.",
      can_apply: false,
      requires_explicit_approval: true,
    },
    capabilities: {
      ...base.capabilities,
      review: { configured: true, available: true, mode: "durable_manifest", reason: "" },
      canonical_apply: { configured: true, available: false, mode: "preview_manifest_confirm", reason: CANONICAL_APPLY_REASON },
    },
    handoff: {
      ...base.handoff,
      state: "completed",
      updated_at: "2026-08-13T01:12:00Z",
      tasks: base.handoff.tasks.map((task) => ({
        ...task,
        state: "completed",
        phase_label: "Report accepted",
        sources_examined: 4,
        duration_ms: 420000,
        thread_id: "019ff-project-history",
        turn_id: "turn-project-history",
        updated_at: "2026-08-13T01:12:00Z",
      })),
      progress: {
        ...base.handoff.progress,
        queued_count: 0,
        completed_count: 1,
        updated_at: "2026-08-13T01:12:00Z",
      },
    },
    controls: { can_start: true, can_pause: false, can_resume: false, can_cancel: false },
  };
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
