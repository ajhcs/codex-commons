export const archaeologyIdentityFixture = Object.freeze({
  principal: "human:fixture",
  displayName: "Taylor Reed",
  handle: "taylor",
});
const PRIMARY_CANDIDATE_ID = "codex-000000000000000000000001";
const SECONDARY_CANDIDATE_ID = "codex-000000000000000000000002";
const SESSION_ID = "AR-000000000000000000000001";
const BATCH_ID = "ARB-000000000000000000000001";
const JOB_ID = "ARJ-000000000000000000000001";


const capabilities = Object.freeze({
  projectCatalog: { configured: true, available: true, mode: "codex_metadata", reason: "" },
  taskLaunch: { configured: true, available: true, mode: "app_server_stdio", reason: "" },
  discovery: { configured: true, available: true, mode: "codex_known_metadata", reason: "" },
  historianHandoff: { configured: true, available: true, mode: "exact_task_claim_report", reason: "" },
  review: { configured: true, available: false, mode: "durable_manifest", reason: "No review-ready native report is available." },
  canonicalApply: { configured: true, available: false, mode: "preview_manifest_confirm", reason: "A signed-in human must review the exact task and evidence diff, then confirm the server-derived manifest digest and source digest." },
});

const candidates = [
  {
    id: PRIMARY_CANDIDATE_ID,
    name: "Codex Commons",
    lastActivity: { iso: "2026-08-12T12:40:00.000Z", relative: "Just now", absolute: "Aug 12, 12:40 PM" },
    signals: { git: true, docs: true, codexHistory: true },
    estimate: { durationSecondsMin: 240, durationSecondsMax: 480, relativeCost: "medium" },
    privacyNote: "Evidence choices govern admissible citations.", repositoryLabel: "openai/codex-commons", sources: ["codex_metadata"], codexThreadCount: 14, selectedByDefault: true,
  },
  {
    id: SECONDARY_CANDIDATE_ID,
    name: "Field Notes",
    lastActivity: { iso: "2026-08-10T18:12:00.000Z", relative: "2d ago", absolute: "Aug 10, 6:12 PM" },
    signals: { git: true, docs: true, codexHistory: false },
    estimate: { durationSecondsMin: 90, durationSecondsMax: 240, relativeCost: "low" },
    privacyNote: "Evidence choices govern admissible citations.", repositoryLabel: "field-notes", sources: ["codex_metadata"], codexThreadCount: 3, selectedByDefault: false,
  },
  ...Array.from({ length: 28 }, (_, index) => ({
    id: `codex-${String(index + 3).padStart(24, "0")}`,
    name: `Codex Project ${index + 3}`,
    repositoryLabel: index % 2 ? `workspace-${index + 3}` : "",
    lastActivity: { iso: new Date(Date.UTC(2026, 7, 9 - (index % 8), 12, 0)).toISOString(), relative: `${index + 1}d ago`, absolute: `Aug ${9 - (index % 8)}` },
    signals: { git: true, docs: index % 3 !== 0, codexHistory: true },
    estimate: { durationSecondsMin: 120, durationSecondsMax: 600, relativeCost: "medium" },
    privacyNote: "Evidence choices govern admissible citations.", sources: ["codex_metadata"], codexThreadCount: (index % 7) + 1, selectedByDefault: false,
  })),
];

export const archaeologyReadyFixture = Object.freeze({
  id: SESSION_ID,
  revision: 3,
  state: "draft",
  discovery: { state: "ready", metadataOnly: true, sourceRootsScanned: 4, candidates },
  config: {
    selectedProjectIds: [PRIMARY_CANDIDATE_ID],
    depth: "standard",
    sources: { git: true, docs: true, codexHistory: true },
    maxConcurrency: 2,
  },
  runs: [],
  review: null,
  capabilities,
  handoff: null,
  controls: { canStart: true, canPause: false, canResume: false, canCancel: false },
});

