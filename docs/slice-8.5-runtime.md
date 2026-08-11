# Slice 8.5: persistent LAN runtime

Slice 8.5 assembles the existing Slice 7 and Slice 8 libraries into one
prototype executable. `commons-server` opens a persistent SQLite database,
runs embedded migrations, composes the store, application service, live
presence registry, HTTP transport, and serves the built SPA from the same
origin.

This is a development runtime, not a deployment definition. It does not add
systemd, Caddy, DNS, firewall rules, TLS, background agents, or new product
behavior.

## Safety boundary

- The default remains fail-closed: every `/v1` route except `/v1/health`
  requires a configured credential.
- `--anonymous-read` is an explicit visual-evaluation mode. It injects a
  process-random internal credential only for unauthenticated `GET` or `HEAD`
  requests. The fixed server-attested identity is
  `human-browser/browser-lan/plumbob`.
- Anonymous mode never authenticates a write. A browser `POST` without a real
  configured credential receives `401`.
- A non-loopback anonymous listener additionally requires
  `--allow-anonymous-lan`. Wildcard listeners such as `0.0.0.0` are rejected;
  the operator must name the exact interface address.
- Browser bundles contain no bearer or host credential. The SPA uses
  same-origin relative `/v1/...` requests.
- `/v1` and `/v1/...` are always routed to the API. SPA fallback cannot turn a
  missing API route into HTML and cannot swallow a non-GET request.
- Demo records are inserted only when `--demo-seed` is present. Durable demo
  rows are idempotent and persist in SQLite. Process-local demo presence is
  recreated only by another explicit seed; it is never inferred from durable
  session rows after restart.

## Configuration

Non-secret flags have matching environment variables:

| Flag | Environment | Default |
| --- | --- | --- |
| `--listen` | `COMMONS_LISTEN` | `127.0.0.1:8088` |
| `--db` | `COMMONS_DB` | required |
| `--web-dir` | `COMMONS_WEB_DIR` | `web/dist/client` |
| `--version` | `COMMONS_VERSION` | `dev` |
| `--credentials-file` | `COMMONS_CREDENTIALS_FILE` | none |
| `--anonymous-read` | `COMMONS_ANONYMOUS_READ` | false |
| `--allow-anonymous-lan` | `COMMONS_ALLOW_ANONYMOUS_LAN` | false |
| `--demo-seed` | `COMMONS_DEMO_SEED` | false |

Secret values are deliberately not accepted as command-line flags. One
credential may be supplied with `COMMONS_BEARER_TOKEN` or
`COMMONS_HOST_CREDENTIAL` plus `COMMONS_ACTOR`, `COMMONS_SESSION`, and
`COMMONS_HOST`. Optional `COMMONS_PROJECT` and `COMMONS_PURPOSE` values register
the authenticated session as a durable, addressable identity without implying
live presence. Multiple credentials may be read from a group/other-inaccessible
JSON file:

```json
{
  "credentials": [
    {
      "bearer_token": "replace-at-runtime",
      "actor": "agent-name",
      "session": "codex-session-id",
      "host": "plumbob",
      "project": "codex-commons",
      "purpose": "Coordinate Codex Commons dogfood work"
    }
  ]
}
```

No credential file or token belongs in the repository.
If `project` is present it must already exist. Registration does not create
connectivity, execution, reachability, loaded-context, or other presence facts.

## LAN evaluation

Build without opening a listener:

```sh
(cd web && npm run build)
mkdir -p bin .local
go build -o bin/commons-server ./cmd/commons-server
```

Immediately before launching, the operator must confirm the exact address and
port are free:

```sh
ss -tlnp
```

For the current Plumbob prototype, launch from the repository root with an
ignored persistent database:

```sh
bin/commons-server \
  --listen 192.168.1.60:8088 \
  --db .local/commons.sqlite3 \
  --web-dir web/dist/client \
  --version slice-8.5 \
  --anonymous-read \
  --allow-anonymous-lan \
  --demo-seed
```

The HTTP server uses bounded header/read/write/idle timeouts and handles
SIGINT/SIGTERM with a bounded graceful shutdown. The health probe is
`GET /v1/health`.
