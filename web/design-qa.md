# Slice 7 and 8 frontend design QA

## Comparison target

- Source visual truth:
  - `/mnt/d/Coding Projects/CodexCommsMockups/Commons_Expanded.png`
  - `/mnt/d/Coding Projects/CodexCommsMockups/ProjectsPage.png`
  - `/mnt/d/Coding Projects/CodexCommsMockups/People.png`
  - `/mnt/d/Coding Projects/CodexCommsMockups/ProjectOverview_Section.png`
- Browser-rendered implementation:
  - `/tmp/commons-attention-contract-final.png`
  - `/tmp/commons-projects.png`
  - `/tmp/commons-people-final2.png`
  - `/tmp/commons-people-modal-final2.png`
  - `/tmp/commons-project-priority-final.png`
- Full-view comparison evidence:
  - `/tmp/qa-attention-contract-final.jpg`
  - `/tmp/qa-projects.jpg`
  - `/tmp/qa-people.jpg`
  - `/tmp/qa-people-modal.jpg`
  - `/tmp/qa-project-priority-final.jpg`
- Focused comparison evidence:
  - `/tmp/qa-focus-controls.jpg` compares the dense filter/control row.
  - `/tmp/qa-focus-metrics.jpg` compares the activity visualization and metric strip.

## Normalization

- Desktop viewport: 1487 × 1058 CSS pixels, device scale factor 1.
- Expanded, Projects, and People sources: 1487 × 1058 pixels.
- Project Overview source: 1493 × 1054 pixels; normalized to 1487 × 1058 for the comparison only.
- Main implementation captures: 1487 × 1058 pixels.
- The Chrome DevTools modal capture was 1487 × 915 because its headless surface excluded browser-owned vertical space. It was padded with white to 1487 × 1058 for composition comparison; the app pixels were not scaled.
- State: light desktop, fixture transport, first bounded page. People was checked both with and without the first session modal open.
- Browser method: installed Chrome headless was used because an in-app Browser tool was not exposed to this subtask.

## Required fidelity surfaces

- Fonts and typography: the permitted system sans stack is used throughout. Page titles, 14 px operational text, 12 px metadata, weights, line heights, tabular timestamps, wrapping, and truncation match the source hierarchy without bundling OpenAI Sans.
- Spacing and layout rhythm: the implementation preserves one left rail, one dominant white plane, thin section dividers, table-first layouts, 4 px-derived spacing, restrained radii, and overlay-only elevation. The refined controls have stronger internal hierarchy and focus treatment than the source's generic controls, as requested.
- Colors and tokens: true white, neutral gray surfaces, low-alpha hairlines, blue focus, and restrained semantic red/blue/green pass visual inspection. Severity always includes a textual label and a shape/glyph, not color alone.
- Image and icon fidelity: the screens require no raster product imagery. All visible UI icons are copied generic sources from the pinned MIT-licensed Apps SDK UI subset, with its license preserved. No proprietary mark, OpenAI font, handcrafted SVG, CSS icon, or placeholder asset is used.
- Copy and content: `Posts` replaces `Topic`; queues, Review Queue, Background Work, recent wiki, durable decision, right rail, profile/team semantics, and unsupported Open Session are absent. Execution and host connectivity remain separate facts. GitHub rows without a canonical destination are disabled rather than made to look actionable.

## Interaction and accessibility verification

- Primary rail navigation moved between General, Projects, and People and maintained the current-page state.
- The custom Source select opened, exposed listbox semantics, selected GitHub checks, and reduced the visible rows to four.
- The first session opened a native modal dialog for SES-4182. Close and Copy Session ID are keyboard-focusable; no unsupported Open Session action is offered.
- Search, filters, date filter, page size, previous/next cursor controls, typed actions, empty/loading/error table states, and reduced-motion CSS paths are implemented.
- Bounded owner/project attention facets use the backend's `owners_truncated` and `projects_truncated` flags; truncated menus say that only the first 50 values are shown and never derive missing options from the current page.
- Fixture adapter queries now use the exact HTTP contract names: `q`, `updated_from`/`updated_to`, `host_connected`, opaque `cursor`, bounded `limit`, and the two overview limits.
- Focus-visible rings, explicit labels, semantic tables and headings, title-backed absolute timestamps, and non-color status text are present.
- Chrome console logging found no application exceptions or React errors.

## Comparison history

1. Initial full-view comparison found a P1 contract mismatch: fixture GitHub rows exposed destinations even though the backend omits untrusted GitHub destinations until persistence exists. The fixtures now omit those destinations and the controls render disabled.
2. Initial Project Overview comparison found a P2 completeness mismatch: only two of the bounded three attention-preview rows rendered. A third task-backed attention item was added and the revised capture shows all three.
3. Final HTTP reconciliation removed human-name/profile assumptions, excluded the unsupported task `review` state, adopted the approved bounded Attention `q` contract, and added truthful 50-value facet truncation metadata.
4. Revised full-view and focused comparisons found no remaining actionable P0, P1, or P2 differences.

## Intentional deviations and follow-up polish

- The independent Commons name and generic book symbol replace the protected product name/mark.
- The human profile/team footer is removed because no human identity contract exists.
- Inactive project tabs are text, not fake controls; their routes can become interactive only when their slices exist.
- Project merged-PR count truthfully displays unavailable until persisted GitHub synchronization exists.
- Sample labels and timestamps follow current Slice 7/8 contracts rather than duplicating unsupported mockup records.
- P3: the implementation rail is slightly narrower than the source and leaves more room for operational tables. This is consistent across the four screens and does not affect hierarchy or use.

final result: passed
