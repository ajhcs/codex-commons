import React from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import {
  AUTHENTICATED_SESSION_DTO,
  NATIVE_BATCH_ID,
  STALE_NATIVE_BATCH_ID,
  nativeJobID,
  apiError,
  apiResponse,
  archaeologyDTO,
  completedArchaeologyDTO,
  deferred,
  PRIMARY_CANDIDATE_ID,
  reconciledArchaeologyDTO,
  startedArchaeologyDTO,
  terminalAttentionArchaeologyDTO,
  uncertainArchaeologyDTO,
} from "../tests/project-archaeology-production-fixture.mjs";
import { codexAuthFixtures } from "../src/data/fixtures.js";
import { archaeologyImportBridgeFixture } from "../src/features/project-archaeology/projectArchaeologyFixtures.js";

const discovery = deferred();
const calls = [];
const scenario = new URLSearchParams(globalThis.location.search).get("scenario") || "success";
const conflictScenario = scenario === "conflict";
const malformedStartScenario = scenario === "malformed-start";
const staleStartScenario = scenario === "stale-start";
const expiredPollScenario = scenario === "expired-poll";
const firstRunScenario = scenario === "nonrail-onboarding";
const recoveredStartScenario = scenario === "recovered-start";
const resolveSuccessScenario = scenario === "resolve-success";
const resolveConflictScenario = scenario === "resolve-conflict";
const identitylessUncertainScenario = scenario === "identityless-uncertain";
const terminalAttentionScenario = scenario === "terminal-attention";
const reviewDetailsScenario = scenario === "review-details";
const historyStatusScenario = scenario === "history-status" || scenario === "history-status-unknown";
const historyStatusUnknownScenario = scenario === "history-status-unknown";
const largeBatchScenario = scenario === "large-batch";
const selectedApplyScenario = scenario === "selected-apply";
const selectedPreviewScenario = scenario === "selected-preview";
const sameSourceChoiceScenario = scenario === "same-source-choice";
const selectedPageConflictScenario = scenario === "selected-page-conflict";
const selectedBypassScenario = scenario === "selected-bypass";
const selectedLostPagesScenario = scenario === "selected-lost-pages";
const catalogPaginationScenario = scenario === "catalog-pagination";
const keyboardCatalogScenario = scenario === "keyboard-catalog";
const visibilityStagesScenario = scenario === "visibility-stages";
const staleSelectionScenario = scenario === "stale-selection";
const selectedOutcomeCount = selectedLostPagesScenario ? 11 : sameSourceChoiceScenario ? 2 : 6;
const directLifecycleScenario = resolveSuccessScenario || resolveConflictScenario || identitylessUncertainScenario || terminalAttentionScenario || reviewDetailsScenario;
const unsafeStartScenario = malformedStartScenario || staleStartScenario;
const catalogCandidates = archaeologyDTO().discovery.candidates;
const paginatedCandidates = Array.from({ length: 101 }, (_, index) => {
  const seed = catalogCandidates[index % catalogCandidates.length];
  if (index === 0) return seed;
  const number = String(index + 1).padStart(3, "0");
  return { ...seed, id: `codex-page-${number}`, name: `Codex Project ${number}`, path_label: `Codex Project ${number}`, repository_label: `codex-project-${number}`, codex_thread_count: 101 - index };
});
const freshEmptyArchaeology = {
  ...archaeologyDTO({ discoveryState: "idle" }),
  discovery: { ...archaeologyDTO({ discoveryState: "idle" }).discovery, candidates: [], tasks_examined: 0, projects_grouped: 0, completed_at: null, app_server_identity: "Codex App Server · 0.147.0" },
};
const paginatedArchaeology = {
  ...archaeologyDTO(),
  discovery: { ...archaeologyDTO().discovery, candidates: paginatedCandidates.slice(0, 100), tasks_examined: 4120, projects_grouped: 101 },
};
let paginatedDiscoveryCalls = 0;
const selectedCandidateIDs = largeBatchScenario ? catalogCandidates.slice(0, 6).map((item) => item.id) : [PRIMARY_CANDIDATE_ID];
const staleSelectedCandidateIDs = catalogCandidates.slice(0, 9).map((item) => item.id);
const catalogDTO = () => {
  return { items: catalogCandidates, next_cursor: "", total: catalogCandidates.length };
};
const completed = completedArchaeologyDTO();
const historyOutcomes = Array.from({ length: 12 }, (_, index) => {
  const base = completed.review.proposed_outcomes[0];
  const number = index + 1;
  return {
    ...base,
    id: `OUT-HISTORY-${String(number).padStart(2, "0")}`,
    title: number === 1 ? base.title : `Retained proposal ${String(number).padStart(2, "0")}`,
    source_count: 1000,
    provenance: [
      ...base.provenance,
      { source_kind: "docs", source_label: `doc:history-${number}`, digest: `sha256:${"c".repeat(64)}`, recorded_at: "2026-08-13T01:10:00Z" },
      { source_kind: "git", source_label: `commit:${"d".repeat(40)}`, digest: `sha256:${"d".repeat(64)}`, recorded_at: "2026-08-13T01:11:00Z" },
    ],
  };
});
const selectedApplyOutcomes = Array.from({ length: selectedOutcomeCount }, (_, index) => ({
  ...completed.review.proposed_outcomes[0],
  id: `OUT-${index + 1}`,
  title: index === 0 ? completed.review.proposed_outcomes[0].title : `Recovered project ${index + 1} decision`,
  project_id: catalogCandidates[index === 1 ? 0 : index].id,
  source_digest: `sha256:${(sameSourceChoiceScenario ? "a" : "0123456789abcdef"[index]).repeat(64)}`,
}));
const applyEnabled = {
  ...completed,
  review: { ...completed.review, proposed_outcomes: selectedApplyOutcomes, can_apply: true },
  capabilities: {
    ...completed.capabilities,
    canonical_apply: { ...completed.capabilities.canonical_apply, available: true, reason: "" },
  },
};
const refreshedManifestDigest = `sha256:${"e".repeat(64)}`;
const selectedReviewSessionToken = "s".repeat(43);
const refreshedReviewSessionToken = "r".repeat(43);
const selectedReviewCompletionToken = "c".repeat(43);
const refreshedReviewCompletionToken = "f".repeat(43);
let selectedPreviewCalls = 0;
let selectedPreviewPageCalls = 0;
let selectedApplyCalls = 0;
const lostPageAttempts = new Map();
function selectedImportResponse({ refreshed = false, page = 0 } = {}) {
  const manifestDigest = refreshed ? refreshedManifestDigest : archaeologyImportBridgeFixture.preview.manifestDigest;
  const disposition = refreshed ? "skipped_current" : "created";
  const receipt = (batchID, taskKey, taskID) => ({
    batch_id: batchID,
    source_digest: archaeologyImportBridgeFixture.preview.sourceDigest,
    manifest_digest: manifestDigest,
    collision_policy: "current_wins",
    state: "preview",
    applied: false,
    tasks: [{ key: taskKey, id: taskID, disposition }],
    counts: {
      project_thread_aliases: 0, tasks: 1, attributions: 1, events: 1,
      created: disposition === "created" ? 1 : 0,
      skipped_current: disposition === "skipped_current" ? 1 : 0,
      replayed: 0,
    },
  });
  const allProjects = Array.from({ length: selectedOutcomeCount }, (_, index) => {
    const number = index + 1;
    const taskKey = index === 1 ? "history-task-1" : `history-task-${number}`;
    const request = index === 0 ? archaeologyImportBridgeFixture.request : {
      ...archaeologyImportBridgeFixture.request,
      batch_id: `archaeology-storyboard-${number}`,
      tasks: archaeologyImportBridgeFixture.request.tasks.map((task) => ({
        ...task,
        key: taskKey,
        title: `Preserve project ${number} exact historian evidence`,
        events: task.events.map((event) => ({ ...event, key: `history-event-${number}` })),
      })),
    };
    return { outcome_id: `OUT-${number}`, project_id: catalogCandidates[index === 1 ? 0 : index].id, request, preview: receipt(request.batch_id, taskKey, `TASK-PREVIEW-${number}`) };
  }).sort((left, right) => left.outcome_id.localeCompare(right.outcome_id));
  const projects = allProjects.slice(page, page + 5);
  return {
    batch_id: NATIVE_BATCH_ID,
    outcome_ids: allProjects.map((project) => project.outcome_id),
    selection_digest: `sha256:${"f".repeat(64)}`,
    manifest_digest: manifestDigest,
    projects,
    review_session_token: refreshed ? refreshedReviewSessionToken : selectedReviewSessionToken,
    review_expires_at: "2026-08-13T23:50:00Z",
    ...(page + 5 < allProjects.length ? { next_cursor: String(page + 5) } : { review_completion_token: refreshed ? refreshedReviewCompletionToken : selectedReviewCompletionToken }),
  };
}
const historySummary = {
  batch_id: NATIVE_BATCH_ID, state: "completed", mode: "app_server_dynamic_tools", depth: completed.handoff.depth,
  sources: completed.handoff.sources, concurrency: completed.handoff.concurrency, selected_total: 1,
  queued_count: 0, active_count: 0, completed_count: 1, attention_count: 0, has_report: true,
  created_at: completed.handoff.created_at, updated_at: completed.handoff.updated_at,
};
let canonicalArchaeology = catalogPaginationScenario
  ? freshEmptyArchaeology
  : selectedApplyScenario || selectedPreviewScenario || selectedPageConflictScenario || selectedBypassScenario || selectedLostPagesScenario || sameSourceChoiceScenario
  ? applyEnabled
  : terminalAttentionScenario
  ? terminalAttentionArchaeologyDTO()
  : reviewDetailsScenario
  ? completedArchaeologyDTO()
  : identitylessUncertainScenario
    ? uncertainArchaeologyDTO({ withIdentity: false })
    : resolveSuccessScenario || resolveConflictScenario
      ? uncertainArchaeologyDTO()
      : staleSelectionScenario
        ? archaeologyDTO({ selectedProjectIds: staleSelectedCandidateIDs, canStart: true })
      : archaeologyDTO();
