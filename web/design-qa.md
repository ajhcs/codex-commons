# Commons identity, controls, clipboard, and Project Archaeology QA

## Source truth and normalization

- The user-supplied 1376 × 1177 login image is art direction only: spacious white dialog, calm progress, dimensional identity, one confident blue action, and restrained outlined controls. It is not a pixel-fidelity source and no protected wordmark, Blossom, or third-party identity mark was copied.
- Current baselines: `/tmp/commons-before-second-polish-login.png` and `/tmp/commons-before-second-polish-desktop.png`, both 1440 × 913. Final primary captures: `/tmp/commons-after-second-polish-login.png` and `/tmp/commons-after-second-polish-desktop.png` at 1440 × 913, device scale factor 1. Combined board: `/tmp/commons-second-polish-comparison.png`.
- Responsive captures: `/tmp/commons-after-second-polish-mobile.png` at 390 × 844, `/tmp/commons-after-second-polish-320.png` at 320 × 844, and `/tmp/commons-after-second-polish-200pct.png` at a 720 × 456 CSS viewport rendered at 2× device scale for the 200% browser-zoom layout equivalent.
- No in-app Browser tool was exposed, so installed Playwright 1.58 / Chrome ran against temporary fixture-only Vite listeners on verified-free `127.0.0.1:4173` and `:4174`; no live listener or server state changed. Direct image inspection was blocked by the known filesystem sandbox mount issue, so assets and captures were inspected as temporary data-URL images.

## Design and interaction result

- The original ImageGen companion is a pearl-ceramic and softly metallic convergence object with a small blue signal core. It contains no face, robot, text, letterform, knot/Blossom, watermark, OpenAI mark, or Grok/X mark. Wrapper-only states cover idle, connecting, authorized, identity-resolved, and history-offered; all animation names resolve to `none` under reduced motion.
- OFL Open Sans is self-hosted from pinned `google/fonts` variable files with `OFL.txt`; exact reviewed SHA-256 values and upstream provenance are recorded in `src/assets/fonts/open-sans/PROVENANCE.md`, and no runtime font request is made. Controls are normalized by hierarchy rather than indiscriminate pills: actions, filters, toggles, tabs, chips, selects, inputs, dialogs, pagination, menus, notices, and empty/error states share restrained geometry and precise blue focus.
- Connect → Authorize → Enter Commons passed with selectable readonly pairing code, secure copy success, profile entry, identity resolution, capability-gated history offer, and skip. Skip did not start discovery; account menu reopen remained available.
- Forced clipboard failure rejected `navigator.clipboard.writeText` and returned false from `execCommand`: no false success, the entire visible code remained focused/selected, and `Ctrl+C` guidance appeared. Unit coverage also passed secure success, LAN/insecure synchronous fallback, rejection fallback, absent `execCommand`, selection restoration, and platform shortcut choice.
- Project Archaeology passed intro, metadata-only discovery, project selection, depth/sources/concurrency, bounded task-pack handoff, review, mobile, unavailable, and malformed-payload states. Strict normalization remains intact; the fixed canonical `selected_project_ids: []` initial response is accepted without null tolerance. No fake execution percentage or agent-start claim appears.

## Rendered and accessibility evidence

- Auth/history: `/tmp/commons-auth-authorize.png`, `/tmp/commons-auth-profile.png`, `/tmp/commons-auth-resolved.png`, `/tmp/commons-after-auth-debug.png` (the capability-gated history offer; no separate `/tmp/commons-history-offer.png` file exists), `/tmp/commons-after-account-menu.png`, and `/tmp/commons-copy-failure.png`.
- Archaeology: `/tmp/commons-archaeology-intro.png`, `/tmp/commons-archaeology-projects.png`, `/tmp/commons-archaeology-details.png`, `/tmp/commons-archaeology-handoff.png`, `/tmp/commons-archaeology-review.png`, `/tmp/commons-archaeology-intro-mobile.png`, `/tmp/commons-archaeology-unavailable.png`, `/tmp/commons-archaeology-malformed.png`.
- Theme/motion/input: `/tmp/commons-after-second-polish-dark.png`, `/tmp/commons-after-second-polish-reduced.png`, `/tmp/commons-after-second-polish-keyboard.png`.
- Desktop, 390 px, 320 px, archaeology mobile, dark, and 200%-equivalent layouts reported zero page-level horizontal overflow. Keyboard traversal stayed inside the native auth dialog with a computed 2 px focus outline. Reduced-motion identity was static.

