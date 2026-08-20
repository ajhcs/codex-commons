#!/bin/sh
set -eu

# Phase 4 PR 6: strict, offline/disposable acceptance ledger. This checker
# never activates a release, starts a service, binds a listener, opens a live
# database, or invokes release-gate. It validates a sanitized ledger, runs
# the three existing disposable suites, and proves mutated ledgers are
# rejected before any aggregate pass can be accepted.
repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
ledger=$repo_root/docs/phase-4-acceptance-ledger.md
test -f "$ledger"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/phase4-acceptance.XXXXXX")
cleanup() {
	chmod -R u+w "$tmp" 2>/dev/null || true
	rm -rf -- "$tmp"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
	printf 'phase4 acceptance: %s\n' "$*" >&2
	exit 1
}

contains_word() {
	case " $1 " in
	*" $2 "*) return 0 ;;
	esac
	return 1
}

record_count() {
	type=$1
	awk -F'|' -v type="$type" '$1 == type { n++ } END { print n + 0 }' "$2"
}

PR1_MARKERS='DEPLOY_LOCK_CONTENTION DEPLOY_LOCK_LOSER_ZERO_MUTATIONS DEPLOY_LEFTOVER_NEXT_CLEANED DEPLOY_SUSPICIOUS_NEXT DEPLOY_INTERRUPT_CLEANUP DEPLOY_LOCK_FD_NOT_INHERITED PHASE4_PR1_DEPLOY_LOCK'
PR2_MARKERS='LAUNCH_ENV_OVERWRITE LAUNCH_PIN_SWAP_DURING_VERIFY LAUNCH_PIN_SWAP_WHILE_HELD LAUNCH_WHITESPACE_ROOT_PIN LAUNCH_TRUSTED_PATH LAUNCH_VERIFY_FAILURE LAUNCH_SERVER_FAILURE LAUNCH_FINAL_PIN PHASE4_PR2_EXACT_LAUNCHER'
PR3_MARKERS='DEPLOY_PREVIOUS_CAPTURE_EXACT_ENV DEPLOY_FIRST_ABSENT DEPLOY_TRUSTED_SHA256SUM DEPLOY_INVALID_PREVIOUS_ZERO_MUTATION DEPLOY_RECEIPT_PATH_REJECT DEPLOY_RECEIPT_MV_FAIL_CLOSED DEPLOY_RECEIPT_SYNC_FAIL_CLOSED DEPLOY_RECEIPT_INTERRUPT_CLEANUP DEPLOY_PREVIOUS_PIN_SURVIVES_CURRENT_SWAP DEPLOY_PREVIOUS_VERIFY_LOCK_FD_NOT_INHERITED DEPLOY_DEFAULT_STATE_DIR PHASE4_PR3_PREVIOUS_TARGET_RECEIPT'
PR4_DEPLOY_MARKERS='DEPLOY_DB_DANGLING_SYMLINK_REJECT DEPLOY_EXACT_BACKUP_CAPTURE DEPLOY_STALE_ROLLBACK_IGNORED PHASE4_PR4_ATOMIC_DB_RESTORE'
PR4_RESTORE_MARKERS='RESTORE_ATOMIC_REPLACE RESTORE_SIDECARS_REMOVED RESTORE_MISSING_DEST_FIRST_DEPLOY RESTORE_MISSING_DEST_SIDECARS_REMOVED RESTORE_STALE_ROLLBACK_IGNORED RESTORE_EXCLUSIVE_TEMP RESTORE_COPY_FAIL_CLOSED RESTORE_VERIFY_FAIL_CLOSED RESTORE_FILE_SYNC_FAIL_CLOSED RESTORE_DIR_SYNC_FAIL_CLOSED RESTORE_MV_FAIL_PRESERVES_SIDECARS RESTORE_DEST_SWAP_AFTER_PRECHECK RESTORE_POST_RENAME_WAL_SWAP RESTORE_POST_RENAME_WAL_RM_FAIL RESTORE_INTERRUPT_CLEANUP VERIFY_RESTORE_EXACT_SOURCE PHASE4_PR4_RESTORE_HELPER'
PR5_MARKERS='DEPLOY_FIRST_DEPLOY_WAL_SYMLINK_REJECT DEPLOY_CANDIDATE_RESTART_FAIL_PREVIOUS_READY_EXIT1 DEPLOY_CANDIDATE_READINESS_FAIL_PREVIOUS_READY_EXIT1 DEPLOY_STOP_FAIL_ZERO_MUTATION DEPLOY_RECEIPT_MISMATCH_ZERO_MUTATION DEPLOY_PREVIOUS_REVERIFY_FAIL_ZERO_MUTATION DEPLOY_BACKUP_VERIFY_FAIL_STOPPED DEPLOY_RESTORE_FAIL_BEFORE_REPLACE_UNCERTAIN DEPLOY_RESTORE_FAIL_AFTER_REPLACE_UNCERTAIN DEPLOY_POINTER_TEMP_FAIL DEPLOY_POINTER_MV_FAIL DEPLOY_POINTER_READBACK_FAIL DEPLOY_PREVIOUS_RESTART_FAIL_STOPPED DEPLOY_PREVIOUS_READINESS_FAIL_STOPPED DEPLOY_PREVIOUS_READINESS_FINAL_STOP_FAIL DEPLOY_NO_PREVIOUS_STOPPED DEPLOY_ROLLBACK_RECEIPT_WRITE_FAIL_SAFE DEPLOY_ROLLBACK_RECEIPT_SYNC_FAIL_SAFE DEPLOY_ROLLBACK_RECEIPT_MV_FAIL_SAFE DEPLOY_STOP_SHOW_FAIL_ZERO_MUTATION DEPLOY_STOP_SHOW_EMPTY_ZERO_MUTATION DEPLOY_STOP_SHOW_MULTILINE_ZERO_MUTATION DEPLOY_STOP_SHOW_CONTROL_ZERO_MUTATION DEPLOY_STOP_SHOW_UNKNOWN_ZERO_MUTATION DEPLOY_STOP_SHOW_ACTIVATING_ZERO_MUTATION DEPLOY_STOP_SHOW_RELOADING_ZERO_MUTATION DEPLOY_STOP_SHOW_DEACTIVATING_ZERO_MUTATION DEPLOY_STOP_STATE_FAILED_PROVES_STOPPED DEPLOY_PREVIOUS_RESTART_FINAL_STOP_FAIL DEPLOY_NO_PREVIOUS_SWAPPED_CURRENT DEPLOY_RECEIPT_MISMATCH_UNSAFE_LEFT DEPLOY_PREVIOUS_READY_PUBLISH_FAIL_STOPPED'
PR5_AGG_MARKERS='PHASE4_PR5_FAIL_CLOSED_ROLLBACK'
ALL_MARKERS="$PR1_MARKERS $PR2_MARKERS $PR3_MARKERS $PR4_DEPLOY_MARKERS $PR4_RESTORE_MARKERS $PR5_MARKERS $PR5_AGG_MARKERS"
DEPLOY_MARKERS="$PR1_MARKERS $PR3_MARKERS $PR4_DEPLOY_MARKERS $PR4_RESTORE_MARKERS $PR5_MARKERS $PR5_AGG_MARKERS"

