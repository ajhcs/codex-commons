import { PROJECT_ARCHAEOLOGY_DEPTHS } from "../../contracts/commons.js";

export const ARCHAEOLOGY_DEPTHS = PROJECT_ARCHAEOLOGY_DEPTHS;
export const ARCHAEOLOGY_SOURCES = Object.freeze(["git", "docs", "codexHistory"]);

export const DEFAULT_ARCHAEOLOGY_CONFIG = Object.freeze({
  selectedProjectIds: [],
  depth: "standard",
  sources: Object.freeze({ git: true, docs: true, codexHistory: false }),
  maxConcurrency: 2,
});

const ACTIVE_STATES = new Set(["launching", "running", "pause_requested", "paused", "cancel_requested", "queued"]);
const ACTIVE_JOB_STATES = new Set(["queued", "starting", "active", "report_ready", "cancel_requested", "preparing", "starting_codex", "task_created", "claimed", "running"]);
const ATTENTION_JOB_STATES = new Set(["failed", "interrupted", "uncertain", "attention"]);
const TERMINAL_BATCH_STATES = new Set(["canceled", "completed", "attention", "failed", "uncertain"]);
const CATALOG_FRESHNESS_MS = 5 * 60 * 1000;
export const PROJECT_SORTS = Object.freeze(["recent", "tasks", "name"]);

export const IDLE_PROJECT_ARCHAEOLOGY_OPERATIONS = Object.freeze({
  backgroundRead: false,
  catalogRefresh: false,
  configCommit: false,
  lifecyclePolling: false,
});

export function projectArchaeologyOperationState(operations = {}) {
  const state = {
    backgroundRead: operations.backgroundRead === true,
    catalogRefresh: operations.catalogRefresh === true,
    configCommit: operations.configCommit === true,
    lifecyclePolling: operations.lifecyclePolling === true,
  };
  return {
    ...state,
    selectionLocked: state.configCommit,
    startBlocked: state.backgroundRead || state.catalogRefresh || state.configCommit,
    anyPending: Object.values(state).some(Boolean),
  };
}

export function sortProjectCandidates(candidates = [], sort = "recent") {
  const next = [...candidates];
  const byName = (a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }) || a.id.localeCompare(b.id);
  const recent = (candidate) => Date.parse(candidate.lastActivity?.iso || "") || Number.NEGATIVE_INFINITY;
  if (sort === "name") return next.sort(byName);
  if (sort === "tasks") return next.sort((a, b) => (Number(b.codexThreadCount) || 0) - (Number(a.codexThreadCount) || 0) || recent(b) - recent(a) || byName(a, b));
  return next.sort((a, b) => recent(b) - recent(a) || byName(a, b));
}

export function discoveryProgressText(discovery = {}) {
  const stage = discovery.stage || (discovery.state === "discovering" ? "reading_codex_metadata" : discovery.state);
  const label = ({ queued: "Refresh queued", reading_codex_metadata: "Reading Codex task metadata", persisting_catalog: "Organizing projects", ready: `${Number(discovery.workspacesGrouped) || discovery.candidates?.length || 0} projects found`, failed: "Refresh needs attention" })[stage] || "Checking project history";
  const facts = [];
  if (Number(discovery.codexThreadsExamined) > 0) facts.push(`${Number(discovery.codexThreadsExamined).toLocaleString()} tasks checked`);
  if (Number(discovery.workspacesGrouped) > 0) facts.push(`${Number(discovery.workspacesGrouped).toLocaleString()} projects found`);
  return [label, ...facts].filter(Boolean).join(" · ");
}

export function handoffProgress(handoff = {}) {
  const tasks = Array.isArray(handoff.tasks) ? handoff.tasks : [];
  const exact = handoff.progress;
  const count = (state) => tasks.filter((task) => task.state === state).length;
  const value = (name, state) => Number.isInteger(exact?.[name]) ? exact[name] : count(state);
  return {
    total: Number.isInteger(exact?.selectedTotal) ? exact.selectedTotal : tasks.length,
    queued: Number.isInteger(exact?.queuedCount) ? exact.queuedCount : count("queued") + count("preparing"),
    active: Number.isInteger(exact?.activeCount) ? exact.activeCount : count("starting") + count("active") + count("report_ready") + count("cancel_requested"),
    preparing: value("preparingCount", "preparing"),
    starting: Number.isInteger(exact?.startingCount) ? exact.startingCount : count("starting_codex") + count("starting"),
    created: value("taskCreatedCount", "task_created"),
    claimed: value("claimedCount", "claimed"),
    running: Number.isInteger(exact?.runningCount) ? exact.runningCount : count("running") + count("active"),
    ready: value("reportReadyCount", "report_ready") + value("completedCount", "completed"),
    attention: Number.isInteger(exact?.attentionCount)
      ? exact.attentionCount + exact.failedCount + exact.uncertainCount
      : count("attention") + count("uncertain") + count("failed") + count("interrupted"),
    failed: Number.isInteger(exact?.failedCount) ? exact.failedCount : count("failed") + count("interrupted"),
    updatedAt: exact?.updatedAt || handoff.updatedAt || null,
  };
}