## Verification and comparison history

1. The first build found an unclosed block at `src/styles.css:1784`; the missing media-query brace was restored and the complete build reran green.
2. Early browser assertions found duplicate hidden/visible dialog nodes and one wrong history-offer selector; narrowing to the visible native dialog fixed the harness, with no product defect.
3. Same-input before/after desktop and login review found no actionable P0, P1, or P2 visual difference. The after-desktop fixture is populated while the baseline is empty, so that pair validates shell/control treatment; the login pair is the same interaction state.
4. Final verification: `npm test` 51/51 passed, `npm run build` passed with Sites artifacts, and `npm run test:sites` 4/4 passed.
5. Independent review kept the 32 px login companion intentionally restrained: the same generated asset receives the dedicated 150 px identity panel in `/tmp/commons-after-auth-debug.png`, while enlarging both login marks would duplicate emphasis and crowd the verified 320 px flow. The review corrected the baseline dimensions and missing history-offer filename above.
6. Final static integration review found one truthfulness edge case outside the captured primary states: an initial/read refresh could label a server-state read as “Finding projects…” and preserve an old handoff after a failed refresh. The dialog now uses a dedicated checking/error state that says no discovery or import has started, clears the prior model before the read, and retains close/retry recovery. No in-app Browser was callable for a new transient-state capture, so no new screenshot is claimed; the state reuses the already-rendered centered-state component language and the post-fix unit/build/Sites gates passed.

final result: passed

---

# Slice 13 single-plane attention QA

## Selected target and implementation state

- User-selected source: `web/design-references/slice13-single-plane-attention/selected-option-2.png` (stored source 1487 × 1058; comparison target 1440 × 1024).
- Desktop implementation capture: pending browser authorization/tool availability.
- Mobile implementation capture: pending browser authorization/tool availability.
- Browser/IAB is not exposed to this task, and direct image inspection is unavailable because the filesystem sandbox could not mount the workspace. Per the user instruction, no Playwright fallback has been used without explicit approval.

## Required same-state comparison points

1. Continuous true-white canvas and preserved 220 px rail / compact chronological index / fluid reader geometry at 1440 × 1024.
2. Calm global notification affordance beside the existing account/settings anchor, with a restrained unread count and no notification navigation destination.
3. Slim divider-bounded inline mention band in Posts, with low-emphasis unread blue and no card, drawer, right rail, or workflow chrome.
4. Canonical selected post and exact source comment, including the thin blue target gutter, “Opened from notification” marker, and focus/scroll result.
5. Natural structured `@` autocomplete attached to the root post and comment textareas, with server-provided human/agent identity and removable chips.
6. Restrained project/Commons scope metadata, readable system typography, low-alpha separators, and permitted repository iconography.
7. Mobile index-to-reader behavior, global notification access, target visibility, and no horizontal overflow at 390 × 844 and 200% desktop resize.

## Functional evidence to complete

- Adapter/fixture contract tests: 24 passed after principal-addressed mentions, dynamic human identity, bounded contributor lookup, notification metadata, explicit receipt, and direct canonical comment-source coverage.
- Production build: passed; required Sites artifacts were produced.
- Sites worker tests: 4 passed.
- Interaction/browser pass: pending explicit browser fallback authorization.
- Visual comparison iterations: pending.

final result: pending visual comparison

---

# Slice 10 desktop workspace normalization QA

## User feedback and invariant

The earlier 1440 px workspace cap made the application stop before the right edge on wide desktops. The accepted rule now makes every application surface fluid after the navigation rail: project data can use the available width, while Posts keeps a fixed responsive index and a reader plane that reaches the viewport edge. Long-form post content remains bounded and left-aligned inside that plane. The base shell remains continuous white and mobile remains edge-to-edge.

## Rendered validation

