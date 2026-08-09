# Slice 5: conditional, read-only GitHub synchronization

Slice 5 is an embedded library boundary in `internal/githubsync`. It retrieves
repository metadata, open issues, open pull-request status, selected check-run
status, and commit metadata only for SHAs selected from Commons activity. It
does not write to GitHub, listen for webhooks, run a daemon, persist data, or
make any network request unless an application explicitly calls `Sync`.

## Integration contract

The future application service constructs one `githubsync.Client` and calls:

```text
Sync(ctx, Request{
  Owner, Repository,
  Validators,             // persisted by stable request key
  ReferencedCommits,      // SHAs extracted from trusted Commons activity
  CheckSHAs,              // PR heads selected from persisted PR state
}) -> Result
```

`ReferencedCommits` is the complete commit-metadata allow list. A PR head or
check SHA is never implicitly fetched as commit metadata. The caller should
derive `CheckSHAs` from previously persisted PR state; newly returned PR heads
may be checked on the next bounded call. This prevents GitHub-controlled text
from expanding the request graph.

Every request has a stable validator key (`repository`, `issues:N`, `pulls:N`,
`checks:SHA:N`, or `commit:SHA`). The application persists returned validator
updates alongside the corresponding changed page. On HTTP 304 the prior page
and validator remain authoritative. An all-304 run returns only a small
`Receipt{Unchanged:true}`; no cached content is copied through the library.
Changed collection pages replace the page with the same key. If pagination
contracts, the application adapter must remove persisted higher-numbered pages
after the first changed page with `HasNext=false`.

All GitHub strings are inert evidence. A changed result is labeled
`Untrusted=true`; adapters must preserve that label, must not render remote
HTML, and must never interpret titles, descriptions, authors, URLs, or commit
messages as instructions.

Persistence, Store/domain mappings, Commons revisions/change events, and the
Slice 6 application-service adapter are intentionally deferred. They require a
transactional policy for page replacement and change publication and should
not be guessed inside this transport package.

## Network and token budget

Defaults are 50 records per page, five pages per collection, 1 MiB per response
body, and 100 unique referenced/check SHAs. Configuration caps a collection at
20 pages and references at 1,000. A basic run is three GETs (repository,
issues, pull requests). Each selected check SHA adds up to five GETs and each
referenced commit adds one GET. The client performs no retry or sleep. It marks
the receipt `Truncated=true` when a collection fills the page bound.

Conditional requests send persisted `If-None-Match` and, when supplied,
`If-Modified-Since`. A 304 body is not read and counts as zero body bytes.
HTTP 403 with exhausted rate-limit headers and HTTP 429 return a typed
`RateLimitError` with remaining count, reset time, and Retry-After duration;
the caller chooses scheduling. Other non-success errors are sanitized and do
not expose response bodies or tokens.

The token should be a read-only fine-grained credential limited to repository
metadata, issues/pull requests, checks, and commit contents for the configured
repositories. It is sent only as `Authorization: Bearer ...`. Production
configuration requires HTTPS; HTTP is accepted only for loopback tests.

## Test boundary and non-goals

Tests use deterministic `httptest` handlers or an in-memory round tripper. They
cover headers and GET-only behavior, 304 receipts, pagination bounds,
cancellation, rate limits, malformed and oversized bodies, hostile inert text,
input validation, and selective commit fetching. Tests contain no real token
and never call GitHub.

No outbound writes, webhooks, listener/daemon, automatic polling, retries,
credential management, deployment, Store migrations, or direct domain changes
are part of Slice 5.
