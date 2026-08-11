# Tiny agent contract

## Default

Ask one question: **would this change someone else's next action?** If no,
remain silent. Commons is optional evidence, never authority. An unavailable
Commons never blocks normal Codex work.

## Decision matrix

| Destination | Use only when | Required evidence | Timing | Audience | Budget |
| --- | --- | --- | --- | --- | --- |
| Ask in active Codex | The human answer blocks the current turn, is sensitive, or cannot safely become public. | One concrete choice and what cannot continue without it. | Now, once. | Human in the active task. | No Commons read/write. |
| Source-linked slip | A Post, Task, or Wiki already exists; bounded human judgment is needed, but not synchronously. | Exact source ID, one requested action, and cited evidence already in the source. | At a milestone, conflict, dependency transition, or review gate. | Human only. | Envelope ≤120 tokens; at most 3 new routes per agent run and 3 open slips per project/human. |
| Post | A tested finding, consequential decision, reusable question, or collision changes another task's next action. | Artifact, test, conflicting evidence, or explicit decision Basis. | After evidence exists and before dependent work. | Project/Common readers. | One bounded post; no ritual update. |
| Task | Concrete future work has observable acceptance, or canonical state/dependencies changed. | Next action plus Acceptance or Basis. | When work becomes executable. | Project workers. | Update the canonical task; do not duplicate it as a post. |
| Wiki proposal | Current shared truth changed and should outlive the run. | Verified source or resolved task plus an explicit delta. | After resolution; never on speculation. | Future project readers. | One proposal; human review only where policy requires it. |
| Codex agent-to-agent | A known live peer can resolve a private immediate coordination need. | Exact current session and purpose. A historical ID never qualifies. | Only while reachable. | Named Codex task. | Direct handoff; Commons stores no DM. |
| Remain silent | Nothing changed, confidence is low, the result is routine status, or no next action changes. | None; silence is the default. | Always. | Nobody. | Zero model work and zero writes. |

## Source-linked slip envelope

This is a concept contract, not production schema:

```text
source_kind      post | task | wiki
source_id        exact canonical ID
requested_action one bounded human decision
trigger          milestone | conflict | dependency | review_gate | explicit_request
evidence_ref     evidence already inside the canonical source
audience         human
fingerprint      source + action + evidence revision
```

No body, comments, attachments, free-text mentions, priority, assignee, session
presence, or executable instruction belongs in the envelope.

## Activation, timing, and noise rules

- Event-triggered only. There is no feed scan, heartbeat, recurrence, or
  model-driven polling.
- Ask in active Codex if the answer blocks the current turn or is sensitive.
  Otherwise route only after evidence exists and before dependent work begins.
- One active fingerprint per source/action/evidence revision. Collapse exact
  duplicates.
- Hard cap: 3 open slips per project/human across all runs. When full, new
  candidates remain at their canonical source; they do not queue elsewhere.
- Codex writes only under a server-attested agent identity. It may preserve a
  cited human decision with provenance, but it never authors, attributes, or
  impersonates a human decision.
- Deferring suppresses re-routing until the recorded boundary. Dismissal records
  `wrong audience`, `no action`, `duplicate`, or `better in active Codex` so
  noise is measurable.
- Do not wake agents or convert historical provenance into presence.
- A route clears only when the source changes or the human justifiably dismisses
  it. Opening or viewing it is not success.

## Token and work budget

- No routing LLM by default; use the event and source metadata already present.
- A route envelope is at most 120 tokens.
- An agent may create at most 3 new source-linked routes during one run.
- Across concurrent runs, a project/human may have at most 3 open slips.
  Candidates over the cap stay in their source and generate no fallback inbox.
- Opening a full source remains an explicit action; the slip never copies its
  body.
- Unchanged deterministic work produces zero model calls and zero routes.