let visibilityPollPending = false;
let qaSession = firstRunScenario ? { authenticated: false, principal: null } : AUTHENTICATED_SESSION_DTO;
let fallbackCopyCalls = 0;
const nativeExecCommand = globalThis.document.execCommand?.bind(globalThis.document);
let autoOpenCalls = 0;
let autoOpenDestination = "";
if (firstRunScenario) {
  Object.defineProperty(globalThis, "isSecureContext", { configurable: true, value: false });
  Object.defineProperty(globalThis.navigator, "clipboard", { configurable: true, value: undefined });
  Object.defineProperty(globalThis.document, "execCommand", {
    configurable: true,
    value(command, ...args) {
      if (command === "copy") { fallbackCopyCalls += 1; return true; }
      return nativeExecCommand?.(command, ...args) === true;
    },
  });
  globalThis.open = () => { autoOpenCalls += 1; return { closed: false, opener: null, document: { title: "" }, location: { replace(value) { autoOpenDestination = String(value); } }, close() { this.closed = true; } }; };

}
function nativeHandoffDTO(candidateIDs, batchID) {
  const base = startedArchaeologyDTO();
  const tasks = candidateIDs.map((candidateID, index) => ({
    ...base.handoff.tasks[0],
    job_id: nativeJobID(index),
    launch_id: nativeJobID(index),
    batch_id: batchID,
    candidate_id: candidateID,
    project_id: candidateID,
  }));
  return archaeologyDTO({
    revision: 14,
    selectedProjectIds: candidateIDs,
    canStart: false,
    state: "running",
    updatedAt: "2026-08-13T01:00:00Z",
    handoff: { ...base.handoff, batch_id: batchID, candidate_ids: candidateIDs, tasks, progress: { ...base.handoff.progress, queued_count: candidateIDs.length, selected_total: candidateIDs.length } },
  });
}

function visibilityStageDTO(state) {
  const base = startedArchaeologyDTO();
  const updatedAt = state === "active" ? "2026-08-13T01:00:02Z" : "2026-08-13T01:00:01Z";
  return archaeologyDTO({
    revision: state === "active" ? 15 : 14,
    selectedProjectIds: [PRIMARY_CANDIDATE_ID],
    canStart: false,
    state: "running",
    updatedAt,
    handoff: {
      ...base.handoff,
      state: "running",
      updated_at: updatedAt,
      tasks: [{ ...base.handoff.tasks[0], state, thread_id: "019f0000-0000-7000-8000-000000000001", phase_label: state === "active" ? "Reading project documentation" : "Confirming final task name", updated_at: updatedAt }],
      progress: { ...base.handoff.progress, queued_count: 0, active_count: 1, starting_count: state === "starting" ? 1 : 0, running_count: state === "active" ? 1 : 0, updated_at: updatedAt },
    },
  });
}

