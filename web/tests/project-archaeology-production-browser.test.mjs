import assert from "node:assert/strict";
import { after, before, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";
import { inspectChromePage } from "../scripts/run-project-archaeology-browser-gate.mjs";
import { NATIVE_BATCH_ID, nativeJobID } from "./project-archaeology-production-fixture.mjs";

const webRoot = fileURLToPath(new URL("../", import.meta.url));
let server;
let origin;

before(async () => {
  server = await createServer({
    root: webRoot,
    logLevel: "error",
    server: { host: "127.0.0.1", port: 0, strictPort: false },
  });
  await server.listen();
  const address = server.httpServer.address();
  if (!address || typeof address === "string") throw new Error("Vite browser gate did not expose a TCP port");
  origin = `http://127.0.0.1:${address.port}`;
});

after(async () => { await server?.close(); });

function scenario(query = "", options = {}) {
  return inspectChromePage({ ...options, url: `${origin}/qa/project-archaeology-production-gate.html${query}` });
}

function assertGatePassed(result) {
  assert.equal(result.ready, "pass", result.text);
  assert.match(result.html, /id="gate-status"[^>]*data-result="pass"/);
}

test("authenticated production composition remains on the native handoff after lifecycle polling", async () => {
  const { html, text } = await scenario();
  assert.match(html, /id="gate-status"[^>]*data-result="pass"/);
  assert.match(html, /data-stable-after-poll="true"/);
  assert.match(html, /<dialog[^>]*open/);
  assert.match(text, /Starting Codex tasks/);
  assert.match(text, /Project history · Codex Commons/);
  assert.match(html, new RegExp(nativeJobID()));
  assert.match(html, new RegExp(NATIVE_BATCH_ID));
});

test("transient start response recovers the canonical native ledger without a duplicate POST", async () => {
  const { html, text } = await scenario("?scenario=recovered-start");
  assert.match(html, /id="gate-status"[^>]*data-result="pass"/);
  assert.match(html, /data-recovered-start="true"/);
  assert.match(html, /data-start-calls="1"/);
  assert.match(text, /Starting Codex tasks/);
  assert.match(html, new RegExp(NATIVE_BATCH_ID));
});

test("Codex visibility appears only after the final-name readback activates the task", async () => {
  const result = await scenario("?scenario=visibility-stages");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-visibility-stages="active-only"/);
  assert.match(text, /1 historian active/);
  assert.match(text, /Visible in Codex · reading project sources/);
  assert.match(text, /Reading project documentation/);
  assert.doesNotMatch(text, /Preparing 1 project/);
});

test("configuration conflict preserves the local choice and never starts a task", async () => {
  const { html, text } = await scenario("?scenario=conflict");
  assert.match(html, /id="gate-status"[^>]*data-result="pass"/);
  assert.match(html, /data-conflict-preserved="true"/);
  assert.match(html, /data-start-calls="0"/);
  assert.match(text, /conflicts with newer Commons activity/);
  assert.match(text, /Codex Commons/);
  assert.match(html, /type="checkbox"[^>]*checked/);
});

for (const scenarioName of ["malformed-start", "stale-start"]) {
  test(`${scenarioName} response is rejected before lifecycle UI or polling begins`, async () => {
    const { html, text } = await scenario(`?scenario=${scenarioName}`);
    assert.match(html, /id="gate-status"[^>]*data-result="pass"/);
    assert.match(html, /data-rejected-start="true"/);
    assert.match(html, /data-start-calls="1"/);
    assert.match(text, /could not verify a fresh Codex task/);
    assert.match(text, /Choose your Codex projects/);
    assert.doesNotMatch(text, /Starting Codex tasks/);
    assert.doesNotMatch(html, /ARJ-[0-9a-f]{24}|ARB-[0-9a-f]{24}/);
  });
}

test("lifecycle poll authentication expiry removes stale admin state and stops polling", async () => {
  const { html, text } = await scenario("?scenario=expired-poll");
  assert.match(html, /id="gate-status"[^>]*data-result="pass"/);
  assert.match(html, /data-auth-expired="true"/);
  assert.match(html, /data-polling-stopped="true"/);
  assert.match(text, /Sign in/);
  assert.doesNotMatch(text, /Starting Codex tasks|Project history · Codex Commons/);
  assert.doesNotMatch(html, /ARJ-[0-9a-f]{24}|ARB-[0-9a-f]{24}|aria-label="Signed in as/);
});

test("exact uncertain task resolves once through the authenticated production composition", async () => {
  const { html, text } = await scenario("?scenario=resolve-success");
  assert.match(html, /id="gate-status"[^>]*data-result="pass"/);
  assert.match(html, /data-resolve-success="true"/);
  assert.match(html, /data-resolve-calls="1"/);
  assert.match(html, /data-uncertainty-copy="true"/);
  assert.match(text, /confirmed stopped by a signed-in human/);
  assert.doesNotMatch(text, /Confirm stopped/);
});

test("resolution conflict refetches canonical state without retrying the mutation", async () => {
  const result = await scenario("?scenario=resolve-conflict");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-resolve-conflict="true"/);
  assert.match(html, /data-resolve-calls="1"/);
  assert.match(text, /conflicts with newer Commons activity/);
  assert.match(text, /Confirm stopped/);
});

