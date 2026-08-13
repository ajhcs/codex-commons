import React from "react";
import { createRoot } from "react-dom/client";
import {
  AUTHENTICATED_SESSION_DTO,
  apiError,
  apiResponse,
  archaeologyDTO,
  deferred,
  startedArchaeologyDTO,
} from "../tests/project-archaeology-production-fixture.mjs";

const discovery = deferred();
const calls = [];
const conflictScenario = new URLSearchParams(globalThis.location.search).get("scenario") === "conflict";

globalThis.fetch = async (url, options = {}) => {
  const path = new URL(String(url), globalThis.location.href).pathname;
  calls.push({
    path,
    method: options.method || "GET",
    body: options.body || "",
    csrf: options.headers?.["X-Commons-CSRF"] || "",
    idempotencyKey: options.headers?.["Idempotency-Key"] || "",
  });
  if (path === "/v1/auth/session") return apiResponse(AUTHENTICATED_SESSION_DTO, "qa-session");
  if (path === "/v1/notifications") return apiResponse({ items: [], next_cursor: "", unread_count: 0 }, "qa-notifications");
  if (path === "/v1/project-archaeology" && (options.method || "GET") === "GET") {
    return apiResponse(archaeologyDTO(), "qa-archaeology-read");
  }
  if (path === "/v1/project-archaeology/discover") return discovery.promise;
  if (path === "/v1/project-archaeology/config" && options.method === "PUT") {
    if (conflictScenario) return apiError(409, "revision_conflict", "qa-config-conflict");
    return apiResponse(archaeologyDTO({
      revision: 13,
      selectedProjectIds: ["codex-commons"],
      canStart: true,
    }), "qa-configured");
  }
  if (path === "/v1/project-archaeology/start" && options.method === "POST") {
    return apiResponse(startedArchaeologyDTO(), "qa-started");
  }
  throw new Error(`Unexpected release-gate request: ${options.method || "GET"} ${path}`);
};

const [
  { AppShell },
  { AuthSessionProvider },
  { NotificationProvider },
  { PreferencesProvider },
] = await Promise.all([
  import("../src/components/AppShell.jsx"),
  import("../src/hooks/AuthSessionContext.jsx"),
  import("../src/hooks/NotificationContext.jsx"),
  import("../src/hooks/usePreferences.jsx"),
  import("../src/styles.css"),
]);

createRoot(document.getElementById("root")).render(
  <React.StrictMode>
    <PreferencesProvider>
      <AuthSessionProvider>
        <NotificationProvider>
          <AppShell route="posts" onNavigate={() => {}}>
            <main><h1>Production composition gate</h1></main>
          </AppShell>
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

async function run() {
  const account = await waitFor(
    () => document.querySelector('summary[aria-label^="Signed in as"]'),
    "authenticated account menu",
  );
  account.click();
  (await waitFor(() => buttonNamed("Bring in project history"), "project-history command")).click();

  await waitFor(() => candidateCheckbox("Codex Commons"), "production candidate catalog");
  await new Promise((resolve) => setTimeout(resolve, 50));
  const checkbox = candidateCheckbox("Codex Commons");
  evidence.push({ stage: "auto_discover_pending", disabled: checkbox.disabled, checked: checkbox.checked });
  if (checkbox.disabled) throw new Error("visible candidate is disabled while automatic discovery is pending");
  checkbox.click();
  await waitFor(() => candidateCheckbox("Codex Commons")?.checked, "local selection toggle");

  const writesAfterSelection = calls.filter((call) => call.path.endsWith("/config") || call.path.endsWith("/start"));
  if (writesAfterSelection.length) throw new Error("selection unexpectedly performed a backend start/config write");
  const pendingStart = await waitFor(() => buttonNamed("Refreshing projects · 1 selected"), "refresh-gated Start copy");
  if (!pendingStart.disabled) throw new Error("Start became active before automatic discovery settled");
  evidence.push({ stage: "selected_during_refresh", startDisabled: pendingStart.disabled, writes: writesAfterSelection.length });

  discovery.resolve(apiResponse(archaeologyDTO({ revision: 12 }), "qa-archaeology-discover"));
  await waitFor(() => candidateCheckbox("Codex Commons")?.checked, "selection after revision replacement");
  const readyStart = await waitFor(
    () => [...document.querySelectorAll("button")].find((button) => button.textContent.includes("Add 1 project to Commons")),
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
  if (conflictScenario) {
    await waitFor(() => document.querySelector('[role="alert"]')?.textContent.includes("conflicts with newer Commons activity"), "revision conflict explanation");
    await waitFor(() => calls.filter((call) => call.path === "/v1/project-archaeology" && call.method === "GET").length >= 2, "conflict refetch");
    await new Promise((resolve) => setTimeout(resolve, 50));
    const startCalls = calls.filter((call) => call.path === "/v1/project-archaeology/start");
    if (startCalls.length) throw new Error("revision conflict incorrectly launched a Codex task");
    if (!candidateCheckbox("Codex Commons")?.checked) throw new Error("revision conflict discarded the still-valid local project selection");
    evidence.push({ stage: "configuration_conflict", startCalls: 0, selectionPreserved: true });
  } else {
    await waitFor(() => document.querySelector("#archaeology-title")?.textContent.includes("Starting Codex tasks"), "native handoff screen");
    const configCalls = calls.filter((call) => call.path === "/v1/project-archaeology/config");
    const startCalls = calls.filter((call) => call.path === "/v1/project-archaeology/start");
    if (configCalls.length !== 1 || startCalls.length !== 1) throw new Error("Start must perform exactly one config write followed by exactly one launch");
    const configIndex = calls.indexOf(configCalls[0]);
    const startIndex = calls.indexOf(startCalls[0]);
    if (configIndex >= startIndex) throw new Error("Codex launch occurred before configuration persisted");
    const configBody = JSON.parse(configCalls[0].body);
    const startBody = JSON.parse(startCalls[0].body);
    if (configBody.base_revision !== 12 || configBody.selected_project_ids?.[0] !== "codex-commons") throw new Error("configuration write did not use the refreshed revision and exact local selection");
    if (startBody.base_revision !== 13) throw new Error("launch did not use the configured revision");
    if (!configCalls[0].idempotencyKey || !startCalls[0].idempotencyKey || configCalls[0].idempotencyKey === startCalls[0].idempotencyKey) throw new Error("config and launch require distinct idempotency keys");
    evidence.push({ stage: "native_handoff", configRevision: 12, startRevision: 13, jobs: 1 });
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
