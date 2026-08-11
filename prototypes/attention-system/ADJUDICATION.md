# Attention-system adjudication

## Decision

The simplest viable direction is a **bounded Project Review using the Office Hours interaction model and Proposal Gate rules**.

Use Office Hours as the human experience: manually opened or made available at a deterministic milestone boundary, capped at three current source-linked judgments, and processed one at a time. Use Proposal Gates as the admission and resolution discipline: consequential human authority only, visible `not live chat` language, required Basis, explicit existing-source mutations, and a compact receipt.

Do **not** add the continuous `Needs you` navigation item or counter in the first pilot. Prototype A proves that source-linked slips can work, but it also creates the strongest second-inbox pressure. Its source-first row anatomy, defer reasons, and kill criteria are useful ingredients—not yet a permanent surface.

## Scored comparison

Scores are 1–5, where 5 is strongest. For implementation, 5 means the least new complexity.

| Method | Human thoughtfulness | Agent token / latency cost | Interruption / noise safety | Durability / coherence | Implementation simplicity | Total |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| A. Source-linked slips | 3.5 | 4.0 | 3.0 | 4.5 | 3.0 | 18.0 / 25 |
| B. Project Office Hours | 5.0 | 4.5 | 4.5 | 4.5 | 4.5 | **23.0 / 25** |
| C. Proposal / Decision Gates | 4.5 | 4.5 | 5.0 | 5.0 | 3.5 | 22.5 / 25 |

### A — Source-linked slips

Best at low-latency, one-off asynchronous routing. The exact source, no-body envelope, hard cap, defer/dismiss reasons, and source-native actions are strong. The persistent `Needs you 3` navigation cue is also the concept most likely to make the human monitor Commons from fear of missing work. A production version would need deduplication plus open/defer/dismiss/resolved lifecycle state.

Verdict: retain its item anatomy and falsifiable 12-route/14-day stop rule, but do not start with its global inbox-like placement.

### B — Project Office Hours

Best aligned with the user's goal of spending more time thinking and writing in Commons. Batching at most three items protects focus, makes the cost visible (~12 minutes), and supports softer judgments as well as hard decisions. It can remain a temporary projection with no body, thread, unread state, or queue.

Risk: it can become ceremony or another place to check. It must remain manual or deterministic-milestone only, never a recurring summary.

Verdict: use this as the first human surface.

### C — Proposal / Decision Gates

Best at authority boundaries, irreversible changes, superseding decisions, and append-only approvals. Its alternatives, required Basis, and durable-operation preview provide the clearest coherence and lowest noise.

Risk: using a gate for ordinary collaboration would over-formalize Commons and require more explanation than the underlying work. Some decisions do not have clean precomputed alternatives.

Verdict: use its admission test and durable receipt inside B, not as a universal standalone workflow.

## The recommended hybrid is still one feature

Working label: **Project Review**. “Office Hours” remains a useful prototype name, but the product should test the plainer label before committing.

A Project Review:

- is an ephemeral view over at most three existing Post, Task, or Wiki references;
- appears only when the human explicitly opens it or a deterministic milestone transition makes it available;
- shows why each source needs human judgment, the evidence and revision, audience, reversibility, and smallest requested decision;
- clearly says `Human judgment · asynchronous · not live chat`;
- never owns content, discussion, priority, unread state, assignment, presence, or delivery;
- resolves only through existing Post, Task, or Wiki operations, or leaves the source unresolved;
- invalidates when a source revision changes;
- never wakes an agent or recursively creates work.

Immediate blockers and sensitive choices remain in active Codex. Narrow coordination remains direct agent-to-agent. Public reusable evidence becomes a Post. Executable state goes to Tasks. Current truth goes to the Wiki. Everything else stays silent.

## Smallest viable pilot

No production schema is justified yet.

1. Keep the three prototypes isolated and visually review them.
2. For 12 real judgments or 14 days, manually assemble a maximum-three-item Project Review from existing dogfood source IDs and current revisions.
3. Use only existing source opens and existing Post/Task/Wiki writes. Do not persist a rich review object.
4. Record a compact evaluation receipt in the existing dogfood evaluation source: route, source fingerprint, durable outcome or bounded dismissal reason, elapsed time, and whether the next action changed.
5. If the manual pilot succeeds, test an ephemeral bridge envelope containing only source kind, ID, expected revision, requested-action enum, trigger enum, and evidence reference. It remains under roughly 120 tokens and stores no body.
6. Only after that pilot demonstrates value should a persisted reference projection be designed.

Immediate failure:

- one live task stalls because an execution-blocking question was sent to asynchronous review;
- a secret, private transcript, DM, assignment, wakeup, or human impersonation enters the flow;
- historical provenance is treated as current reachability;
- the human checks the surface from fear of missing work on three consecutive workdays.

End-of-pilot failure:

- fewer than 6 of 12 items produce a durable source change or an explicit deferral that changes the next action;
- more than 3 of 12 are wrong audience, duplicate, or no action;
- the review needs more explanation than the underlying judgment;
- unchanged events cause any model call or review item.

If the pilot fails, delete the view. Keep the routing policy and ordinary Post/Task/Wiki behavior.