SCHEMAS='deployment_attempt_v1 rollback_outcome_v1'
OUTCOMES='candidate_failed stop_failed receipt_mismatch previous_reverify_failed backup_verify_failed restore_failed first_deploy_cleanup_failed pointer_switch_failed previous_restart_failed previous_readiness_failed previous_stop_failed previous_ready no_previous'
CASE_OUTCOMES='candidate_failed receipt_mismatch no_published_outcome'

# Schema digests are reproducible SHA-256 values over the exact canonical
# newline-delimited field-name lists. They never hash receipt payloads.
RECORDED_ATTEMPT_SCHEMA_DIGEST=$(printf '%s\n' \
	kind status candidate_id candidate_digest previous_state previous_id previous_digest |
		sha256sum | awk '{print $1}')
ROLLBACK_OUTCOME_SCHEMA_DIGEST=$(printf '%s\n' \
	kind status candidate_id candidate_digest previous_state previous_id previous_digest \
	deploy_outcome service_state database_state |
		sha256sum | awk '{print $1}')

static_parse() {
	file=$1
	test -f "$file" || return 1
	awk -F'|' '
	function bad(message) { print "ledger: " message > "/dev/stderr"; exit 2 }
	NR == 1 {
		if ($0 != "phase4_acceptance_ledger|1") bad("wrong header")
		header++
		next
	}
	{
		if ($0 == "" || $0 ~ /\r/) bad("blank or CR-containing line")
		if ($1 == "meta") {
			if (NF != 3) bad("meta field count")
			if ($2 !~ /^[a-z][a-z0-9_]*$/) bad("invalid meta field")
			key = "meta|" $2
		} else if ($1 == "ref") {
			if (NF != 4 || $2 !~ /^[a-z][a-z0-9_]*$/ || $3 !~ /^[A-Z0-9_]+$/) bad("invalid ref row")
			key = "ref|" $2 "|" $3
		} else if ($1 == "schema") {
			if (NF != 3 || $2 !~ /^[a-z][a-z0-9_]*$/ || $3 !~ /^[0-9a-f]+$/) bad("invalid schema row")
			key = "schema|" $2
		} else if ($1 == "suite") {
			if (NF != 6 || $2 !~ /^[A-Z0-9_]+$/ || $3 !~ /^[a-z0-9_./-]+$/ || $4 !~ /^[0-9]+$/ || $5 !~ /^[a-z-]+$/ || $6 !~ /^[0-9a-f]+$/) bad("invalid suite row")
			key = "suite|" $2
		} else if ($1 == "marker") {
			if (NF != 5 || $2 !~ /^[A-Z0-9_]+$/ || $3 !~ /^[A-Z0-9_]+$/ || $4 !~ /^(na|[0-9]+)$/ || $5 !~ /^[a-z-]+$/) bad("invalid marker row")
			key = "marker|" $2 "|" $3
		} else if ($1 == "outcome") {
			if (NF != 6 || $2 !~ /^[a-z][a-z0-9_]*$/ || $3 !~ /^[a-z][a-z0-9_]*$/ || $4 !~ /^[0-9a-f]+$/ || $5 !~ /^[A-Z0-9_]+$/ || $6 !~ /^[a-z-]+$/) bad("invalid outcome row")
			key = "outcome|" $2
		} else if ($1 == "case") {
			if (NF != 7 || $2 !~ /^[a-z][a-z0-9_]*$/ || $3 !~ /^[a-z][a-z0-9_]*$/ || $4 !~ /^[A-Z0-9_]+$/ || $5 !~ /^[a-z][a-z0-9_]*$/ || $6 !~ /^[a-z-]+$/ || $7 !~ /^[a-z-]+$/) bad("invalid case row")
			key = "case|" $2
		} else {
			bad("unknown row or field")
		}
		if (seen[key]++) bad("duplicate row: " key)
	}
	END {
		if (header != 1) bad("missing header")
	}
	' "$file"
}

