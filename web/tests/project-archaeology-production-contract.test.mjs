import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { createHTTPAdapter } from "../src/data/adapter.js";
import { canStartArchaeology, configFromModel } from "../src/features/project-archaeology/projectArchaeologyState.js";
import {
  apiResponse,
  ARCHAEOLOGY_SESSION_ID,
  archaeologyDTO,
  CANONICAL_APPLY_REASON,
  NATIVE_BATCH_ID,
  nativeJobID,
  PRIMARY_CANDIDATE_ID,
  startedArchaeologyDTO,
} from "./project-archaeology-production-fixture.mjs";

test("release-gate fixture preserves the production circular-gate precondition", async () => {
  const payload = archaeologyDTO({ taskLaunchAvailable: false });
  assert.deepEqual(Object.keys(payload.discovery).sort(), [
    "app_server_identity",
    "candidates",
    "completed_at",
    "discovered_at",
    "metadata_only",
    "projects_grouped",
    "source_roots_scanned",
    "state",
    "tasks_examined",
    "truncated",
  ], "authenticated composition uses only fields emitted by application.ArchaeologyDiscovery");

  const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
  const model = await adapter.readProjectArchaeology();

  assert.equal(model.state, "draft");
  assert.equal(model.revision, 11);
  assert.equal(model.discovery.candidates.length, 26);
  assert.equal(model.discovery.telemetry.tasksExamined, 179);
  assert.equal(model.discovery.telemetry.projectsGrouped, 26);
  assert.equal(model.discovery.telemetry.truncated, false);
  assert.equal(model.discovery.telemetry.appServerIdentity, "Codex App Server · 0.147.0");
  assert.equal(model.discovery.candidates.every((item) => item.sources.includes("codex_metadata")), true);
  assert.deepEqual(model.config.selectedProjectIds, []);
  assert.equal(model.controls.canStart, false);
  assert.equal(model.capabilities.taskLaunch.configured, true);
  assert.equal(model.capabilities.taskLaunch.available, false);
  assert.equal(model.capabilities.discovery.mode, "codex_known_metadata");
  assert.deepEqual(payload.capabilities.review, { configured: true, available: false, mode: "durable_manifest" });
  assert.deepEqual(payload.capabilities.canonical_apply, { configured: true, available: false, mode: "preview_manifest_confirm", reason: CANONICAL_APPLY_REASON });

  const localConfig = {
    ...configFromModel(model.config),
    selectedProjectIds: [PRIMARY_CANDIDATE_ID],
  };
  assert.equal(canStartArchaeology(localConfig, model.discovery.candidates), true,
    "local selection is intrinsically valid even though the persisted pre-start DTO cannot authorize it yet");
});

test("corrected projection separates installation capability from persisted start authorization", async () => {
  const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(archaeologyDTO()) });
  const model = await adapter.readProjectArchaeology();

  assert.equal(model.config.selectedProjectIds.length, 0);
  assert.equal(model.controls.canStart, false, "a persisted empty configuration cannot start yet");
  assert.equal(model.capabilities.taskLaunch.available, true,
    "installation launch capability is independent of the current persisted selection");
});

test("native handoff fixture matches the production application projection", async () => {
  const payload = startedArchaeologyDTO();
  const candidate = payload.discovery.candidates[0];
  const handoff = payload.handoff;
  const task = handoff.tasks[0];

  assert.match(payload.id, /^AR-[0-9a-f]{24}$/);
  assert.equal(payload.id, ARCHAEOLOGY_SESSION_ID);
  assert.match(candidate.id, /^codex-[0-9a-f]{24}$/);
  assert.equal(candidate.id, PRIMARY_CANDIDATE_ID);
  assert.equal(candidate.path_label, "Codex Commons");
  assert.deepEqual(Object.keys(candidate.estimate).sort(), ["duration_seconds_max", "duration_seconds_min", "relative_cost"]);
  assert.equal(candidate.estimate.duration_seconds_min, 60);
  assert.equal(candidate.estimate.duration_seconds_max, 600);
  assert.equal(handoff.id, "");
  assert.match(handoff.batch_id, /^ARB-[0-9a-f]{24}$/);
  assert.equal(handoff.batch_id, NATIVE_BATCH_ID);
  assert.equal(handoff.state, "queued");
  assert.deepEqual(handoff.allowed_actions, []);
  assert.deepEqual(handoff.candidate_ids, [PRIMARY_CANDIDATE_ID]);
  assert.match(task.job_id, /^ARJ-[0-9a-f]{24}$/);
  assert.equal(task.job_id, nativeJobID());
  assert.equal(task.launch_id, task.job_id);
  assert.equal(task.batch_id, handoff.batch_id);
  assert.equal(task.candidate_id, PRIMARY_CANDIDATE_ID);
  assert.equal(task.project_id, PRIMARY_CANDIDATE_ID);
  assert.equal(task.phase_label, "");
  assert.equal(typeof task.created_at, "string");
  assert.equal(typeof task.updated_at, "string");
  assert.ok(Date.parse(payload.updated_at) >= Date.parse(handoff.updated_at));
  assert.ok(Date.parse(payload.updated_at) >= Date.parse(task.updated_at));
  assert.deepEqual(task.available_actions, []);
});

test("inventory discovery copy discloses and bounds Codex preview bytes truthfully", async () => {
  const source = await readFile(new URL("../src/features/project-archaeology/ProjectArchaeologyDialog.jsx", import.meta.url), "utf8");
  assert.match(source, /Codex 0\.147 may send preview bytes on the inventory wire; Commons immediately discards them and retains only sanitized workspace metadata/);
  assert.doesNotMatch(source, /No file or conversation contents are being read/);
});