test("identity-less uncertain task exposes no resolution control or request", async () => {
  const { html, text } = await scenario("?scenario=identityless-uncertain");
  assert.match(html, /id="gate-status"[^>]*data-result="pass"/);
  assert.match(html, /data-identityless-safe="true"/);
  assert.match(html, /data-resolve-calls="0"/);
  assert.match(text, /Task identity is not yet reconcilable/);
  assert.doesNotMatch(text, /Check Codex task|Confirm stopped/);
});

test("terminal attention preserves evidence and opens a zero-selection fresh manual run", async () => {
  const result = await scenario("?scenario=terminal-attention");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-terminal-attention="6"/);
  assert.match(html, /data-terminal-copy="true"/);
  assert.match(html, /data-fresh-selected="0"/);
  assert.match(html, /data-resolve-calls="0"/);
  assert.match(text, /0 of 30 selected/);
  assert.doesNotMatch(text, /Check Codex task|Confirm stopped/);
});

test("completed native review details stay batch-bound, factual, and view-only", async () => {
  const result = await scenario("?scenario=review-details");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-review-details="true"/);
  assert.match(html, /data-batch-bound="true"/);
  assert.match(html, /data-can-apply="false"/);
  assert.match(text, /Project history run complete/);
  assert.doesNotMatch(text, /Apply reviewed history|Review exact proposal/);
});

test("Run history keeps installation health and exact task provenance inside Project Archaeology", async () => {
  const result = await scenario("?scenario=history-status");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-history-status="true"/);
  assert.match(text, /Run history/);
  assert.match(text, /Schema 15/);
  assert.match(text, /0\.147\.0/);
  assert.match(text, /Exact Codex IDs/);
  assert.match(text, /Project history · Archived Project 101/);
  assert.match(text, /4 exact citations retained/);
  assert.match(text, /Daily-use evidence/);
  assert.match(text, /12 completed/);
  assert.match(text, /Completed without report\s*0/);
  assert.match(text, /Ready for your Beta review/);
  assert.match(text, /12 reports received/);
  assert.match(text, /Repeated report reads\s*Verified/);
  assert.match(text, /Duplicate launch check\s*Verified/);
  assert.doesNotMatch(text, /1000 cited sources/);
  assert.match(html, /data-history-outcomes="12"/);
  assert.match(html, /data-history-outcome-pages="3"/);
  assert.match(html, new RegExp(nativeJobID()));
  assert.match(html, new RegExp(NATIVE_BATCH_ID));
});

test("unknown installation evidence stays visibly unverified and cannot recommend Beta", async () => {
  const result = await scenario("?scenario=history-status-unknown");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-history-unknown-safe="true"/);
  assert.match(text, /Not yet ready for Beta review/);
  assert.match(text, /Not yet verified/);
  assert.match(text, /Browser session revocation pending/);
  assert.doesNotMatch(text, /Ready for your Beta review|No duplicate launches|No unexplained changes/);
});

test("more than five projects requires an exact second usage confirmation", async () => {
  const result = await scenario("?scenario=large-batch", { media: { reducedMotion: true } });
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-large-batch="true"/);
  assert.match(html, /data-modal-keyboard="true"/);
  assert.equal(result.reducedMotion, true);
  assert.match(text, /6 projects and 3 sources selected/);
  assert.match(text, /Commons submitted every manually confirmed task in this run/);
  assert.match(text, /Codex governs execution capacity/);
  assert.match(text, /may use your signed-in Codex allowance/);
  assert.match(text, /Project history · Codex Commons/);
});

test("same-project same-source proposals remain mutually exclusive before preview", async () => {
  const result = await scenario("?scenario=same-source-choice");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-same-source-choice="true"/);
  assert.match(html, /data-selected-count="1"/);
  assert.match(html, /data-preview-calls="0"/);
  assert.match(text, /Some proposals are alternatives/);
  assert.match(text, /1 of 2 proposals selected/);
  assert.match(text, /Review exact diff/);
});
test("selected Apply refreshes a stale exact diff in place and requires a second explicit review", async () => {
  const result = await scenario("?scenario=selected-apply");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-selected-apply="true"/);
  assert.match(html, /data-preview-calls="2"/);
  assert.match(html, /data-preview-page-calls="2"/);
  assert.match(html, /data-apply-calls="2"/);
  assert.match(text, /Reviewed history is now current/);
  assert.match(text, /Selected proposals\s*6/);
  assert.match(text, /Audit receipt\s*AUDIT-SELECTED-1/);
});

