import assert from "node:assert/strict";
import test from "node:test";
import { codexAuthFixtures } from "./fixtures.js";
import { createHTTPAdapter } from "./adapter.js";

function apiResponse(data, { status = 200, ok = status >= 200 && status < 300, error } = {}) {
  return new Response(JSON.stringify({
    ok,
    ...(ok ? { data } : { error: error || { code: "request_failed", message: "failed" } }),
    meta: { request_id: "auth-test-request", untrusted: false },
  }), { status, headers: { "content-type": "application/json" } });
}

const session = {
  authenticated: true,
  principal: { kind: "human", principal: "human:local-admin", handle: "alex", display_name: "Alex Lee" },
  csrf_token: "csrf-codex",
  auth_method: "codex",
  profile_revision: 1,
};

test("HTTP adapter validates and transports the complete Codex pairing flow", async () => {
  const calls = [];
  let pollCount = 0;
  const adapter = createHTTPAdapter({
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      if (url === "/v1/auth/codex/status") return apiResponse(codexAuthFixtures.status);
      if (url === "/v1/auth/codex/start") return apiResponse(codexAuthFixtures.start);
      if (url === "/v1/auth/codex/poll") {
        pollCount += 1;
        return apiResponse(pollCount === 1 ? codexAuthFixtures.poll_waiting : session);
      }
      if (url === "/v1/auth/codex/profile") return apiResponse(session);
      if (url === "/v1/auth/codex/cancel") return apiResponse({ authenticated: false, principal: null });
      if (url === "/v1/auth/profile") return apiResponse(session);
      throw new Error(`unexpected request ${url}`);
    },
  });

  const status = await adapter.readCodexStatus();
  const start = await adapter.startCodexPairing();
  const waiting = await adapter.pollCodexPairing(start.attemptID);
  const authenticated = await adapter.pollCodexPairing(start.attemptID);
  const completed = await adapter.completeCodexProfile(start.attemptID, { displayName: "Alex Lee", handle: "Alex" });
  const updated = await adapter.updateProfile({ displayName: "Alex Updated", handle: "alex-updated" }, 1, "csrf-codex", "profile-key");
  const cancelled = await adapter.cancelCodexPairing(start.attemptID);

  assert.equal(status.available, true);
  assert.equal(status.bindingState, "unbound");
  assert.equal(start.verificationURL, codexAuthFixtures.start.verification_url);
  assert.equal(waiting.state, "waiting_for_user");
  assert.equal(authenticated.state, "authenticated");
  assert.equal(authenticated.session.principal.displayName, "Alex Lee");
  assert.equal(completed.authMethod, "codex");
  assert.equal(updated.principal.handle, "alex");
  assert.equal(cancelled.authenticated, false);
  assert.equal(typeof codexAuthFixtures.profile_session.principal.display_name, "string");
  assert.equal(Object.hasOwn(codexAuthFixtures.profile_session.principal, "displayName"), false);

  const startCall = calls.find((call) => call.url === "/v1/auth/codex/start");
  assert.equal(startCall.options.credentials, "same-origin");
  assert.equal(startCall.options.headers.Authorization, undefined);
  assert.deepEqual(JSON.parse(startCall.options.body), {});
  const pollCall = calls.find((call) => call.url === "/v1/auth/codex/poll");
  assert.deepEqual(JSON.parse(pollCall.options.body), { attempt_id: codexAuthFixtures.start.attempt_id });
  const updateCall = calls.find((call) => call.url === "/v1/auth/profile");
  assert.equal(updateCall.options.method, "PUT");
  assert.equal(updateCall.options.headers["X-Commons-CSRF"], "csrf-codex");
  assert.equal(updateCall.options.headers["Idempotency-Key"], "profile-key");
  assert.deepEqual(JSON.parse(updateCall.options.body), { display_name: "Alex Updated", handle: "alex-updated", base_revision: 1 });
});

test("Codex adapter rejects untrusted verification URLs and malformed poll DTOs", async () => {
  const invalidURL = createHTTPAdapter({
    fetchImpl: async () => apiResponse({ ...codexAuthFixtures.start, verification_url: "https://evil.example/device" }),
  });
  await assert.rejects(invalidURL.startCodexPairing(), (error) => error.code === "invalid_payload");

  const invalidPoll = createHTTPAdapter({
    fetchImpl: async () => apiResponse({ state: "waiting_for_user", poll_after_ms: -1 }),
  });
  await assert.rejects(invalidPoll.pollCodexPairing("attempt"), (error) => error.code === "invalid_payload");
});
