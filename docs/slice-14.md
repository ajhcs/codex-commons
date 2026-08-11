# Slice 14 — Codex-connected Commons sign-in

Slice 14 adds a browser device-code sign-in path backed by the local Codex App
Server. Commons starts the narrowly scoped command
`codex app-server --listen stdio://`, completes the `initialize`/
`initialized` handshake, and uses only account-read, device-login, and
device-login-cancel methods. It does not accept or persist OAuth tokens,
provider emails, device codes, or verification URLs.

## Durable identity

Migration `009_codex_human_auth.sql` adds the single
`human:local-admin` binding and append-only `human_auth_events`. The binding
stores only `HMAC-SHA256(binding-key, lower(trim(email)))`, a public display
name, a validated lowercase handle, and a revision. The binding key must be a
dedicated 32-byte mode-0600 regular file; the server rejects symlinks,
group/other-readable files, wrong lengths, and all-zero keys.

Historical human provenance remains `human:local-admin` with the stable
`human-local-admin` session. Display names and handles are loaded from the
binding at startup and are used dynamically in application read models.

## Configuration

The managed path is opt-in:

```text
COMMONS_CODEX_AUTH=true
COMMONS_CODEX_BIN=/absolute/path/to/codex
COMMONS_CODEX_BINDING_KEY_FILE=/run/commons/codex-binding.key
```

`COMMONS_ALLOW_FIRST_CODEX_BIND_LAN=true` is required for a first bind that
originates from a non-loopback client. The separate recovery-key login stays
hidden unless `COMMONS_ENABLE_RECOVERY_LOGIN=true` is set alongside its
protected secret source.

If the Codex executable is missing or the App Server cannot initialize,
Commons still starts and `/v1/auth/codex/status` reports the capability as
unavailable. A managed process receives one bounded restart attempt; it never
enters an unbounded restart loop.

## HTTP and browser behavior

The additive session DTO includes `auth_method` and `profile_revision`.
Pairing state is memory-only, capped at four concurrent attempts, ten minutes,
three starts per remote per ten minutes, and sixty polls at one-second
minimum spacing. The `commons_pairing` cookie is HttpOnly, SameSite Strict,
scoped to `/v1/auth/codex`, and contains no account identity.

The selected UI mockup is documented in
[`slice-14-auth-mockup.md`](slice-14-auth-mockup.md). Its explicit browser
state machine is `loading → unauthenticated → pairing → needs_profile →
authenticated`, with recoverable errors, redirect restoration, draft
preservation, same-origin writes, CSRF-backed logout/profile edits, and no
human credential material in browser storage.

## Verification

Run from the repository root:

```text
GOCACHE=/tmp/codex-go-cache go test ./internal/codexauth ./internal/httpapi ./internal/store ./internal/server ./internal/application ./internal/appbackend
cd web && npm test && npm run build && npm run test:sites
```

Live device-code acceptance is intentionally manual and must use a disposable
test account; it is not part of automated tests and does not run in CI.