meta_value() {
	key=$1
	file=$2
	awk -F'|' -v key="$key" '$1 == "meta" && $2 == key { print $3 }' "$file"
}

# The first parser is intentionally separate from semantic checks below:
# negative fixtures can exercise the whole static contract without rerunning
# any command or touching a database-shaped fixture.
validate_static() {
	file=$1
	static_parse "$file" || return 1
	if grep -Eqi '(^|[|])/|file://|\.\.' "$file"; then
		printf '%s\n' 'ledger contains an absolute/source path' >&2
		return 1
	fi
	if grep -Eqi 'prompt|token|password|secret|credential|transcript|personal|payload|binding\.key' "$file"; then
		printf '%s\n' 'ledger contains forbidden content' >&2
		return 1
	fi

	meta_value_for() {
		key=$1
		awk -F'|' -v key="$key" '$1 == "meta" && $2 == key { print $3 }' "$file"
	}
	check_meta() {
		key=$1
		expected=$2
		actual=$(meta_value_for "$key")
		[ "$actual" = "$expected" ] || {
			printf 'ledger meta %s mismatch\n' "$key" >&2
			return 1
		}
	}
	check_meta authority_sha d9b2655af78cb242e557b4f42f52ae817bff36a0 || return 1
	check_meta authority_ref origin/main || return 1
	check_meta source_sha d9b2655af78cb242e557b4f42f52ae817bff36a0 || return 1
	check_meta source_ref phase4-pr5-main || return 1
	check_meta branch main || return 1
	check_meta base_ref origin/main || return 1
	check_meta worktree_status clean || return 1
	check_meta ancestry_status pass || return 1
	check_meta release_gate_plan_fingerprint 06f2f871a57bdcc1b2815c869f93d8ef2c42d521a017ad3b4adb65465e2cf6f9 || return 1
	check_meta release_gate_ref pending-exact-candidate || return 1
	check_meta release_gate_status pending || return 1
	check_meta live_boundary NO-GO || return 1
	check_meta activation_performed false || return 1
	check_meta evidence_scope offline-disposable-only || return 1
	check_meta candidate_state not-built || return 1
	check_meta receipt_scope sanitized-schema-and-digests-only || return 1
	check_meta ledger_status NO-GO || return 1
	check_meta marker_row_count 82 || return 1
	check_meta pr5_expected_failure_count 32 || return 1
	check_meta positive_fixture_count 1 || return 1
	check_meta negative_fixture_count 8 || return 1

	[ "$(record_count meta "$file")" -eq 21 ] || return 1
	[ "$(record_count ref "$file")" -eq 11 ] || return 1
	[ "$(record_count schema "$file")" -eq 2 ] || return 1
	[ "$(record_count suite "$file")" -eq 6 ] || return 1
	[ "$(record_count marker "$file")" -eq 82 ] || return 1
	[ "$(record_count outcome "$file")" -eq 13 ] || return 1
	[ "$(record_count case "$file")" -eq 6 ] || return 1

	for row in \
		'ref|pr|PR1|https://github.com/ajhcs/codex-commons/pull/18' \
		'ref|pr|PR2|https://github.com/ajhcs/codex-commons/pull/19' \
		'ref|pr|PR3|https://github.com/ajhcs/codex-commons/pull/20' \
		'ref|pr|PR4|https://github.com/ajhcs/codex-commons/pull/21' \
		'ref|pr|PR5|https://github.com/ajhcs/codex-commons/pull/22' \
		'ref|cursor|PR1|cursor-cloud:reviewed' \
		'ref|cursor|PR2|cursor-cloud:reviewed' \
		'ref|cursor|PR3|cursor-cloud:reviewed' \
		'ref|cursor|PR4|cursor-cloud:reviewed' \
		'ref|cursor|PR5|cursor-cloud:reviewed' \
		'ref|phase4_merge|PR5|d9b2655af78cb242e557b4f42f52ae817bff36a0'; do
		grep -Fqx "$row" "$file" || return 1
	done

	for row in \
		"schema|deployment_attempt_v1|$RECORDED_ATTEMPT_SCHEMA_DIGEST" \
		"schema|rollback_outcome_v1|$ROLLBACK_OUTCOME_SCHEMA_DIGEST"; do
		grep -Fqx "$row" "$file" || return 1
	done

	for row in \
		'suite|PR1|ops/test-deploy.sh|0|pass|bf927760412ad402d73b1cad4325022cc5a5abaa515e66e2fbdc4b141a08e184' \
		'suite|PR2|ops/test-launch.sh|0|pass|7233a2b45732b1cc228f0f75fd885aa1f65e92b415a533faff59360b9f9ef795' \
		'suite|PR3|ops/test-deploy.sh|0|pass|bf927760412ad402d73b1cad4325022cc5a5abaa515e66e2fbdc4b141a08e184' \
		'suite|PR4_DEPLOY|ops/test-deploy.sh|0|pass|bf927760412ad402d73b1cad4325022cc5a5abaa515e66e2fbdc4b141a08e184' \
		'suite|PR4_RESTORE|ops/test-restore.sh|0|pass|a8a568bc7f762b2ceb7c6b43ecdee14e0228cb2672243c68579706b97fbdf1b8' \
		'suite|PR5|ops/test-deploy.sh|0|pass|bf927760412ad402d73b1cad4325022cc5a5abaa515e66e2fbdc4b141a08e184'; do
		grep -Fqx "$row" "$file" || return 1
	done

	check_markers() {
		suite=$1
		expected=$2
		for name in $expected; do
			row=$(awk -F'|' -v suite="$suite" -v name="$name" '$1 == "marker" && $2 == suite && $3 == name { print; n++ } END { if (n != 1) exit 1 }' "$file") || return 1
			IFS='|' read -r kind got_suite got_name child status <<EOF
$row
EOF
			[ "$kind" = marker ] && [ "$got_suite" = "$suite" ] && [ "$got_name" = "$name" ] && [ "$status" = pass ] || return 1
			if contains_word "$PR5_MARKERS" "$name"; then
				[ "$child" = 1 ] || return 1
			else
				[ "$child" = na ] || return 1
			fi
		done
	}
	check_markers PR1 "$PR1_MARKERS" || return 1
	check_markers PR2 "$PR2_MARKERS" || return 1
	check_markers PR3 "$PR3_MARKERS" || return 1
	check_markers PR4_DEPLOY "$PR4_DEPLOY_MARKERS" || return 1
	check_markers PR4_RESTORE "$PR4_RESTORE_MARKERS" || return 1
	check_markers PR5 "$PR5_MARKERS $PR5_AGG_MARKERS" || return 1

	for outcome in $OUTCOMES; do
		row=$(awk -F'|' -v outcome="$outcome" '$1 == "outcome" && $2 == outcome { print; n++ } END { if (n != 1) exit 1 }' "$file") || return 1
		IFS='|' read -r kind got_outcome schema digest marker status <<EOF
$row
EOF
		contains_word "$SCHEMAS" "$schema" || return 1
		contains_word "$ALL_MARKERS" "$marker" || return 1
		[ "$kind" = outcome ] && [ "$got_outcome" = "$outcome" ] && [ "$status" = pass ] || return 1
		case "$schema" in
		deployment_attempt_v1) expected_digest=$RECORDED_ATTEMPT_SCHEMA_DIGEST ;;
		rollback_outcome_v1) expected_digest=$ROLLBACK_OUTCOME_SCHEMA_DIGEST ;;
		*) return 1 ;;
		esac
		[ "$digest" = "$expected_digest" ] || return 1
		case "$outcome" in
		candidate_failed|receipt_mismatch)
			[ "$schema" = rollback_outcome_v1 ] || return 1
			;;
		*)
			contains_word "$OUTCOMES" "$outcome" || return 1
			;;
		esac
	done

	for row in \
		'case|actual_receipt_mismatch_published|receipt_mismatch|DEPLOY_RECEIPT_MISMATCH_ZERO_MUTATION|rollback_outcome_v1|published|pass' \
		'case|unsafe_receipt_left_untouched|no_published_outcome|DEPLOY_RECEIPT_MISMATCH_UNSAFE_LEFT|deployment_attempt_v1|left-untouched|pass' \
		'case|receipt_write_failure_recorded_preserved|no_published_outcome|DEPLOY_ROLLBACK_RECEIPT_WRITE_FAIL_SAFE|deployment_attempt_v1|recorded-preserved|pass' \
		'case|receipt_sync_failure_recorded_preserved|no_published_outcome|DEPLOY_ROLLBACK_RECEIPT_SYNC_FAIL_SAFE|deployment_attempt_v1|recorded-preserved|pass' \
		'case|receipt_mv_failure_recorded_preserved|no_published_outcome|DEPLOY_ROLLBACK_RECEIPT_MV_FAIL_SAFE|deployment_attempt_v1|recorded-preserved|pass' \
		'case|previous_ready_publish_failure_last_candidate_failed|candidate_failed|DEPLOY_PREVIOUS_READY_PUBLISH_FAIL_STOPPED|rollback_outcome_v1|candidate-failed-last-durable|pass'; do
		grep -Fqx "$row" "$file" || return 1
	done
	while IFS='|' read -r kind name outcome marker schema result status; do
		[ "$kind" = case ] || continue
		contains_word "$CASE_OUTCOMES" "$outcome" || return 1
		contains_word "$SCHEMAS" "$schema" || return 1
		[ "$status" = pass ] || return 1
	done <<EOF
