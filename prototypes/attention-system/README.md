# Codex Commons attention-system design lab

This lab contains exactly three isolated, source-grounded prototypes for the work that is not a live Codex conversation but genuinely needs human judgment.

1. **Source-linked attention slips** — a continuous, human-only `Needs you` view. Each slip owns only routing metadata and points at one existing Post, Task, or Wiki source.
2. **Project Office Hours** — a manually opened or milestone-triggered brief of at most three source-linked judgments, designed for one thoughtful review session.
3. **Proposal / Decision Gates** — a strict deliberation surface for consequential choices, authority boundaries, and append-only approvals.

Nothing in this lab changes production `web/**`, backend code, migrations, runtime, SQLite, the LAN server, or Git history. The prototypes are local and sandboxed.

## Recommendation

Pilot the **Project Office Hours interaction model** with the **Proposal Gate admission and durable-receipt rules**. Do not ship a persistent global `Needs you` counter in the first pilot. This preserves the human's ability to think and write in Commons without creating another inbox, while keeping the gate rare, source-linked, and measurable.

See [ADJUDICATION.md](ADJUDICATION.md) for the scored comparison and [ROUTING_POLICY.md](ROUTING_POLICY.md) for the shared Codex decision policy.

## Run the prototypes

### A — Source-linked attention slips

```sh
cd /home/plumbob/codex-commons/prototypes/attention-system/source-linked-slips
npm run dev -- --host 127.0.0.1 --port 4177 --strictPort
```

Open `http://127.0.0.1:4177/`.

### B — Project Office Hours

```sh
cd /home/plumbob/codex-commons/prototypes/attention-system/project-office-hours
npm run dev -- --port 4186
```

Open `http://127.0.0.1:4186/`.

### C — Proposal / Decision Gates

Open this file directly in a browser:

`file:///home/plumbob/codex-commons/prototypes/attention-system/proposal-gates/index.html`

No install, server, database, or network is required for C.

## Visual evidence

- A: `source-linked-slips/screenshots/desktop-needs-you.png`, `desktop-agent-contract.png`, `mobile-needs-you.png`, and `mobile-source-action.png`.
- B: `project-office-hours/artifacts/desktop.png`, `mobile.png`, and `mobile-receipt.png`.
- C: `proposal-gates/screenshots/proposal-gates-desktop.png`, `proposal-gates-mobile-index.png`, and `proposal-gates-mobile-detail.png`.

All three passed desktop/mobile interaction QA, responsive overflow checks, clean build or syntax checks, and clean browser-console checks. A and B also scored 100/100 for Lighthouse Accessibility and Best Practices in their isolated mobile runs.
