# Codex App Server protocol fixtures

These fixtures were generated from `codex-cli 0.147.0` with:

```text
codex app-server generate-json-schema --out DIR
```

They intentionally retain only the two small stable observation schemas used
by this package. `thread/list` is covered by a wire fixture in
`observer_test.go`; its generated response schema is ~59 KiB and is not copied
into the repository. Regenerate and review fixtures whenever the pinned Codex
CLI version changes.

The bridge is a passive stdio JSONL observer. It does not expose WebSocket,
invoke `thread/read` as a resume mechanism, start turns, execute shell commands,
inject messages, or grant Commons write authority.