$(awk -F'|' '$1 == "case" { print }' "$file")
EOF
}

validate_static "$ledger" || fail 'ledger static contract rejected'

head=$(git -C "$repo_root" rev-parse HEAD)
source_sha=$(meta_value source_sha "$ledger")
authority_sha=$(meta_value authority_sha "$ledger")
git -C "$repo_root" cat-file -e "$authority_sha^{commit}" || fail 'authority commit is unavailable'
git -C "$repo_root" cat-file -e "$source_sha^{commit}" || fail 'source commit is unavailable'
git -C "$repo_root" merge-base --is-ancestor "$authority_sha" "$head" || fail 'authority is not an ancestor of HEAD'
phase5_sha=$(awk -F'|' '$1 == "ref" && $2 == "phase4_merge" && $3 == "PR5" { print $4 }' "$ledger")
git -C "$repo_root" cat-file -e "$phase5_sha^{commit}" || fail 'recorded Phase 4 PR5 merge is unavailable'
git -C "$repo_root" show -s --format='%s' "$phase5_sha" | grep -Fq 'Phase 4 fail-closed rollback state machine' || fail 'recorded PR5 merge subject mismatch'
# Durable recorded branch is main. Checkout may be main, the original
# Phase 4 PR6 feature branch, or detached HEAD; do not require the
# current name to match. Live identity remains authority ancestry and
# a clean worktree.

