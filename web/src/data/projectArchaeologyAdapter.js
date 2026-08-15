import { MAX_PROJECT_ARCHAEOLOGY_SELECTION } from "../contracts/commons.js";
const DIGEST_PATTERN = /^sha256:[0-9a-f]{64}$/;
const HANDOFF_STATES = new Set(["queued", "running", "cancel_requested", "canceled", "completed", "attention", "launching", "failed", "uncertain", "ready_to_claim", "claimed"]);
const TASK_STATES = new Set(["queued", "starting", "active", "report_ready", "cancel_requested", "completed", "failed", "interrupted", "canceled", "uncertain", "attention", "preparing", "starting_codex", "task_created", "claimed", "running"]);

function record(value) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw new TypeError("record required");
  return value;
}

function string(value) {
  if (typeof value !== "string") throw new TypeError("string required");
  return value;
}

function boundedString(value, maximum, { optional = false } = {}) {
  if (optional && value == null) return "";
  const result = string(value);
  if (result.length > maximum) throw new TypeError("bounded string required");
  return result;
}

function optionalInteger(value, maximum) {
  if (value == null) return null;
  const result = integer(value);
  if (result > maximum) throw new TypeError("bounded integer required");
  return result;
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
  const batchId = boundedString(value.batch_id, 200, { optional: true });
  const nativeBatch = batchId.length > 0;
  if (!Array.isArray(value.tasks) || value.tasks.length > (nativeBatch ? MAX_PROJECT_ARCHAEOLOGY_SELECTION : 100)) throw new TypeError("invalid task list");
  if (nativeBatch && typeof value.policy_attested !== "boolean") throw new TypeError("native handoff attestation required");

  const sources = record(value.sources);
  const policyAttested = value.policy_attested === true;
  const normalizedSources = { git: sources.git === true, docs: sources.docs === true, codexHistory: sources.codex_history === true };
  if (nativeBatch && policyAttested && (!["quick", "standard", "deep"].includes(value.depth) || !Object.values(normalizedSources).some(Boolean) || ![1, 2].includes(value.concurrency))) throw new TypeError("invalid attested handoff policy");

  const tasks = value.tasks.map((rawTask) => {
    const task = record(rawTask);
    if (!TASK_STATES.has(task.state)) throw new TypeError("invalid task state");
    return {
      jobId: boundedString(task.job_id, 200, { optional: true }),
      batchId: boundedString(task.batch_id, 200, { optional: true }),
      candidateId: boundedString(task.candidate_id, 240, { optional: true }),
      projectId: boundedString(task.project_id, 200),
      state: task.state,
      mode: boundedString(task.mode, 80, { optional: true }),
      phaseLabel: boundedString(task.phase_label, 120, { optional: true }),
      sourcesExamined: integer(task.sources_examined ?? 0),
      durationMs: optionalInteger(task.duration_ms, 604800000),
      launchId: boundedString(task.launch_id, 200, { optional: true }),
      threadId: boundedString(task.thread_id, 240, { optional: true }),
      turnId: boundedString(task.turn_id, 240, { optional: true }),
      createdAt: timestamp(task.created_at, timestampLabel),
      updatedAt: timestamp(task.updated_at, timestampLabel),
      error: boundedString(task.error, 320, { optional: true }),
      availableActions: stringList(task.available_actions || [], 8),
    };
  });
  const candidateIds = stringList(value.candidate_ids, nativeBatch ? MAX_PROJECT_ARCHAEOLOGY_SELECTION : 100);
  const progress = value.progress == null ? null : (() => {
    const raw = record(value.progress);
    return {
      queuedCount: integer(raw.queued_count ?? 0),
      activeCount: integer(raw.active_count ?? 0),
      attentionCount: integer(raw.attention_count ?? 0),
      selectedTotal: integer(raw.selected_total),
      preparingCount: integer(raw.preparing_count),
      startingCount: integer(raw.starting_count),
      taskCreatedCount: integer(raw.task_created_count),
      claimedCount: integer(raw.claimed_count),
      runningCount: integer(raw.running_count),
      reportReadyCount: integer(raw.report_ready_count),
      completedCount: integer(raw.completed_count),
      failedCount: integer(raw.failed_count),
      uncertainCount: integer(raw.uncertain_count),
      updatedAt: timestamp(raw.updated_at, timestampLabel),
    };
  })();

  if (nativeBatch) {
    const unattestedTerminalStates = new Set(["attention", "completed", "failed", "interrupted", "canceled", "uncertain"]);
    const unattestedAuditStates = new Set(["attention", "completed", "canceled"]);
    if (!policyAttested && (value.depth !== "" || Object.values(normalizedSources).some(Boolean) || !unattestedAuditStates.has(value.state) || ![1, 2].includes(value.concurrency) || tasks.length < 1 || !tasks.every((task) => unattestedTerminalStates.has(task.state)))) throw new TypeError("invalid unattested handoff policy");
    if (!progress || candidateIds.length < 1 || tasks.length !== candidateIds.length || new Set(candidateIds).size !== candidateIds.length || candidateIds.some((id) => !id)) throw new TypeError("invalid native handoff cardinality");

    const candidateSet = new Set(candidateIds);
    const jobIds = tasks.map((task) => task.jobId);
    const projectIds = tasks.map((task) => task.projectId);
    const taskCandidateIds = tasks.map((task) => task.candidateId);
    if (tasks.some((task) => !task.jobId || !task.projectId || !task.candidateId || task.batchId !== batchId)
      || new Set(jobIds).size !== tasks.length
      || new Set(projectIds).size !== tasks.length
      || new Set(taskCandidateIds).size !== tasks.length
      || taskCandidateIds.some((id) => !candidateSet.has(id))) throw new TypeError("invalid native handoff identities");

    const expected = {
      queuedCount: 0,
      activeCount: 0,
      attentionCount: 0,
      selectedTotal: tasks.length,
      preparingCount: 0,
      startingCount: 0,
      taskCreatedCount: 0,
      claimedCount: 0,
      runningCount: 0,
      reportReadyCount: 0,
      completedCount: 0,
      failedCount: 0,
      uncertainCount: 0,
    };
    tasks.forEach((task) => {
      if (task.state === "queued") expected.queuedCount += 1;
      else if (task.state === "starting") { expected.startingCount += 1; expected.activeCount += 1; }
      else if (task.state === "active") { expected.runningCount += 1; expected.activeCount += 1; }
      else if (task.state === "cancel_requested") expected.activeCount += 1;
      else if (task.state === "canceled") expected.failedCount += 1;
      else if (task.state === "attention") expected.attentionCount += 1;
      else if (task.state === "interrupted") expected.failedCount += 1;
      else if (task.state === "preparing") expected.preparingCount += 1;
      else if (task.state === "starting_codex") expected.startingCount += 1;
      else if (task.state === "task_created") expected.taskCreatedCount += 1;
      else if (task.state === "claimed") expected.claimedCount += 1;
      else if (task.state === "running") expected.runningCount += 1;
      else if (task.state === "report_ready") { expected.reportReadyCount += 1; expected.activeCount += 1; }
      else if (task.state === "completed") expected.completedCount += 1;
      else if (task.state === "failed") expected.failedCount += 1;
      else if (task.state === "uncertain") expected.uncertainCount += 1;
    });
    if (Object.entries(expected).some(([key, count]) => progress[key] !== count)) throw new TypeError("invalid native handoff progress");
  }

  return {
    id: boundedString(value.id, 200),
    batchId,
    state: value.state,
    createdAt: timestamp(value.created_at, timestampLabel),
    updatedAt: timestamp(value.updated_at, timestampLabel),
    depth: string(value.depth),
    sources: normalizedSources,
    concurrency: integer(value.concurrency),
    candidateIds,
    policyAttested: nativeBatch ? policyAttested : null,
    tasks,
    allowedActions: stringList(value.allowed_actions, 8),
    progress,
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
  if (!HISTORICAL_KEY_PATTERN.test(value.batch_id) || !DIGEST_PATTERN.test(value.source_digest) || !DIGEST_PATTERN.test(value.manifest_digest) || value.collision_policy !== "current_wins" || !["preview", "applied"].includes(value.state) || typeof value.applied !== "boolean" || value.applied !== (value.state === "applied") || !Array.isArray(value.tasks) || value.tasks.length < 1 || value.tasks.length > 25) throw new TypeError("invalid historical import result");
  const seenKeys = new Set();
  const tasks = value.tasks.map((rawTask) => {
    const task = record(rawTask);
    const key = byteBounded(task.key, 64, { required: true });
    if (!HISTORICAL_KEY_PATTERN.test(key) || seenKeys.has(key) || !["created", "skipped_current", "replayed"].includes(task.disposition)) throw new TypeError("invalid historical import receipt");
    seenKeys.add(key);
    return { key, id: byteBounded(task.id, 200, { required: true }), disposition: task.disposition };
  });
  const counts = historicalCounts(value.counts);
  if (counts.tasks !== tasks.length || counts.projectThreadAliases > 20 || counts.attributions > 200 || counts.events > 500 || counts.created + counts.skippedCurrent + counts.replayed !== tasks.length || counts.created !== tasks.filter((task) => task.disposition === "created").length || counts.skippedCurrent !== tasks.filter((task) => task.disposition === "skipped_current").length || counts.replayed !== tasks.filter((task) => task.disposition === "replayed").length) throw new TypeError("invalid historical import counts");
  const recordedAt = timestamp(value.recorded_at, timestampLabel);
  if (value.applied && !recordedAt) throw new TypeError("applied import requires recorded time");
  return {
    batchId: value.batch_id,
    sourceDigest: value.source_digest,
    manifestDigest: value.manifest_digest,
    collisionPolicy: value.collision_policy,
    state: value.state,
    applied: value.applied,
    recordedAt,
    tasks,
    counts,
  };
}

const HISTORICAL_KEY_PATTERN = /^[a-z0-9][a-z0-9._-]{0,63}$/;
const HISTORICAL_ROLES = new Set(["originator", "implementer", "reviewer", "evaluator"]);
const HISTORICAL_CONFIDENCES = new Set(["verified", "supported", "uncertain"]);
const HISTORICAL_EVENT_KINDS = new Set(["completed", "reviewed", "failed", "retried", "remediated", "evaluated"]);
const lexical = (left, right) => left < right ? -1 : left > right ? 1 : 0;

function byteBounded(value, maximum, { required = false } = {}) {
  value = string(value);
  if (new TextEncoder().encode(value).length > maximum || value.includes("\0") || (required && !value.trim())) throw new TypeError("bounded text required");
  return value;
}

function historicalTime(value, timestampLabel) {
  value = string(value);
  const date = new Date(value);
  if (!Number.isFinite(date.getTime()) || date.getTime() > Date.now() + 300000) throw new TypeError("invalid historical timestamp");
  return timestampLabel ? timestampLabel(value) : value;
}

function historicalSource(value, timestampLabel) {
  value = record(value);
  const digest = string(value.digest);
  if (!DIGEST_PATTERN.test(digest)) throw new TypeError("invalid historical source digest");
  return {
    kind: byteBounded(value.kind, 40, { required: true }),
    stableId: byteBounded(value.stable_id, 300, { required: true }),
    digest,
    occurredAt: historicalTime(value.occurred_at, timestampLabel),
  };
}

function deepFreeze(value) {
  Object.freeze(value);
  Object.values(value).forEach((item) => {
    if (item && typeof item === "object" && !Object.isFrozen(item)) deepFreeze(item);
  });
  return value;
}

function normalizeHistoricalRequest(value, timestampLabel) {
  value = record(value);
  const aliases = value.project_thread_aliases || [];
  if (value.schema_version !== 1 || !HISTORICAL_KEY_PATTERN.test(value.batch_id) || !DIGEST_PATTERN.test(value.source_digest) || value.collision_policy !== "current_wins" || !Array.isArray(value.tasks) || value.tasks.length < 1 || value.tasks.length > 25 || !Array.isArray(aliases) || aliases.length > 20) throw new TypeError("invalid historical import request");
  if ((value.confirm_source_digest != null && value.confirm_source_digest !== "") || (value.confirm_manifest_digest != null && value.confirm_manifest_digest !== "")) throw new TypeError("preview cannot arrive confirmed");

  const seenAliases = new Set();
  const aliasSessions = new Set();
  const projectThreadAliases = aliases.map((rawAlias) => {
    const alias = record(rawAlias);
    const key = byteBounded(alias.alias, 64, { required: true });
    const session = byteBounded(alias.session, 200, { required: true });
    if (!HISTORICAL_KEY_PATTERN.test(key) || seenAliases.has(key) || aliasSessions.has(session)) throw new TypeError("invalid historical alias");
    seenAliases.add(key);
    aliasSessions.add(session);
    return { alias: key, session, source: historicalSource(alias.source, timestampLabel) };
  }).sort((left, right) => lexical(left.alias, right.alias));

  let attributionCount = 0;
  let eventCount = 0;
  const seenTaskKeys = new Set();
  const tasks = value.tasks.map((rawTask) => {
    const task = record(rawTask);
    const key = byteBounded(task.key, 64, { required: true });
    if (!HISTORICAL_KEY_PATTERN.test(key) || seenTaskKeys.has(key) || task.state !== "done" || !Array.isArray(task.attributions) || task.attributions.length < 1 || task.attributions.length > 20 || !Array.isArray(task.events || []) || (task.events || []).length > 25) throw new TypeError("invalid historical task");
    seenTaskKeys.add(key);
    const attributedSessions = new Set();
    const seenAttributions = new Set();
    const attributions = task.attributions.map((rawAttribution) => {
      const attribution = record(rawAttribution);
      const session = byteBounded(attribution.session, 200, { required: true });
      const role = string(attribution.role);
      const confidence = string(attribution.confidence);
      const identity = `${session}\0${role}`;
      if (aliasSessions.has(session) || !HISTORICAL_ROLES.has(role) || !HISTORICAL_CONFIDENCES.has(confidence) || seenAttributions.has(identity)) throw new TypeError("invalid historical attribution");
      seenAttributions.add(identity);
      attributedSessions.add(session);
      attributionCount += 1;
      return { session, role, confidence, source: historicalSource(attribution.source, timestampLabel) };
    }).sort((left, right) => lexical(`${left.session}\0${left.role}\0${left.source.kind}\0${left.source.stableId}\0${left.source.digest}`, `${right.session}\0${right.role}\0${right.source.kind}\0${right.source.stableId}\0${right.source.digest}`));
    const seenEvents = new Set();
    const events = (task.events || []).map((rawEvent) => {
      const event = record(rawEvent);
      const eventKey = byteBounded(event.key, 64, { required: true });
      const session = byteBounded(event.session || "", 200);
      const kind = string(event.kind);
      const confidence = string(event.confidence);
      if (!HISTORICAL_KEY_PATTERN.test(eventKey) || seenEvents.has(eventKey) || !HISTORICAL_EVENT_KINDS.has(kind) || (session && (aliasSessions.has(session) || !attributedSessions.has(session))) || !HISTORICAL_CONFIDENCES.has(confidence)) throw new TypeError("invalid historical event");
      seenEvents.add(eventKey);
      eventCount += 1;
      return { key: eventKey, kind, summary: byteBounded(event.summary, 1000, { required: true }), session, confidence, source: historicalSource(event.source, timestampLabel) };
    }).sort((left, right) => lexical(left.key, right.key));
    if (!Number.isInteger(task.priority) || task.priority < -1000 || task.priority > 1000) throw new TypeError("invalid historical task priority");
    return {
      key,
      title: byteBounded(task.title, 300, { required: true }),
      description: byteBounded(task.description || "", 12000),
      acceptance: byteBounded(task.acceptance || "", 4000),
      state: "done",
      priority: task.priority,
      source: historicalSource(task.source, timestampLabel),
      attributions,
      events,
    };
  }).sort((left, right) => lexical(left.key, right.key));
  if (attributionCount > 200 || eventCount > 500) throw new TypeError("historical proposal exceeds bounds");
  const request = deepFreeze(JSON.parse(JSON.stringify(value)));
  return {
    request,
    proposal: {
      batchId: value.batch_id,
      sourceDigest: value.source_digest,
      collisionPolicy: value.collision_policy,
      projectThreadAliases,
      tasks,
      counts: { projectThreadAliases: projectThreadAliases.length, tasks: tasks.length, attributions: attributionCount, events: eventCount },
    },
  };
}

export function normalizeArchaeologyImportPreview(value, timestampLabel) {
  value = record(value);
  const { request, proposal } = normalizeHistoricalRequest(value.request, timestampLabel);
  const preview = normalizeHistoricalImportResult(value.preview, timestampLabel);
  assertHistoricalPreviewIdentity(request, proposal, preview);
  return deepFreeze({ projectId: byteBounded(value.project_id, 200, { required: true }), request, proposal, preview });
}

function assertHistoricalPreviewIdentity(request, proposal, preview) {
  const previewTaskKeys = preview.tasks.map((task) => task.key);
  const proposalTaskKeys = proposal.tasks.map((task) => task.key);
  if (request.source_digest !== preview.sourceDigest || request.batch_id !== preview.batchId || preview.counts.tasks !== proposal.counts.tasks || preview.counts.projectThreadAliases !== proposal.counts.projectThreadAliases || preview.counts.attributions !== proposal.counts.attributions || preview.counts.events !== proposal.counts.events || previewTaskKeys.length !== proposalTaskKeys.length || previewTaskKeys.some((key, index) => key !== proposalTaskKeys[index])) throw new TypeError("preview identity mismatch");
}

export function confirmedHistoricalImportRequest(value, preview, confirmation) {
  const { request, proposal } = normalizeHistoricalRequest(value);
  preview = record(preview);
  if (!DIGEST_PATTERN.test(preview.manifestDigest) || preview.sourceDigest !== request.source_digest || preview.batchId !== request.batch_id || confirmation !== preview.manifestDigest) throw new TypeError("exact manifest digest confirmation required");
  assertHistoricalPreviewIdentity(request, proposal, preview);
  return { ...request, confirm_source_digest: request.source_digest, confirm_manifest_digest: preview.manifestDigest };
}
