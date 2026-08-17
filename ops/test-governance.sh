#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
ledger="$root/docs/phase-0-remediation-baseline.md"

test -f "$root/AGENTS.md"
test -f "$root/web/AGENTS.md"
test -f "$ledger"
test -f "$root/.codex/release-gate.toml"

grep -Fq 'name = "ops-and-deployment"' "$root/.codex/release-gate.toml"
grep -Fq 'command = ["sh", "ops/test-ops.sh"]' "$root/.codex/release-gate.toml"

for phrase in \
	"Preserve every pre-existing tracked and untracked change" \
	"/home/plumbob/.codex/memories/plumbob-server/" \
	"ss -tlnp"; do
	grep -Fq "$phrase" "$root/AGENTS.md"
done
for phrase in "NO-GO" "Phase 1 handoff checklist"; do
	grep -Fq "$phrase" "$ledger"
done

# These are release inputs, not secrets or generated output. They must remain
# discoverable by normal Git review and must not be hidden by an ignore rule.
for path in \
	.codex/release-gate.toml \
	deploy/bin/codex-commons-launch \
	ops/backup.sh \
	ops/build-release.sh \
	ops/check-readiness.sh \
	ops/deploy-release.sh \
	ops/install-launcher.sh \
	ops/record-evidence.sh \
	ops/seal-archive.sh \
	ops/stage-release.sh \
	ops/test-deploy-release.sh \
	ops/test-launcher.sh \
	ops/test-governance.sh \
	ops/test-ops.sh \
	ops/verify-release.sh \
	ops/verify-restore.sh \
	deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md \
	deploy/caddy/commons.Caddyfile \
	deploy/systemd/codex-commons-backup.service \
	deploy/systemd/codex-commons-backup.timer \
	deploy/systemd/codex-commons.service \
	deploy/systemd/dogfood.env.example \
	migrations/014_archaeology_execution_policy.sql \
	migrations/015_continuous_dogfood.sql \
	internal/domain/human_session.go \
	internal/httpapi/project_archaeology_response_budget_test.go \
	internal/server/notify.go \
	internal/server/project_archaeology_real_acceptance_test.go \
	internal/store/human_sessions.go \
	internal/store/human_sessions_test.go \
	internal/store/installation_status.go \
	internal/store/migration15_test.go \
	internal/store/project_archaeology_apply.go \
	internal/store/project_archaeology_apply_test.go \
	internal/store/project_archaeology_catalog.go \
	internal/store/project_archaeology_history.go \
	internal/store/project_archaeology_policy.go \
	internal/store/project_archaeology_policy_test.go \
	internal/store/project_archaeology_review.go \
	internal/store/project_archaeology_review_test.go \
	web/scripts/run-project-archaeology-browser-gate.mjs \
	web/src/features/project-archaeology/ProjectArchaeologyHistory.jsx \
	web/tests/project-archaeology-production-browser.test.mjs \
	web/src/assets/fonts/open-sans/OpenSans-Variable.woff2 \
	web/src/assets/identity/PROVENANCE.md \
	web/src/assets/identity/commons-mark-connecting.png \
	web/src/assets/identity/commons-mark-resolved.png; do
	# --no-index is essential here: several release inputs are tracked in later
	# phases, and ordinary check-ignore deliberately suppresses tracked matches.
	if git -C "$root" check-ignore --no-index -q -- "$path"; then
		echo "required release input is ignored: $path" >&2
		exit 1
	fi
done

printf '%s\n' 'governance checks passed'
