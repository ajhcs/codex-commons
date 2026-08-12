const DIGEST_PATTERN = /^sha256:[0-9a-f]{64}$/;
const HANDOFF_STATES = new Set(["launching", "running", "completed", "failed", "uncertain", "ready_to_claim", "claimed"]);
const TASK_STATES = new Set(["preparing", "starting_codex", "task_created", "claimed", "running", "report_ready", "completed", "failed", "uncertain"]);

function record(value) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw new TypeError("record required");
  return value;
}

function string(value) {
  if (typeof value !== "string") throw new TypeError("string required");
  return value;
}

function integer(value) {
  if (!Number.isInteger(value) || value < 0) throw new TypeError("non-negative integer required");
  return value;
}

function timestamp(value, timestampLabel) {
  return value == null ? null : timestampLabel(value);
}

function stringList(value, maximum) {
  if (!Array.isArray(value) || value.length > maximum) throw new TypeError("bounded string list required");
  return value.map(string);
}

function capability(value) {
  value = record(value);
  if (typeof value.configured !== "boolean" || typeof value.available !== "boolean") throw new TypeError("capability facts required");
  return {
    configured: value.configured,
    available: value.available,
    mode: string(value.mode),
    reason: typeof value.reason === "string" ? value.reason : "",
  };
}

export function normalizeArchaeologyCapabilities(value) {
  value = record(value);
  return {
    projectCatalog: capability(value.project_catalog),
    taskLaunch: capability(value.task_launch),
    discovery: capability(value.discovery),
    historianHandoff: capability(value.historian_handoff),
    review: capability(value.review),
    canonicalApply: capability(value.canonical_apply),
  };
}

export function normalizeArchaeologyHandoff(value, timestampLabel) {
  if (value == null) return null;
  value = record(value);
  if (!HANDOFF_STATES.has(value.state)) throw new TypeError("invalid handoff state");
  const sources = record(value.sources);
  if (!Array.isArray(value.tasks) || value.tasks.length > 100) throw new TypeError("invalid task list");
  return {
    id: string(value.id),
    state: value.state,
    createdAt: timestamp(value.created_at, timestampLabel),
    updatedAt: timestamp(value.updated_at, timestampLabel),
    depth: string(value.depth),
    sources: { git: sources.git === true, docs: sources.docs === true, codexHistory: sources.codex_history === true },
    concurrency: integer(value.concurrency),
    candidateIds: stringList(value.candidate_ids, 100),
    tasks: value.tasks.map((rawTask) => {
      const task = record(rawTask);
      if (!TASK_STATES.has(task.state)) throw new TypeError("invalid task state");
      return {
        projectId: string(task.project_id), state: task.state,
        threadId: typeof task.thread_id === "string" ? task.thread_id : "",
        turnId: typeof task.turn_id === "string" ? task.turn_id : "",
        createdAt: timestamp(task.created_at, timestampLabel),
        updatedAt: timestamp(task.updated_at, timestampLabel),
        availableActions: stringList(task.available_actions || [], 8),
      };
    }),
    allowedActions: stringList(value.allowed_actions, 8),
  };
}

function historicalCounts(value) {
  value = record(value);
  return {
    projectThreadAliases: integer(value.project_thread_aliases),
    tasks: integer(value.tasks),
    attributions: integer(value.attributions),
    events: integer(value.events),
    created: integer(value.created),
    skippedCurrent: integer(value.skipped_current),
    replayed: integer(value.replayed),
  };
}

export function normalizeHistoricalImportResult(value, timestampLabel) {
  value = record(value);
  if (!DIGEST_PATTERN.test(value.source_digest) || !DIGEST_PATTERN.test(value.manifest_digest) || value.collision_policy !== "current_wins" || typeof value.applied !== "boolean" || !Array.isArray(value.tasks) || value.tasks.length > 100) throw new TypeError("invalid historical import result");
  return {
    batchId: string(value.batch_id),
    sourceDigest: value.source_digest,
    manifestDigest: value.manifest_digest,
    collisionPolicy: value.collision_policy,
    state: string(value.state),
    applied: value.applied,
    recordedAt: timestamp(value.recorded_at, timestampLabel),
    tasks: value.tasks.map((task) => {
      task = record(task);
      return { key: string(task.key), id: string(task.id), disposition: string(task.disposition) };
    }),
    counts: historicalCounts(value.counts),
  };
}

function cloneHistoricalRequest(value) {
  value = record(value);
  if (!Number.isInteger(value.schema_version) || value.schema_version < 1 || !DIGEST_PATTERN.test(value.source_digest) || value.collision_policy !== "current_wins" || !Array.isArray(value.tasks) || value.tasks.length > 100 || !Array.isArray(value.project_thread_aliases || []) || (value.project_thread_aliases || []).length > 100) throw new TypeError("invalid historical import request");
  if (value.confirm_source_digest != null && value.confirm_source_digest !== "") throw new TypeError("preview cannot arrive confirmed");
  return JSON.parse(JSON.stringify(value));
}

export function normalizeArchaeologyImportPreview(value, timestampLabel) {
  value = record(value);
  const request = cloneHistoricalRequest(value.request);
  const preview = normalizeHistoricalImportResult(value.preview, timestampLabel);
  if (request.source_digest !== preview.sourceDigest || request.batch_id !== preview.batchId) throw new TypeError("preview identity mismatch");
  return { projectId: string(value.project_id), request, preview };
}

export function confirmedHistoricalImportRequest(value, confirmation) {
  const request = cloneHistoricalRequest(value);
  if (confirmation !== request.source_digest) throw new TypeError("exact source digest confirmation required");
  return { ...request, confirm_source_digest: request.source_digest };
}
