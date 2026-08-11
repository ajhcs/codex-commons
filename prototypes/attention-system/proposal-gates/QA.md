# Visual and interaction QA

## Source and method

- Accepted visual system: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice9-concept-3-reading-canvas.png` plus `web/src/styles.css` and the current AppShell/Posts components.
- The concept screenshot was inspected at its native `1487 × 1058` size in Chrome DevTools.
- The prototype was rendered directly from `index.html` with no network or server. It was inspected at `1487 × 1058`, `1440 × 1024`, and mobile `390 × 844`.
- The dedicated `view_image` helper was attempted on both the accepted concept and `/tmp/proposal-gates-desktop.png`, but the environment's known protected `/home/.git` bubblewrap mount failed before either image could load. Both images were instead opened and directly inspected at native size through Chrome DevTools; this is the one verification-method deviation.

## Captures

- `screenshots/proposal-gates-desktop.png` — open-gate index and selected decision at `1440 × 1024`.
- `screenshots/proposal-gates-mobile-index.png` — mobile gate index at `390 × 844`.
- `screenshots/proposal-gates-mobile-detail.png` — mobile gate detail at `390 × 844`.

## Fidelity ledger

| Comparison point | Concept evidence | Render evidence | Result |
| --- | --- | --- | --- |
| Container model | One persistent left rail, compact chronological index, dominant reader; continuous white canvas | 220 px rail, responsive 360–460 px gate index, fluid decision canvas with bounded copy | Matched; no right rail, dashboard cards, or outer band |
| Typography | System sans, compact metadata, strong reader title, restrained weights | Same stack/tokens; 14 px body, 11–12 px metadata, 36 px desktop decision title | Matched hierarchy without protected fonts |
| Palette and borders | True white, neutral hairlines, sparse blue focus/selection | Exact Commons token values copied into the isolated prototype | Matched; sandbox notice is the only functional blue tint |
| Spacing and density | Dense index with generous reading canvas | 17–20 px row rhythm and 38–56 px reader gutters | Matched the reading-canvas relationship while leaving the deliberation form breathable |
| Selection behavior | Index selection is tied to the open reader | Selecting any gate updates the reader; mobile swaps index for detail and exposes Back to gates | Matched and interactive |
| Source treatment | Full objects require an explicit open | Open source preview uses a native modal and names the exact dogfood key or reviewed import source | Preserved explicit-open and provenance boundaries |
| Responsive behavior | Three planes collapse into one focused mobile flow | `390 × 844` index/detail states had `clientWidth = scrollWidth = 390` | Passed; no horizontal overflow |
| Motion and focus | Restrained control motion and visible keyboard focus | Native controls, two-pixel focus ring, reduced-motion override | Passed |

The above-the-fold copy inventory was limited to the assigned product brief, dogfood/historical source wording, the functional sandbox notice, and routing labels needed to operate the prototype. No marketing claims, fake metrics, unread counts, urgency labels, or fabricated record IDs were added.

## Interaction verification

- Selected the boundary gate and opened its exact source preview.
- Expanded the reason the item was routed here instead of chat.
- Selected “Expand Commons for larger-project collaboration,” entered a required basis, and verified Preview stayed disabled until both were present.
- Previewed the three durable operations and recorded a simulated receipt.
- Opened the Agent policy modal and verified all six routes plus hard caps were keyboard-accessible.
- Switched between Open and Completed views.
- On mobile, selected a gate, returned with Back to gates, and verified no horizontal overflow.
- Chrome reported no console warnings or errors.

## Intentional deviations

- This is a structural prototype, so no ImageGen concept was created. The already accepted Commons reading canvas is the visual target; the new work changes the interaction model rather than the brand.
- The gate is not a production object and does not fabricate persistence. “Record simulated receipt” changes only in-memory state and explains the existing Commons writes that would be required.
- No icons were introduced where the prototype could communicate cleanly with text. This avoids inventing or approximating visual assets.

Within the `view_image` tooling limitation above, the implementation was faithfully verified against the accepted Commons visual system. No material visual or interaction mismatch remains in the tested states.
