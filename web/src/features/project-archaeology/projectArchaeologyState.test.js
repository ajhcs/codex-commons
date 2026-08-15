import test from "node:test";
import assert from "node:assert/strict";
import {
  archaeologyConfigVersion,
  archaeologyConfigCommitReady,
  archaeologyStartCommitReady,
  archaeologyBatchIsTerminal,
  archaeologyTaskPresentation,
  archaeologyView,
  canStartArchaeology,
  canSubmitArchaeologyConfig,
  freshManualArchaeologyConfig,
  configFromModel,
  formatDurationRange,
  memberFacts,
  projectArchaeologyOperationState,
  runProgressText,
  discoveryProgressText,
  handoffProgress,
  isProjectCandidateSelectable,
  reconcileConfigAfterCatalogRefresh,
  reconcileOutcomeSelection,
  sortProjectCandidates,
  shouldRefreshProjectCatalog,
  shouldPreserveLocalArchaeologyConfig,
} from "./projectArchaeologyState.js";
import { archaeologyAttentionFixture, archaeologyHandoffFixture, archaeologyImportBridgeFixture, archaeologyReadyFixture, archaeologyReviewFixture } from "./projectArchaeologyFixtures.js";
import { normalizeArchaeologyImportPreview } from "../../data/projectArchaeologyAdapter.js";

test("operation state locks selection only for the durable config commit", () => {
  for (const operation of ["backgroundRead", "catalogRefresh", "lifecyclePolling"]) {
    const state = projectArchaeologyOperationState({ [operation]: true });
    assert.equal(state.selectionLocked, false, `${operation} must leave the rendered catalog interactive`);
  }
  assert.equal(projectArchaeologyOperationState({ configCommit: true }).selectionLocked, true);
  assert.equal(projectArchaeologyOperationState({ lifecyclePolling: true }).startBlocked, false);
  assert.equal(projectArchaeologyOperationState({ catalogRefresh: true }).startBlocked, true);
});

test("configured response must exactly and safely advance before start", () => {
  const candidate = { id: "alpha", sources: ["codex_metadata"], signals: { git: true, docs: true, codexHistory: true } };
  const submitted = { selectedProjectIds: ["alpha"], depth: "deep", sources: { git: true, docs: false, codexHistory: true }, maxConcurrency: 1 };
  const configured = {
    revision: 12,
    config: submitted,
    controls: { canStart: true },
    capabilities: { taskLaunch: { available: true } },
    discovery: { candidates: [candidate] },
  };
  assert.equal(archaeologyConfigCommitReady(submitted, configured, 11), true);
  assert.equal(archaeologyConfigCommitReady(submitted, { ...configured, revision: 11 }, 11), false);
  assert.equal(archaeologyConfigCommitReady(submitted, { ...configured, config: { ...submitted, depth: "quick" } }, 11), false);
  assert.equal(archaeologyConfigCommitReady(submitted, { ...configured, config: { ...submitted, sources: { ...submitted.sources, docs: true } } }, 11), false);
  assert.equal(archaeologyConfigCommitReady(submitted, { ...configured, controls: { canStart: false } }, 11), false);
  assert.equal(archaeologyConfigCommitReady(submitted, { ...configured, discovery: { candidates: [{ ...candidate, sources: [] }] } }, 11), false);
});

test("configured response accepts the same exact nine-project set in canonical order", () => {
  const selectedProjectIds = Array.from({ length: 9 }, (_, index) => "project-" + index);
  const candidates = selectedProjectIds.map((id) => ({ id, sources: ["codex_metadata"], signals: { git: true } }));
  const submitted = { selectedProjectIds: [...selectedProjectIds].reverse(), depth: "standard", sources: { git: true, docs: false, codexHistory: false }, maxConcurrency: 2 };
  const configured = {
    revision: 8,
    config: { ...submitted, selectedProjectIds },
    controls: { canStart: true },
    capabilities: { taskLaunch: { available: true } },
    discovery: { candidates },
  };
  assert.equal(archaeologyConfigCommitReady(submitted, configured, 7), true);
  assert.equal(archaeologyConfigCommitReady(submitted, { ...configured, config: { ...configured.config, selectedProjectIds: selectedProjectIds.slice(1) } }, 7), false);
  assert.equal(archaeologyConfigCommitReady(submitted, { ...configured, config: { ...configured.config, selectedProjectIds: [...selectedProjectIds.slice(0, 8), selectedProjectIds[0]] } }, 7), false);
});

