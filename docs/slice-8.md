# Slice 8: Project Overview

Slice 8 provides the bounded backend read model for the approved Project
Overview mockup. It is a summary over canonical sources, not an analytics
warehouse and not a source of work authority.

## Contract

Authenticated clients read:

~~~text
GET /v1/projects/{project}/overview?attention_limit=5&work_limit=5
~~~

Both preview limits default to 5 and accept 1 through 20. The response contains:

- canonical project identity, purpose, status, milestone, current text, and revision;
- exactly 14 UTC calendar-day activity buckets, oldest first and explicitly
  zero-filled;
- open/high attention counts and a bounded preview from the same append-only
  attention ledger used by General;
- an open-work count and bounded task preview for `in_progress`, `blocked`, and
  `ready` tasks;
- a count of connected process-local sessions attributed to the requested
  project by the shared live-first/durable-fallback rule;
- a merged-pull-request availability object;
- the most recent canonical action-changing activity time, when one exists;
- the UTC request snapshot time.

There are no queues. Open work means canonical tasks and sources. The response
contains no wiki body, post body, basis, generated summary, inferred severity,
remote HTML, job controls, or GitHub mutation.

## Time semantics

The pilot timezone is UTC. If the request snapshot is 2026-08-09T12:00:00Z,
the activity interval is `[2026-07-27T00:00:00Z,
2026-08-10T00:00:00Z)`. An event exactly at the first boundary is included; an
event exactly at the exclusive end is not. Ordinary heartbeats cannot enter
the activity ledger and therefore cannot inflate the chart.

The live registry is captured once before the durable snapshot. Durable project,
session attribution, attention, task, and activity fields are read in one
SQLite read transaction.
This does not claim a distributed transaction with process-local presence; the
response exposes `snapshot_at` so the observation time is explicit.

## Metric truthfulness

`merged_pull_requests` currently returns:

~~~json
{"available": false}
~~~

Slice 5 provides conditional read-only GitHub transport but no canonical
persisted GitHub snapshot. The page read therefore performs no network call and
does not turn missing persistence into a guessed zero. Once persisted snapshots
exist, the repository seam can populate a count without changing the response
shape.

Task `updated_at` is optional. It is emitted only when a canonical
`task_claimed` or `task_status_changed` activity event exists for that task.
Each work item has the typed target `{ "kind": "task", "ref": "..." }`;
unsupported menus and mutations are absent.
Attention preview items likewise expose a typed destination only for a task or
post proven to exist locally. GitHub references remain untrusted and have no
invented destination until their canonical persistence exists.

## Verification

Coverage includes empty and missing projects, populated snapshots, latest
attention state, project scoping, UTC boundary inclusion/exclusion, explicit
zero days, deterministic task ordering, optional canonical timestamps, live
connected-session counts, unavailable GitHub metrics, concurrent snapshot
consistency, bounded JSON, authenticated routing, client path escaping, and
benchmarks.

~~~sh
go test ./...
go test -race ./internal/store ./internal/application ./internal/httpapi ./internal/appbackend
go vet ./...
go test ./internal/store -run '^$' -bench BenchmarkProjectOverviewSnapshot -benchtime=2s -count=1
go test ./internal/application -run '^$' -bench BenchmarkProjectOverviewReadModel -benchtime=2s -count=1
~~~

Measured on Plumbob on 2026-08-09, the maximum representative response was
19,697 bytes. The SQLite snapshot measured 1,426,570 ns/op, 39,445 B/op, and
862 allocs/op; application composition measured 12,370 ns/op, 10,544 B/op,
and 38 allocs/op. These are local library measurements, not LAN or browser
latencies.

## Deferred integration

Existing task, GitHub, and forum adapters must append their explicit
action-changing events for real charts and attention previews to populate.
Persisted GitHub snapshots remain a later slice. No listener, scheduler,
frontend, credential issuer, deployment, or multi-host presence service is
introduced here.
