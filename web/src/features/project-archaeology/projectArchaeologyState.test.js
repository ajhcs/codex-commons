import test from "node:test";
import assert from "node:assert/strict";
import {
  archaeologyView,
  canStartArchaeology,
  configFromModel,
  formatDurationRange,
  memberFacts,
  taskPackText,
  runProgressText,
} from "./projectArchaeologyState.js";

test("archaeology view follows real discovery and run state", () => {
  assert.equal(archaeologyView(null), "intro");
  assert.equal(archaeologyView({ discovery: { state: "discovering" }, state: "draft" }), "discovering");
  assert.equal(archaeologyView({ discovery: { state: "ready" }, state: "draft", handoff: { state: "ready_to_claim" } }), "handoff");
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

test("task pack text preserves the durable handoff and exact bounded prompts", () => {
  const text = taskPackText({ id: "HAND-1", depth: "deep", sources: { git: true, docs: false, codexHistory: true }, concurrency: 2, pack: { title: "History pack", instructions: "Claim once.", projects: [{ candidateId: "p", label: "Codex Commons", taskPrompt: "Review Git and docs." }] } });
  assert.match(text, /HAND-1/);
  assert.match(text, /Codex Commons/);
  assert.match(text, /Review Git and docs/);
  assert.match(text, /Depth: deep/);
  assert.match(text, /Enabled sources: Git, Codex history/);
  assert.match(text, /Maximum concurrent historians: 2/);
});