test("start response must prove one fresh native job for every selected candidate", () => {
  const submitted = { selectedProjectIds: ["alpha", "beta"], depth: "standard", sources: { git: true, docs: true, codexHistory: false }, maxConcurrency: 2 };
  const configured = { revision: 12, config: submitted, handoff: { batchId: "BATCH-OLD" } };
  const task = (candidateId, projectId, jobId, batchId = "BATCH-NEW") => ({ candidateId, projectId, jobId, batchId, state: "queued" });
  const started = {
    revision: 13,
    config: { ...submitted, selectedProjectIds: [...submitted.selectedProjectIds].reverse() },
    handoff: {
      batchId: "BATCH-NEW",
      candidateIds: ["alpha", "beta"],
      policyAttested: true, depth: submitted.depth, sources: submitted.sources, concurrency: submitted.maxConcurrency,
      tasks: [task("alpha", "project-alpha", "JOB-A"), task("beta", "project-beta", "JOB-B")],
    },
  };
  assert.equal(archaeologyStartCommitReady(submitted, started, configured), true);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, revision: 12 }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, batchId: "" } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, batchId: "BATCH-OLD", tasks: started.handoff.tasks.map((item) => ({ ...item, batchId: "BATCH-OLD" })) } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, candidateIds: ["alpha"], tasks: [started.handoff.tasks[0]] } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, tasks: [started.handoff.tasks[0], { ...started.handoff.tasks[0] }] } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, tasks: [started.handoff.tasks[0], { ...started.handoff.tasks[1], projectId: "project-alpha" }] } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, config: { ...submitted, selectedProjectIds: ["alpha"] } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, config: { ...submitted, sources: { ...submitted.sources, docs: false } } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, started, { ...configured, config: { ...submitted, sources: { ...submitted.sources, codexHistory: true } } }), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, depth: "quick" } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, sources: { ...started.handoff.sources, docs: false } } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, concurrency: 1 } }, configured), false);
  assert.equal(archaeologyStartCommitReady(submitted, { ...started, handoff: { ...started.handoff, policyAttested: false } }, configured), false);
});

test("local valid selection can submit before persisted controls become startable", () => {
  const candidate = { id: "alpha", sources: ["codex_metadata"], signals: { git: true, docs: true, codexHistory: true } };
  const model = {
    state: "draft",
    controls: { canStart: false },
    capabilities: { taskLaunch: { configured: true, available: true } },
    discovery: { candidates: [candidate] },
  };
  const config = { selectedProjectIds: ["alpha"], sources: { git: true, docs: true, codexHistory: true } };
  assert.equal(canSubmitArchaeologyConfig(config, model), true);
  assert.equal(canSubmitArchaeologyConfig(config, { ...model, capabilities: { taskLaunch: { configured: true, available: false } } }), false);
});

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
    maxConcurrency: 2,
  });
});

test("every server revision preserves a dirty local project selection", () => {
  assert.equal(shouldPreserveLocalArchaeologyConfig({ dirty: true, operations: { backgroundRead: true } }), true);
  assert.equal(shouldPreserveLocalArchaeologyConfig({ dirty: true, refreshing: true }), true);
  assert.equal(shouldPreserveLocalArchaeologyConfig({ dirty: false, operations: { backgroundRead: true } }), false);
  assert.equal(shouldPreserveLocalArchaeologyConfig({ dirty: true, operations: { lifecyclePolling: true } }), true);
  assert.equal(shouldPreserveLocalArchaeologyConfig({ dirty: false, operations: { backgroundRead: true } }), false);
});

