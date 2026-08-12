import test from "node:test";
import assert from "node:assert/strict";
import {
  archaeologyConfigVersion,
  archaeologyView,
  canStartArchaeology,
  configFromModel,
  formatDurationRange,
  memberFacts,
  runProgressText,
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
  const candidates = [{ id: "alpha", signals: { git: true, docs: false, codexHistory: false } }];
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
