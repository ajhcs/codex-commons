import { PROJECT_ARCHAEOLOGY_DEPTHS } from "../../contracts/commons.js";

export const ARCHAEOLOGY_DEPTHS = PROJECT_ARCHAEOLOGY_DEPTHS;
export const ARCHAEOLOGY_SOURCES = Object.freeze(["git", "docs", "codexHistory"]);

export const DEFAULT_ARCHAEOLOGY_CONFIG = Object.freeze({
  selectedProjectIds: [],
  depth: "standard",
  sources: Object.freeze({ git: true, docs: true, codexHistory: false }),
  maxConcurrency: 2,
});

const ACTIVE_STATES = new Set(["launching", "running", "pause_requested", "paused", "cancel_requested"]);

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

export function selectedSourceCount(config) {
  return ARCHAEOLOGY_SOURCES.filter((source) => config?.sources?.[source]).length;
}

export function canStartArchaeology(config, candidates = []) {
  if (!config?.selectedProjectIds?.length || selectedSourceCount(config) === 0) return false;
  const available = new Map(candidates.map((candidate) => [candidate.id, candidate]));
  return config.selectedProjectIds.every((id) => {
    const candidate = available.get(id);
    if (!candidate) return false;
    return ARCHAEOLOGY_SOURCES.some((source) => config.sources[source] && candidate.signals?.[source]);
  });
}

export function archaeologyView(model) {
  const discoveryState = model?.discovery?.state || "idle";
  const sessionState = model?.state || "draft";
  if (discoveryState === "idle") return "intro";
  if (discoveryState === "discovering") return "discovering";
  if (discoveryState === "failed" && sessionState === "draft") return "discovery_failed";
  if (sessionState === "completed" && model?.review) return "review";
  if (model?.handoff?.tasks?.length) return "handoff";
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
