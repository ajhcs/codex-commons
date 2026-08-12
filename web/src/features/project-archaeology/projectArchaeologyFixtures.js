export const archaeologyIdentityFixture = Object.freeze({
  principal: "human:fixture",
  displayName: "Taylor Reed",
  handle: "taylor",
});

const capabilities = Object.freeze({
  projectCatalog: { configured: true, available: true, mode: "codex_metadata", reason: "" },
  taskLaunch: { configured: true, available: true, mode: "app_server_stdio", reason: "" },
  discovery: { configured: true, available: true, mode: "allowlisted_metadata", reason: "" },
  historianHandoff: { configured: true, available: true, mode: "exact_task_claim_report", reason: "" },
  review: { configured: true, available: true, mode: "validated_manifest", reason: "" },
  canonicalApply: { configured: true, available: true, mode: "preview_digest_confirm", reason: "" },
});

const candidates = [
  {
    id: "codex-commons",
    name: "Codex Commons",
    lastActivity: { iso: "2026-08-12T12:40:00.000Z", relative: "Just now", absolute: "Aug 12, 12:40 PM" },
    signals: { git: true, docs: true, codexHistory: true },
    estimate: { durationSecondsMin: 240, durationSecondsMax: 480, relativeCost: "medium" },
    privacyNote: "Only selected sources are read after start.", repositoryLabel: "openai/codex-commons", sources: ["codex_metadata"], codexThreadCount: 14, selectedByDefault: true,
  },
  {
    id: "field-notes",
    name: "Field Notes",
    lastActivity: { iso: "2026-08-10T18:12:00.000Z", relative: "2d ago", absolute: "Aug 10, 6:12 PM" },
    signals: { git: true, docs: true, codexHistory: false },
    estimate: { durationSecondsMin: 90, durationSecondsMax: 240, relativeCost: "low" },
    privacyNote: "Only selected sources are read after start.", repositoryLabel: "field-notes", sources: ["codex_metadata"], codexThreadCount: 3, selectedByDefault: false,
  },
  ...Array.from({ length: 28 }, (_, index) => ({
    id: `codex-project-${index + 3}`,
    name: `Codex Project ${index + 3}`,
    repositoryLabel: index % 2 ? `workspace-${index + 3}` : "",
    lastActivity: { iso: new Date(Date.UTC(2026, 7, 9 - (index % 8), 12, 0)).toISOString(), relative: `${index + 1}d ago`, absolute: `Aug ${9 - (index % 8)}` },
    signals: { git: true, docs: index % 3 !== 0, codexHistory: true },
    estimate: { durationSecondsMin: 120, durationSecondsMax: 600, relativeCost: "medium" },
    privacyNote: "Only selected sources are read after start.", sources: ["codex_metadata"], codexThreadCount: (index % 7) + 1, selectedByDefault: false,
  })),
];

export const archaeologyReadyFixture = Object.freeze({
  id: "ARCH-1",
  revision: 3,
  state: "draft",
  discovery: { state: "ready", metadataOnly: true, sourceRootsScanned: 4, candidates },
  config: {
    selectedProjectIds: ["codex-commons"],
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
    id: "HAND-CC-1", state: "running", depth: "standard",
    sources: { git: true, docs: true, codexHistory: true }, concurrency: 2, candidateIds: ["codex-commons"], allowedActions: [],
    tasks: [{ projectId: "codex-commons", state: "running", threadId: "019ff-commons-history", turnId: "turn-1", createdAt: null, updatedAt: null, availableActions: [] }],
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
  state: "completed",
  handoff: { ...archaeologyHandoffFixture.handoff, state: "completed", tasks: archaeologyHandoffFixture.handoff.tasks.map((task) => ({ ...task, state: "completed" })), allowedActions: [] },
  review: {
    requiresExplicitApproval: true,
    canApply: true,
    provenanceSummary: "Exact source digests are retained. Explicit human approval is required before import.",
    proposedOutcomes: [{
      id: "OUT-1",
      title: "Established exact-session provenance",
      summary: "Connected durable contributions to their originating Codex sessions.",
      projectId: "codex-commons",
      sourceCount: 4,
      provenance: [
        { sourceKind: "codex_history", sourceLabel: "Session record", digest: `sha256:${"a".repeat(64)}`, recordedAt: { iso: "2026-08-07T15:20:00.000Z", relative: "5d ago", absolute: "Aug 7, 3:20 PM" } },
        { sourceKind: "git", sourceLabel: "Repository history", digest: `sha256:${"b".repeat(64)}`, recordedAt: { iso: "2026-08-07T15:42:00.000Z", relative: "5d ago", absolute: "Aug 7, 3:42 PM" } },
      ],
      memberSessions: [member],
    }],
    memberSessions: [member],
  },
  runs: [{ id: "RUN-1", projectId: "codex-commons", state: "completed", phaseLabel: "Historian report received", completedUnits: 1, totalUnits: 1, outcomesFound: 1, sourcesExamined: 4 }],
  controls: { canStart: false, canPause: false, canResume: false, canCancel: false },
});