# The acceptance ledger is evidence for one exact clean authority. The
# checker never grants a pre-commit or scoped-dirty exception.
while IFS= read -r status_line; do
	[ -n "$status_line" ] || continue
	fail "authority worktree is dirty: $status_line"
done <<EOF
$(git -C "$repo_root" status --porcelain=v1)
EOF

run_suite() {
	suite=$1
	script=$2
	out=$tmp/$suite.out
	err=$tmp/$suite.err
	if (cd "$repo_root" && sh "$script") >"$out" 2>"$err"; then
		status=0
	else
		status=$?
	fi
	[ "$status" -eq 0 ] || {
		sed -n '1,80p' "$err" >&2
		fail "$script exited $status"
	}
	printf '%s\n' "$out"
}

deploy_output=$(run_suite deploy ops/test-deploy.sh)
launch_output=$(run_suite launch ops/test-launch.sh)
restore_output=$(run_suite restore ops/test-restore.sh)

marker_digest() {
	file=$1
	awk '/^[A-Z][A-Z0-9_]*=(pass|fail|[0-9]+)$/' "$file" | sha256sum | awk '{print $1}'
}
[ "$(marker_digest "$deploy_output")" = bf927760412ad402d73b1cad4325022cc5a5abaa515e66e2fbdc4b141a08e184 ] || fail 'test-deploy marker digest changed'
[ "$(marker_digest "$launch_output")" = 7233a2b45732b1cc228f0f75fd885aa1f65e92b415a533faff59360b9f9ef795 ] || fail 'test-launch marker digest changed'
[ "$(marker_digest "$restore_output")" = a8a568bc7f762b2ceb7c6b43ecdee14e0228cb2672243c68579706b97fbdf1b8 ] || fail 'test-restore marker digest changed'

