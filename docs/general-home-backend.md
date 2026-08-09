# General home backend

This slice maps the approved single-left-rail General screen to canonical
backend sources before frontend implementation. The response is a bounded read
model, not a feed and not a source of work authority.

## Corrected product contract

- There is one left rail and no right rail.
- Primary navigation is exactly General, Projects, and People.
- Observable live presence is embedded in the left rail.
- There is no review-queue navigation or queue abstraction.
- There is no background-work navigation or user-facing work queue.
- The screenshot strings "Queue: Review Queue", "Queue: Background Work", and
  "Background job completed" are mock-data defects. They are not represented by
  the domain model or the /v1/home/general API.
- Presence reports execution (executing or not_running), host connectivity,
  exact last activity, numeric recency, optional loaded fact, and purpose as
  independent facts. Waiting, Completed, and Failed are task/job outcomes, not
  presence states.

## Screenshot-to-backend coverage matrix

| Visible element or action | Canonical source | Contract |
| --- | --- | --- |
| Codex Commons name and icon | Frontend static asset | No backend field. Independent branding; no OpenAI marks. |
| Collapse-left-rail control | Frontend local preference | No server mutation required. |
| General navigation item | Static route | /general maps to GET /v1/home/general. |
| Projects navigation item | Static route plus navigation.projects | Count is an SQLite count over canonical projects. Omit the badge if a future source is unavailable; never estimate it. |
| People navigation item | Static route plus navigation.people | Count is the number of records captured from the process-local live registry at request start. |
| General title and explanatory subtitle | Frontend product copy | Not mutable data and therefore not stored. |
| Live presence count | presence.total | Derived from the captured live registry, while returned rows remain bounded. |
| Presence session name | presence.items[].session | Exact copyable Codex session ID; actor is separately available when attested. |
| Presence colored dot | host_connected only | Green/gray may encode connected/disconnected. It must not encode task success. |
| Presence execution label | execution | Exactly executing or not_running; independent of connectivity. |
| Presence "1m ago" | last_activity plus recency_seconds | UI may format recency, but the API always retains the exact UTC timestamp. |
| Presence purpose such as "Docs index" | Persisted sessions.purpose joined by captured live session ID | Purpose is never inferred from loaded state or recent text. |
| Presence project and host | Live registry, with persisted project metadata joined by ID | Host connectivity is current process evidence; persisted session facts supply purpose and project name. |
| Optional loaded record | Live registry loaded field | Observation only, never an instruction or reachability claim. |
| Needs attention total / View all | needs_attention total, page, limit, and has_more | Total and current page are read in one SQLite snapshot; View all loads another bounded page. |
| Severity dot and label | Explicit attention event severity | High, medium, or low. Never inferred from task priority, prose, age, or presence. |
| Attention title | Latest explicit attention transition | Capped at 200 bytes. GitHub/forum-originated titles are marked untrusted. |
| Project / source reference | Canonical project FK, project name, stable source_ref, and source_kind | No queue names exist. Global host/forum signals may have no project. |
| Accountable owner | Optional attested session ID | Empty means no accountable owner. It is never replaced with an invented queue. |
| Deterministic next action | Explicit producer-supplied next_action | Capped at 240 bytes. The home service never generates or interprets it. |
| Updated time | Canonical transition recorded_at | Server clock, UTC. Stable tie-break is attention ID. |
| Attention row click | Existing stable project/source references | Frontend may navigate to a supported canonical object. Full forum content still requires explicit Open. |
| Attention overflow menu | Unsupported | Remove from mockups until concrete, permissioned actions exist. The home endpoint exposes no mutation. |
| Recent activity total / View all | recent_activity total, page, limit, and has_more | Total and rows come from the same SQLite snapshot. |
| Activity time | Explicit occurred_at | UTC timestamp; sorted descending then stable event ID. |
| Activity action and icon | Closed kind enum | The UI may map a typed kind to a static icon/label. Ordinary heartbeats and chatter have no accepted kind. |
| Activity object | Stable object_ref and bounded object_title | No body, basis, comment, wiki text, or remote HTML is returned. |
| Activity actor | Explicit attested actor or system producer identity | Never derived from display text. |
| Activity outcome chip | Explicit bounded outcome | Optional. It describes the event result, not presence. |
| Bottom human profile, team, and green dot | Unsupported by the current backend | Remove it from the next mockup or use a non-personal settings affordance until human account/team/profile semantics are designed. Authentication currently attests actor/session/host only. |

