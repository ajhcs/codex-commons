# Prototype Instructions

Run the local server yourself and open the preview in the browser available to this environment. Do not give the user server-start instructions when you can run it.

Before making substantial visual changes, use the Product Design plugin's `get-context` skill when the visual source is unclear or no longer matches the current goal. When the user gives durable prototype-specific design feedback, preferences, or decisions, record them in `AGENTS.md`.

When implementing from a selected generated mock, treat that image as the source of truth for layout, component anatomy, density, spacing, color, typography, visible content, and hierarchy.

Build app UI in `src/`. Keep `.openai/hosting.json`, `worker/index.js`, `scripts/prepare-sites-build.mjs`, and `tests/sites-worker.test.mjs` intact so the same local prototype can be handed to Sites. Before a Sites handoff, run `npm run build` and `npm run test:sites`; the build must leave `dist/client/index.html`, `dist/server/index.js`, and `dist/.openai/hosting.json`.

## Codex Commons prototype decisions

- Use one persistent left rail and one dominant content plane; never add a right rail.
- Use `Posts`, not `Topic`, in project navigation.
- Do not introduce queues, a global review queue, background-work UI, human team/profile semantics, private messages, or unsupported row actions.
- Report execution, host connectivity, last activity, and loaded context as separate observable facts.
- Treat `src/contracts/commons.js` and `src/data/adapter.js` as the only frontend/backend reconciliation seam. Screens never consume raw fixture JSON.
- Source the shell presence preview from the same bounded People read adapter; do not maintain a second hard-coded presence model.
- Use the system sans stack. Do not bundle OpenAI Sans, the Blossom, OpenAI product marks, or other protected brand assets.
- Reused generic icon sources are from the pinned MIT-licensed Apps SDK UI subset; preserve `third_party/openai-apps-sdk-ui/LICENSE`.