export function isNativeArchaeologyTask(task) {
  return Boolean(task?.jobId && task?.batchId && task?.candidateId && task?.projectId);
}

export function archaeologyTaskPresentation(task = {}) {
  if (!isNativeArchaeologyTask(task)) return {
    tone: "legacy",
    primary: "Legacy historian · status not reconciled",
    secondary: "This pre-scheduler row is retained for audit only. Its earlier status is not a current execution claim.",
  };
  const copy = {
    queued: ["Queued in Commons", "Waiting for a launch slot."],
    starting: ["Asking Codex to create the task", "Commons is durably tracking this request."],
    active: ["Historian is examining project sources", task.phaseLabel || "Codex reported this task as active."],
    report_ready: ["Report ready for Commons", "Preparing it for your review."],
    cancel_requested: ["Cancellation requested", "Queued work will stop; Commons asked Codex to interrupt active work."],
    completed: ["Ready to review", "Nothing has been imported automatically."],
    failed: ["Task needs attention", task.error || "This Codex task stopped without a review-ready report."],
    interrupted: ["Task was interrupted", task.error || "Human review is needed before project history can continue."],
    uncertain: ["Codex may have accepted this task", task.error || "Commons will not retry automatically."],
    attention: ["Task needs human attention", task.error || "The queue is blocked until a human reviews this state."],
  }[task.state] || ["Task state unavailable", "Commons preserved the last bounded scheduler facts."];
  return { tone: ATTENTION_JOB_STATES.has(task.state) ? "attention" : task.state, primary: copy[0], secondary: copy[1] };
}

export function archaeologyTaskIsActive(task) {
  return ACTIVE_JOB_STATES.has(task?.state);
}

export function archaeologyBatchIsTerminal(handoff) {
  return TERMINAL_BATCH_STATES.has(handoff?.state);
}

export function configFromModel(config = {}) {
  const selectedProjectIds = Array.isArray(config.selectedProjectIds)
    ? config.selectedProjectIds.filter((value) => typeof value === "string" && value)
    : [];
  const sources = config.sources || {};
  return {
    selectedProjectIds,
    depth: ARCHAEOLOGY_DEPTHS.includes(config.depth) ? config.depth : DEFAULT_ARCHAEOLOGY_CONFIG.depth,
    sources: {
      git: sources.git !== false,
      docs: sources.docs !== false,
      codexHistory: sources.codexHistory === true,
    },
    maxConcurrency: config.maxConcurrency === 1 ? 1 : 2,
  };
}

export function shouldPreserveLocalArchaeologyConfig({ dirty = false } = {}) {
  return dirty === true;
}

export function reconcileConfigAfterCatalogRefresh(config = {}, model = {}) {
  const current = configFromModel(config);
  const selectable = new Set(
    (Array.isArray(model?.discovery?.candidates) ? model.discovery.candidates : [])
      .filter(isProjectCandidateSelectable)
      .map((candidate) => candidate.id),
  );
  return {
    ...current,
    selectedProjectIds: [...new Set(current.selectedProjectIds.filter((id) => selectable.has(id)))],
  };
}

export function archaeologyConfigVersion(model) {
  return `${model?.id || ""}:${model?.revision ?? ""}`;
}

export function isProjectCandidateSelectable(candidate) {
  return candidate?.sources?.some((source) => source === "codex_metadata" || source === "configured_root") === true;
}
export function shouldRefreshProjectCatalog(model, now = Date.now()) {
  if (!model || model.state !== "draft" || model.capabilities?.projectCatalog?.available !== true) return false;
  if (model.handoff?.tasks?.length) return false;
  const candidates = Array.isArray(model.discovery?.candidates) ? model.discovery.candidates : [];
  const hasCodexMetadata = candidates.some((candidate) => candidate.sources?.includes("codex_metadata"));
  const discoveredAt = Date.parse(model.discovery?.discoveredAt?.iso || "");
  if (!hasCodexMetadata || !Number.isFinite(discoveredAt)) return true;
  return discoveredAt > now + CATALOG_FRESHNESS_MS || now - discoveredAt >= CATALOG_FRESHNESS_MS;
}

export function selectedSourceCount(config) {
  return ARCHAEOLOGY_SOURCES.filter((source) => config?.sources?.[source]).length;
}

