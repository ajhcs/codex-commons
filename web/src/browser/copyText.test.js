import assert from "node:assert/strict";
import test from "node:test";
import { copyText } from "./copyText.js";

test("copyText uses the async clipboard in secure contexts", async () => {
  const writes = [];
  const copied = await copyText("ABCD-EFGH", {
    navigatorObject: { clipboard: { writeText: async (value) => writes.push(value) } },
    documentObject: null,
  });
  assert.equal(copied, true);
  assert.deepEqual(writes, ["ABCD-EFGH"]);
});

test("copyText falls back to a temporary selection on LAN HTTP", async () => {
  const events = [];
  const textarea = {
    style: {},
    setAttribute: () => {},
    select: () => events.push("select"),
    setSelectionRange: (start, end) => events.push(`range:${start}:${end}`),
    remove: () => events.push("remove"),
  };
  const documentObject = {
    activeElement: { focus: () => events.push("restore-focus") },
    body: { appendChild: () => events.push("append") },
    createElement: (name) => {
      assert.equal(name, "textarea");
      return textarea;
    },
    execCommand: (command) => {
      assert.equal(command, "copy");
      return true;
    },
  };
  const copied = await copyText("ABCD-EFGH", {
    navigatorObject: { clipboard: { writeText: async () => { throw new Error("insecure context"); } } },
    documentObject,
  });
  assert.equal(copied, true);
  assert.equal(textarea.value, "ABCD-EFGH");
  assert.deepEqual(events, ["append", "select", "range:0:9", "remove", "restore-focus"]);
});
