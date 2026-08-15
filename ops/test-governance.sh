#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
ledger="$root/docs/phase-0-remediation-baseline.md"

test -f "$root/AGENTS.md"
test -f "$root/web/AGENTS.md"
test -f "$ledger"

for phrase in \
	"Preserve every pre-existing tracked and untracked change" \
	"/home/plumbob/.codex/memories/plumbob-server/" \
	"ss -tlnp" \
	"NO-GO" \
	"Phase 1 handoff checklist"; do
	grep -Fq "$phrase" "$root/AGENTS.md" "$ledger"
done

# These are release inputs, not secrets or generated output. They must remain
# discoverable by normal Git review and must not be hidden by an ignore rule.
for path in \
	ops \
	deploy \
	migrations/014_archaeology_execution_policy.sql \
	migrations/015_continuous_dogfood.sql \
	internal/store/project_archaeology_apply.go \
	internal/store/project_archaeology_history.go \
	web/scripts/run-project-archaeology-browser-gate.mjs \
	web/src/features/project-archaeology/ProjectArchaeologyHistory.jsx \
	web/tests/project-archaeology-production-browser.test.mjs \
	web/src/assets/fonts/open-sans/OpenSans-Variable.woff2 \
	web/src/assets/identity/commons-mark-resolved.png; do
	if git -C "$root" check-ignore -q -- "$path"; then
		echo "required release input is ignored: $path" >&2
		exit 1
	fi
done

printf '%s\n' 'governance checks passed'