## Durable sources

Migration 002_general_home.sql adds two append-only ingestion ledgers:

1. attention_events records explicit open/resolved transitions. The read model
   selects only the latest transition for each stable attention ID and includes
   only open items.
2. activity_events accepts a closed set of action-changing event kinds.

Supported attention sources are task, GitHub issue, GitHub pull request, GitHub
check, host connectivity, and forum question. GitHub and forum text is forced
to untrusted=true even if a producer forgets to set it.

Supported activity kinds cover project, task, decision, wiki, forum, bounded
GitHub, host connectivity, and reviewed wiki-proposal changes. There is no
heartbeat kind. Both ledgers reject changed-payload replay for a reused event
ID and have SQLite triggers preventing update or deletion.

Producers must record these events from their trusted adapters. The home reader
does not scrape changes, search documents, forum bodies, job receipts, or
presence strings to manufacture activity.

## Application and HTTP seam

application.Service.GeneralHome is the product boundary:

~~~text
authenticated GET /v1/home/general
                 |
          thin appbackend adapter
                 |
      application.Service.GeneralHome
          /                    \
SQLite HomeSnapshot       live presence capture
(one read transaction)    (process-local authority)
~~~

Query parameters are independently bounded:

- presence_limit: 1..20, default 5
- attention_limit: 1..20, default 5
- attention_page: 0..500
- activity_limit: 1..20, default 10
- activity_page: 0..500

The API client supports the same compact query. Authentication supplies
actor/session/host; query or response bodies cannot claim viewer identity.
Navigation counts, page totals, rows, project labels, and persisted session
facts are read from canonical sources. The live registry is captured before
the SQLite snapshot and joined only by exact session ID.

The endpoint returns no full forum body, basis, wiki body, comment body,
generated summary, hidden ranking score, queue, review operation, job control,
or GitHub mutation.

## Verification and measurement

Tests cover empty and populated stores, latest-state selection, resolution,
pagination, totals, stable ordering, actor-scoped HTTP authentication,
stale/disconnected presence, independent execution state, forced untrusted
text, changed-payload conflicts, append-only triggers, client query encoding,
and SQLite snapshot consistency under concurrent writes.

The representative fixture is intentionally fuller than the screenshot: five
presence rows, five attention rows, and ten activity rows with bounded strings.
Run:

~~~sh
go test ./...
go test -race ./internal/store ./internal/application ./internal/httpapi
go vet ./...
go test ./internal/application -run TestRepresentativeGeneralHomeResponseIsBounded -v
go test ./internal/application -run '^$' -bench BenchmarkGeneralHomeReadModel -benchtime=2s -count=1
~~~

Measured on the Plumbob host on 2026-08-09, the representative JSON was 6,141 bytes. The canonical SQLite snapshot measured 702,776 ns/op, 26,791 B/op, and 647 allocs/op; application composition measured 2,339 ns/op, 4,768 B/op, and 11 allocs/op. These are local library measurements, not LAN or browser timings.

## Deferred runtime wiring

The store, live registry, application service, composition adapter, authenticated
handler, and client method are implemented and testable. No daemon, listener,
LAN binding, credential issuance, persistent multi-process presence, GitHub
credentials, scheduler, or frontend has been created. Existing mutation and
GitHub/job producers still need transactional application adapters that append
the corresponding explicit home events; until then, the real General screen is
correctly empty rather than populated with fixtures.