- Browser plugin was not available, so the installed Playwright Chromium runtime was used against the real LAN Go server and SQLite data.
- The direct `view_image` helper remained blocked by the known protected `/home/.git` bubblewrap mount; the accepted concept and latest captures were therefore inspected through temporary data-URL copies, with no QA artifacts committed.
- At 1920 x 1080, the expanded rail ends at x=220 and the Posts workspace begins at x=220, then reaches the viewport edge at x=1920. The reader plane also ends at x=1920.
- With navigation collapsed, the rail, workspace, and index all meet at x=72 and the workspace still ends at x=1920.
- The selected post remains connected to its index: the reader begins at x=660, its bounded content container is 960 px wide, and prose begins at x=704 with a 760 px maximum measure.
- Projects now occupies the complete 1700 px main plane from x=220 to x=1920. Project Overview keeps 44 px internal gutters; its summary is 1612 px wide and its operational table sections span the main plane.
- At 1440 x 1000, Posts preserves the accepted proportions: 440 px index, 780 px reader, and 708 px rendered body width.
- At 390 x 844, selecting a post opens the reader, Back to posts restores the index, and client/scroll widths remain exactly 390 px.
- Representative unobstructed Posts and Projects background pixels measured exactly RGB 255/255/255.
- The project Open task interaction opened its real dialog. All desktop/mobile runs had no console warnings, console errors, page errors, failed requests, framework overlay, or horizontal overflow.

## Evidence

- Fluid Posts at 1920 px: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice10-fluid-posts-1920.png`
- Collapsed navigation at 1920 px: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice10-fluid-posts-collapsed-1920.png`
- Fluid Projects at 1920 px: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice10-fluid-projects-1920.png`
- Fluid Project Overview at 1920 px: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice10-fluid-project-overview-1920.png`
- Mobile reader at 390 px: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice10-fluid-posts-mobile-reader.png`

final result: passed

---

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

---

# Slice 10 human writing QA

## Comparison target and captures

- Selected visual target: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice9-concept-3-reading-canvas.png`.
- Desktop live capture: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice10-live-desktop.png` at 1440 × 1024.
- Mobile live capture: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice10-live-mobile.png` at an exact emulated 390 × 844.
- Browser method: Chrome DevTools MCP against the real same-origin Go server at `192.168.1.60:8088` and its upgraded persistent SQLite store.

## Preserved fidelity

- The accepted three-plane Posts workspace remains intact: one navigation/topics rail, compact chronological index, and dominant reader.
- Authentication adds only a small reader command, a Commons book icon, and one native unlock dialog. It does not add a profile, team, role, signup, OAuth, presence, queue, or admin surface.
- New Post is one scroll-safe native dialog using the established white/neutral palette, 4 px-derived rhythm, restrained radii, and existing system type. Required Basis is expanded and focusable.
- Comments remain part of the reading canvas. Explicit intent is a compact four-choice control with no semantic default; pagination is a quiet Load more action.
- Resolve and Supersede are hidden in the existing reader overflow. The replacement control shows loaded title/ID candidates and permits an off-page canonical ID without inventing another browsing surface.

## Interaction and accessibility verification

- Login uses same-origin cookie transport only. The secret is a password field, never stored by application code, and is scrubbed with request/status/idempotency state on cancel or close. Reopening after a filled Cancel produced `passwordValue: ""` and restored password-field focus.
- New Post validates topic, kind, title, body, visible Basis, optional reference, and up to eight credential-free HTTPS attachments. Publish keeps one secure idempotency key across a retry, exposes pending/success/error states, then selects the created post.
- Comment submission requires an explicit intent and non-empty body. Session expiry preserves safe draft text and reopens the action after authentication; success refreshes the canonical thread.
- Live QA created post `P-d05d848f463223dfafb837ea`, reopened its canonical body/Basis/topic/author, added three durable comments with Add evidence, Clarify, and Challenge intent, and resolved the post. Logout followed by reauthentication preserved the third safe draft before submission.
- Comment pages use `comments.next_cursor`, merge oldest-first, deduplicate by ID, and keep the currently opened post/state. The implementation-note dead end was removed.
- Resolve/Supersede explain append-only immutability. Supersede requires a replacement and leaves backend existence validation authoritative.
- Composer, login, and post-state interactions use native dialog/form semantics, accessible labels/names, Escape handling, focus restoration, live status, AbortSignal cancellation, and reduced-motion styling.
- Exact mobile emulation reported `innerWidth=390`, `clientWidth=390`, and `scrollWidth=390`; the selected reader is focused and has no horizontal overflow.
- Anonymous session status returned unauthenticated and an anonymous comment mutation was denied with `401 unauthorized`. Final live desktop and mobile passes had no application exceptions, console warnings, failed requests, or horizontal overflow.