export const archaeologyHandoffFixture = Object.freeze({
  ...archaeologyReadyFixture,
  revision: 5,
  handoff: {
    id: "", batchId: BATCH_ID, state: "running", depth: "standard", policyAttested: true,
    sources: { git: true, docs: true, codexHistory: true }, concurrency: 2, candidateIds: [PRIMARY_CANDIDATE_ID], allowedActions: [],
    createdAt: { iso: "2026-08-12T12:41:00.000Z", relative: "Just now", absolute: "Aug 12, 12:41 PM" }, updatedAt: { iso: "2026-08-12T12:42:00.000Z", relative: "Just now", absolute: "Aug 12, 12:42 PM" },
    tasks: [{ jobId: JOB_ID, batchId: BATCH_ID, candidateId: PRIMARY_CANDIDATE_ID, projectId: PRIMARY_CANDIDATE_ID, launchId: JOB_ID, mode: "app_server_dynamic_tools", state: "active", phaseLabel: "Examining repository documentation", sourcesExamined: 4, durationMs: null, threadId: "019ff-commons-history", turnId: "turn-1", createdAt: { iso: "2026-08-12T12:41:00.000Z", relative: "Just now", absolute: "Aug 12, 12:41 PM" }, updatedAt: { iso: "2026-08-12T12:42:00.000Z", relative: "Just now", absolute: "Aug 12, 12:42 PM" }, error: "", availableActions: [] }],
    progress: { queuedCount: 0, activeCount: 1, attentionCount: 0, selectedTotal: 1, preparingCount: 0, startingCount: 0, taskCreatedCount: 0, claimedCount: 0, runningCount: 1, reportReadyCount: 0, completedCount: 0, failedCount: 0, uncertainCount: 0, updatedAt: { iso: "2026-08-12T12:42:00.000Z", relative: "Just now", absolute: "Aug 12, 12:42 PM" } },
  },
  controls: { canStart: false, canPause: false, canResume: false, canCancel: true },
});

export const archaeologyAttentionFixture = Object.freeze({
  ...archaeologyHandoffFixture,
  revision: 6,
  handoff: {
    ...archaeologyHandoffFixture.handoff,
    state: "attention",
    allowedActions: ["resolve"],
    tasks: archaeologyHandoffFixture.handoff.tasks.map((task) => ({ ...task, state: "uncertain", error: "Codex may have accepted this task, but Commons cannot safely retry it.", availableActions: ["resolve"] })),
    progress: { ...archaeologyHandoffFixture.handoff.progress, activeCount: 0, attentionCount: 0, runningCount: 0, uncertainCount: 1 },
  },
  controls: { canStart: false, canPause: false, canResume: false, canCancel: false },
});

export const archaeologyCanceledFixture = Object.freeze({
  ...archaeologyHandoffFixture,
  revision: 7,
  state: "draft",
  handoff: {
    ...archaeologyHandoffFixture.handoff,
    state: "canceled",
    tasks: archaeologyHandoffFixture.handoff.tasks.map((task) => ({ ...task, state: "interrupted", durationMs: 93000, error: "This Codex task stopped without a review-ready report." })),
    progress: { ...archaeologyHandoffFixture.handoff.progress, activeCount: 0, runningCount: 0, failedCount: 1 },
  },
  controls: { canStart: true, canPause: false, canResume: false, canCancel: false },
});

export const archaeologyLegacyFixture = Object.freeze({
  ...archaeologyReadyFixture,
  revision: 4,
  handoff: {
    id: "HAND-LEGACY", batchId: "", state: "claimed", depth: "standard",
    sources: { git: true, docs: true, codexHistory: true }, concurrency: 2,
    candidateIds: Array.from({ length: 9 }, (_, index) => `codex-${String(index + 3).padStart(24, "0")}`), allowedActions: [],
    tasks: Array.from({ length: 9 }, (_, index) => ({ projectId: `codex-${String(index + 3).padStart(24, "0")}`, launchId: `LEGACY-${index + 1}`, state: "claimed", threadId: `legacy-thread-${index + 1}`, turnId: `legacy-turn-${index + 1}`, sourcesExamined: 0, durationMs: null, error: "", createdAt: null, updatedAt: null, availableActions: [] })),
    progress: { queuedCount: 0, activeCount: 0, attentionCount: 0, selectedTotal: 9, preparingCount: 0, startingCount: 0, taskCreatedCount: 0, claimedCount: 9, runningCount: 0, reportReadyCount: 0, completedCount: 0, failedCount: 0, uncertainCount: 0, updatedAt: null },
  },
  controls: { canStart: false, canPause: false, canResume: false, canCancel: false },
});

const member = Object.freeze({
  sessionId: "SES-4168",
  displayName: "Integration historian",
  reachability: "historical_or_unknown",
  execution: "not_attested",
  authority: "provenance_only",
  contributionCount: 6,
  sourceCount: 4,
  collaborationCount: 2,
  demonstratedStrengths: ["Provenance design", "Integration work"],
  uncertainties: ["Current reachability was not observed"],
});