globalThis.fetch = async (url, options = {}) => {
  const path = new URL(String(url), globalThis.location.href).pathname;
  calls.push({
    url: String(url),
    path,
    method: options.method || "GET",
    body: options.body || "",
    csrf: options.headers?.["X-Commons-CSRF"] || "",
    idempotencyKey: options.headers?.["Idempotency-Key"] || "",
  });
  if (path === "/v1/auth/session") return apiResponse(qaSession, "qa-session");
  if (path === "/v1/auth/codex/status") return apiResponse(codexAuthFixtures.status, "qa-codex-status");
  if (path === "/v1/auth/codex/start") return apiResponse(codexAuthFixtures.start, "qa-codex-start");
  if (path === "/v1/auth/codex/poll") return apiResponse(codexAuthFixtures.poll_needs_profile, "qa-codex-poll");
  if (path === "/v1/auth/codex/profile") { qaSession = AUTHENTICATED_SESSION_DTO; return apiResponse(qaSession, "qa-codex-profile"); }
  if (path === "/v1/auth/codex/cancel") return apiResponse({ authenticated: false, principal: null }, "qa-codex-cancel");
  if (path === "/v1/notifications") return apiResponse({ items: [], next_cursor: "", unread_count: 0 }, "qa-notifications");
  if (path === "/v1/project-archaeology/catalog") {
    if (catalogPaginationScenario) {
      const cursor = new URL(String(url), globalThis.location.href).searchParams.get("cursor") || "";
      return apiResponse(paginatedDiscoveryCalls >= 3
        ? { items: paginatedCandidates.slice(0, 100), next_cursor: "", total: 100 }
        : cursor === "page-2"
        ? { items: paginatedCandidates.slice(100), next_cursor: "", total: 101 }
        : { items: paginatedCandidates.slice(0, 100), next_cursor: "page-2", total: 101 }, "qa-archaeology-catalog-page");
    }
    return apiResponse(catalogDTO(), "qa-archaeology-catalog");
  }
  if (path === "/v1/project-archaeology/batches" && (options.method || "GET") === "GET") return apiResponse({ items: [historySummary], next_cursor: "" }, "qa-archaeology-history");
  if (path === `/v1/project-archaeology/batches/${NATIVE_BATCH_ID}` && (options.method || "GET") === "GET") return apiResponse({
    ...historySummary,
    tasks: completed.handoff.tasks.map((task) => ({ ...task, project_id: "codex-off-page-101", project_name: "Archived Project 101" })),
    review: { ...completed.review, proposed_outcomes: historyOutcomes.slice(0, 5) },
    outcomes_next_cursor: "5",
  }, "qa-archaeology-batch");
  if (path === `/v1/project-archaeology/batches/${NATIVE_BATCH_ID}/outcomes` && (options.method || "GET") === "GET") {
    const cursor = new URL(String(url), globalThis.location.href).searchParams.get("cursor");
    if (cursor === "5") return apiResponse({ items: historyOutcomes.slice(5, 10), next_cursor: "10" }, "qa-archaeology-outcomes-2");
    if (cursor === "10") return apiResponse({ items: historyOutcomes.slice(10), next_cursor: "" }, "qa-archaeology-outcomes-3");
  }
  if (path === "/v1/installation-status") return apiResponse({
    service: { version: "dogfood.15" }, database: { schema_version: 15 },
    codex: { configured: true, available: true, version: "0.147.0", account_state: "signed_in", compatibility_status: "compatible", compatibility_checked_at: "2026-08-13T01:11:00Z", session_revocation_pending: historyStatusUnknownScenario },
    archaeology: { catalog_completed_at: "2026-08-13T00:40:28Z", active_count: 0, uncertain_count: 0 },
    backup: { last_verified_at: "2026-08-13T00:10:00Z", status: "verified" },
    reconciliation: { last_at: "2026-08-13T01:12:00Z", status: "healthy" },
    evidence: historyStatusUnknownScenario
      ? { completed_historians: 12, failed_historians: 0, uncertain_historians: 0, distinct_projects: 7, reports_received: 12, lost_reports: 0, reviewed_imports: 3, cancellations: 2, report_recovery: { status: "unknown", violations: 0 }, duplicate_launch_check: { status: "unknown", violations: 0 }, repository_immutability: { status: "unknown", violations: 0 }, canonical_immutability: { status: "unknown", violations: 0 }, restore_drill: { status: "unknown" }, beta_prerequisites_met: false }
      : { completed_historians: 12, failed_historians: 0, uncertain_historians: 0, distinct_projects: 7, reports_received: 12, lost_reports: 0, reviewed_imports: 3, cancellations: 2, report_recovery: { status: "verified", violations: 0, checked_at: "2026-08-13T00:02:00Z" }, duplicate_launch_check: { status: "verified", violations: 0, checked_at: "2026-08-13T00:03:00Z" }, repository_immutability: { status: "verified", violations: 0, checked_at: "2026-08-13T00:04:00Z" }, canonical_immutability: { status: "verified", violations: 0, checked_at: "2026-08-13T00:04:30Z" }, restore_drill: { status: "verified", last_verified_at: "2026-08-13T00:05:00Z" }, beta_prerequisites_met: true },
  }, "qa-installation-status");
  if (path === `/v1/project-archaeology/batches/${NATIVE_BATCH_ID}/import-preview` && options.method === "POST") {
    selectedPreviewCalls += 1;
    if (selectedLostPagesScenario && selectedPreviewCalls === 1) throw new TypeError("simulated accepted page-zero response loss");
    return apiResponse(selectedImportResponse({ refreshed: selectedApplyScenario && selectedPreviewCalls > 1 }), "qa-selected-preview");
  }
  if (path === `/v1/project-archaeology/batches/${NATIVE_BATCH_ID}/import-preview-page` && options.method === "POST") {
    selectedPreviewPageCalls += 1;
    if (selectedPageConflictScenario && selectedPreviewPageCalls === 1) return apiError(409, "review_conflict", "qa-selected-page-expired");
    const body = JSON.parse(options.body);
    const cursor = new URL(String(url), globalThis.location.href).searchParams.get("cursor");
    if (selectedLostPagesScenario) {
      const attempts = (lostPageAttempts.get(cursor) || 0) + 1;
      lostPageAttempts.set(cursor, attempts);
      if (attempts === 1) throw new TypeError(`simulated accepted page ${cursor} response loss`);
    }
    const expectedSessionToken = selectedApplyScenario && selectedPreviewCalls > 1 ? refreshedReviewSessionToken : selectedReviewSessionToken;
    if (!["5", "10"].includes(cursor) || body.review_session_token !== expectedSessionToken) return apiError(409, "review_conflict", "qa-selected-page-conflict");
    return apiResponse(selectedImportResponse({ refreshed: selectedApplyScenario && selectedPreviewCalls > 1, page: Number(cursor) }), "qa-selected-preview-page");
  }
  if (path === `/v1/project-archaeology/batches/${NATIVE_BATCH_ID}/import-apply` && options.method === "POST") {
    selectedApplyCalls += 1;
    if (selectedApplyCalls === 1) return apiError(409, "revision_conflict", "qa-selected-conflict");
    const receipt = selectedImportResponse({ refreshed: true, page: selectedOutcomeCount - 1 });
    if (JSON.parse(options.body).review_completion_token !== refreshedReviewCompletionToken) return apiError(409, "review_conflict", "qa-selected-token-conflict");
    return apiResponse({
      batch_id: receipt.batch_id,
      outcome_ids: receipt.outcome_ids,
      selection_digest: receipt.selection_digest,
      manifest_digest: receipt.manifest_digest,
      applied: true,
      audit_id: "AUDIT-SELECTED-1",
    }, "qa-selected-applied");
  }
  if (path === "/v1/project-archaeology" && (options.method || "GET") === "GET") {
    if (expiredPollScenario && canonicalArchaeology.handoff?.batch_id) {
      return apiError(401, "unauthorized", "qa-lifecycle-expired");
    }
    if (visibilityPollPending) {
      visibilityPollPending = false;
      canonicalArchaeology = visibilityStageDTO("active");
    }
    return apiResponse(canonicalArchaeology, "qa-archaeology-read");
  }
  if (path === "/v1/project-archaeology/discover") {
    if (catalogPaginationScenario) {
      paginatedDiscoveryCalls += 1;
      canonicalArchaeology = paginatedDiscoveryCalls >= 3 ? { ...paginatedArchaeology, revision: paginatedArchaeology.revision + paginatedDiscoveryCalls, discovery: { ...paginatedArchaeology.discovery, candidates: paginatedCandidates.slice(0, 100), projects_grouped: 100 } } : { ...paginatedArchaeology, revision: paginatedArchaeology.revision + paginatedDiscoveryCalls };
      return apiResponse(canonicalArchaeology, "qa-paginated-discover");
    }
    return firstRunScenario ? apiResponse(canonicalArchaeology, "qa-first-run-discover") : discovery.promise;
  }
  if (path === "/v1/project-archaeology/config" && options.method === "PUT") {
    if (conflictScenario) {
      canonicalArchaeology = archaeologyDTO({ revision: 13 });
      return apiError(409, "revision_conflict", "qa-config-conflict");
    }
    canonicalArchaeology = archaeologyDTO({
      revision: 13,
      selectedProjectIds: JSON.parse(options.body).selected_project_ids,
      canStart: true,
    });
    return apiResponse(canonicalArchaeology, "qa-configured");
  }
  if (path === "/v1/project-archaeology/start" && options.method === "POST") {
    if (malformedStartScenario) canonicalArchaeology = nativeHandoffDTO(selectedCandidateIDs, "");
    else if (staleStartScenario) canonicalArchaeology = { ...nativeHandoffDTO(selectedCandidateIDs, STALE_NATIVE_BATCH_ID), revision: 13 };
    else if (visibilityStagesScenario) { canonicalArchaeology = visibilityStageDTO("starting"); visibilityPollPending = true; }
    else canonicalArchaeology = largeBatchScenario ? nativeHandoffDTO(selectedCandidateIDs, NATIVE_BATCH_ID) : startedArchaeologyDTO();
    if (recoveredStartScenario) return apiError(503, "temporarily_unavailable", "qa-start-transient");
    return apiResponse(canonicalArchaeology, "qa-started");
  }
  if (path === "/v1/project-archaeology/resolve" && options.method === "POST") {
    if (resolveConflictScenario) {
      canonicalArchaeology = uncertainArchaeologyDTO({ revision: 22 });
      return apiError(409, "revision_conflict", "qa-resolve-conflict");
    }
    if (resolveSuccessScenario) {
      canonicalArchaeology = reconciledArchaeologyDTO();
      return apiResponse(canonicalArchaeology, "qa-resolved");
    }
  }
  throw new Error(`Unexpected release-gate request: ${options.method || "GET"} ${path}`);
};

const [
  { AppShell },
  { LoginDialog },
  { AuthSessionProvider },
  { useAuthSession },
  { NotificationProvider },
  { PreferencesProvider },
] = await Promise.all([
  import("../src/components/AppShell.jsx"),
  import("../src/components/AuthControls.jsx"),
  import("../src/hooks/AuthSessionContext.jsx"),
  import("../src/hooks/useAuthSession.js"),
  import("../src/hooks/NotificationContext.jsx"),
  import("../src/hooks/usePreferences.jsx"),
  import("../src/styles.css"),
]);

function FirstRunEntry() {
  const auth = useAuthSession();
  const [loginOpen, setLoginOpen] = React.useState(false);
  const [resumed, setResumed] = React.useState(false);

  function authenticated(session) {
    auth.accept(session);
    setLoginOpen(false);
    setResumed(true);
  }

  return (
    <AppShell route="posts" onNavigate={() => {}}>
      <main>
        <h1>Screen-local first run</h1>
        <button id="screen-local-sign-in" type="button" onClick={() => setLoginOpen(true)}>Write from this screen</button>
        <span id="screen-local-resume" data-resumed={String(resumed)} />
        <LoginDialog open={loginOpen} onClose={() => setLoginOpen(false)} onAuthenticated={authenticated} />
      </main>
    </AppShell>
  );
}

createRoot(document.getElementById("root")).render(
  <React.StrictMode>
    <PreferencesProvider>
      <AuthSessionProvider>
        <NotificationProvider>
          {firstRunScenario ? <FirstRunEntry /> : (
            <AppShell route="posts" onNavigate={() => {}}>
              <main><h1>Production composition gate</h1></main>
            </AppShell>
          )}
        </NotificationProvider>
      </AuthSessionProvider>
    </PreferencesProvider>
  </React.StrictMode>,
);

