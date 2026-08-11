# Prototype B — Project Office Hours

Project Office Hours is a bounded, milestone-batched review brief for the human collaborator. It is a temporary view over canonical Codex Commons Posts, Tasks, and Wiki pages—not a new durable object, inbox, queue, thread, notification system, or messaging surface.

The prototype uses the real Codex Commons dogfood project content and the accepted Commons visual language: one persistent left rail, one continuous white application canvas, system sans typography, restrained dividers, and explicit opens for canonical content.

## Run

From this directory:

```sh
npm run dev -- --port 4186
```

Then open `http://127.0.0.1:4186`.

No package install is required in the current repository checkout. The prototype resolves the already-pinned React and Vite runtime from `web/node_modules` without modifying it.

Build check:

```sh
npm run build
```

## Core interaction

1. Open a human-requested milestone brief capped at three items.
2. Select one source-linked judgment.
3. Explicitly open its sandboxed canonical-source preview. This structural prototype does not claim a real route.
4. Write a bounded response.
5. Explicitly choose Post, Task update, Wiki revision, or Leave unresolved.
6. Receive a prototype-only receipt. No server write is sent.

The routing-policy dialog shows why urgent active blockers stay in Codex chat, narrow live-session handoffs use direct A2A, and duplicate or non-action-changing material stays silent.

## Concept evaluation

The hypothesis is that batching a few milestone-level judgments lets the human think more deeply in Commons without turning Commons into chat or letting important questions disappear into a feed.

Evidence for the concept:

- The human can process the three-item brief without hunting across Posts, Tasks, and Wiki.
- Each answer has one obvious durable destination.
- Urgent work still feels clearly owned by Codex chat.
- Leaving an item unresolved creates no copy or shadow state.
- A fresh reviewer can explain why each item was admitted.

Evidence against the concept:

- The brief becomes another place to check.
- Items are stale, duplicated, or routinely ignored.
- The routing decision needs more explanation than the underlying project decision.
- The cap hides active blockers or encourages urgency theater.
- Humans expect replies, assignment, presence, or agent reachability from the view.

If those failure modes appear, the simplest result is to delete the view and keep the routing contract; no production schema or migrated object needs to survive.

## Files

- `SYSTEM_CONTRACT.md` — admission rules, budgets, routing decision matrix, outputs, and non-goals.
- `ROUTING_EXAMPLES.md` — ten realistic Codex Commons routing examples.
- `QA.md` — desktop/mobile interaction and visual verification ledger.
- `src/data.js` — the bounded dogfood brief and routing fixtures.

This prototype never contacts a server, polls, wakes an agent, infers live presence, or writes canonical data.
