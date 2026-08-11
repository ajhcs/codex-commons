# Codex Commons dogfood content

## Decision context

The first useful Commons workspace is Codex Commons itself. Demo records made
the human experience difficult to judge, while building a wider agent bridge
against an unvalidated workspace would couple two uncertain interfaces. The
dogfood bootstrap therefore creates one real project through the same HTTP API
the future bridge will use.

This is a worked example, not a required project template. Empty surfaces are
valid. The initial corpus stays deliberately small, and a record earns its
place only when it helps a human manage the project or changes a fresh Codex
task's next action.

## Content boundaries

- A **milestone** names an outcome boundary, not a bucket for historical slices.
- A **task** is future work with an observable acceptance condition.
- A **Wiki page** states current, revisable truth.
- A **post** records a dated decision, finding, notice, or open question whose
  discussion should remain visible.

The bootstrap corpus contains three milestones, three tasks, four Wiki pages,
and three posts. It intentionally omits exhaustive slice history, routine status,
session chatter, speculative features, invented deadlines, and records whose
only purpose would be to make the interface look populated.

## Why each record exists

| Record | Decision or action it changes |
| --- | --- |
| Human workspace foundation | Makes the already-proven human workspace a completed boundary instead of repeating its implementation history as tasks. |
| Dogfood one real project | Directs current work toward validating the product through use. |
| Agent-assisted pilot | Keeps bridge and pilot work visible without treating it as current or complete. |
| Evaluate Codex Commons through real work | Gives the human a concrete first action and a standard for useful feedback. |
| Bring README current with the running product | Removes a known orientation error before the agent bridge depends on repository context. |
| Specify the minimum Codex bridge contract | Stops implementation until observed human use and the low-context contract agree. |
| Start here | Gives either consumer the shortest honest orientation. |
| Product model | Assigns one responsibility to each surface and identifies deliberate exclusions. |
| Agent operating contract | Defines sparse, event-triggered agent behavior and the authority boundary. |
| Architecture and operations | Lets a fresh engineer reason about persistence, runtime, auth, and deferred wiring. |
| Codex remains the conversation and control plane | Prevents Commons from drifting into chat, assignment, or orchestration. |
| Dogfood one real project before widening the bridge | Records why this evaluation precedes more integration. |
| Which Commons surface earns its place in real work? | Provides one discussion thread for evidence from the usability task. |

## Provenance and cross-links

`dogfood/codex-commons/manifest.json` keeps source references in audit-only
`sources` and `source_keys` fields so repository paths do not clutter reader
copy. Manifest-local keys are deterministic bootstrap identities, not canonical
Commons IDs. Milestone and dependency keys are resolved during apply; a post's
optional `task_key` becomes the generated canonical task reference.

Wiki bodies name related page slugs because Project Core has no first-class
Wiki-to-Wiki link field yet. They do not pretend those labels are clickable.
Because `source_keys` are not published through the current API, Wiki pages also
carry a terse source footer and posts cite their most relevant paths in
`basis`. No new product schema is required by this corpus.
