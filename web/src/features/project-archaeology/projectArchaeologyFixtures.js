export const archaeologyIdentityFixture = Object.freeze({
  principal: "human:fixture",
  displayName: "Taylor Reed",
  handle: "taylor",
});

const capabilities = Object.freeze({
  discovery: { configured: true, available: true, mode: "allowlisted_metadata", reason: "" },
  historianHandoff: { configured: true, available: true, mode: "export_claim_report", reason: "" },
  review: { configured: true, available: true, mode: "validated_manifest", reason: "" },
  canonicalApply: { configured: true, available: true, mode: "preview_digest_confirm", reason: "" },
});

const candidates = [
  {
    id: "codex-commons",
    name: "Codex Commons",
    pathLabel: "~/codex-commons",
    lastActivity: { iso: "2026-08-12T12:40:00.000Z", relative: "Just now", absolute: "Aug 12, 12:40 PM" },
    signals: { git: true, docs: true, codexHistory: true },
    estimate: { durationSecondsMin: 240, durationSecondsMax: 480, relativeCost: "medium" },
    privacyNote: "Only selected sources are read after you prepare the task pack.",
  },
  {
    id: "field-notes",
    name: "Field Notes",
    pathLabel: "~/field-notes",
    lastActivity: { iso: "2026-08-10T18:12:00.000Z", relative: "2d ago", absolute: "Aug 10, 6:12 PM" },
    signals: { git: true, docs: true, codexHistory: false },
    estimate: { durationSecondsMin: 90, durationSecondsMax: 240, relativeCost: "low" },
    privacyNote: "Only selected sources are read after you prepare the task pack.",
  },
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
    id: "HAND-CC-1",
    state: "ready_to_claim",
    claimedBy: "",
    failure: "",
    depth: "standard",
    sources: { git: true, docs: true, codexHistory: true },
    concurrency: 2,
    candidateIds: ["codex-commons"],
    allowedActions: ["claim"],
    pack: {
      title: "Codex Commons project archaeology",
      instructions: "Claim this handoff once, review only the selected sources, and report one bounded historical-import proposal.",
      projects: [{ candidateId: "codex-commons", label: "Codex Commons", taskPrompt: "Review the selected Git, documentation, and Codex-history evidence. Return source digests, exact contributor session IDs, uncertainties, and one current-wins historical import proposal." }],
    },
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
  handoff: { ...archaeologyHandoffFixture.handoff, state: "completed", claimedBy: "SES-4168", allowedActions: [] },
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
