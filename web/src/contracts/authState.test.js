import assert from "node:assert/strict";
import test from "node:test";
import { AUTH_ACTIONS, authReducer, initialAuthState } from "./authState.js";

const session = {
  authenticated: true,
  principal: { kind: "human", principal: "human:local-admin", handle: "alex", displayName: "Alex Lee" },
  csrfToken: "csrf-1",
  authMethod: "codex",
  profileRevision: 1,
};

test("auth reducer models the Codex first-run path explicitly", () => {
  let state = initialAuthState();
  assert.equal(state.status, "loading");

  state = authReducer(state, { type: AUTH_ACTIONS.SESSION, session: { authenticated: false, principal: null, csrfToken: "" } });
  assert.equal(state.status, "unauthenticated");

  state = authReducer(state, { type: AUTH_ACTIONS.START_PAIRING, pairing: { attemptID: "A-1", state: "waiting_for_user" }, redirect: "#post/P-1" });
  assert.equal(state.status, "pairing");
  assert.equal(state.pairing.attemptID, "A-1");
  assert.equal(state.redirect, "#post/P-1");

  state = authReducer(state, { type: AUTH_ACTIONS.NEEDS_PROFILE, pairing: { ...state.pairing, state: "needs_profile" }, profileDraft: { displayName: "Alex Lee", handle: "alex" } });
  assert.equal(state.status, "needs_profile");
  assert.equal(state.profileDraft.handle, "alex");

  state = authReducer(state, { type: AUTH_ACTIONS.AUTHENTICATED, session, redirect: "" });
  assert.equal(state.status, "authenticated");
  assert.equal(state.session.principal.displayName, "Alex Lee");
  assert.equal(state.pairing, null);
  assert.equal(state.redirect, "");

  state = authReducer(state, { type: AUTH_ACTIONS.SIGN_OUT });
  assert.equal(state.status, "unauthenticated");
  assert.equal(state.session.authenticated, false);
});

test("auth reducer preserves a retry target through recoverable errors", () => {
  let state = authReducer(initialAuthState(), { type: AUTH_ACTIONS.SESSION, session: { authenticated: false, principal: null, csrfToken: "" } });
  state = authReducer(state, { type: AUTH_ACTIONS.START_PAIRING, pairing: { attemptID: "A-2", state: "waiting_for_user" } });
  state = authReducer(state, { type: AUTH_ACTIONS.ERROR, error: { code: "poll_failed", message: "Try again.", resumeState: "pairing" } });
  assert.equal(state.status, "error");
  assert.equal(state.error.resumeState, "pairing");
  state = authReducer(state, { type: AUTH_ACTIONS.CLEAR_ERROR, resumeState: "pairing" });
  assert.equal(state.status, "pairing");
  assert.equal(state.pairing.attemptID, "A-2");
});
