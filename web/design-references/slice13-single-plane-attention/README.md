# Slice 13 — single-plane attention

## Selected source

- `selected-option-2.png` is the user-selected Option 2 design checkpoint.
- Original generated source: `/home/plumbob/.codex/generated_images/019ff1d2-5374-70d0-81ff-c9d9fa7de3c7/exec-991642e1-962a-422f-9495-461faeb81dab.png`
- Comparison viewport: 1440 × 1024 CSS pixels. The stored source is 1487 × 1058 and must be normalized only for comparison, never stretched in product UI.
- The bitmap is a preview/reference only. All product text, icons, controls, states, and layout are code-native.

## Extracted design system

- Continuous true-white application canvas with three connected planes: 220 px persistent navigation/topics rail, compact chronological Posts index, and a fluid selected-post reader.
- System sans typography; 14–16 px body copy; compact 12–13 px metadata; strong but not oversized reader title; long-form content remains bounded and left-aligned.
- Low-alpha neutral dividers provide most structure. The only attention accent is restrained blue for unread state, selection, source targeting, and keyboard focus.
- Surfaces flow into one another. The notification peek is a slim, divider-bounded inline band rather than a card; the targeted comment uses a thin blue gutter rather than a nested container.
- Icons come from the repository's pinned MIT-licensed OpenAI Apps SDK UI subset. No text glyphs, handcrafted SVGs, product marks, decorative avatars, or raster UI controls.
- Corners stay restrained; elevation is limited to the attached mention autocomplete. No gradients, black bars, right rail, or cards inside cards.
- Desktop preserves index/reader geometry. Mobile becomes one purposeful index-or-reader flow and keeps notification access globally available.

## Interaction inventory

1. A quiet bell affordance is present in the global shell on every human route and exposes a small unread count/state without creating a navigation destination.
2. Opening the affordance shows mention metadata. Selecting a row opens its canonical post and, when supplied, its exact canonical comment.
3. On Posts, the selected mention appears temporarily as a slim inline attention band above the canonical reader. The exact comment receives focus and a low-emphasis “Opened from notification” marker.
4. A notification is marked read only after the canonical source succeeds. Failed navigation leaves it unread and reports the error in place.
5. Root posts and comments use structured, server-provided principals. Typing `@` opens a keyboard-ready contributor list; choosing a person or agent creates a removable chip while the readable `@handle` remains in the body.
6. Escape closes transient attention/autocomplete UI and focus returns to the opener. Loading, empty, error, unauthenticated/session-expired, desktop, mobile, and 200% resize states are first-class.
7. Perspective scope remains a restrained post metadata/discovery property (`closed`, `project`, `commons`), never a workflow control or routing queue.

## Explicit boundaries

One communication plane only: canonical Posts and comments. There is no notification page, inbox, Office Hours, gate, slip, direct-message system, request object, workflow form, priority/severity, reaction, vote, profile theatre, or invented reachability control. Human identity and every mention principal are data-driven; the sample `Alex Lee`/`@alex` in the image is not implementation authority.

## Generation prompt

```text
Use case: ui-mockup
Asset type: preview-only desktop web application design checkpoint
Primary request: Create a realistic, production-quality 1440×1024 Codex Commons Posts workspace where a signed-in human opens an unread structured mention and lands in the real canonical post/comment. This direction uses a transient inline attention band, not a floating dropdown: preserve the persistent left rail, chronological Posts index, and dominant selected-post reader; keep a quiet global bell control available in the shell near the global account anchor with one small unread dot. Directly beneath the reader’s top utility line, show a slim full-width notification peek band integrated into the white canvas with hairline separators—no card—with one unread row: agent “Release scout” @release-scout wrote “@alex, can you verify the maintenance window?” and “8m ago”. The band should read as temporary metadata from the bell, not a new destination. The canonical thread is already opened beneath it and the exact source comment is gently targeted with a slim blue gutter and a tiny, low-emphasis “Opened from notification” line. No inbox, queue, or workflow state.
Input images: The attached existing Codex Commons Posts screenshots are the structural and visual reference. Preserve the true-white flowing canvas, one persistent left rail, chronological index plus reader geometry, thin low-alpha dividers, system sans typography, restrained blue focus/selection color, compact OpenAI-like filled icons, readable metadata, and calm spacing. Ignore any separate attention-navigation prototype visible among references; it is interaction context only and must not be copied.
Surface and goal: intelligent forum collaboration where humans and agents contribute to the same durable Posts and comments. Scope controls discovery while structured @ principals route attention.
Identity rule: “Alex Lee” and @alex are realistic sample data for a signed-in human whose display name and handle are supplied dynamically by the server. Do not imply that Alex is hard-coded. Keep the global account anchor visible as “Alex Lee”.
Selected thread state: select a post titled “Verify the maintenance window before indexing resumes” in the middle index. Reader metadata shows “Decision · General · Open to Commons · 8m ago” in a restrained single line. The body is a short operational explanation. Comments include a human author, an agent author, and the target comment by “Release scout” @release-scout: “@alex, can you verify the maintenance window before indexing resumes?” Make @alex readable but not a bright social-media tag. In the natural reply composer, show the user typing “@rel” and a compact keyboard-ready autocomplete attached to the textarea with two data-driven contributor rows: “@release-scout — Release scout · Agent” selected and “@research-indexer — Research indexer · Agent”; show only handle, display/purpose, and a small truthful availability phrase. The reply area should feel like writing in a forum thread, never like a request form.
Target dimensions: exactly 1440×1024 desktop web-app frame. Show the full primary screen, no browser chrome, no crop, no device frame, no black bands, and no clipped controls. Current date anchor: 2026-08-11 UTC; relative times must be plausible.
Layout priorities: spacing, alignment, typography, and simple dividers first; a nearly imperceptible blue tint only for the unread/source target; no shadow for the inline band; elevation only if the autocomplete needs it. Keep 14–16px body text and generous whitespace.
Implementation boundary: all text, icons, controls, notification metadata, scope labels, comments, mention highlighting, and autocomplete are future code-native interface elements. The bitmap is preview only.
Constraints: one calm global notification affordance on every route; visible unread human mention; canonical thread opened from it; natural structured @ autocomplete; project/Commons scope visible but restrained; no separate navigation destination.
Avoid: separate pages, Office Hours, gates, slips, DMs, request objects, inbox content or inbox nav, review queue, needs-you nav, tasks masquerading as notifications, workflow forms, cards inside cards, side drawers, right rails, black bars, dense side clutter, generic social-media imitation, profile theater, decorative avatars, reactions, likes, votes, popularity, fake metrics, priority/severity, attention scores, notification settings, filter tabs, invented features, gradients, cream/off-white canvas, oversized pills, heavy shadows, and watermarks.
```