test("catalog refresh preserves valid local choices and advanced settings", () => {
  const current = {
    selectedProjectIds: ["kept", "removed", "legacy"],
    depth: "deep",
    sources: { git: false, docs: true, codexHistory: true },
    maxConcurrency: 1,
  };
  const refreshed = {
    discovery: {
      candidates: [
        { id: "kept", sources: ["codex_metadata"] },
        { id: "legacy", sources: [] },
        { id: "new", sources: ["codex_metadata"] },
      ],
    },
  };
  assert.deepEqual(reconcileConfigAfterCatalogRefresh(current, refreshed), {
    selectedProjectIds: ["kept"],
    depth: "deep",
    sources: { git: false, docs: true, codexHistory: true },
    maxConcurrency: 2,
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

test("switching history batches drops stale selected proposal IDs", () => {
  assert.deepEqual(reconcileOutcomeSelection(["OUT-A", "OUT-B"], [{ id: "OUT-B" }, { id: "OUT-C" }]), ["OUT-B"]);
  assert.deepEqual(reconcileOutcomeSelection(["OUT-A"], [{ id: "OUT-C" }]), []);
});

test("operation summaries use only real stages and exact counts", () => {
  assert.equal(discoveryProgressText({ stage: "reading_codex_metadata", codexThreadsExamined: 4842, workspacesGrouped: 26 }), "Reading Codex task metadata · 4,842 tasks checked · 26 projects found");
  assert.equal(discoveryProgressText({ stage: "ready", codexThreadsExamined: 2431, workspacesGrouped: 26 }), "26 projects found · 2,431 tasks checked");
  assert.deepEqual(handoffProgress({ tasks: [
    { state: "claimed" }, { state: "running" }, { state: "failed" }, { state: "report_ready" },
  ] }), { total: 4, queued: 0, active: 1, preparing: 0, starting: 0, created: 0, claimed: 1, running: 1, ready: 1, attention: 1, failed: 1, updatedAt: null });
});

test("native scheduler states and legacy rows never overclaim execution", () => {
  const native = { jobId: "JOB-1", batchId: "BATCH-1", candidateId: "catalog", projectId: "canonical", state: "active", phaseLabel: "Reading docs" };
  assert.deepEqual(archaeologyTaskPresentation(native), { tone: "active", primary: "Visible in Codex · reading project sources", secondary: "Reading docs" });
  assert.deepEqual(archaeologyTaskPresentation({ ...native, state: "starting", threadId: "thread-bound" }), {
    tone: "starting",
    primary: "Codex identity bound",
    secondary: "Confirming the final named task before Commons reports it as visible.",
  });
  assert.doesNotMatch(archaeologyTaskPresentation({ ...native, state: "starting", threadId: "thread-bound" }).primary, /visible/i);
  const legacy = archaeologyTaskPresentation({ launchId: "LEGACY-1", projectId: "catalog", state: "claimed" });
  assert.equal(legacy.primary, "Legacy historian · status not reconciled");
  assert.match(legacy.secondary, /not a current execution claim/i);
  assert.equal(archaeologyBatchIsTerminal({ state: "canceled" }), true);
  assert.equal(archaeologyBatchIsTerminal({ state: "running" }), false);
});


test("storyboard fixtures preserve the production native scheduler contract", () => {
  assert.match(archaeologyReadyFixture.id, /^AR-[0-9]{24}$/);
  assert.match(archaeologyReadyFixture.discovery.candidates[0].id, /^codex-[0-9]{24}$/);
  assert.match(archaeologyHandoffFixture.handoff.batchId, /^ARB-[0-9]{24}$/);
  assert.match(archaeologyHandoffFixture.handoff.tasks[0].jobId, /^ARJ-[0-9]{24}$/);
  assert.equal(archaeologyHandoffFixture.handoff.policyAttested, true);
  assert.deepEqual(archaeologyHandoffFixture.handoff.allowedActions, []);
  assert.equal(archaeologyAttentionFixture.handoff.state, "attention");
  assert.deepEqual(archaeologyAttentionFixture.handoff.allowedActions, ["resolve"]);
  assert.deepEqual(archaeologyAttentionFixture.handoff.tasks[0].availableActions, ["resolve"]);
  assert.equal(archaeologyReviewFixture.state, "draft");
  assert.equal(archaeologyReviewFixture.handoff.state, "completed");
  assert.equal(archaeologyReviewFixture.review.batchId, archaeologyReviewFixture.handoff.batchId);
  assert.equal(archaeologyImportBridgeFixture.request.tasks.length, 1);
  const normalized = normalizeArchaeologyImportPreview({
    project_id: archaeologyImportBridgeFixture.projectId,
    request: archaeologyImportBridgeFixture.request,
    preview: {
      batch_id: archaeologyImportBridgeFixture.preview.batchId,
      source_digest: archaeologyImportBridgeFixture.preview.sourceDigest,
      manifest_digest: archaeologyImportBridgeFixture.preview.manifestDigest,
      collision_policy: archaeologyImportBridgeFixture.preview.collisionPolicy,
      state: archaeologyImportBridgeFixture.preview.state,
      applied: archaeologyImportBridgeFixture.preview.applied,
      tasks: archaeologyImportBridgeFixture.preview.tasks,
      counts: {
        project_thread_aliases: archaeologyImportBridgeFixture.preview.counts.projectThreadAliases,
        tasks: archaeologyImportBridgeFixture.preview.counts.tasks,
        attributions: archaeologyImportBridgeFixture.preview.counts.attributions,
        events: archaeologyImportBridgeFixture.preview.counts.events,
        created: archaeologyImportBridgeFixture.preview.counts.created,
        skipped_current: archaeologyImportBridgeFixture.preview.counts.skippedCurrent,
        replayed: archaeologyImportBridgeFixture.preview.counts.replayed,
      },
    },
  }, (value) => value);
  assert.equal(normalized.proposal.tasks[0].key, archaeologyImportBridgeFixture.proposal.tasks[0].key);
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

test("a fresh manual run clears every persisted project while preserving reviewed settings", () => {
  assert.deepEqual(freshManualArchaeologyConfig({
    selectedProjectIds: ["stale-a", "stale-b", "stale-a"],
    depth: "deep",
    sources: { git: true, docs: false, codexHistory: true },
    maxConcurrency: 1,
  }), {
    selectedProjectIds: [],
    depth: "deep",
    sources: { git: true, docs: false, codexHistory: true },
    maxConcurrency: 2,
  });
});
