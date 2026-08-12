import assert from "node:assert/strict";
import test from "node:test";
import { navigateAuthDestination, preopenAuthDestination } from "./authDestination.js";

test("auth destination is opened synchronously and severs its opener", () => {
  const destination = {
    document: { title: "" },
    opener: {},
    closed: false,
    close() {},
  };
  const opened = preopenAuthDestination((url, name) => {
    assert.equal(url, "");
    assert.equal(name, "codex-commons-authorization");
    return destination;
  });
  assert.equal(opened, destination);
  assert.equal(destination.opener, null);
  assert.equal(destination.document.title, "Opening Codex sign-in…");
});

test("auth destination reports popup blocking without throwing", () => {
  assert.equal(preopenAuthDestination(() => null), null);
  assert.equal(preopenAuthDestination(() => { throw new Error("blocked"); }), null);
});

test("supported verification URLs navigate the pre-opened destination", () => {
  const navigations = [];
  const destination = {
    closed: false,
    location: { replace: (url) => navigations.push(url) },
  };
  assert.equal(navigateAuthDestination(destination, "https://auth.openai.com/device"), true);
  assert.deepEqual(navigations, ["https://auth.openai.com/device"]);
  assert.equal(navigateAuthDestination({ ...destination, closed: true }, "https://auth.openai.com/device"), false);
});

test("navigation failure closes the isolated destination", () => {
  let closed = false;
  const destination = {
    closed: false,
    location: { replace() { throw new Error("failed"); } },
    close() { closed = true; },
  };
  assert.equal(navigateAuthDestination(destination, "https://auth.openai.com/device"), false);
  assert.equal(closed, true);
});
