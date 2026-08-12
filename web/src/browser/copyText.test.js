import assert from "node:assert/strict";
import test from "node:test";
import { copyText, manualCopyShortcut } from "./copyText.js";

test("copyText uses the async clipboard in secure contexts", async () => {
  const writes = [];
  const copied = await copyText("ABCD-EFGH", {
    navigatorObject: { clipboard: { writeText: async (value) => writes.push(value) } },
    documentObject: null,
    isSecureContext: true,
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
    navigatorObject: { clipboard: { writeText: async () => { throw new Error("must not be called"); } } },
    documentObject,
    isSecureContext: false,
  });
  assert.equal(copied, true);
  assert.equal(textarea.value, "ABCD-EFGH");
  assert.deepEqual(events, ["append", "select", "range:0:9", "remove", "restore-focus"]);
});

test("copyText falls back after a secure Clipboard API rejection", async () => {
  let fallbackCalls = 0;
  const textarea = { style: {}, setAttribute() {}, select() {}, setSelectionRange() {}, remove() {} };
  const copied = await copyText("ABCD-EFGH", {
    navigatorObject: { clipboard: { writeText: async () => { throw new Error("denied"); } } },
    documentObject: {
      body: { appendChild() {} },
      activeElement: null,
      createElement: () => textarea,
      execCommand(command) { fallbackCalls += 1; return command === "copy"; },
    },
    isSecureContext: true,
  });
  assert.equal(copied, true);
  assert.equal(fallbackCalls, 1);
});

test("copyText returns false when execCommand is absent", async () => {
  const textarea = { style: {}, setAttribute() {}, select() {}, setSelectionRange() {}, remove() {} };
  const copied = await copyText("ABCD-EFGH", {
    navigatorObject: {},
    documentObject: { body: { appendChild() {} }, activeElement: null, createElement: () => textarea },
    isSecureContext: false,
  });
  assert.equal(copied, false);
});

test("copyText restores focus, input selection, and document ranges", async () => {
  const events = [];
  const activeElement = {
    selectionStart: 2,
    selectionEnd: 6,
    selectionDirection: "backward",
    focus: () => events.push("focus"),
    setSelectionRange: (start, end, direction) => events.push(`input:${start}:${end}:${direction}`),
  };
  const selection = {
    rangeCount: 1,
    getRangeAt: () => ({ cloneRange: () => ({ id: "saved-range" }) }),
    removeAllRanges: () => events.push("clear-ranges"),
    addRange: (range) => events.push(`range:${range.id}`),
  };
  const textarea = { style: {}, setAttribute() {}, select() {}, setSelectionRange() {}, remove() {} };
  const copied = await copyText("ABCD-EFGH", {
    navigatorObject: {},
    documentObject: {
      activeElement,
      body: { appendChild() {} },
      createElement: () => textarea,
      execCommand: () => true,
      getSelection: () => selection,
    },
    isSecureContext: false,
  });
  assert.equal(copied, true);
  assert.deepEqual(events, ["focus", "input:2:6:backward", "clear-ranges", "range:saved-range"]);
});

test("copyText restores an originally empty document selection", async () => {
  const events = [];
  const selection = {
    rangeCount: 0,
    getRangeAt: () => { throw new Error("no ranges"); },
    removeAllRanges: () => events.push("clear-ranges"),
    addRange: () => events.push("unexpected-range"),
  };
  const textarea = { style: {}, setAttribute() {}, select() {}, setSelectionRange() {}, remove() {} };
  const copied = await copyText("ABCD-EFGH", {
    navigatorObject: {},
    documentObject: {
      activeElement: null,
      body: { appendChild() {} },
      createElement: () => textarea,
      execCommand: () => true,
      getSelection: () => selection,
    },
    isSecureContext: false,
  });
  assert.equal(copied, true);
  assert.deepEqual(events, ["clear-ranges"]);
});

test("copyText never reports success when both copy paths fail", async () => {
  const textarea = { style: {}, setAttribute() {}, select() {}, setSelectionRange() {}, remove() {} };
  const copied = await copyText("ABCD-EFGH", {
    navigatorObject: { clipboard: { writeText: async () => { throw new Error("denied"); } } },
    documentObject: {
      body: { appendChild() {} },
      activeElement: null,
      createElement: () => textarea,
      execCommand: () => false,
    },
    isSecureContext: true,
  });
  assert.equal(copied, false);
});

test("manualCopyShortcut uses the platform-appropriate chord", () => {
  assert.equal(manualCopyShortcut({ platform: "MacIntel" }), "Command+C");
  assert.equal(manualCopyShortcut({ platform: "Linux x86_64" }), "Ctrl+C");
  assert.equal(manualCopyShortcut({ userAgentData: { platform: "macOS" } }), "Command+C");
});
