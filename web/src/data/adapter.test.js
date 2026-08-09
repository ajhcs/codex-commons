import assert from "node:assert/strict";
import test from "node:test";
import { fixtureAdapter } from "./adapter.js";

globalThis.window = { setTimeout, clearTimeout };

test("attention filters preserve typed task destinations and bounded pages", async () => {
  const result = await fixtureAdapter.readAttention({ q: "", severity: "high", source: "task", owner: "", project: "", updated_from: "2026-07-10T12:00:00Z", cursor: "", limit: 1 });
  assert.equal(result.items.length, 1);
  assert.equal(result.items[0].severity, "high");
  assert.equal(typeof result.items[0].destination.kind, "string");
  assert.ok(result.nextCursor);
  assert.equal(result.facets.owners_truncated, false);
  assert.equal(result.facets.projects_truncated, false);
});

test("people keep execution and connectivity as separate facts", async () => {
  const result = await fixtureAdapter.readPeople({ q: "", project: "", execution: "not_running", host: "", host_connected: true, cursor: "", limit: 10 });
  assert.ok(result.items.length > 0);
  assert.ok(result.items.every((item) => item.execution === "not_running" && item.connected));
});

test("project overview returns fourteen explicit activity days", async () => {
  const result = await fixtureAdapter.readProjectOverview("billing-orchestrator", { attention_limit: 3, work_limit: 4 });
  assert.equal(result.activity.days.length, 14);
  assert.equal(result.metrics.merged_pull_requests.available, false);
  assert.deepEqual(result.work.items.map((item) => [item.state, item.priority]), [
    ["in_progress", 2],
    ["in_progress", 3],
    ["blocked", 1],
    ["ready", 2],
  ], "adapter preserves canonical state order and lower numeric priority within a state");
});