export const archaeologyReviewFixture = Object.freeze({
  ...archaeologyHandoffFixture,
  state: "draft",
  handoff: { ...archaeologyHandoffFixture.handoff, state: "completed", tasks: archaeologyHandoffFixture.handoff.tasks.map((task) => ({ ...task, state: "completed", durationMs: 212000 })), progress: { ...archaeologyHandoffFixture.handoff.progress, activeCount: 0, runningCount: 0, completedCount: 1 }, allowedActions: [] },
  capabilities: {
    ...capabilities,
    review: { configured: true, available: true, mode: "durable_manifest", reason: "" },
    canonicalApply: capabilities.canonicalApply,
  },
  review: {
    batchId: BATCH_ID,
    batchRelation: "current",
    requiresExplicitApproval: true,
    canApply: false,
    provenanceSummary: "Exact source digests are retained. Explicit human approval is required before import.",
    proposedOutcomes: [{
      id: "OUT-1",
      title: "Established exact-session provenance",
      summary: "Connected durable contributions to their originating Codex sessions.",
      projectId: PRIMARY_CANDIDATE_ID,
      sourceCount: 4,
      provenance: [
        { sourceKind: "codex_history", sourceLabel: "thread:019ff4168aabbcc", digest: `sha256:${"a".repeat(64)}`, recordedAt: { iso: "2026-08-07T15:20:00.000Z", relative: "5d ago", absolute: "Aug 7, 3:20 PM" } },
        { sourceKind: "git", sourceLabel: `commit:${"b".repeat(40)}`, digest: `sha256:${"b".repeat(64)}`, recordedAt: { iso: "2026-08-07T15:42:00.000Z", relative: "5d ago", absolute: "Aug 7, 3:42 PM" } },
      ],
      memberSessions: [member],
    }],
    memberSessions: [member],
  },
  runs: [{ id: "RUN-1", projectId: PRIMARY_CANDIDATE_ID, state: "completed", phaseLabel: "Historian report received", completedUnits: 1, totalUnits: 1, outcomesFound: 1, sourcesExamined: 4 }],
  controls: { canStart: true, canPause: false, canResume: false, canCancel: false },
});

const storyboardSourceDigest = `sha256:${"c".repeat(64)}`;
const storyboardManifestDigest = `sha256:${"d".repeat(64)}`;
const storyboardOccurredAt = "2026-08-12T12:30:00Z";
const storyboardRawSource = Object.freeze({ kind: "git", stable_id: `commit:${"c".repeat(40)}`, digest: storyboardSourceDigest, occurred_at: storyboardOccurredAt });
const storyboardSource = Object.freeze({ kind: "git", stableId: `commit:${"c".repeat(40)}`, digest: storyboardSourceDigest, occurredAt: { iso: storyboardOccurredAt, relative: "Just now", absolute: "Aug 12, 12:30 PM" } });
const storyboardSession = "019ff-storyboard-historian";

export const archaeologyImportBridgeFixture = Object.freeze({
  projectId: "codex-commons",
  request: {
    schema_version: 1,
    batch_id: "archaeology-storyboard",
    source_digest: storyboardSourceDigest,
    confirm_source_digest: "",
    confirm_manifest_digest: "",
    collision_policy: "current_wins",
    project_thread_aliases: [],
    tasks: [{
      key: "history-task-1",
      priority: 0,
      title: "Preserve exact historian evidence",
      description: "Bind this completed work to the sources reviewed by the human.",
      acceptance: "Canonical history retains exact source identity.",
      state: "done",
      source: storyboardRawSource,
      attributions: [{ session: storyboardSession, role: "implementer", confidence: "verified", source: { ...storyboardRawSource, kind: "codex_history", stable_id: "thread:019ffstoryboard" } }],
      events: [{ key: "history-event-1", kind: "completed", summary: "Historian verified the durable result.", session: storyboardSession, confidence: "verified", source: storyboardRawSource }],
    }],
  },
  proposal: {
    batchId: "archaeology-storyboard",
    sourceDigest: storyboardSourceDigest,
    collisionPolicy: "current_wins",
    projectThreadAliases: [],
    tasks: [{
      key: "history-task-1",
      priority: 0,
      title: "Preserve exact historian evidence",
      description: "Bind this completed work to the sources reviewed by the human.",
      acceptance: "Canonical history retains exact source identity.",
      state: "done",
      source: storyboardSource,
      attributions: [{ session: storyboardSession, role: "implementer", confidence: "verified", source: { ...storyboardSource, kind: "codex_history", stableId: "thread:019ffstoryboard" } }],
      events: [{ key: "history-event-1", kind: "completed", summary: "Historian verified the durable result.", session: storyboardSession, confidence: "verified", source: storyboardSource }],
    }],
    counts: { projectThreadAliases: 0, tasks: 1, attributions: 1, events: 1 },
  },
  preview: {
    batchId: "archaeology-storyboard",
    sourceDigest: storyboardSourceDigest,
    manifestDigest: storyboardManifestDigest,
    collisionPolicy: "current_wins",
    state: "preview",
    applied: false,
    recordedAt: null,
    tasks: [{ key: "history-task-1", id: "TASK-PREVIEW-1", disposition: "created" }],
    counts: { tasks: 1, projectThreadAliases: 0, attributions: 1, events: 1, created: 1, skippedCurrent: 0, replayed: 0 },
  },
});