const status = document.getElementById("gate-status");
const evidence = [];

function waitFor(find, label, timeout = 4000) {
  const started = performance.now();
  return new Promise((resolve, reject) => {
    const inspect = () => {
      const value = find();
      if (value) return resolve(value);
      if (performance.now() - started > timeout) return reject(new Error(`Timed out waiting for ${label}`));
      requestAnimationFrame(inspect);
    };
    inspect();
  });
}

function buttonNamed(name) {
  return [...document.querySelectorAll("button")].find((button) => button.textContent.trim().includes(name));
}

function candidateCheckbox(name) {
  return [...document.querySelectorAll(".archaeology-candidate")]
    .find((label) => label.textContent.includes(name))?.querySelector('input[type="checkbox"]');
}

function typeInto(input, value) {
  input.focus();
  input.select();
  globalThis.document.execCommand("insertText", false, value);
}

function changeControlledInput(input, value) {
  const setter = Object.getOwnPropertyDescriptor(globalThis.HTMLInputElement.prototype, "value")?.set;
  if (!setter) throw new Error("native input setter unavailable");
  setter.call(input, value);
  input.dispatchEvent(new globalThis.InputEvent("input", { bubbles: true, inputType: "insertText", data: value }));
}

async function runFirstRun() {
  await waitFor(() => document.querySelector(".session-sign-in"), "unauthenticated app shell");
  (await waitFor(() => document.querySelector("#screen-local-sign-in"), "screen-local sign-in trigger")).click();
  (await waitFor(() => buttonNamed("Continue with Codex"), "Codex sign-in action")).click();
  await waitFor(() => document.querySelector(".pairing-code-value"), "one-time code");
  if (globalThis.isSecureContext !== false) throw new Error("LAN scenario did not run in an insecure context");
  (await waitFor(() => buttonNamed("Copy code"), "Copy code action")).click();
  await waitFor(() => document.querySelector(".pairing-code-card small")?.textContent.includes("Code copied"), "LAN fallback copy success");
  if (fallbackCopyCalls !== 1) throw new Error("Copy code did not call the LAN-safe fallback exactly once");
  (await waitFor(() => buttonNamed("I’ve finished"), "manual pairing check")).click();
  const displayName = await waitFor(() => document.querySelector('input[name="display-name"]'), "profile display name", 7000);
  flushSync(() => typeInto(displayName, "Commons Operator"));
  await new Promise((resolve) => setTimeout(resolve, 50));
  const handle = document.querySelector('input[name="handle"]');
  flushSync(() => typeInto(handle, "commons-operator"));
  if (autoOpenCalls !== 1) throw new Error("Codex sign-in did not auto-open exactly once");
  if (autoOpenDestination !== codexAuthFixtures.start.verification_url) throw new Error("Codex sign-in auto-opened the wrong authorization destination");
  await new Promise((resolve) => setTimeout(resolve, 50));
  const committedDisplayName = document.querySelector('input[name="display-name"]');
  flushSync(() => typeInto(committedDisplayName, "Commons Operator"));
  await new Promise((resolve) => setTimeout(resolve, 50));
  (await waitFor(() => buttonNamed("Save profile") && !buttonNamed("Save profile").disabled ? buttonNamed("Save profile") : null, "valid profile submission")).click();
  await waitFor(() => document.querySelector("#auth-history-offer-title")?.textContent.includes("Bring your Codex work"), "first profile history offer", 7000);
  (await waitFor(() => buttonNamed("Choose projects"), "Choose projects action")).click();
  await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Choose your Codex projects"), "central project history dialog", 7000);
  await waitFor(() => candidateCheckbox("Codex Commons"), "project catalog after first profile");
  const resume = document.querySelector("#screen-local-resume");
  if (resume?.dataset.resumed !== "false") throw new Error("screen-local write resumed instead of honoring the onboarding choice");
  const pairingStarts = calls.filter((call) => call.path === "/v1/auth/codex/start");
  const profileWrites = calls.filter((call) => call.path === "/v1/auth/codex/profile");
  if (pairingStarts.length !== 1 || profileWrites.length !== 1) throw new Error("first-run auth did not issue exactly one pairing and profile write");
  evidence.push({ stage: "nonrail_first_run", lanFallbackCalls: fallbackCopyCalls, autoOpenCalls, autoOpenDestination, sharedProjectHistory: true, priorWriteResumed: false });
  status.dataset.lanCopy = "true";
  status.dataset.centralOnboarding = "true";
  status.dataset.resumeSuppressed = "true";
  status.dataset.result = "pass";
  status.textContent = "PASS: LAN copy and shared first-run onboarding";
  globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence, calls };
}
  status.dataset.autoOpen = "true";

async function runDirectLifecycle() {
  if (resolveSuccessScenario || resolveConflictScenario || identitylessUncertainScenario) {
    await waitFor(() => document.body.textContent.includes("Human review is needed before another queue can start.") && document.body.textContent.includes("uncertain work automatically"), "genuine uncertainty copy");
    status.dataset.uncertaintyCopy = "true";
  }
  if (terminalAttentionScenario) {
    await waitFor(() => document.body.textContent.includes("6 tasks ended without a review-ready report.") && document.body.textContent.includes("Audit evidence is preserved") && document.body.textContent.includes("fresh manual run"), "terminal attention audit copy");
    if (document.body.textContent.includes("uncertain work") || buttonNamed("Check Codex task") || buttonNamed("Confirm stopped")) throw new Error("terminal attention was rendered as unresolved uncertainty");
    const chooseMore = await waitFor(() => buttonNamed("Choose more projects"), "fresh manual run action");
    if (calls.some((call) => ["/v1/project-archaeology/config", "/v1/project-archaeology/start", "/v1/project-archaeology/resolve"].includes(call.path))) throw new Error("terminal attention mutated before the local reset");
    chooseMore.click();
    await waitFor(() => document.body.textContent.includes("0 of 30 selected") && catalogCandidates.slice(0, 6).every((candidate) => candidateCheckbox(candidate.name)?.checked === false), "fresh terminal-attention selection reset");
    const writes = calls.filter((call) => ["/v1/project-archaeology/config", "/v1/project-archaeology/start", "/v1/project-archaeology/resolve"].includes(call.path));
    if (writes.length) throw new Error("fresh terminal-attention reset performed a write or launch");
    evidence.push({ stage: "terminal_attention_fresh_run", attention: 6, selected: 0, resolveControls: 0, writes: 0 });
    status.dataset.terminalAttention = "6";
    status.dataset.terminalCopy = "true";
    status.dataset.freshSelected = "0";
    status.dataset.resolveCalls = "0";
  } else if (reviewDetailsScenario) {
    await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Project history run complete"), "completed native run");
    if (!document.body.textContent.includes(NATIVE_BATCH_ID) || !document.body.textContent.includes("Report from this run")) throw new Error("completed report was not bound to the current native batch");
    (await waitFor(() => buttonNamed("Review report details"), "review details action")).click();
    await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Review what Commons found"), "review details");
    if (!document.body.textContent.includes(`batch ${NATIVE_BATCH_ID}`)) throw new Error("review details lost the native batch identity");
    const provenance = await waitFor(() => document.querySelector(".archaeology-provenance > summary"), "retained provenance");
    provenance.click();
    const members = await waitFor(() => document.querySelector(".archaeology-members details > summary"), "member facts");
    members.click();
    await waitFor(() => document.body.textContent.includes("4 sources were examined") && document.body.textContent.includes("2 exact citations") && document.body.textContent.includes("Recorded collaboration links") && !document.body.textContent.includes("Cited sources"), "truthful review facts");
    const actionLabels = [...document.querySelectorAll(".archaeology-review button")].map((button) => button.textContent.trim());
    if (actionLabels.includes("Apply reviewed history") || actionLabels.includes("Review exact proposal")) throw new Error("view-only native review exposed canonical apply");
    const back = await waitFor(() => buttonNamed("Back to run"), "Back to run action");
    evidence.push({ stage: "batch_bound_review", batchID: NATIVE_BATCH_ID, retainedCitations: 2, canApply: false, backToRun: true });
    back.click();
    await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Project history run complete"), "return to completed run");
    status.dataset.reviewDetails = "true";
    status.dataset.batchBound = "true";
    status.dataset.canApply = "false";
  } else if (identitylessUncertainScenario) {
    await waitFor(() => document.body.textContent.includes("Task identity is not yet reconcilable"), "identity-less uncertain state");
    if (buttonNamed("Check Codex task") || buttonNamed("Confirm stopped")) throw new Error("identity-less task exposed a human resolution control");
    await new Promise((resolve) => setTimeout(resolve, 150));
    if (calls.some((call) => call.path === "/v1/project-archaeology/resolve")) throw new Error("identity-less task issued a resolution request");
    evidence.push({ stage: "identityless_uncertain", resolveControls: 0, resolveCalls: 0 });
    status.dataset.identitylessSafe = "true";
    status.dataset.resolveCalls = "0";
  } else {
    const check = await waitFor(() => buttonNamed("Check Codex task"), "exact task check");
    check.click();
    await waitFor(() => document.querySelector("#archaeology-resolution-title")?.textContent.includes("Confirm this exact Codex task is stopped"), "resolution confirmation");
    const checkbox = document.querySelector(".archaeology-resolution-panel input[type=checkbox]");
    if (!checkbox || !document.body.textContent.includes("019ff-project-history") || !document.body.textContent.includes("turn-project-history")) throw new Error("resolution did not expose exact task identity");
    checkbox.click();
    (await waitFor(() => buttonNamed("Confirm stopped") && !buttonNamed("Confirm stopped").disabled ? buttonNamed("Confirm stopped") : null, "enabled resolution action")).click();
    if (resolveConflictScenario) {
      await waitFor(
        () => [...document.querySelectorAll('[role="alert"]')].find((alert) => alert.textContent.includes("conflicts with newer Commons activity")),
        "resolution conflict",
      );
      await waitFor(() => calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length >= 2, "resolution conflict refetch");
      if (!buttonNamed("Confirm stopped")) throw new Error("resolution conflict discarded the exact task review");
      status.dataset.resolveConflict = "true";
    } else {
      await waitFor(() => document.body.textContent.includes("The exact Codex task was confirmed stopped by a signed-in human."), "resolved task state");
      if (buttonNamed("Confirm stopped")) throw new Error("resolved task retained the resolution action");
      status.dataset.resolveSuccess = "true";
    }
    const resolveCalls = calls.filter((call) => call.path === "/v1/project-archaeology/resolve");
    if (resolveCalls.length !== 1) throw new Error("resolution must issue exactly one request");
    const body = JSON.parse(resolveCalls[0].body);
    if (body.job_id !== nativeJobID() || body.thread_id !== "019ff-project-history" || body.turn_id !== "turn-project-history" || body.resolution !== "confirmed_stopped") throw new Error("resolution request lost exact task identity");
    evidence.push({ stage: resolveConflictScenario ? "resolve_conflict" : "resolve_success", resolveCalls: 1, body });
    status.dataset.resolveCalls = "1";
  }
  status.dataset.result = "pass";
  status.textContent = `PASS: production-composition ${scenario}`;
  globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence, calls };
}

