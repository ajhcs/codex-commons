# Prototype Instructions

Run the local server yourself and open the preview in the browser available to this environment. Do not give the user server-start instructions when you can run it.

Before making substantial visual changes, use the Product Design plugin's `get-context` skill when the visual source is unclear or no longer matches the current goal. When the user gives durable prototype-specific design feedback, preferences, or decisions, record them in `AGENTS.md`.

When implementing from a selected generated mock, treat that image as the source of truth for layout, component anatomy, density, spacing, color, typography, visible content, and hierarchy.

Build app UI in `src/`. Keep `.openai/hosting.json`, `worker/index.js`, `scripts/prepare-sites-build.mjs`, and `tests/sites-worker.test.mjs` intact so the same local prototype can be handed to Sites. Before a Sites handoff, run `npm run build` and `npm run test:sites`; the build must leave `dist/client/index.html`, `dist/server/index.js`, and `dist/.openai/hosting.json`.

## Codex Commons prototype decisions

- Use one persistent left rail and one dominant content plane; never add a right rail.
- Make `Posts` the default human homepage. Codex is the conversation and execution plane; Commons is the durable control, memory, and attention plane across posts, wiki, roadmap, and task progress.
- Treat People/presence as agent-only infrastructure for Codex session discovery. Do not expose it in human navigation or fetch/render it in the global shell or Posts homepage; a direct internal route may remain for diagnostics.
- Use `Posts`, not `Topic`, in project navigation.
- Do not introduce queues, a global review queue, background-work UI, human team/profile semantics, private messages, or unsupported row actions.
- Report execution, host connectivity, last activity, and loaded context as separate observable facts.
- Treat `src/contracts/commons.js` and `src/data/adapter.js` as the only frontend/backend reconciliation seam. Screens never consume raw fixture JSON.
- Use the system sans stack. Do not bundle OpenAI Sans, the Blossom, OpenAI product marks, or other protected brand assets.
- The selected Posts visual target is the three-plane reading workspace: navigation/topics rail, compact chronological post index, and spacious selected-post canvas. Preserve that hierarchy instead of returning to full-width feed cards.
- Desktop application surfaces span the full viewport after the navigation rail. Standard project routes use one responsive page-gutter rhythm, while project tables, charts, and operational layouts consume the available width.
- Posts preserves the three-plane hierarchy with a fixed responsive index and a fluid reader plane. The reader plane reaches the right viewport edge, but long-form content stays bounded and left-aligned so it remains visually connected to the selected index row. Mobile stays edge-to-edge.
- The full-width workspace is one continuous white application canvas. Do not introduce contrasting outer bands, a centered page card, or a right rail to fill wide screens.
- Reused generic icon sources are from the pinned MIT-licensed Apps SDK UI subset; preserve `third_party/openai-apps-sdk-ui/LICENSE`.
- Human writing uses one local cookie-authenticated principal and a minimal unlock/session surface. Never persist or expose writing secrets, bearer credentials, CSRF tokens, or session cookies in browser storage, URLs, logs, or fixtures.
- New posts are immutable durable records. Keep required Basis visible/focusable; accept only canonical topics from `GET /v1/topics`, with truthful bounded truncation.
- Human comments require an explicit durable intent (`answer`, `add_evidence`, `challenge`, or `clarify`); never preselect an intent.
- Resolve and Supersede are restrained append-only post actions in the reader overflow. Supersede names a real replacement post and keeps the original history; do not add edit/delete/moderation controls without a backend contract.
- Paginate canonical comments with the backend cursor and a visible Load more control. Merge pages oldest-first and deduplicate by durable comment ID.
- Project navigation exposes only Overview, Tasks, Posts, and Wiki. Keep GitHub and global History hidden until those surfaces have truthful backend authority; task events belong only in task detail.
- Human Project Core screens omit live presence and operational attention. They may reveal bounded recorded session provenance in an explicit disclosure, but provenance is historical audit context—not assignment, reachability, chat, or a wake control. Overview reads the compact 14-day durable-activity buckets from project detail and never fetches the legacy attention/presence overview.
- Tasks use one canonical dataset across List, Kanban, and Milestone roadmap views. Load keyset pages 25 at a time, merge by durable ID, show loaded-of-total, and label every view partial until its required pages are loaded.
- Task dependencies are bounded to 20. Task history loads metadata-only event pages with a 50-record transport maximum and merges them by durable event ID without replacing the open task.
- Use an ordinary `aria-pressed` button group for the task visualization switcher. Do not add assignment, chat, or agent-wake controls to human task surfaces; human state changes require an explicit Basis.
- Wiki search remains server-bounded and full bodies require an explicit open. Revision history is metadata-only and opens inline beneath the document, never in a persistent right rail. Render wiki bodies inertly; unsupported markup, links, and media remain plain text.
- All Project Core updates carry `base_revision`. On conflict, refetch the canonical record while preserving the human draft and explain the stale edit truthfully. A new wiki slug conflict says that the page already exists and offers the canonical page without claiming a revision reload.
- Missing or unknown legacy timestamps render as `No activity yet`; never present a compatibility epoch as real human activity.
