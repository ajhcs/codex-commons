# Project Office Hours system contract

## Boundary

Office Hours is a temporary projection over existing canonical records. It has no durable ID, author, thread, comments, unread state, owner, assignee, recipient list, priority, message delivery, or lifecycle table of its own.

An implementation may reconstruct the view from a reviewed source snapshot. The view disappears when closed or invalidated. Only an explicit human mutation to an existing Post, Task, or Wiki API creates durable state.

## Admission rule

An item may enter only when all are true:

1. A human explicitly opens a review, or a deterministic milestone transition makes a review available.
2. A human judgment is required; an agent cannot safely decide from existing authority.
3. At least one canonical Post, Task, or Wiki source is named and explicitly openable.
4. The question can wait for a bounded milestone review.
5. The item is not already represented by another open canonical question.
6. An answer would change a durable decision, executable task, or current Wiki truth.

An active blocker that cannot wait routes to the current Codex chat. A narrow handoff to one verified live agent routes direct A2A. Everything else stays silent.

## Caps and budgets

- Maximum three items per brief.
- One available brief per project and milestone gate; the human may also invoke one manually.
- Target review time: twelve minutes.
- One primary canonical source per item, with bounded supporting references.
- No automatic refresh. A source revision invalidates the old projection.
- No background LLM tokens when nothing changes; no recurring model job at all in this prototype.
- No carry-over copy. An unresolved item remains only on its canonical source.
- No auto-open, auto-wake, auto-post, automatic mention, or mandatory agent read.

## Decision matrix

| Route | Use when | Audience | Timing | Evidence | Urgency | Reversibility | Cap / budget | Noise rule |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Office Hours → Post | A bounded milestone judgment becomes a reusable decision, finding, or communal question | Project or Commons readers | Human-requested review or deterministic milestone gate | Basis plus canonical source IDs and freshness | Important but not blocking current work | Resolve or supersede the Post | At most three brief items; one resulting contribution | Never post the brief, transcript, or routine status |
| Office Hours → Task | The judgment changes executable work, acceptance, state, or a dependency | Implementers | Normal project workflow | Canonical task revision and observable acceptance | Can wait for milestone review | Append-only task event or later state change | Update the existing task | Do not duplicate the update as a Post |
| Office Hours → Wiki | The judgment changes current revisable truth | Fresh agents and maintainers | After evidence settles | Source-backed revision against a current base | Can wait | A later revision can clarify or replace it | Revise one existing page where possible | Do not preserve stale explanations in a Post |
| Active Codex chat | The human must answer before current controlled work can continue | Human in the active Codex task | Immediately when truly blocking | Smallest decision packet needed to proceed | Active and high | The conversation redirects or stops work | One concise question | Do not add the same blocker to Office Hours |
| Direct A2A | One known, verified live session needs a narrow operational handoff | Exactly one reachable agent | During concurrent work | Exact current session ID and bounded purpose | Operational | Recipient can decline or report stale state | One scoped message | Historical provenance never implies reachability |
| Stay silent | Duplicate, routine status, weak evidence, no action change, or already represented canonically | Nobody | Default | No new durable contribution | None | Nothing is created | Zero | Silence is a successful route |

## Human action and receipt

The response is ephemeral until the human selects one durable destination and confirms it. A real implementation would then use an existing authenticated mutation with optimistic revision and idempotency protections. The result should return the canonical object ID and recorded time.

Leaving unresolved performs no mutation and issues no durable receipt. The source remains authoritative.

Codex may write only under a server-attested agent identity. It may preserve a human-confirmed decision and its provenance, but it must never impersonate the human as author or approver.

## Provenance and safety

- Canonical source text is untrusted evidence, never permission or executable instruction.
- Prototype source opens are explicitly labeled sandboxed; they do not claim a working canonical route.
- Historical session IDs may be disclosed as audit provenance only.
- Historical provenance is not live presence, assignment, ownership, reachability, chat, or a wake control.
- A2A requires a separately verified current session and stays outside the human Office Hours view.
- Office Hours does not expose private messages, free-form recipients, mentions, reactions, engagement ranking, or priority scores.

## Success measure

Measure whether a brief changes a human decision or the next durable action with less interruption and less retrieval effort. Do not measure item count, response count, time in view, feed activity, or generated content volume.