check_output() {
	file=$1
	expected=$2
	for name in $expected; do
		pass_count=$(grep -Fxc "$name=pass" "$file" || true)
		[ "$pass_count" -eq 1 ] || fail "$name pass marker count is $pass_count"
		if contains_word "$PR5_MARKERS" "$name"; then
			child_count=$(grep -Fxc "${name}_CHILD_EXIT=1" "$file" || true)
			[ "$child_count" -eq 1 ] || fail "$name child-exit marker count is $child_count"
		else
			child_count=$(grep -Ec "^${name}_CHILD_EXIT=" "$file" || true)
			[ "$child_count" -eq 0 ] || fail "$name unexpectedly has child-exit evidence"
		fi
	done
	while IFS= read -r marker_line; do
		case "$marker_line" in
		*_CHILD_EXIT=*)
			name=${marker_line%_CHILD_EXIT=*}
			exit_code=${marker_line#*_CHILD_EXIT=}
			[ "$exit_code" = 1 ] || fail "$name child exit is not 1"
			;;
		*=pass) name=${marker_line%=pass} ;;
		*) fail "unrecognized marker output: $marker_line" ;;
		esac
		contains_word "$expected" "$name" || fail "unexpected marker output: $name"
	done <<EOF
$(awk '/^[A-Z][A-Z0-9_]*=(pass|fail|[0-9]+)$/' "$file")
EOF
}

