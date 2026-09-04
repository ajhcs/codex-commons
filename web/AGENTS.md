# Commons web

Application UI lives in `src/`. Follow the root AGENTS.md production and
working-tree safeguards.

## Context for the change

- Before changing UI or behavior, read the relevant sections of
  [web product contracts](../docs/web-product-contracts.md): visual/layout,
  identity/assets, authentication, Project Archaeology, Posts/notifications,
  or Project Core. They preserve the owner's durable product decisions.
- Selected mockups guide visual implementation. Runtime behavior remains
  governed by the product contracts and backend API. Screens consume the
  `src/contracts/commons.js` and `src/data/adapter.js` reconciliation seam,
  not raw fixtures.
- For unclear visual requirements, resolve the missing context from the task
  and existing references. Use an available design skill when the task calls
  for it; no particular plugin is required for routine implementation.
- Record durable new product decisions in the relevant product-contract section.

## Verification and handoff

- For visual changes, run the local preview and inspect it in the available
  browser when the task authorizes that workflow. Check free ports and follow
  the root operational rules before starting a listener.
- Checks from `web/`: `npm test` and `npm run build`; use
  `npm run test:project-archaeology:browser` for Archaeology browser behavior.
- Storyboards do not replace `tests/project-archaeology-production-contract.test.mjs`
  and the authenticated AppShell composition in
  `qa/project-archaeology-production-gate.jsx`.
- Preserve `.openai/hosting.json`, `worker/index.js`,
  `scripts/prepare-sites-build.mjs`, and `tests/sites-worker.test.mjs`.
  Before a Sites handoff, run `npm run build` and `npm run test:sites`; expect
  `dist/client/index.html`, `dist/server/index.js`, and
  `dist/.openai/hosting.json`.
