# Project Office Hours QA

Verified 2026-08-10 against the isolated prototype at `127.0.0.1:4186`. No production route, runtime, database, or server was touched.

## Acceptance

- Desktop: 1440 × 1024, DPR 1.
- Mobile: 390 × 844, DPR 2, mobile and touch emulation.
- Build: `npm run build` passed with Vite 6.4.2 (27 modules).
- Mobile Lighthouse snapshot: Accessibility 100; Best Practices 100. The non-product SEO/agentic misses were only the isolated dev server's absent metadata, `robots.txt`, and `llms.txt`.
- Browser console: zero errors, warnings, or issues after a clean reload.
- Network: all 13 local runtime/module requests returned 200; no external request or mutation occurred.
- Horizontal overflow: desktop 1440/1440; mobile agenda, detail, source modal, and receipt 390/390.

## Core-flow verification

| Flow | Result |
| --- | --- |
| Agenda → item detail | Pass; mobile uses a deliberate agenda/detail transition and a working Review brief back control |
| Open source | Pass; opens an explicit modal labeled Sandboxed canonical source |
| Why these three? | Pass; exposes all five routing outcomes and their timing/noise boundaries |
| Empty submission | Pass; requires both a destination and a substantive response unless Leave unresolved is chosen |
| Response → Post | Pass; typed response plus explicit Post choice yields a prototype-only receipt |
| Leave unresolved | Pass; yields a no-write receipt and retains one canonical open record |
| Next item | Pass; advances to the next open item |
| Reset | Pass; clears local prototype state and states that no durable record changed |
| Mobile end-to-end | Pass; agenda → source → response → receipt completed at true 390 px with no overflow |

## Visual fidelity ledger

| Criterion | Reference comparison | Result |
| --- | --- | --- |
| Copy and hierarchy | Commons uses a terse page title, small metadata, and one dominant reading plane | Matched; Office Hours adds only the milestone/budget context required by this concept |
| Layout and containers | Persistent left rail, narrow index plane, broad detail canvas | Matched on desktop; collapsed into agenda/detail states on mobile |
| Typography | System sans, restrained weights, compact metadata, readable document title | Matched; no decorative or competing type family |
| Palette | True-white canvas, quiet gray dividers, black text, OpenAI-like blue focus/action | Matched; no dark bars, card wallpaper, gradients, or ornamental color |
| Icons | Existing Commons/OpenAI Apps SDK icon shapes | Matched; the document icon is imported from the existing Commons source rather than approximated |
| Spacing and responsive behavior | Dense but calm desktop; touch-safe mobile controls | Matched; no clipped copy, horizontal scroll, or accidental whitespace gutters |
| Interaction | Explicit opens and writes, no hidden automation | Matched; no default durable destination, auto-write, auto-refresh, polling, or wake control |

The accepted Posts reading-canvas reference and final 1440 × 1024 prototype screenshot were inspected together. The final composition preserves the Commons rail, continuous white canvas, typography, divider density, icon treatment, and blue selection language while changing only the center structure needed for a bounded review brief.

## Agency signoff

Pass for concept evaluation. The human can inspect the evidence, choose the destination, leave an item unresolved, or exit without creating anything. Codex cannot claim a real source route or durable write in this prototype, and the system contract requires any future Codex write to use a server-attested agent identity without impersonating a human decision maker.

## Falsification

Delete this view if it becomes another place to check, routinely surfaces stale or duplicate questions, obscures urgent blockers, or needs more explanation than the project judgment itself. The routing contract can survive without a new production object or migration.

## Artifacts

- `artifacts/desktop.png`
- `artifacts/mobile.png`
- `artifacts/mobile-receipt.png`
