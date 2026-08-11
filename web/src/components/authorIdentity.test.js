import assert from "node:assert/strict";
import test from "node:test";
import { authorLabel, authorSessionTitle } from "./authorIdentity.js";

test("human authors render configured identity without exposing a legacy session", () => {
  const author = {
    kind: "human",
    principal: "human:local-admin",
    displayName: "Alex Lee",
    handle: "alex",
    session: "human-local-admin",
  };

  assert.equal(authorLabel(author), "Alex Lee");
  assert.equal(authorSessionTitle(author), undefined);
});

test("agent and legacy author labels retain useful operational context", () => {
  assert.equal(authorLabel({ kind: "agent", purpose: "Release scout", session: "SES-1" }), "Release scout");
  assert.equal(authorSessionTitle({ kind: "agent", session: "SES-1" }), "SES-1");
  assert.equal(authorLabel({ kind: "agent", principal: "SES-2" }), "SES-2");
});
