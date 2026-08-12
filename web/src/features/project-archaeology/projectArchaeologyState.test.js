import test from "node:test";
import assert from "node:assert/strict";
import {
  archaeologyConfigVersion,
  archaeologyBatchIsTerminal,
  archaeologyTaskPresentation,
  archaeologyView,
  canStartArchaeology,
  configFromModel,
  formatDurationRange,
  memberFacts,
  runProgressText,
  discoveryProgressText,
  handoffProgress,
  isProjectCandidateSelectable,
  sortProjectCandidates,
  shouldRefreshProjectCatalog,
} from "./projectArchaeologyState.js";

test("configuration versions change when rediscovery updates the same session", () => {
  assert.equal(archaeologyConfigVersion({ id: "ARCH-1", revision: 4 }), "ARCH-1:4");
  assert.notEqual(
    archaeologyConfigVersion({ id: "ARCH-1", revision: 4 }),
    archaeologyConfigVersion({ id: "ARCH-1", revision: 5 }),
  );
});

test("opening a draft refreshes stale or configured-root-only project catalogs", () => {
  const now = Date.parse("2026-08-12T20:00:00Z");
  const base = {
    state: "draft",
    capabilities: { projectCatalog: { available: true } },
    discovery: { discoveredAt: { iso: "2026-08-12T19:59:00Z" }, candidates: [] },
    handoff: null,
  };
  assert.equal(shouldRefreshProjectCatalog(base, now), true);
  assert.equal(shouldRefreshProjectCatalog({
    ...base,
    discovery: { ...base.discovery, candidates: [{ sources: ["configured_root"] }] },
  }, now), true);
  assert.equal(shouldRefreshProjectCatalog({
    ...base,
    discovery: { discoveredAt: { iso: "2026-08-12T19:59:00Z" }, candidates: [{ sources: ["codex_metadata"] }] },
  }, now), false);
  assert.equal(shouldRefreshProjectCatalog({
    ...base,
    discovery: { discoveredAt: { iso: "2026-08-12T19:50:00Z" }, candidates: [{ sources: ["codex_metadata"] }] },
  }, now), true);
  assert.equal(shouldRefreshProjectCatalog({ ...base, state: "running" }, now), false);
  assert.equal(shouldRefreshProjectCatalog({ ...base, handoff: { tasks: [{ projectId: "alpha" }] } }, now), false);
  assert.equal(shouldRefreshProjectCatalog({ ...base, capabilities: { projectCatalog: { available: false } } }, now), false);
});

test("configured roots are selectable while source-less retained rows are audit-only", () => {
  assert.equal(isProjectCandidateSelectable({ sources: ["configured_root"] }), true);
  assert.equal(isProjectCandidateSelectable({ sources: ["codex_metadata"] }), true);
  assert.equal(isProjectCandidateSelectable({ sources: [] }), false);
  const configured = [{ id: "root", sources: ["configured_root"], signals: { git: true, docs: false, codexHistory: false } }];
  assert.equal(canStartArchaeology({ selectedProjectIds: ["root"], sources: { git: true } }, configured), true);
  const retained = [{ id: "legacy", sources: [], signals: { git: true, docs: true, codexHistory: true } }];
  assert.equal(canStartArchaeology({ selectedProjectIds: ["legacy"], sources: { git: true } }, retained), false);
});

test("archaeology view follows real discovery and run state", () => {
  assert.equal(archaeologyView(null), "intro");
  assert.equal(archaeologyView({ discovery: { state: "discovering" }, state: "draft" }), "discovering");
  assert.equal(archaeologyView({ discovery: { state: "ready" }, state: "draft", handoff: { state: "running", tasks: [{ projectId: "alpha", state: "running" }] } }), "handoff");
  assert.equal(archaeologyView({ discovery: { state: "ready" }, state: "paused" }), "paused");
  assert.equal(archaeologyView({ discovery: { state: "ready" }, state: "completed", review: {} }), "review");
});

test("configuration accepts the normalized adapter model without exposing invalid values", () => {
  assert.deepEqual(configFromModel({ selectedProjectIds: ["alpha", ""], depth: "deep", sources: { git: true, docs: false, codexHistory: true }, maxConcurrency: 1 }), {
    selectedProjectIds: ["alpha"],
    depth: "deep",
    sources: { git: true, docs: false, codexHistory: true },
    maxConcurrency: 1,
  });
});

test("start requires selected known projects and at least one source", () => {
  const candidates = [{ id: "alpha", sources: ["codex_metadata"], signals: { git: true, docs: false, codexHistory: false } }];
  assert.equal(canStartArchaeology({ selectedProjectIds: ["alpha"], sources: { git: true } }, candidates), true);
  assert.equal(canStartArchaeology({ selectedProjectIds: ["alpha"], sources: { docs: true } }, candidates), false);
  assert.equal(canStartArchaeology({ selectedProjectIds: ["missing"], sources: { git: true } }, candidates), false);
  assert.equal(canStartArchaeology({ selectedProjectIds: ["alpha"], sources: {} }, candidates), false);
});

