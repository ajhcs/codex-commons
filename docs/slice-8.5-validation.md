# Slice 8.5: live-data integration and LAN validation

Slice 8.5 connects the completed Slice 7 and Slice 8 readers to one persistent
Go runtime and the React prototype. It does not add a new product surface.

## Proven boundaries

- `internal/demodata.Seed` is an explicit prototype action. Store open,
  migrations, and normal server startup never seed records.
- Every durable demo identifier is namespaced with `demo-` or `DEMO-`, project
  status is `demo`, and visible purposes and attention titles say `[Demo]`.
- The seed writes through canonical store methods. It does not maintain a
  parallel fixture schema or populate frontend JSON.
- Durable projects, tasks, session facts, attention events, and activity events
  survive a server restart in SQLite.
- Live presence remains process-local authority. It is populated directly in
  `presence.Registry` only while the explicit demo seed runs. A restart without
  `--demo-seed` has zero live People even though durable session facts remain.
- Repeating the seed is safe: deterministic event IDs replay, uniqueness
  conflicts for demo-namespaced base records are accepted, and ledger
  cardinalities remain exactly 4 projects, 8 tasks, 5 attention items, and 18
  activity events.
- Anonymous reads are a prototype-only opt-in. The server supplies an internal
  attested read identity; it does not make any POST operation anonymous.

## Runtime activation

The complete LAN evaluation mode requires all three explicit switches:

```text
--demo-seed --anonymous-read --allow-anonymous-lan
```

It also requires a literal LAN address, a persistent database path, and the
built frontend directory. Wildcard listeners and in-memory server databases are
rejected. `--allow-anonymous-lan` is an acknowledgement, not an authentication
mechanism; use this mode only on a trusted evaluation LAN.

For a non-demo run, omit `--demo-seed`. For an authenticated run, omit both
anonymous flags and configure credentials using the environment or a mode-0600
credential file.

## Automated proof

Run from the repository root:

```text
go test -count=1 ./internal/demodata ./internal/appbackend ./internal/server
go test -count=1 ./...
go test -race -count=1 ./internal/demodata ./internal/appbackend ./internal/server
cd web && npm test && npm run build && npm run test:sites
```

The focused tests prove:

1. deterministic repeat seeding without duplicate ledgers;
2. durable restart persistence and the absence of inferred live presence;
3. Store -> Application -> Adapter -> authenticated HTTP responses for General,
   Attention, Projects, People, and Project Overview;
4. anonymous GET access only when explicitly enabled; and
5. anonymous POST denial.

## Temporary Playwright LAN plan

Browser plugin status for this implementation session: **not available**.
Use the repository's Playwright dependency or the existing cached Playwright
runtime without installing or committing browser artifacts. Screenshots,
traces, and temporary scripts belong under `/tmp`.

The flow under test is: LAN root -> General/Attention uses live API data -> Projects list
-> open `billing-orchestrator` -> overview renders -> Back to projects returns
to the list -> Open task shows the task preview -> People renders process-local
presence, all without console or failed-network errors.

Before binding, read the Plumbob runtime notes and run `ss -tlnp`. Choose an
unused literal LAN address and port; never use `0.0.0.0` for this server.

Validation sequence:

1. Build `web/dist/client`, then start `commons-server` with a fresh persistent
   database, the three explicit prototype flags above, and the selected literal
   LAN address.
2. Verify `/v1/health` returns `ok` and `/v1/projects?limit=10` returns four
   demo projects without an Authorization header.
3. Verify an anonymous `POST /v1/posts` returns `401 Unauthorized`.
4. Open the LAN root at a 1440x1000 desktop viewport. Confirm meaningful
   General content, no framework overlay, no relevant console warning/error,
   and network requests to `/v1/...` rather than fixture timers.
5. Confirm General/Attention shows the live-presence rail and five attention
   items. At least one title must visibly contain `[Demo]`.
6. Open Projects and select `billing-orchestrator`. Confirm the URL/screen uses
   project ID `demo-billing-orchestrator`, metrics show two attention items and
   four open work items, and merged pull requests is unavailable rather than a
   guessed zero.
7. Exercise Back to projects and Open task. Confirm the first restores the list
   and the second presents the selected durable task ID/title.
8. Open People. Confirm connected/disconnected and
   executing/not-running states both appear. These values must come from the
   current Registry, not durable session rows.
9. Repeat the primary flow at a 390x844 viewport and inspect for clipping,
   overlap, unreadable text, scroll traps, or broken controls.
10. Capture desktop General, Project Overview, task preview, and mobile evidence
    under `/tmp`; record page identity, DOM-not-blank, overlay, console, and
    interaction results.
11. Stop and restart against the same database without `--demo-seed`: durable
    Projects/Attention/Overview must remain, while People becomes empty.
12. Restart once more with `--demo-seed`: ledger totals must not increase and
    the six process-local demo sessions must reappear.

Slice 8.5 is ready for human LAN evaluation only after the automated suite and
this browser loop both pass against the same assembled Go server.