check_output "$deploy_output" "$DEPLOY_MARKERS"
check_output "$launch_output" "$PR2_MARKERS"
check_output "$restore_output" "$PR4_RESTORE_MARKERS"

negative_cases=0
expect_static_reject() {
	label=$1
	fixture=$tmp/negative-$negative_cases.ledger
	shift
	cp -- "$ledger" "$fixture"
	"$@" "$fixture"
	if validate_static "$fixture" >/dev/null 2>&1; then
		fail "negative fixture accepted: $label"
	fi
	negative_cases=$((negative_cases + 1))
	printf 'PHASE4_ACCEPTANCE_NEGATIVE_%s=pass\n' "$label"
}

append_unknown() { printf '%s\n' 'meta|unknown_field|value' >> "$1"; }
append_duplicate() { printf '%s\n' 'marker|PR1|DEPLOY_LOCK_CONTENTION|na|pass' >> "$1"; }
mutate_child_exit() { sed -i 's#marker|PR5|DEPLOY_STOP_FAIL_ZERO_MUTATION|1|pass#marker|PR5|DEPLOY_STOP_FAIL_ZERO_MUTATION|0|pass#' "$1"; }
remove_marker() { sed -i '/^marker|PR5|DEPLOY_STOP_SHOW_CONTROL_ZERO_MUTATION|1|pass$/d' "$1"; }
mutate_activation() { sed -i 's#meta|activation_performed|false#meta|activation_performed|true#' "$1"; }
mutate_path() { sed -i 's#ref|cursor|PR1|cursor-cloud:reviewed#ref|cursor|PR1|/var/lib/evidence#' "$1"; }
remove_aggregate() { sed -i '/^marker|PR5|DEPLOY_/d' "$1"; }
append_outcome() { printf 'outcome|unknown_outcome|rollback_outcome_v1|%s|DEPLOY_STOP_FAIL_ZERO_MUTATION|pass\n' "$ROLLBACK_OUTCOME_SCHEMA_DIGEST" >> "$1"; }

expect_static_reject UNKNOWN_FIELD append_unknown
expect_static_reject DUPLICATE_MARKER append_duplicate
expect_static_reject WRONG_CHILD_EXIT mutate_child_exit
expect_static_reject MISSING_MARKER remove_marker
expect_static_reject ACTIVATION_TRUE mutate_activation
expect_static_reject ABSOLUTE_PATH mutate_path
expect_static_reject LONE_AGGREGATE remove_aggregate
expect_static_reject UNKNOWN_OUTCOME append_outcome
[ "$negative_cases" -eq 8 ] || fail "negative fixture count is $negative_cases"

printf 'PHASE4_ACCEPTANCE_STATIC=pass\n'
printf 'PHASE4_ACCEPTANCE_POSITIVE_CASES=1\n'
printf 'PHASE4_ACCEPTANCE_NEGATIVE_CASES=%s\n' "$negative_cases"
printf 'PHASE4_PR6_ACCEPTANCE=pass\n'
