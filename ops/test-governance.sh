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
	"ss -tlnp"; do
	grep -Fq "$phrase" "$root/AGENTS.md"
done
for phrase in "NO-GO" "Phase 1 handoff checklist"; do
	grep -Fq "$phrase" "$ledger"
done
phase4_ledger="$root/docs/phase-4-acceptance-ledger.md"
phase4_checker="$root/ops/test-phase4-acceptance.sh"
test -f "$phase4_ledger"
test -f "$phase4_checker"
grep -Fq 'docs/phase-4-acceptance-ledger.md' "$root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md"
grep -Fq 'sh ops/test-phase4-acceptance.sh' "$root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md"
grep -Fq 'phase4_acceptance_ledger|1' "$phase4_ledger"
grep -Fq 'live_boundary|NO-GO' "$phase4_ledger"
grep -Fq 'activation_performed|false' "$phase4_ledger"
grep -Fq 'base_ref|origin/main' "$phase4_ledger"
grep -Fq 'release_gate_plan_fingerprint|06f2f871a57bdcc1b2815c869f93d8ef2c42d521a017ad3b4adb65465e2cf6f9' "$phase4_ledger"
grep -Fq 'release_gate_ref|pending-exact-candidate' "$phase4_ledger"
grep -Fq 'release_gate_status|pending' "$phase4_ledger"
grep -Fq 'schema|deployment_attempt_v1|7cb4eb963cda3d3b31f0402e15fe9e0b31d8d16af79a4540944a449df0e79fb2' "$phase4_ledger"
grep -Fq 'schema|rollback_outcome_v1|477c14bd3a79e93d8b6eed6debaf6e306c9ef5937f9a4686ac0f19ffa3a48ba3' "$phase4_ledger"
grep -Fq 'marker_row_count|82' "$phase4_ledger"
grep -Fq 'pr5_expected_failure_count|32' "$phase4_ledger"
grep -Fq 'negative_fixture_count|8' "$phase4_ledger"
grep -Fq '31 expected-failure markers' "$root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md"
grep -Fq 'DEPLOY_FIRST_DEPLOY_WAL_SYMLINK_REJECT' "$root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md"
grep -Fq 'no-published-outcome cases' "$root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md"

# These are release inputs, not secrets or generated output. They must remain
# discoverable by normal Git review and must not be hidden by an ignore rule.
for path in \
	ops/backup.sh \
	ops/build-release.sh \
	ops/check-readiness.sh \
	ops/commons-launch.sh \
	ops/deploy-release.sh \
	ops/record-evidence.sh \
	ops/restore-database.sh \
	ops/seal-archive.sh \
	ops/stage-release.sh \
	ops/test-deploy.sh \
	ops/test-governance.sh \
	ops/test-launch.sh \
	ops/test-ops.sh \
	ops/test-phase4-acceptance.sh \
	ops/test-restore.sh \
	ops/verify-release.sh \
	ops/verify-restore.sh \
	deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md \
	docs/phase-4-acceptance-ledger.md \
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
