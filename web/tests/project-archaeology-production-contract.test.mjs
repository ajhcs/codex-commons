import assert from "node:assert/strict";
import test from "node:test";

import { createHTTPAdapter } from "../src/data/adapter.js";
import { canStartArchaeology, configFromModel } from "../src/features/project-archaeology/projectArchaeologyState.js";
import { apiResponse, archaeologyDTO } from "./project-archaeology-production-fixture.mjs";

test("release-gate fixture preserves the production circular-gate precondition", async () => {
  const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(archaeologyDTO({ taskLaunchAvailable: false })) });
  const model = await adapter.readProjectArchaeology();

  assert.equal(model.state, "draft");
  assert.equal(model.revision, 11);
  assert.equal(model.discovery.candidates.length, 26);
  assert.equal(model.discovery.candidates.every((item) => item.sources.includes("codex_metadata")), true);
  assert.deepEqual(model.config.selectedProjectIds, []);
  assert.equal(model.controls.canStart, false);
  assert.equal(model.capabilities.taskLaunch.configured, true);
  assert.equal(model.capabilities.taskLaunch.available, false);

  const localConfig = {
    ...configFromModel(model.config),
    selectedProjectIds: ["codex-commons"],
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
