# Prototype A — Source-linked attention slips

This isolated React prototype tests one narrow hypothesis: Commons can hold a
quiet `Needs you` view without becoming chat if every item is only a routing
envelope pointing at an existing Post, Task, or Wiki source.

The slip owns no post body, thread, reply, attachment, task state, Wiki text,
priority, presence, or direct message. Its useful state is limited to routing:
open, accepted, deferred, dismissed, or resolved, plus an action fingerprint
and a dismissal/defer receipt. The durable change happens in the linked source.

## Run

The prototype reuses the already-installed React, Vite, and MIT-licensed
Commons icon modules under `web/`; it does not install another dependency tree.

```sh
cd /home/plumbob/codex-commons/prototypes/attention-system/source-linked-slips
npm run dev -- --host 127.0.0.1 --port 4177 --strictPort
```

Then open `http://127.0.0.1:4177/`.

Build check:

```sh
npm run build
```

## Core interaction

1. Select a quiet source-linked row. It names one requested human judgment,
   the audience, trigger, evidence threshold, and exact canonical source.
2. Inspect the Post, Task, or Wiki in the reader. `Open live source` opens the
   real dogfood record read-only in the current LAN app.
3. Accept the routing, defer it, or dismiss the routing with a noise reason.
4. An accepted route exposes the source-native action: explicit Post reply
   intent, Task outcome plus Basis, or Wiki revision review.
5. A successful prototype action writes a local receipt and clears the slip.
   The live Commons service is never mutated.

State persists in versioned browser local storage so the flow can be explored.
`Reset prototype` restores the initial state. There is no network write,
polling, recurrence, wakeup, or background work.

## Grounding

The records and IDs come from the real Codex Commons dogfood corpus:

- `P-8d960b4871124b0a99584fbe` — Codex remains the conversation and control
  plane.
- `P-b7bfc686c03f172549b5d0c3` — Which Commons surface earns its place in real
  work?
- `T-5c94d9b81c3ce24ebe3ad56d` — Evaluate Codex Commons through real work.
- `W-ff4c18a0ada95f9122887b9c` — Agent operating contract.

Historical contributor IDs come from the deterministic historical import and
are visibly labeled provenance-only: they are not live, reachable, assigned,
or usable as a chat control.

## Product boundary

- Active, execution-blocking, or sensitive questions stay in Codex.
- Reusable public findings and decisions go to Posts.
- Executable future work and state changes go to Tasks.
- Current canonical truth goes to Wiki revisions.
- A `Needs you` slip exists only when a canonical source already exists and a
  bounded human judgment is durable but not synchronous.
- Other agents use Codex agent-to-agent communication or public source context;
  this view is human-only.
- At most 3 slips may be open per project/human across all agent runs; excess
  candidates stay at their canonical source.
- Codex writes under server-attested agent identity. It may preserve a cited
  human decision, but never impersonates human authorship or judgment.
- Routine status, acknowledgements, unchanged checks, low-confidence claims,
  and anything that changes no next action remain silent.

See [AGENT_CONTRACT.md](AGENT_CONTRACT.md),
[routing-examples.md](routing-examples.md), [EVALUATION.md](EVALUATION.md), and
[design-qa.md](design-qa.md).

## Screenshots

- `screenshots/desktop-needs-you.png`
- `screenshots/desktop-agent-contract.png`
- `screenshots/mobile-needs-you.png`
- `screenshots/mobile-source-action.png`
- `screenshots/source-concept-reading-canvas.png` (visual-language reference)
