# Design QA — Prototype A

## Visual truth and test conditions

- Visual-system reference: `screenshots/source-concept-reading-canvas.png`, the
  accepted Slice 9 reading-canvas direction. This is a language reference, not
  a one-to-one screen specification.
- Live product grounding: the current Codex Commons LAN app at
  `http://192.168.1.60:8088/`, including the real Post, Task, and Wiki IDs used
  in the prototype.
- Implementation captures:
  - `screenshots/desktop-needs-you.png`
  - `screenshots/desktop-agent-contract.png`
  - `screenshots/mobile-needs-you.png`
  - `screenshots/mobile-source-action.png`
- Verified viewports: desktop 1440 × 913 at DPR 1; mobile 390 × 844 at DPR 1;
  narrow mobile 320 × 780 at DPR 1.
- Chrome DevTools was used as the browser fallback. `view_image` was attempted
  for both the reference and implementation, but the environment returned
  `bwrap: Can't mkdir /home/.git: Permission denied`. The reference and final
  renders were instead inspected inline in Chrome.

## Comparison findings

1. **Layout:** retained the accepted continuous white canvas, thin vertical
   separators, 220 px Commons rail, and calm reader hierarchy. Prototype A adds
   a 440 px source-reference index between navigation and the fluid reader.
2. **Typography:** uses the existing Commons font stack, restrained weights,
   sentence-case labels, and a single large route question rather than a dense
   dashboard hierarchy.
3. **Color and elevation:** white remains the dominant surface; selection uses
   a quiet blue tint; Post, Task, and Wiki accents only identify source type.
   Elevation is reserved for the bounded action form and modal.
4. **Iconography:** reuses the repository's existing Commons icons. Icons label
   source kind or action and do not imply severity, presence, priority, or an
   unavailable capability.
5. **Spacing:** list rows scan compactly while the source reader retains the
   accepted generous reading measure. Hairlines, 16/24 px content insets, and
   aligned metadata keep density consistent.
6. **Responsive behavior:** at 390 px the route index occupies the full width;
   selecting a route replaces it with a reader and a clear `Needs you` back
   control. The source action stays within 16 px margins. At 320 px, document,
   body, action, and choice-row widths all remain overflow-free.
7. **Interaction:** inspect scrolls to the canonical source; accept exposes only
   a source-native Post/Task/Wiki action; defer suppresses re-routing; dismiss
   captures a bounded noise reason; successful local actions create a receipt
   and clear the route.
8. **Boundary communication:** desktop visibly reads `Not live chat · no
   message body lives here`; historical session provenance explicitly says it
   is not live, reachable, assigned, or a chat control.

## Findings history

- Initial icon imports expected global React and rendered a blank page. Fixed by
  exposing the imported React runtime before reused Commons icons render.
- The first detail toolbar did not distinguish the concept strongly enough from
  chat. Added the persistent `Not live chat` line without changing hierarchy.
- The second-inbox risk was initially implicit. Added a visible risk band and a
  falsifiable 12-route/14-day delete rule.
- A per-run route cap did not bound concurrent agents. Added a hard cap of three
  open slips per project/human; candidates over the cap stay at their source.
- Agent authorship could be confused with preserved human decisions. Added a
  server-attested agent-identity rule and an explicit prohibition on
  impersonating human judgment.
- Form validation remained visible after correction. Input/choice changes now
  clear the stale error; Post, Task, and Wiki resolution flows were re-run.
- A favicon request produced the only console error. Replaced it with an inline
  data favicon; the final console pass is clean.

## Copy and capability fidelity

Above-the-fold copy intentionally differs from the visual reference because
this is a distinct concept test: `Needs you`, `Source-linked route`, the bounded
requested judgment, and `Not live chat` are required to explain the hypothesis.
All Post, Task, Wiki, project, and provenance content is grounded in the real
Commons dogfood corpus. The prototype labels its actions local-only and does not
claim production writes, DMs, wakeups, assignments, polling, or live-session
reachability.

## Functional verification

- Production build completed with no warnings; final Chrome console has no errors, warnings, or issues.
- Lighthouse mobile snapshot: Accessibility 100 and Best Practices 100. SEO 60 and Agentic Browsing 50 reflect intentionally absent crawler metadata (`meta description`, `robots.txt`, and `llms.txt`) in this isolated Vite lab, not a production surface.
- Post explicit-intent resolution: passed.
- Task outcome plus Basis resolution: passed.
- Wiki proposal outcome plus Basis resolution: passed.
- Defer and suppression disclosure: passed.
- Dismiss with bounded reason: passed.
- Desktop horizontal overflow: none at 1440 px.
- Mobile horizontal overflow: none at 390 px or 320 px.
- Mobile index → source → action → back flow: passed.
- Historical session non-reachability disclosure: present in source provenance.

## Final result

**Passed.** No unresolved P0–P2 visual or functional findings. Intentional
deviation: this remains an isolated, removable local-state prototype; live
source links are read-only and no production schema or backend behavior was
introduced.