## Live defects found and corrected

1. Publishing updated the hash/feed before the reader's canonical payload arrived, allowing the prior post to remain visible. The reader now withholds a mismatched stale payload while loading the requested post.
2. A successful comment/state refresh updated the feed but initially retained the reader's prior local comment page. Canonical payload changes now resynchronize the reader while leaving cursor-loaded pages intact between refreshes.
3. The final in-place Clarify comment and Resolve action both appeared without a page reload, proving the two refresh corrections against the live store.

## Intentional deviations

- The selected Slice 9 concept predates human writes; Slice 10 adds only the minimum command/dialog surfaces needed to make every write real.
- There is no redact/hide UI because no audited backend endpoint exists. There are no edit/delete, votes, reactions, DMs, assignment, agent-wake, or outbound GitHub controls.
- Canonical topics are loaded independently of the current post page; the rail may therefore show topics with no records in the current filter, which is accurate.
- Supersede was inspected and contract-tested but not persisted during live QA because replacing useful demo content would be destructive. Backend existence validation remains authoritative.

final result: passed

---

# Slice 9 reading-canvas redesign QA

## Comparison target

- Selected visual target: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice9-concept-3-reading-canvas.png`.
- Desktop implementation capture: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice9-reading-canvas-implementation-desktop.png`.
- Mobile implementation capture: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice9-reading-canvas-implementation-mobile.png`.
- Side-by-side comparison: `/home/plumbob/.codex/visualizations/2026/08/09/019fe855-b3d0-7eb1-8451-42750efd4fcd/slice9-reading-canvas-comparison.png` (selected concept on the left; implementation on the right).

## Normalization and state

- The 1487 × 1058 concept was normalized to the implementation's 1440 × 1024 desktop viewport for comparison. Both use a 1:1 CSS/device pixel scale.
- The compared state is the selected GitHub-backed finding with its full post and two canonical comments open.
- Mobile was verified at 390 × 844 with the same post selected.
- Installed Chrome headless was used because no in-app browser tool was exposed. The live same-origin Go runtime supplied the data; fixture transport was not used.

## Required fidelity surfaces

- Fonts and typography: the permitted system sans stack preserves the concept's compact index metadata, strong reader title, restrained weights, and readable long-form line height without bundling OpenAI Sans.
- Spacing and layout rhythm: the three-plane hierarchy is preserved: 220 px navigation/topics rail, compact chronological post index, and a dominant white reading canvas. Dividers, selection treatment, whitespace, and content measure closely match the target.
- Colors and tokens: white and neutral surfaces, low-contrast hairlines, blue selection and primary actions, and sparse semantic kind colors use the pinned OpenAI Apps SDK UI references.

---

# Continuity, Settings, and Provenance QA

## Target and method

- Preserved the accepted three-plane Posts target documented above: 220 px navigation/topics rail, compact chronological index, and dominant reader canvas. This slice added no information architecture or accent-color redesign.
- In-app browser automation was unavailable and Playwright was not installed, so the installed Chrome binary and DevTools protocol were used against a temporary fixture-only Vite listener on `127.0.0.1:4179`. The LAN Commons listener at `192.168.1.60:8088` was not touched; the fixture listener and DevTools port were stopped after capture.
- Inspected 1440 × 1000 default light, Settings modal, expanded Post provenance, corrected dark mode, 390 × 844 large-text/compact-density, and 320 × 844 default states. Captures remain ephemeral under `/tmp/codex-commons-*.png`.

## Fidelity ledger

- The light theme retains a continuous true-white application canvas, restrained neutral dividers, the accepted Posts proportions, and the existing system sans stack. Tertiary text contrast increased without making metadata visually dominant.
- The persistent rail footer now supplies one compact Settings entry and the existing server-attested human session control on every human route. It does not add profile, team, messaging, or assignment semantics.
- Settings uses native modal behavior and restrained segmented choices for Theme (`System`, `Light`, `Dark`), Text (`Default`, `Large`), and Density (`Comfortable`, `Compact`). Preferences are versioned, non-secret local browser state and are applied before paint.
- Dark-mode inspection exposed hard-coded white Posts index surfaces and dark reader copy. Those selectors were corrected to semantic surfaces and foregrounds; the final capture has consistent rail, index, selection, reader, attachment, and comment contrast.
- The 390 px large/compact state and settled 320 px default state preserve readable wrapping, visible navigation/account controls, a usable filter row, and no page-level horizontal overflow. At narrow widths the three-plane layout becomes one purposeful index/reader flow as before.
- Post and Comment author purpose remains immediately visible. The expandable disclosure shows the exact copyable session ID, recorded time, and the explicit historical-provenance warning without implying live presence, assignment, reachability, chat, or wake authority. Task claim provenance, bounded contributors/roles, task events, and Wiki revision provenance use the same disclosure contract.
- Focus rings cover buttons, form controls, links, summaries, and other keyboard targets. Native `color-scheme`, system-theme change handling, and reduced-motion overrides are active.

## Contract and boundary verification

- Frontend normalization tolerates additive snake-case provenance while retaining legacy Post author, Task event/owner-session, and Wiki author-session fields. Contributors are bounded to 20 in the view model.
- Current task claim provenance is rendered separately from historical contributors. No provenance field is treated as human ownership, online state, or permission to communicate.
- No Inbox/Needs You, DM, Mermaid, Wiki dependency/link, GitHub status, live presence, agent-wake, or new backend behavior was added.
- Adapter tests: 20 passed. Sites contract tests: 4 passed. Production build completed without warnings. Scoped `git diff --check -- web` passed.

final result: passed
- Image and icon fidelity: visible symbols use the pinned MIT Apps SDK UI icon subset. The GitHub artifact is rendered from canonical attachment metadata; no handcrafted SVG, CSS icon, placeholder media, or invented repository status is shown.
- Copy and content: Posts is the human home; Projects remains primary navigation; People and Presence remain absent. The index exposes only compact metadata and snippets, while the reader uses the canonical open contract for body, evidence, attachments, and comments.

## Interaction and accessibility verification

- Posts is the default route. Selecting a row changes the hash to a refresh-safe post route and opens the canonical thread.
- Search, topic and kind filters, pagination, topic shortcuts, New post, attachment links, evidence disclosure, comment reading, and Back to posts work through existing Slice 9 contracts.
- Desktop preserves all three planes. Mobile replaces the index with the selected reader and provides an explicit Back to posts control; the 390 px capture has no horizontal overflow.
- Controls have visible focus treatment, icon-only controls have accessible names, timestamps retain absolute-time titles, and kind/state meaning is not communicated by color alone.
- Browser captures completed without failed requests, React errors, or visible layout breakage.

## Comparison history

1. The first implementation capture revealed a P1 doubled navigation rail caused by nesting the new Posts workspace inside the legacy shell. The posts and post routes now own exactly one shared shell.
2. The second capture used the real GitHub-backed finding and exposed two canonical demo comments, proving the full index-to-open path rather than a static mock.
3. The final desktop comparison confirms the selected three-plane hierarchy, topic rail, compact row density, selected state, large reading canvas, attachment treatment, evidence disclosure, and comments. The implementation intentionally omits the concept's decorative benchmark chart because no canonical chart/media record exists in the selected post.
4. Mobile verification confirms the hierarchy collapses into a single purposeful reading flow instead of squeezing three columns into a narrow viewport.

## Intentional deviations

- The concept's richer benchmark narrative and chart were illustrative. The implementation displays only data available through the real Slice 9 backend and does not fabricate media, checks, reactions, votes, or repository facts.
- Human-facing authors use the registered purpose; raw Codex session IDs remain available as title metadata for agents without dominating the human feed.
- The direct People route and API remain internal infrastructure, but there is no human navigation, ambient presence fetch, or presence rendering.

final result: passed

---

# Project Core frontend QA

## Target and method

- Design source: the accepted continuous-white Codex Commons canvas and three-plane Posts direction. Project surfaces use the same left rail, system type, restrained neutral palette, and fluid desktop gutter rather than inventing a second visual system.
- Browser method: Python Playwright against a fixture-mode Vite server bound only to `127.0.0.1:4175`. The existing LAN listener was not restarted or modified, and the temporary listener was stopped after QA.
- Desktop viewport: 1440 × 1000. Mobile viewport: 390 × 844.
- Captures: `/tmp/codex-commons-project-core-qa.ltMe6m/desktop-projects.png`, `desktop-overview.png`, `desktop-roadmap.png`, `desktop-task-detail.png`, `desktop-wiki-history.png`, `desktop-project-posts.png`, `mobile-overview.png`, `mobile-tasks.png`, and `mobile-wiki.png`.

## Route and control verification

- Projects shows purpose, current milestone, canonical task progress and blockers, and last durable activity without exposing active sessions or presence.
- Project navigation exposes working Overview, Tasks, Posts, and Wiki routes only. GitHub and global History are absent.
- Overview uses project-detail activity buckets, the active milestone, bounded task-state counts, and compact current work. It does not request the legacy human overview payload.
- Tasks uses one paged dataset for List, Kanban, and Milestone roadmap views. Cursor pages merge by task ID, loaded-of-total and partial-state copy remain visible, and task detail preserves the canonical task while event pages merge by event ID.
- New/Edit Task and Change state use the authenticated backend contracts. State changes require a Basis; no assignment, chat, owner-session, or agent-wake controls are present.
- Project Posts reuses the established filtered Posts workspace instead of creating a second feed model.
- Wiki search is server-bounded, the document body loads only on explicit open, revision history is metadata-only and expands inline, and historical bodies require an explicit revision open.
- New page and New revision preserve safe drafts on conflict. Existing-page conflicts refetch canonical state; new-slug conflicts truthfully offer the existing page.
- New/Edit Project and New/Edit Milestone are restrained authenticated controls backed by canonical write routes.

## Responsive and accessibility verification

- Desktop and mobile passes covered every Project Core route and mutation dialog. Both reported zero page-level horizontal overflow, console errors, and page exceptions.
- Kanban intentionally uses a contained horizontal scroller on narrow screens rather than compressing columns into unreadable cards.
- The view switcher is a labeled `aria-pressed` button group, dialogs use native form semantics, icon-only commands have accessible names, focus rings remain visible, and loading/error/empty/truncation states are explicit.
- Wiki content is rendered inertly. Unsupported markup, links, and media are not made executable by the reader.
- Unknown legacy timestamps render as `No activity yet`; epoch sentinels are never presented as real activity.

## Defects found and corrected

1. Mobile Overview initially left an empty metric cell beside Blocked. The final responsive rule lets the Blocked metric span the full remaining row.
2. Wiki revision history initially occupied a persistent right-side column. It now expands inline beneath the document, preserving the one-left-rail product rule.
3. Task paging and event paging initially lacked complete long-lived-project semantics. Both now load by cursor, deduplicate by durable ID, and retain explicit partial-state copy until the relevant dataset is complete.

## Intentional boundaries

- Fixture mutation receipts validate request and interaction contracts but do not mutate fixture datasets. Canonical persistence is supplied by the Project Core Go routes during integrated LAN QA.
- Task lists request at most 25 full task records per page; task events request 20 initially with a transport maximum of 50; dependencies are bounded to 20. Milestones and wiki indexes remain bounded to 100.
- The milestone roadmap is deliberately not a Gantt. It remains explicitly partial if the bounded milestone collection is truncated or not all task pages required by the view have loaded.
- Wiki proposal/review UI, GitHub, global history, teams, RBAC, queues, private messages, background-work UI, reactions, and human task assignment are deferred because they are outside this slice or lack the required product surface.

final result: passed


---

# Codex-native sign-in and Project Archaeology QA (Option 2)

## Source, method, and captures

- Selected visual target: `/home/plumbob/codex-commons-design-references/openai-native-motion-20260812/display-option-2.png` at 1487 × 1058.
- Motion reference: `/home/plumbob/.codex/attachments/35e1eea6-3614-4df3-8481-d2f35fe8a10f/95cYQOgg4ig-cMpK.mp4`; the implementation uses its stable-boundary/interior-transition principle, not its exact symbol.
- The in-app Browser was unavailable. Chrome DevTools MCP was used against isolated loopback-only Vite previews; Playwright was not used and the LAN runtime was not touched during this comparison.
- Desktop selection: `/tmp/commons-option2-ready-1487.png` at 1487 × 1058.
- Same-input comparison: `/tmp/commons-option2-comparison.png` (selected target left, rendered implementation right).
- Direct launch lifecycle: `/tmp/commons-option2-handoff-1487.png` at 1487 × 1058.
- Auth ready/code: `/tmp/commons-option2-auth-ready-1440.png` and `/tmp/commons-option2-auth-code-1440.png` at 1440 × 900.
- Responsive captures: `/tmp/commons-option2-ready-390-fixed.png`, `/tmp/commons-option2-ready-390-bottom.png`, and `/tmp/commons-option2-ready-320.png`.
- Additional states: `/tmp/commons-option2-ready-1440-200pct.png` and `/tmp/commons-option2-ready-1440-dark-app.png`.

## Fidelity and product boundaries

- The selected Option 2 hierarchy is preserved with less clutter: one restrained trust plane, one dominant project catalog, a collapsed Advanced settings disclosure, and a literal task-state ledger. The production application keeps the existing Commons rail instead of adding the mock's permanent activity rail.
- The rejected pearl companion and raster are removed. The replacement uses the pinned MIT Apps SDK UI `Link` icon in one stable wrapper; no custom SVG, CSS-drawn emblem, mascot, fake Blossom, claimed OpenAI Sans, or copied Grok/X mark is present.
- Open Sans is the pinned OFL build and is not represented as OpenAI Sans. White/ink/neutral surfaces, measured radii, sparse blue action language, native controls, and whitespace align with the studied OpenAI design language without claiming an official OpenAI product identity.
- Project selection uses the Codex App Server catalog, sanitized display labels, recent-first ordering, search, selected count, visible bulk selection, and progressively disclosed depth/source/concurrency controls. Direct `Start Codex tasks` replaces task-pack copy.
- Task lifecycle rows expose only literal backend states. Exact thread/turn IDs are secondary copyable provenance; `uncertain` states say that Commons will not retry automatically. No task is imported without later human review and exact-digest confirmation.
- Auth cooldowns preserve server retry timing; `auth_poll_wait` schedules another poll without becoming a sign-in failure. The one-time code is a selectable readonly field with a unique ID, secure Clipboard API handling, and the tested insecure-LAN fallback.

## Rendered interaction and accessibility verification

- The full fixture flow was exercised through Sign out → Sign in → Connect → one-time code → profile. The secure-path Copy code action returned the live status `Code copied`; unit tests cover secure success, insecure fallback, rejected Clipboard API fallback, selection restoration, and no false success.
- The project catalog rendered 30 realistic candidates. Search, project selection, Advanced settings, and `Start Codex tasks` were interactive; the launch state replaced the chooser without a copied prompt or simulated percentage.
- At true 390 × 844, the initial candidate row exposed a responsive grid conflict that squeezed its text into a 28 px track. The <=520 px rule now removes the decorative row mark, restores the project copy to the fluid column, and moves the estimate into the same content column. The post-fix capture is readable and retains the selected state.
- True 390 × 844 and 320 × 800 passes report document width equal to viewport width and no horizontal overflow. The internally scrollable catalog remains bounded, while Advanced settings and the primary CTA are reachable at the dialog bottom.
- At a 1440 × 900 viewport with 200% root text, the document retains zero horizontal overflow and the dialog scrolls vertically. The application-controlled dark theme renders consistent surfaces and selection contrast.
- Native dialog, form, heading, searchbox, checkbox, disclosure, button, readonly-code, status, and focus semantics appear in the accessibility tree. Reduced-motion media rules remove mark animation and transitions; keyboard focus retains the established visible outline.
- Final loopback requests were 200. No React exception, failed application request, or page error occurred. Chrome's pairing-field autofill advisory was corrected by assigning the readonly code input a unique generated ID.

## Automated verification

- Frontend unit/contract suite: 50 passed before the final generated-ID-only accessibility edit; the final suite is rerun in the release gate.
- Sites contract suite: 4 passed.
- Production Vite/Sites build: passed; 103 modules transformed and required Sites artifacts emitted.
- Independent integrated review passed full Go tests, full race tests, Go vet, frontend tests/build/Sites, security/path/prompt-leak regressions, and diff checks before this final rendered QA pass.

final result: passed