test("duration and progress labels remain truthful when totals are unknown", () => {
  assert.equal(formatDurationRange({ durationSecondsMin: 240, durationSecondsMax: 480 }), "About 4–8 min");
  assert.equal(formatDurationRange({}), "Time varies by project");
  assert.equal(runProgressText({ completedUnits: 3, totalUnits: 8 }), "3 of 8 sources examined");
  assert.equal(runProgressText({ sourcesExamined: 3 }), "3 sources examined");
  assert.equal(runProgressText({ phaseLabel: "Reviewing documentation" }), "Reviewing documentation");
});

test("session membership facts do not infer reachability or authority", () => {
  assert.deepEqual(memberFacts({ sessionId: "SES-1", contributionCount: 4, sourceCount: 3, demonstratedStrengths: ["Review"] }), {
    displayName: "",
    reachability: "historical_or_unknown",
    execution: "not_attested",
    authority: "provenance_only",
    sessionID: "SES-1",
    contributionCount: 4,
    sourceCount: 3,
    collaborationCount: 0,
    strengths: ["Review"],
    uncertainties: [],
  });
  assert.equal(memberFacts({ sessionId: "SES-2" }).sessionID, "SES-2");
});


test("project sorting is stable, preserves duplicate labels, and keeps null activity last", () => {
  const rows = [
    { id: "b", name: "Same", codexThreadCount: 9, lastActivity: null },
    { id: "a", name: "Same", codexThreadCount: 2, lastActivity: { iso: "2026-08-12T10:00:00Z" } },
    { id: "c", name: "Alpha", codexThreadCount: 4, lastActivity: { iso: "2026-08-11T10:00:00Z" } },
  ];
  assert.deepEqual(sortProjectCandidates(rows, "recent").map((row) => row.id), ["a", "c", "b"]);
  assert.deepEqual(sortProjectCandidates(rows, "tasks").map((row) => row.id), ["b", "c", "a"]);
  assert.deepEqual(sortProjectCandidates(rows, "name").map((row) => row.id), ["c", "a", "b"]);
  assert.deepEqual(rows.map((row) => row.id), ["b", "a", "c"]);
});

test("operation summaries use only real stages and exact counts", () => {
  assert.equal(discoveryProgressText({ stage: "reading_codex_metadata", codexThreadsExamined: 4842, workspacesGrouped: 26 }), "Reading Codex task metadata · 4,842 tasks checked · 26 projects found");
  assert.deepEqual(handoffProgress({ tasks: [
    { state: "claimed" }, { state: "running" }, { state: "failed" }, { state: "report_ready" },
  ] }), { total: 4, queued: 0, active: 1, preparing: 0, starting: 0, created: 0, claimed: 1, running: 1, ready: 1, attention: 1, failed: 1, updatedAt: null });
});

test("native scheduler states and legacy rows never overclaim execution", () => {
  const native = { jobId: "JOB-1", batchId: "BATCH-1", candidateId: "catalog", projectId: "canonical", state: "active", phaseLabel: "Reading docs" };
  assert.deepEqual(archaeologyTaskPresentation(native), { tone: "active", primary: "Historian is examining project sources", secondary: "Reading docs" });
  const legacy = archaeologyTaskPresentation({ launchId: "LEGACY-1", projectId: "catalog", state: "claimed" });
  assert.equal(legacy.primary, "Legacy historian · status not reconciled");
  assert.match(legacy.secondary, /not a current execution claim/i);
  assert.equal(archaeologyBatchIsTerminal({ state: "canceled" }), true);
  assert.equal(archaeologyBatchIsTerminal({ state: "running" }), false);
});

test("native aggregate progress trusts bounded backend counts", () => {
  assert.deepEqual(handoffProgress({ progress: {
    queuedCount: 3, activeCount: 2, attentionCount: 1, selectedTotal: 8,
    preparingCount: 0, startingCount: 1, taskCreatedCount: 0, claimedCount: 0,
    runningCount: 1, reportReadyCount: 1, completedCount: 1, failedCount: 1, uncertainCount: 1,
    updatedAt: { iso: "2026-08-12T12:00:00Z" },
  }, tasks: [] }), {
    total: 8, queued: 3, active: 2, preparing: 0, starting: 1, created: 0, claimed: 0,
    running: 1, ready: 2, attention: 3, failed: 1, updatedAt: { iso: "2026-08-12T12:00:00Z" },
  });
});