async function runSameSourceChoice() {
  await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Project history run complete"), "completed same-source pilot");
  (await waitFor(() => buttonNamed("Review report details"), "same-source review action")).click();
  const outcomes = await waitFor(() => document.querySelectorAll('.archaeology-outcomes input[aria-label^="Select"]').length === 2 ? [...document.querySelectorAll('.archaeology-outcomes input[aria-label^="Select"]')] : null, "same-source proposal choices");
  await waitFor(() => document.body.textContent.includes("Some proposals are alternatives") && document.body.textContent.includes("same project and source digest"), "same-source explanation");
  outcomes[0].click();
  await waitFor(() => outcomes[0].checked && document.body.textContent.includes("1 of 2 proposals selected"), "first alternative selected");
  outcomes[1].click();
  await waitFor(() => !outcomes[0].checked && outcomes[1].checked && document.body.textContent.includes("1 of 2 proposals selected"), "second alternative replaced first");
  const review = buttonNamed("Review exact diff");
  if (!review || review.disabled || buttonNamed("Review exact combined diff")) throw new Error("same-source alternatives exposed a combined selection");
  if (calls.some((call) => call.path.includes("/import-preview"))) throw new Error("same-source choice issued a preview before explicit review");
  const status = document.querySelector("#gate-status");
  status.dataset.sameSourceChoice = "true";
  status.dataset.selectedCount = "1";
  status.dataset.previewCalls = "0";
  status.dataset.result = "pass";
  status.textContent = "PASS: same-source proposals remain mutually exclusive";
  globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence: [{ stage: "same_source_choice", selectedCount: 1, previewCalls: 0 }], calls };
}
async function runSelectedApply({ previewOnly = false } = {}) {
  await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Project history run complete"), "completed Apply pilot");
  (await waitFor(() => buttonNamed("Review report details"), "review report action")).click();
  const outcomes = await waitFor(() => document.querySelectorAll('.archaeology-outcomes input[aria-label^="Select"]').length === selectedOutcomeCount ? [...document.querySelectorAll('.archaeology-outcomes input[aria-label^="Select"]')] : null, "selected proposal checkboxes");
  outcomes.forEach((outcome) => outcome.click());
  const combinedDiff = await waitFor(() => buttonNamed("Review exact combined diff") && !buttonNamed("Review exact combined diff").disabled ? buttonNamed("Review exact combined diff") : null, "combined diff action");
  combinedDiff.click();
  if (selectedLostPagesScenario) {
    await waitFor(() => document.body.textContent.includes("Commons could not be reached"), "lost page-zero response");
    combinedDiff.click();
  }
  await waitFor(() => document.querySelector("#historical-import-title")?.textContent.includes("Review the exact history proposal"), "exact diff dialog");
  const lockedControls = await waitFor(() => {
    const acknowledgement = document.querySelector(".historical-import-reviewed input");
    const applyButton = buttonNamed("Apply selected history");
    return acknowledgement && applyButton ? { acknowledgement, applyButton } : null;
  }, "locked review controls");
  if (!lockedControls.acknowledgement.disabled || !lockedControls.applyButton.disabled) throw new Error("Apply unlocked before server-attested final exact-diff page");
  if (selectedBypassScenario) {
    const reactPropsKey = Object.keys(lockedControls.applyButton).find((key) => key.startsWith("__reactProps"));
    const directHandler = reactPropsKey ? lockedControls.applyButton[reactPropsKey]?.onClick : null;
    if (typeof directHandler !== "function") throw new Error("direct Apply handler was unavailable");
    directHandler();
    await waitFor(() => document.body.textContent.includes("Complete the server-attested review"), "direct bypass rejection");
    if (selectedApplyCalls !== 0 || document.body.textContent.includes("Reviewed history is now current")) throw new Error("direct bypass reached Apply without a completion token");
    const status = document.querySelector("#gate-status");
    status.dataset.selectedBypass = "true";
    status.dataset.applyCalls = "0";
    status.dataset.result = "pass";
    status.textContent = "PASS: direct Apply bypass rejected without completion token";
    globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence: [{ stage: "selected_bypass", applyCalls: 0, visibleSuccess: false }], calls };
    return;
  }
  (await waitFor(() => buttonNamed("Review next exact-diff page"), "next exact-diff page")).click();
  if (selectedLostPagesScenario) {
    await waitFor(() => document.body.textContent.includes("Commons could not be reached"), "lost intermediate response");
    buttonNamed("Review next exact-diff page").click();
    await waitFor(() => document.body.textContent.includes("2 exact-diff pages reviewed"), "recovered intermediate response");
    buttonNamed("Review next exact-diff page").click();
    await waitFor(() => document.body.textContent.includes("Commons could not be reached"), "lost final response");
    buttonNamed("Review next exact-diff page").click();
  }
  if (selectedPageConflictScenario) {
    await waitFor(() => document.body.textContent.includes("started a fresh review with the same proposals"), "fresh review after page conflict");
    if (!document.querySelector(".historical-import-reviewed input")?.disabled) throw new Error("page conflict reused stale review completion");
    (await waitFor(() => buttonNamed("Review next exact-diff page"), "replacement exact-diff page")).click();
  }
  await waitFor(() => document.body.textContent.includes("completion verified by Commons") && !document.querySelector(".historical-import-reviewed input")?.disabled, "server-attested review completion");
  if (previewOnly) {
    document.querySelector("#historical-import-title")?.focus();
    const previewDialog = document.querySelector(".historical-import-dialog");
    if (previewDialog) previewDialog.scrollTop = 0;
    const status = document.querySelector("#gate-status");
    status.dataset.selectedPreview = "true";
    if (selectedPageConflictScenario) status.dataset.selectedPageConflict = "true";
    if (selectedLostPagesScenario) {
      const previewCalls = calls.filter((call) => call.path.endsWith("/import-preview"));
      const pageCalls = calls.filter((call) => call.path.endsWith("/import-preview-page"));
      const keysByCursor = new Map();
      pageCalls.forEach((call) => {
        const cursor = new URL(call.url, globalThis.location.href).searchParams.get("cursor");
        keysByCursor.set(cursor, [...(keysByCursor.get(cursor) || []), call.idempotencyKey]);
      });
      if (previewCalls.length !== 2 || previewCalls[0].idempotencyKey !== previewCalls[1].idempotencyKey
        || keysByCursor.get("5")?.length !== 2 || keysByCursor.get("5")[0] !== keysByCursor.get("5")[1]
        || keysByCursor.get("10")?.length !== 2 || keysByCursor.get("10")[0] !== keysByCursor.get("10")[1]
        || new Set([previewCalls[0].idempotencyKey, keysByCursor.get("5")[0], keysByCursor.get("10")[0]]).size !== 3) throw new Error("preview page idempotency keys were not stable per retry and unique across pages");
      status.dataset.selectedLostPages = "true";
      status.dataset.previewPageKeys = "stable-unique";
    }
    status.dataset.result = "pass";
    status.textContent = "PASS: grouped selected-proposal exact diff";
    globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence: [{ stage: "selected_preview", projects: selectedOutcomeCount, pages: selectedLostPagesScenario ? 3 : 2, serverAttested: true }], calls };
    return;
  }
  await new Promise((resolve) => setTimeout(resolve, 100));
  const reviewed = await waitFor(() => document.querySelector(".historical-import-reviewed input"), "review acknowledgement");
  const confirmation = document.querySelector('input[name="manifest-digest-confirmation"]');
  flushSync(() => {
    reviewed.click();
    changeControlledInput(confirmation, archaeologyImportBridgeFixture.preview.manifestDigest);
  });
  await new Promise((resolve) => setTimeout(resolve, 50));
  if (!reviewed.checked || confirmation.value !== archaeologyImportBridgeFixture.preview.manifestDigest) throw new Error(`first manifest interaction did not settle (reviewed=${reviewed.checked}, length=${confirmation.value.length})`);
  (await waitFor(() => !buttonNamed("Apply selected history")?.disabled && buttonNamed("Apply selected history"), "first Apply action")).click();
  await waitFor(() => document.body.textContent.includes("Commons refreshed the exact diff"), "fresh diff after conflict");
  if (selectedPreviewCalls !== 2 || selectedPreviewPageCalls !== 1 || selectedApplyCalls !== 1) throw new Error("stale Apply did not refresh the exact selected diff once");
  await waitFor(() => {
    const acknowledgement = document.querySelector(".historical-import-reviewed input");
    const digest = document.querySelector('input[name="manifest-digest-confirmation"]');
    return acknowledgement && !acknowledgement.checked && digest && !digest.value && buttonNamed("Apply selected history")?.disabled;
  }, "refreshed diff acknowledgement reset");
  if (!document.body.textContent.includes(refreshedManifestDigest) || !document.body.textContent.includes("Keeps current")) throw new Error("refreshed diff did not expose its changed manifest and disposition");
  if (!document.querySelector(".historical-import-reviewed input")?.disabled) throw new Error("stale re-preview reused the prior completion token");
  (await waitFor(() => buttonNamed("Review next exact-diff page"), "refreshed next exact-diff page")).click();
  await waitFor(() => document.body.textContent.includes("completion verified by Commons") && !document.querySelector(".historical-import-reviewed input")?.disabled, "refreshed server review completion");
  flushSync(() => {
    document.querySelector(".historical-import-reviewed input").click();
    changeControlledInput(document.querySelector('input[name="manifest-digest-confirmation"]'), refreshedManifestDigest);
  });
  (await waitFor(() => !buttonNamed("Apply selected history")?.disabled && buttonNamed("Apply selected history"), "second explicit Apply action")).click();
  await waitFor(() => document.querySelector("#historical-import-title")?.textContent.includes("Reviewed history is now current"), "atomic Apply receipt");
  if (!document.body.textContent.includes("AUDIT-SELECTED-1") || !document.body.textContent.includes("2")) throw new Error("bounded Apply receipt did not expose audit and selection identity");
  if (selectedApplyCalls !== 2) throw new Error("selected Apply did not require a second explicit submission");
  const status = document.querySelector("#gate-status");
  status.dataset.selectedApply = "true";
  status.dataset.previewCalls = String(selectedPreviewCalls);
  status.dataset.previewPageCalls = String(selectedPreviewPageCalls);
  status.dataset.applyCalls = String(selectedApplyCalls);
  status.dataset.result = "pass";
  status.textContent = "PASS: selected Apply conflict refresh and atomic receipt";
  globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence: [{ stage: "selected_apply", previewCalls: selectedPreviewCalls, previewPageCalls: selectedPreviewPageCalls, applyCalls: selectedApplyCalls, staleAcknowledgementReset: true, completionTokenRotated: true }], calls };
}

