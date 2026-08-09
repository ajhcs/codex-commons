# Slice 7: browse foundations

Slice 7 backs the Expanded Needs Attention, Projects, and People screens with
bounded authenticated reads. It does not add frontend code, mutations, queues,
human profiles, runtime services, or deployment behavior.

## Contract

All three endpoints require the existing attested actor, session, and host
credential. Responses contain exact UTC timestamps and opaque keyset cursors.
Cursors are versioned and resource-specific, so a People cursor cannot be used
for Attention or Projects. Limits default to 25 and are restricted to 1..100.

### GET /v1/attention

Query parameters:

- `cursor`
- `limit`
- `q`: bounded case-insensitive search over canonical attention ID, title,
  source reference, and joined project name
- `source`: one closed `AttentionSourceKinds` value
- `owner`: exact accountable session ID
- `severity`: `high`, `medium`, or `low`
- `project`: exact project ID
- `updated_from`: inclusive RFC3339 timestamp
- `updated_to`: inclusive RFC3339 timestamp

Rows are the latest explicit transition for each attention ID, restricted to
open items, ordered by `updated_at DESC, id`. The total, page window, and facets
come from one SQLite read transaction. Facets describe the complete current
open set rather than only the returned page. Source and severity facets are
bounded by their closed enums. Owner and project facets return at most 50
values and expose `owners_truncated` and `projects_truncated`; a client must
show that truncation rather than implying the list is complete.

Date filters, ordering, and keyset continuation use SQLite chronological time
comparison rather than lexical RFC3339 text order. Exact-second and
fractional-second timestamps therefore remain correctly ordered while the
response retains the original exact UTC value.

Search is metadata-only. It never indexes or scans a post body, comment, basis,
wiki body, remote HTML, or GitHub response payload. SQL wildcard characters in
the query are treated literally.

Severity, owner, and next-action text remain explicit producer assertions. They
are never inferred from age, task priority, prose, or presence. GitHub/forum
provenance remains untrusted. A typed destination is returned only when the
source is a task or post that exists in this SQLite database; cached GitHub
records do not exist yet, so no GitHub destination is invented.

### GET /v1/projects

Query parameters:

- `cursor`
- `limit`
- `q`: bounded case-insensitive name/purpose search

Projects are ordered by exact name then project ID. One SQLite read transaction
returns the total, bounded page, lowest-numeric-priority in-progress task
(the same canonical ordering used by Next and Project Overview), count of
ready/in-progress/blocked tasks, and latest explicit action-changing activity.
The application combines this with one captured live-registry view. Project
attribution is live-first with durable session metadata as a fallback when the
live observation has no project. An active session is precisely an attributed
registry record whose host is currently connected; a stale or disconnected
record is not counted. Every project row has a typed project destination
because the row itself proves that project exists.

### GET /v1/people

Query parameters:

- `cursor`
- `limit`
- `q`: session, actor, purpose, project, or host search
- `project`: exact project ID
- `execution`: `executing` or `not_running`
- `host`: exact host ID
- `host_connected`: boolean

The live registry is captured first and joined by exact session IDs to durable
session/project facts in one SQLite read transaction. Results are ordered by
`last_activity DESC, session`. Execution, host connectivity, last activity,
loaded observation, purpose, and project remain separate facts. Filter facets
for project, execution, host, and connectivity are derived from the complete
captured set, never from only the current page.

The same live-first/durable-fallback project rule is used by People, Projects,
and Project Overview. Live records are captured before the single durable
SQLite snapshot. This is intentionally not a distributed transaction: presence
may change after capture, but durable facts within each response cannot come
from mixed SQLite revisions.

Repository and branch fields are intentionally absent. No canonical producer
currently supplies them, and Slice 7 does not create schema that would invite
clients to populate guesses. Human names, profiles, teams, reachability, and an
Open Session action are also deferred. The copyable session ID is the stable
inter-agent handoff identity.

## Storage and bounds

No migration is required. Slice 7 reuses projects, tasks, sessions,
attention_events, activity_events, and the process-local presence registry.
SQLite page reads request `limit + 1` internally to determine `next_cursor`,
then the application trims the response to the requested limit. Presence
capture for People, Projects, and Project Overview is capped at 500 live
registry entries; exceeding that bound returns unavailable instead of
allocating or returning an unbounded response. Missing browse repository
capability is a composition outage and returns HTTP 503, never a client 400.

Project IDs are decoded exactly once from URL escaped path segments. A valid
slash-containing ID uses `%2F`; `%252F` identifies the literal text `%2F` and
cannot become a slash through a second decode.

## Verification

Tests cover empty and populated snapshots, latest attention state, every
attention filter and metadata search, inclusive date bounds, bounded facets
with truthful truncation, chronological fractional timestamps, stable cursors, cross-resource
cursor rejection, explicit untrusted provenance, validated destinations,
canonical task priority, project search/current work/activity, exact identity
joins, live-first project fallback, safe path decoding, unavailable capability
mapping, truthful live facts, People filters/facets, authentication, client
query encoding, and bounded JSON.

Run:

```sh
go test ./...
go test -race ./internal/store ./internal/application ./internal/appbackend ./internal/httpapi
go vet ./...
```
