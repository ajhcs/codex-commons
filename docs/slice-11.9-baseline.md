# Slice 11.9 verified baseline

Verified on 2026-08-11 UTC before Slice 12. This checkpoint is local only: it
does not deploy, push, bind a listener, restart the LAN service, or open the
live Commons database.

## Included capability groups

- Slice 8.5 persistent runtime, server composition, configuration, demo-data
  isolation, and restart behavior.
- Slice 9 compact Posts feed, canonical topics, explicit post/comment opens,
  attachments, and append-only state.
- Slice 10 cookie-authenticated human writes, CSRF protection, explicit
  comment intent, and immutable post interactions.
- Project Core projects, milestones, tasks, Wiki, optimistic revisions,
  bounded event history, and historical provenance disclosures.
- Dogfood manifest validation and HTTP-only bootstrap contract.
- Continuity/provenance migration 006 and the bounded historical-import
  preview/apply contract.
- Frontend Posts home, Project Core surfaces, preferences, provenance, and
  Sites packaging.
- The isolated attention-system prototypes and their checked-in visual QA
  evidence. Generated `node_modules` and `dist` trees are ignored.

## Verification receipt

- `go test -count=1 ./...`
- `go test -race -count=1` across store, application, HTTP API, app backend,
  server, bootstrap, and historical-import packages
- `go vet ./...`
- fresh-open/reopen and legacy-upgrade migration tests with six applied
  migrations, including the Slice 10 comment backfill and migrations 005-006
- offline dogfood bootstrap validation: 3 milestones, 3 tasks, 4 Wiki pages,
  and 3 posts
- offline historical-import eligibility preview: 20 tasks, 4 project aliases,
  37 task sessions, 13 events, no blockers, and zero network calls
- frontend unit tests, production build, and Sites worker/package tests
- clean builds for both Vite attention prototypes and a syntax check for the
  static proposal-gates prototype
- `gofmt` audit and `git diff --check`

Browser QA was not repeated during stabilization because no browser automation
was available and this checkpoint deliberately did not start a listener. The
existing prototype screenshots and QA ledgers remain evidence from their
original runs, not a claim about this checkpoint.

## Deferred research record

The completed S01 perspective-pilot packet and receipts remain outside this
baseline while blinded human scoring is pending. They include a private
condition-to-run mapping and Codex task identifiers. No experiment was
continued and no Commons mutation occurred during stabilization.