test("selected proposal preview groups the complete exact diff by project before acknowledgement", async () => {
  const result = await scenario("?scenario=selected-preview");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-selected-preview="true"/);
  assert.match(text, /Project\s+Codex Commons\s+codex-000000000000000000000001\s+2 Tasks/);
  assert.match(text, /Project\s+Codex Project 06\s+codex-000000000000000000000006\s+1 Task/);
  assert.match(text, /Description\s+Bind this completed work/);
  assert.match(text, /Acceptance\s+Canonical history retains exact source identity/);
  assert.match(text, /Attributions · 1/);
  assert.match(text, /Task events · 1/);
  assert.match(text, /Will create/);
  assert.match(text, /I reviewed every selected proposal/);
  assert.match(text, /completion verified by Commons/);
});

test("expired or stale preview-page token restarts the same selected review without reusing completion", async () => {
  const result = await scenario("?scenario=selected-page-conflict");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-selected-page-conflict="true"/);
  assert.match(text, /completion verified by Commons/);
  assert.doesNotMatch(text, /Reviewed history is now current/);
});

test("direct Apply bypass without a server completion token shows an error and performs no write", async () => {
  const result = await scenario("?scenario=selected-bypass");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-selected-bypass="true"/);
  assert.match(html, /data-apply-calls="0"/);
  assert.match(text, /Complete the server-attested review/);
  assert.doesNotMatch(text, /Reviewed history is now current/);
});

test("lost page-zero, intermediate, and final responses reuse one key per page and recover exactly", async () => {
  const result = await scenario("?scenario=selected-lost-pages");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-selected-lost-pages="true"/);
  assert.match(html, /data-preview-page-keys="stable-unique"/);
  assert.match(text, /All 3 exact-diff pages reviewed · completion verified by Commons/);
  assert.match(text, /11 selected proposals/);
});

test("fresh discovery loads the server catalog before paginating 101 projects with selection preserved", async () => {
  const result = await scenario("?scenario=catalog-pagination");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-catalog-pagination="true"/);
  assert.match(html, /data-catalog-calls="2"/);
  assert.match(html, /data-selected-across-pages="2"/);
  assert.match(html, /data-mixed-start-enabled="true"/);
  assert.match(html, /data-page-two-only-start-enabled="true"/);
  assert.match(html, /data-catalog-epoch-safe="true"/);
  assert.match(html, /data-removed-selection="pruned"/);
  assert.match(text, /0 of 30 selected/);
  assert.match(text, /Choose a project to continue/);
  assert.doesNotMatch(text, /Add 1 project to Commons/);
});

test("keyboard focus enters the project catalog with a visible named control", async () => {
  const result = await scenario("?scenario=keyboard-catalog");
  const { html, metrics } = result;
  assertGatePassed(result);
  assert.match(html, /data-keyboard-catalog="true"/);
  assert.match(metrics.activeLabel, /Search projects/i);
});

test("200 percent browser zoom keeps the modal inside the visual viewport", async () => {
  const result = await scenario("?scenario=keyboard-catalog", { viewport: { width: 720, height: 900 }, pageScaleFactor: 2 });
  assertGatePassed(result);
  assert.ok(result.metrics.dialogLeft >= 0, `dialog left ${result.metrics.dialogLeft}`);
  assert.ok(result.metrics.dialogRight <= result.metrics.visualWidth + 20, `dialog right ${result.metrics.dialogRight} > ${result.metrics.visualWidth} plus scrollbar allowance`);
  assert.equal(result.metrics.scrollWidth, result.metrics.clientWidth);
});

test("LAN copy and non-rail first profile route into shared project onboarding", async () => {
  const { html, text } = await scenario("?scenario=nonrail-onboarding");
  assert.match(html, /id="gate-status"[^>]*data-result="pass"/);
  assert.match(html, /data-lan-copy="true"/);
  assert.match(html, /data-central-onboarding="true"/);
  assert.match(html, /data-resume-suppressed="true"/);
  assert.match(text, /Choose your Codex projects/);
  assert.match(html, /data-auto-open="true"/);
  assert.match(text, /Codex Commons/);
  assert.doesNotMatch(html, /data-resumed="true"/);
});

test("fresh manual pilot clears a persisted nine-project draft and submits only the visible one-project set", async () => {
  const result = await scenario("?scenario=stale-selection");
  const { html, text } = result;
  assertGatePassed(result);
  assert.match(html, /data-stale-selection-cleared="true"/);
  assert.match(html, /data-stale-selection-exact-submit="true"/);
  assert.match(html, /data-submitted-projects="1"/);
  assert.match(text, /Project history · Codex Commons/);
  assert.doesNotMatch(text, /Start 9 named Codex tasks|Preparing 9 projects/);
});
