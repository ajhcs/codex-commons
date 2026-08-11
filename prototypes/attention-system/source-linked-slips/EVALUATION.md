# Concept evaluation

## Hypothesis

A small human-only view can bridge live Codex work and durable Commons records
without becoming messaging if it is reference-only, event-triggered, and clears
through a source-native change.

## What this direction does well

- It reuses the existing three-plane reading workspace and canonical Posts,
  Tasks, and Wiki rather than introducing another rich object.
- Human attention is visually distinct from live Codex chat: the row is a quiet
  source reference, the reader says `Not live chat`, and the action resolves in
  the source.
- Agent activation can be cheap: a bounded metadata envelope, explicit source
  open, no feed scan, and zero work on unchanged events.
- Dismiss/defer receipts make wrong routing and interruption cost measurable.
- The idea is easy to remove because the durable project content remains in
  existing sources.

## Main risk: a second inbox

Even a sparse count can train the human to check another place from fear that a
blocking question might be waiting. That would violate the product boundary
even if the screen looks calm. The feature must prove that it captures durable,
asynchronous judgment—not merely relocate Codex prompts.

## Falsifiable pilot and stop rule

Run the concept for **12 real routes or 14 days**, whichever occurs first.
Record only route trigger, source fingerprint, outcome, elapsed time, and a
bounded dismissal reason.

Delete `Needs you` rather than iterating it if any immediate-fail condition
occurs:

- one active task stalls because an execution-blocking question was routed here
  instead of asked in active Codex;
- sensitive content, a DM, a wakeup, or an unsupported assignment enters a
  route;
- more than 3 slips are open for one project/human, or an agent-authored change
  is presented as a human decision rather than server-attested agent work;
- the human reports checking the view from fear of missing work on 3 consecutive
  workdays.

At the end of the bounded pilot, delete it if fewer than **6 of 12** routes
produce a durable source change, or more than **3 of 12** are dismissed as
wrong audience, no action, duplicate, or better in active Codex.

These are prototype thresholds, not a universal product KPI. The point is to
make deletion a real outcome before backend coupling exists.

## Measures

Primary:

- durable source changes caused by a route;
- justified dismissals by reason;
- deferred routes later resolved without duplicate re-routing;
- any synchronous blocker misrouted here;
- time from route open to canonical source open/change.

Do not optimize views, open count, posts, comments, reactions, feed time, route
volume, or response streaks.

## Smallest backend implication if selected

Do not add it during prototype comparison. If selected after the pilot design
review, the minimum backend surface is one deduplicated route per
human/source/action fingerprint with append-only open, defer, dismiss, and
resolved receipts. It stores no rich body and grants no authority. Source
changes remain ordinary Post, Task, or Wiki operations.

## Recommendation

This is the lowest-coupling option of the likely attention concepts and the
easiest to kill. It is viable only if `Ask in active Codex` remains the hard rule
for live blockers and the bounded pilot beats the stop conditions above.