export function archaeologyConfigCommitReady(submittedConfig, configuredModel, baseRevision) {
  const submitted = configFromModel(submittedConfig);
  const committed = configFromModel(configuredModel?.config);
  const sameIDs = submitted.selectedProjectIds.length === committed.selectedProjectIds.length
    && submitted.selectedProjectIds.every((id, index) => committed.selectedProjectIds[index] === id);
  const sameSources = ARCHAEOLOGY_SOURCES.every((source) => submitted.sources[source] === committed.sources[source]);
  const revisionAdvanced = Number.isInteger(baseRevision)
    && Number.isInteger(configuredModel?.revision)
    && configuredModel.revision > baseRevision;
  return revisionAdvanced
    && sameIDs
    && submitted.depth === committed.depth
    && sameSources
    && submitted.maxConcurrency === committed.maxConcurrency
    && canStartArchaeology(committed, configuredModel?.discovery?.candidates || [])
    && configuredModel?.controls?.canStart === true
    && configuredModel?.capabilities?.taskLaunch?.available === true;
}

export function canSubmitArchaeologyConfig(config, model = {}) {
  return model.state === "draft"
    && model.capabilities?.taskLaunch?.configured === true
    && model.capabilities?.taskLaunch?.available === true
    && canStartArchaeology(config, model.discovery?.candidates || []);
}

export function canStartArchaeology(config, candidates = []) {
  if (!config?.selectedProjectIds?.length || selectedSourceCount(config) === 0) return false;
  const available = new Map(candidates.map((candidate) => [candidate.id, candidate]));
  return config.selectedProjectIds.every((id) => {
    const candidate = available.get(id);
    if (!candidate || !isProjectCandidateSelectable(candidate)) return false;
    return ARCHAEOLOGY_SOURCES.some((source) => config.sources[source] && candidate.signals?.[source]);
  });
}

export function archaeologyView(model) {
  const discoveryState = model?.discovery?.state || "idle";
  const sessionState = model?.state || "draft";
  if (discoveryState === "idle") return "intro";
  if (discoveryState === "discovering") return "discovering";
  if (discoveryState === "failed" && sessionState === "draft") return "discovery_failed";
  if (model?.handoff?.tasks?.length) return "handoff";
  if (sessionState === "completed" && model?.review) return "review";
  if (ACTIVE_STATES.has(sessionState)) return sessionState === "paused" ? "paused" : "running";
  if (sessionState === "failed") return "failed";
  return "configure";
}

export function formatDurationRange(estimate = {}) {
  const minimum = Number(estimate.durationSecondsMin);
  const maximum = Number(estimate.durationSecondsMax);
  if (!Number.isFinite(minimum) || !Number.isFinite(maximum) || minimum < 0 || maximum < minimum) return "Time varies by project";
  if (maximum < 60) return "Under a minute";
  if (minimum >= 60 && maximum < 3600) {
    const low = Math.max(1, Math.round(minimum / 60));
    const high = Math.max(1, Math.round(maximum / 60));
    return low === high ? `About ${low} min` : `About ${low}–${high} min`;
  }
  const format = (seconds) => {
    if (seconds < 60) return "under a minute";
    const minutes = Math.max(1, Math.round(seconds / 60));
    if (minutes < 60) return `${minutes} min`;
    const hours = Math.round(minutes / 6) / 10;
    return `${hours} hr`;
  };
  const low = format(minimum);
  const high = format(maximum);
  return low === high ? `About ${low}` : `About ${low}–${high}`;
}

export function sourceLabels(signals = {}) {
  const labels = [];
  if (signals.git) labels.push("Git");
  if (signals.docs) labels.push("Docs");
  if (signals.codexHistory) labels.push("Codex history");
  return labels;
}

export function runProgressText(run) {
  const completed = Number(run?.completedUnits);
  const total = Number(run?.totalUnits);
  if (Number.isFinite(completed) && Number.isFinite(total) && total > 0) {
    return `${Math.max(0, completed)} of ${total} sources examined`;
  }
  const examined = Number(run?.sourcesExamined);
  if (Number.isFinite(examined) && examined > 0) return `${examined} source${examined === 1 ? "" : "s"} examined`;
  return run?.phaseLabel || "Preparing project history";
}

export function memberFacts(member = {}) {
  return {
    displayName: member.displayName || "",
    reachability: member.reachability || "historical_or_unknown",
    execution: member.execution || "not_attested",
    authority: member.authority || "provenance_only",
    sessionID: member.sessionId || "Unknown session",
    contributionCount: Number(member.contributionCount) || 0,
    sourceCount: Number(member.sourceCount) || 0,
    collaborationCount: Number(member.collaborationCount) || 0,
    strengths: Array.isArray(member.demonstratedStrengths) ? member.demonstratedStrengths : [],
    uncertainties: Array.isArray(member.uncertainties) ? member.uncertainties : [],
  };
}
