# Slice 6: bounded background work

Slice 6 is a deterministic library prototype for exactly two bounded jobs. It
does not install a scheduler, daemon, listener, credential, model client, or
GitHub client.

## Jobs

1. `github_watcher` consumes one project and one opaque upstream cursor through
   `GitHubReader.Check`. Its token budget is zero and its only capability is
   `github.read`. An unchanged check returns a compact serializable sync receipt
   and cannot reach the interpreter. The interface is deliberately narrow so a
   later adapter can implement it with Slice 5 conditional synchronization.
2. `wiki_curator` accepts one bounded task resolution only when its state is
   exactly `resolved` and its resolution revision is positive. Its deterministic
   test interpreter produces a `wiki_revision_proposal` with a declared token
   count. A valid proposal always has `requires_review=true`. No canonical wiki
   writer is present in the package, so review and application are necessarily
   separate future operations.

## Execution contract

Every definition names its input scope, capabilities, maximum wall runtime,
token budget, minimum interval, and sole output type. Definitions containing
`jobs.create` or `agents.wake` are invalid. A run validates its typed and bounded
input before performing work, requires every declared capability, checks the
kill switch, and uses a deadline derived from the maximum runtime. Interpreter
output is bounded and must report token use at or below the job budget.

`StateStore.Acquire` is the future persistence boundary. The runner supplies a
SHA-256 digest of a versioned, map-free JSON encoding containing only the
caller's semantic watcher or curator input. Definitions, capabilities, clocks,
and other runner configuration are deliberately excluded. Acquire must
atomically:

- permanently bind a job and run ID to its first accepted semantic-input digest;
- return the prior receipt only when a replay has the identical digest;
- reject a changed-payload replay before job code runs;
- reject a second unexpired lease for the job;
- enforce the definition's minimum interval; and
- acquire a lease through the run's maximum wall runtime.

`StateStore.Finish` durably records the JSON-serializable receipt before
releasing the lease. The supplied `MemoryStore` models these transitions under
a mutex and permits expired-lease recovery; it is not durable across process
restart. Production persistence must make acquire and finish transactional and
must use a bounded storage context independent of a canceled execution context.
It must durably retain the input-digest binding from the first accepted acquire,
including across lease expiry, failed execution, and process restart.

The state machine is intentionally small: `eligible -> leased -> executing ->
succeeded|failed`. Validation, capability, kill-switch, interval, and active
lease failures stop before execution. An expired lease returns to `eligible`;
a final receipt makes the run ID idempotently terminal. No transition schedules
another run or signals an agent.

GitHub/model errors, cancellation, invalid output, missing capabilities, and
budget violations fail closed. Failure produces no forum write, GitHub write,
canonical wiki edit, recursive job, or agent wakeup; none of those capabilities
exist in the runner's dependency graph.

## Deferred wiring

- Adapt Slice 5's eventual conditional read result to `GitHubReader`; do not
  import its concrete package into this library.
- Persist definitions, leases, last-start times, idempotency keys, and receipts
  in a later migration with atomic compare-and-set semantics.
- Add an explicit human review/apply workflow for wiki proposals. That workflow
  is not part of this job and must never be inferred from a successful receipt.
- Invoke `Runner.Run` from a separately reviewed scheduling boundary. Slice 6
  intentionally performs no polling and starts no goroutine.

## Verification

`internal/jobs/runner_test.go` uses only fakes. It covers unchanged watcher
checks with interpreter call count zero, resolved-only curator triggering,
review-required proposals, capability and token failures, kill switches,
outages, cancellation, JSON receipts, idempotent replay, minimum intervals,
exclusive leases, expired-lease recovery, identical idempotent replay, and
changed-payload conflicts for both jobs. The unchanged watcher receipt is
measured in the test and remains below 260 bytes; the current encoded size is
184 bytes.