async function run() {
  if (firstRunScenario) {
    await runFirstRun();
    return;
  }
  const account = await waitFor(
    () => document.querySelector('summary[aria-label^="Signed in as"]'),
    "authenticated account menu",
  );
  account.click();
  (await waitFor(() => buttonNamed("Bring in project history"), "project-history command")).click();

  if (keyboardCatalogScenario) {
    const search = await waitFor(() => document.querySelector('input[name="project-search"]'), "keyboard catalog search");
    await new Promise((resolve) => setTimeout(resolve, 100));
    search.focus();
    if (document.activeElement !== search) throw new Error("project search did not accept keyboard focus");
    const status = document.querySelector("#gate-status");
    status.dataset.keyboardCatalog = "true";
    status.dataset.result = "pass";
    status.textContent = "PASS: keyboard focus enters the responsive catalog";
    globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence: [{ stage: "keyboard_catalog", activeName: search.name }], calls };
    return;
  }

  if (historyStatusScenario) {
    (await waitFor(() => buttonNamed("Run history"), "Run history action")).click();
    await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Run history"), "Run history surface");
    const installation = await waitFor(() => document.querySelector(".archaeology-installation"), "installation status");
    installation.querySelector("summary").click();
    await waitFor(() => document.body.textContent.includes("Schema 15") && document.body.textContent.includes("0.147.0") && document.body.textContent.includes("Verified backup") && document.body.textContent.includes(historyStatusUnknownScenario ? "Not yet ready for Beta review" : "Ready for your Beta review"), "installation status facts");
    if (historyStatusUnknownScenario && (!document.body.textContent.includes("Not yet verified") || !document.body.textContent.includes("Browser session revocation pending") || document.body.textContent.includes("No duplicate launches") || document.body.textContent.includes("No unexplained changes"))) throw new Error("unknown or degraded evidence was presented as verified health");
    const runRow = await waitFor(() => document.querySelector(".archaeology-history-rows button"), "history row");
    runRow.click();
    await waitFor(() => document.querySelector(".archaeology-history-detail")?.textContent.includes("Project history · Archived Project 101") && document.body.textContent.includes("Exact Codex IDs"), "bounded off-page run detail");
    const firstProposal = document.querySelector(".archaeology-history-report input[type=checkbox]");
    firstProposal.click();
    (await waitFor(() => buttonNamed("Load 5 more proposals"), "second proposal page")).click();
    await waitFor(() => document.querySelectorAll(".archaeology-history-report label").length === 10 && firstProposal.checked, "second proposal page merge");
    (await waitFor(() => buttonNamed("Load 5 more proposals"), "third proposal page")).click();
    await waitFor(() => document.querySelectorAll(".archaeology-history-report label").length === 12 && firstProposal.checked && !buttonNamed("Load 5 more proposals"), "complete proposal pagination");
    if (!document.body.textContent.includes("4 exact citations retained") || document.body.textContent.includes("1000 cited sources")) throw new Error("history overclaimed examined sources as retained citations");
    const provenance = [...document.querySelectorAll(".archaeology-history-detail code")].map((node) => node.textContent);
    if (!provenance.includes(nativeJobID()) || !provenance.includes(NATIVE_BATCH_ID)) throw new Error("bounded history lost exact Codex provenance");
    evidence.push({ stage: "history_status", schema: 15, codex: "0.147.0", batches: 1, details: true, outcomes: 12, outcomePages: 3, offPageProjectName: true });
    status.dataset.historyStatus = "true";
    status.dataset.historyJob = nativeJobID();
    status.dataset.historyBatch = NATIVE_BATCH_ID;
    status.dataset.historyOutcomes = "12";
    status.dataset.historyOutcomePages = "3";
    if (historyStatusUnknownScenario) status.dataset.historyUnknownSafe = "true";
    status.dataset.result = "pass";
    status.textContent = "PASS: bounded Run history and installation status";
    globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence, calls };
    return;
  }

  if (catalogPaginationScenario) {
    const first = await waitFor(() => candidateCheckbox("Codex Commons"), "first discovered catalog page");
    if (calls.filter((call) => call.path === "/v1/project-archaeology/discover").length !== 1 || calls.filter((call) => call.path === "/v1/project-archaeology/catalog").length !== 1) throw new Error("fresh catalog did not load exactly once after discovery");
    first.click();
    await waitFor(() => candidateCheckbox("Codex Commons")?.checked, "first-page selection");
    (await waitFor(() => buttonNamed("Load more · 1 remaining"), "next catalog page")).click();
    const last = await waitFor(() => candidateCheckbox("Codex Project 101"), "second catalog page");
    last.click();
    await waitFor(() => candidateCheckbox("Codex Commons")?.checked && candidateCheckbox("Codex Project 101")?.checked && document.body.textContent.includes("2 of 30 selected"), "cross-page selection");
    const mixedStart = await waitFor(() => buttonNamed("Add 2 projects to Commons · start 2 named Codex tasks"), "mixed-page Start action");
    if (mixedStart.disabled) throw new Error("mixed cross-page selection was checked but Start remained disabled");
    first.click();
    await waitFor(() => !buttonNamed("Add 1 project to Commons · start 1 named Codex task")?.disabled && buttonNamed("Add 1 project to Commons · start 1 named Codex task"), "page-two-only Start action");
    const paginatedCatalogCalls = calls.filter((call) => call.path === "/v1/project-archaeology/catalog");
    if (paginatedCatalogCalls.length !== 2 || !String(paginatedCatalogCalls[1].url || "").includes("cursor=page-2")) {
      const secondCursor = new URLSearchParams(String(paginatedCatalogCalls[1]?.body || "")).get("cursor");
      if (secondCursor !== "page-2") throw new Error("second catalog page did not use the server cursor");
    }
    const sort = document.querySelector('select[name="project-sort"]');
    sort.value = "name";
    sort.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => calls.filter((call) => call.path === "/v1/project-archaeology/catalog").length >= 3 && document.body.textContent.includes("1 of 30 selected") && !buttonNamed("Add 1 project to Commons · start 1 named Codex task")?.disabled, "page-two selection after sort");
    changeControlledInput(document.querySelector('input[name="project-search"]'), "Codex Commons");
    await waitFor(() => calls.filter((call) => call.path === "/v1/project-archaeology/catalog").length >= 4 && document.body.textContent.includes("1 of 30 selected") && !buttonNamed("Add 1 project to Commons · start 1 named Codex task")?.disabled, "off-page selection after search");
    changeControlledInput(document.querySelector('input[name="project-search"]'), "");
    sort.value = "recent";
    sort.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => buttonNamed("Refresh Codex projects"), "catalog controls restored");
    buttonNamed("Refresh Codex projects").click();
    await waitFor(() => document.body.textContent.includes("selected project is unavailable") && buttonNamed("Add 1 project to Commons · start 1 named Codex task")?.disabled, "pending page-two selection after fresh epoch");
    (await waitFor(() => buttonNamed("Load more · 1 remaining"), "refreshed page-two cursor")).click();
    await waitFor(() => candidateCheckbox("Codex Project 101")?.checked && !buttonNamed("Add 1 project to Commons · start 1 named Codex task")?.disabled, "page-two selection revalidated");
    buttonNamed("Refresh Codex projects").click();
    await waitFor(() => document.body.textContent.includes("0 of 30 selected") && buttonNamed("Choose a project to continue")?.disabled && !buttonNamed("Add 1 project to Commons · start 1 named Codex task") && !buttonNamed("Load more · 1 remaining"), "removed selected project is pruned truthfully");
    const status = document.querySelector("#gate-status");
    status.dataset.catalogPagination = "true";
    status.dataset.catalogCalls = String(paginatedCatalogCalls.length);
    status.dataset.selectedAcrossPages = "2";
    status.dataset.mixedStartEnabled = "true";
    status.dataset.pageTwoOnlyStartEnabled = "true";
    status.dataset.catalogEpochSafe = "true";
    status.dataset.removedSelection = "pruned";
    status.dataset.result = "pass";
    status.textContent = "PASS: fresh discovery and 101-project pagination";
    globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence: [{ stage: "catalog_pagination", total: 101, paginatedCatalogCalls: 2, selectedAcrossPages: 2, mixedStartEnabled: true, pageTwoOnlyStartEnabled: true, preservedAcrossQueryAndSort: true }], calls };
    return;
  }

  if (sameSourceChoiceScenario) {
    await runSameSourceChoice();
    return;
  }
  if (selectedApplyScenario) {
    await runSelectedApply();
    return;
  }
  if (selectedPreviewScenario || selectedPageConflictScenario || selectedBypassScenario || selectedLostPagesScenario) {
    await runSelectedApply({ previewOnly: true });
    return;
  }

  if (directLifecycleScenario) {
    await runDirectLifecycle();
    return;
  }

  await waitFor(() => candidateCheckbox("Codex Commons"), "production candidate catalog");
  await new Promise((resolve) => setTimeout(resolve, 50));
  const checkbox = candidateCheckbox("Codex Commons");
  evidence.push({ stage: "auto_discover_pending", disabled: checkbox.disabled, checked: checkbox.checked });
  if (checkbox.disabled) throw new Error("visible candidate is disabled while automatic discovery is pending");
  if (staleSelectionScenario) {
    await waitFor(() => document.body.textContent.includes("0 of 30 selected") && catalogCandidates.slice(0, 9).every((candidate) => candidateCheckbox(candidate.name)?.checked === false), "persisted selection cleared on fresh open");
    if (calls.some((call) => call.path.endsWith("/config") || call.path.endsWith("/start"))) throw new Error("clearing the persisted draft selection performed a write");
    evidence.push({ stage: "stale_selection_cleared", persisted: staleSelectedCandidateIDs.length, visibleSelected: 0, writes: 0 });
    status.dataset.staleSelectionCleared = "true";
  }
  if (largeBatchScenario) {
    catalogCandidates.slice(0, 6).forEach((candidate) => candidateCheckbox(candidate.name)?.click());
    await waitFor(() => catalogCandidates.slice(0, 6).every((candidate) => candidateCheckbox(candidate.name)?.checked), "six local selections");
  } else {
    checkbox.click();
    await waitFor(() => candidateCheckbox("Codex Commons")?.checked, "local selection toggle");
  }
  if (staleSelectionScenario && (!document.body.textContent.includes("Selected to launch · 1") || !document.body.textContent.includes("Codex Commons")))
    throw new Error("single-project launch summary did not expose the exact visible selection");

  const writesAfterSelection = calls.filter((call) => call.path.endsWith("/config") || call.path.endsWith("/start"));
  if (writesAfterSelection.length) throw new Error("selection unexpectedly performed a backend start/config write");
  const pendingStart = await waitFor(() => buttonNamed(`Refreshing projects · ${selectedCandidateIDs.length} selected`), "refresh-gated Start copy");
  if (!pendingStart.disabled) throw new Error("Start became active before automatic discovery settled");
  evidence.push({ stage: "selected_during_refresh", startDisabled: pendingStart.disabled, writes: writesAfterSelection.length });

  canonicalArchaeology = archaeologyDTO({ revision: 12 });
  discovery.resolve(apiResponse(canonicalArchaeology, "qa-archaeology-discover"));
  await waitFor(() => candidateCheckbox("Codex Commons")?.checked, "selection after revision replacement");
  const readyStart = await waitFor(
    () => [...document.querySelectorAll("button")].find((button) => button.textContent.includes(`Add ${selectedCandidateIDs.length} project`)),
    "ready Start action",
  );
  evidence.push({
    stage: "revision_replaced",
    checked: candidateCheckbox("Codex Commons").checked,
    startDisabled: readyStart.disabled,
    startCopy: readyStart.textContent.trim(),
  });
  if (readyStart.disabled) {
    throw new Error("circular gate: valid local selection cannot persist because production DTO reports pre-selection can_start/task_launch unavailable");
  }

  readyStart.click();
  if (largeBatchScenario) {
    await waitFor(() => document.querySelector("#archaeology-confirm-title")?.textContent.includes("Start 6 named Codex tasks"), "large batch confirmation");
    if (!document.body.textContent.includes("Luna Max") || !document.body.textContent.includes("Git + Project docs + Codex history") || !document.body.textContent.includes("Codex-managed") || !document.body.textContent.includes("signed-in Codex allowance")) throw new Error("large batch confirmation omitted exact usage policy");
    if (document.activeElement?.id !== "archaeology-confirm-title") throw new Error("large batch confirmation did not move keyboard focus to its heading");
    if (!readyStart.closest("[inert]")) throw new Error("large batch confirmation left background controls interactive");
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await waitFor(() => !document.querySelector("#archaeology-confirm-title") && document.activeElement === readyStart, "Escape close and focus restore");
    readyStart.click();
    await waitFor(() => document.activeElement?.id === "archaeology-confirm-title", "reopened confirmation focus");
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", shiftKey: true, bubbles: true }));
    if (document.activeElement?.textContent.trim() !== "Back") throw new Error("large batch confirmation did not trap reverse Tab focus");
    const confirm = buttonNamed("Start 6 tasks");
    if (!confirm?.disabled) throw new Error("large batch confirmation was active before acknowledgement");
    document.querySelector(".archaeology-confirm input[type=checkbox]").click();
    await waitFor(() => !buttonNamed("Start 6 tasks")?.disabled && buttonNamed("Start 6 tasks"), "acknowledged large batch");
    buttonNamed("Start 6 tasks").click();
  }
  if (conflictScenario) {
    await waitFor(() => [...document.querySelectorAll('[role="alert"]')].some((node) => node.textContent.includes("conflicts with newer Commons activity")), "revision conflict explanation");
    await waitFor(() => calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length >= 2, "conflict refetch");
    await new Promise((resolve) => setTimeout(resolve, 50));
    const startCalls = calls.filter((call) => call.path === "/v1/project-archaeology/start");
    if (startCalls.length) throw new Error("revision conflict incorrectly launched a Codex task");
    if (!candidateCheckbox("Codex Commons")?.checked) throw new Error("revision conflict discarded the still-valid local project selection");
    evidence.push({ stage: "configuration_conflict", startCalls: 0, selectionPreserved: true });
    status.dataset.conflictPreserved = "true";
    status.dataset.startCalls = "0";
  } else if (unsafeStartScenario) {
    const alert = await waitFor(
      () => [...document.querySelectorAll('[role="alert"]')].find((node) => node.textContent.includes("could not verify a fresh Codex task")),
      "bounded start verification alert",
    );
    const startCalls = calls.filter((call) => call.path === "/v1/project-archaeology/start");
    const configCalls = calls.filter((call) => call.path === "/v1/project-archaeology/config");
    if (configCalls.length !== 1 || startCalls.length !== 1) throw new Error("unsafe start must perform exactly one config and one start request");
    if (!document.querySelector("#archaeology-title")?.textContent.includes("Choose your Codex projects")) throw new Error("unsafe start left the recoverable Configure state");
    if (document.body.textContent.includes("Starting Codex tasks") || document.querySelector(".archaeology-run-list")) throw new Error("unsafe start rendered a task lifecycle claim");
    const readsAfterStart = calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length;
    await new Promise((resolve) => setTimeout(resolve, 1700));
    if (calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length !== readsAfterStart) throw new Error("unsafe start began lifecycle polling");
    evidence.push({ stage: "unsafe_start_rejected", scenario, alert: alert.textContent.trim(), startCalls: 1, configurePreserved: true, lifecyclePolls: 0 });
    status.dataset.rejectedStart = "true";
    status.dataset.startCalls = "1";
  } else {
    const readsBeforeStart = calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length;
    await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Starting Codex tasks"), "native handoff screen");
    if (visibilityStagesScenario && (!document.body.textContent.includes("Codex identity bound") || document.body.textContent.includes("Visible in Codex"))) throw new Error("identity-bound starting state overclaimed Codex visibility");
    const configCalls = calls.filter((call) => call.path === "/v1/project-archaeology/config");
    const startCalls = calls.filter((call) => call.path === "/v1/project-archaeology/start");
    if (configCalls.length !== 1 || startCalls.length !== 1) throw new Error("Start must perform exactly one config write followed by exactly one launch");
    const configIndex = calls.indexOf(configCalls[0]);
    const startIndex = calls.indexOf(startCalls[0]);
    if (configIndex >= startIndex) throw new Error("Codex launch occurred before configuration persisted");
    const configBody = JSON.parse(configCalls[0].body);
    const startBody = JSON.parse(startCalls[0].body);
    const exactSubmittedSet = Array.isArray(configBody.selected_project_ids)
      && configBody.selected_project_ids.length === selectedCandidateIDs.length
      && selectedCandidateIDs.every((id) => configBody.selected_project_ids.includes(id));
    if (configBody.base_revision !== 12 || !exactSubmittedSet) throw new Error("configuration write did not use the refreshed revision and exact local selection");
    if (startBody.base_revision !== 13) throw new Error("launch did not use the configured revision");
    if (startBody.acknowledge_large_batch !== largeBatchScenario) throw new Error("launch acknowledgement did not match the selected task count");
    if (!configCalls[0].idempotencyKey || !startCalls[0].idempotencyKey || configCalls[0].idempotencyKey === startCalls[0].idempotencyKey) throw new Error("config and launch require distinct idempotency keys");
    await waitFor(
      () => calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length > readsBeforeStart,
      "first lifecycle poll",
      7000,
    );
    if (expiredPollScenario) {
      await waitFor(() => document.querySelector(".session-sign-in")?.textContent.includes("Sign in"), "signed-out control after lifecycle auth expiry");
      await waitFor(() => !document.querySelector(".archaeology-dialog[open]") && !document.querySelector(".session-menu"), "stale authenticated archaeology UI to close");
      if (document.body.textContent.includes(nativeJobID()) || document.body.textContent.includes(NATIVE_BATCH_ID)) throw new Error("expired lifecycle session retained stale native task identities");
      const readsAfterExpiry = calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length;
      await new Promise((resolve) => setTimeout(resolve, 1700));
      if (calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length !== readsAfterExpiry) throw new Error("lifecycle polling continued after authentication expired");
      evidence.push({ stage: "lifecycle_auth_expired", pollingStopped: true, staleAdminState: false });
      status.dataset.authExpired = "true";
      status.dataset.pollingStopped = "true";
    } else {
      await waitFor(() => visibilityStagesScenario
        ? document.body.textContent.includes("Visible in Codex · reading project sources") && document.body.textContent.includes("Reading project documentation")
        : document.querySelector("#archaeology-title")?.textContent.includes("Starting Codex tasks") && document.body.textContent.includes("Project history · Codex Commons"), "stable native handoff after lifecycle poll");
      evidence.push({ stage: largeBatchScenario ? "large_batch" : "native_handoff", configRevision: 12, startRevision: 13, jobs: selectedCandidateIDs.length, largeBatchAcknowledged: startBody.acknowledge_large_batch, stableAfterPoll: true });
      if (largeBatchScenario) { status.dataset.largeBatch = "true"; status.dataset.modalKeyboard = "true"; }
      if (visibilityStagesScenario) status.dataset.visibilityStages = "active-only";
      status.dataset.stableAfterPoll = "true";
      if (staleSelectionScenario) {
        evidence.push({ stage: "stale_selection_exact_submit", persisted: staleSelectedCandidateIDs.length, submitted: configBody.selected_project_ids.length, visibleCount: 1 });
        status.dataset.staleSelectionExactSubmit = "true";
        status.dataset.submittedProjects = String(configBody.selected_project_ids.length);
      }
      if (recoveredStartScenario) {
        const startCalls = calls.filter((call) => call.path === "/v1/project-archaeology/start");
        if (startCalls.length !== 1) throw new Error("canonical start recovery issued a duplicate start request");
        evidence.push({ stage: "canonical_start_recovery", startCalls: 1, recoveredBatchID: NATIVE_BATCH_ID });
        status.dataset.recoveredStart = "true";
        status.dataset.startCalls = "1";
      }
    }
  }

  status.dataset.result = "pass";
  status.textContent = "PASS: production-composition Project Archaeology gate";
  globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "pass", evidence, calls };
}

run().catch((error) => {
  status.dataset.result = "fail";
  status.textContent = `FAIL: ${error.message}`;
  globalThis.__PROJECT_ARCHAEOLOGY_GATE__ = { result: "fail", error: error.message, evidence, calls };
  console.error("Project Archaeology production-composition gate failed", error);
});
