#!/usr/bin/env bash
# Checks the eval tasks in this directory. For each task it verifies that:
#   - task.json is well formed and every protected file exists in repo/,
#   - repo/ fails test_command, and the output shows the intended failure,
#   - repo/ plus solution.patch (with protected files restored) passes,
#   - Go code in the patched copy is gofmt-clean.
# Usage: validate.sh [task-id ...]   (no arguments: all tasks)
set -uo pipefail

TASKS_DIR="$(cd "$(dirname "$0")" && pwd)"
export GOFLAGS=-count=1 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off PYTHONDONTWRITEBYTECODE=1

# expected_failure prints an extended regex that the output of the unpatched
# run must match, so a task cannot "fail" for an unrelated reason.
expected_failure() {
	case "$1" in
	go-lru-eviction) echo 'FAIL: TestGetMarksKeyAsRecentlyUsed' ;;
	go-duration-config) echo 'Parse\("250ms"\) error: duration: missing number|Parse\("1\.5h"\) = 1h0m0s, want 1h30m0s' ;;
	go-cli-flags) echo 'run panicked: .*index out of range|-n=3.*exit code 2, want 0' ;;
	go-http-status) echo 'POST /notes: status 200, want 201' ;;
	go-counter-race) echo 'WARNING: DATA RACE' ;;
	go-dedupe-perf) echo 'did not finish within' ;;
	go-worker-pool) echo 'Run did not return within|Run returned while a started call was still running' ;;
	go-invoice-rounding) echo 'no discount: Tax = 4\.66, want 4\.68' ;;
	py-pagination) echo 'FAIL: test_first_page' ;;
	py-dotenv-parser) echo 'ERROR: test_value_containing_equals' ;;
	py-sales-report) echo 'FAIL: test_average_rounds_half_up' ;;
	py-sla-deadline) echo 'FAIL: test_deadline_at_end_of_day' ;;
	py-shipping-rules-refactor) echo 'rules module is missing' ;;
	py-inventory-reservations) echo '(FAIL|ERROR): test_reserve_last_unit' ;;
	py-sessionize-perf) echo 'did not finish within' ;;
	*) return 1 ;;
	esac
}

# Output that means the tests did not run at all.
BROKEN_RUN='\[build failed\]|\[setup failed\]|Failed to import test module|SyntaxError|NO TESTS RAN|^Ran 0 tests'

failures=0
fail() {
	echo "  FAIL: $*"
	failures=$((failures + 1))
}

run_with_timeout() {
	local secs=$1
	shift
	perl -e 'alarm shift @ARGV; exec @ARGV or die "exec failed: $!\n"' "$secs" "$@"
}

WORK="$(mktemp -d "${TMPDIR:-/tmp}/eval-validate.XXXXXX")"
trap 'chmod -R u+w "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

if [ $# -gt 0 ]; then
	ids=("$@")
else
	ids=()
	for d in "$TASKS_DIR"/*/; do
		[ -f "$d/task.json" ] && ids+=("$(basename "$d")")
	done
fi

n_go=0 n_py=0 n_easy=0 n_medium=0 n_hard=0

for id in "${ids[@]}"; do
	dir="$TASKS_DIR/$id"
	echo "== $id"
	if [ ! -f "$dir/task.json" ] || [ ! -d "$dir/repo" ] || [ ! -f "$dir/solution.patch" ]; then
		fail "missing task.json, repo/ or solution.patch"
		continue
	fi

	if ! jq -e --arg id "$id" '
		.id == $id
		and (.language == "go" or .language == "python")
		and (.difficulty == "easy" or .difficulty == "medium" or .difficulty == "hard")
		and (.prompt | type == "string" and length > 0)
		and (.test_command | type == "array" and length > 0 and all(type == "string"))
		and (.protected | type == "array" and length > 0 and all(type == "string"))
		and (.timeout_seconds | type == "number" and . > 0)
	' "$dir/task.json" >/dev/null; then
		fail "task.json does not match the schema"
		continue
	fi

	lang=$(jq -r .language "$dir/task.json")
	difficulty=$(jq -r .difficulty "$dir/task.json")
	timeout_seconds=$(jq -r .timeout_seconds "$dir/task.json")
	cmd=()
	while IFS= read -r arg; do cmd+=("$arg"); done < <(jq -r '.test_command[]' "$dir/task.json")
	protected=()
	while IFS= read -r p; do protected+=("$p"); done < <(jq -r '.protected[]' "$dir/task.json")

	case "$lang" in go) n_go=$((n_go + 1)) ;; python) n_py=$((n_py + 1)) ;; esac
	case "$difficulty" in easy) n_easy=$((n_easy + 1)) ;; medium) n_medium=$((n_medium + 1)) ;; hard) n_hard=$((n_hard + 1)) ;; esac

	for p in "${protected[@]}"; do
		[ -f "$dir/repo/$p" ] || fail "protected file $p does not exist"
	done
	patched_files=$(sed -n -E 's|^\+\+\+ b/([^[:space:]]+).*|\1|p' "$dir/solution.patch")
	for p in "${protected[@]}"; do
		if printf '%s\n' "$patched_files" | grep -qxF "$p"; then
			fail "solution.patch modifies protected file $p"
		fi
	done
	if [ "$lang" = go ]; then
		grep -qx 'module evaltask' "$dir/repo/go.mod" && grep -qx 'go 1.26' "$dir/repo/go.mod" ||
			fail "go.mod must declare module evaltask and go 1.26"
	fi
	pattern=$(expected_failure "$id") || {
		fail "no expected failure pattern registered in validate.sh"
		pattern='^$'
	}

	# 1. The starting repository must fail for the intended reason.
	start="$WORK/$id-start"
	cp -R "$dir/repo" "$start"
	out=$(cd "$start" && run_with_timeout "$timeout_seconds" "${cmd[@]}" 2>&1)
	code=$?
	if [ $code -eq 0 ]; then
		fail "starting repo passes test_command"
	elif [ $code -eq 142 ]; then
		fail "starting repo timed out after ${timeout_seconds}s"
	elif printf '%s\n' "$out" | grep -qE "$BROKEN_RUN"; then
		fail "starting repo fails without running the tests:"
		printf '%s\n' "$out" | tail -20 | sed 's/^/    /'
	elif ! printf '%s\n' "$out" | grep -qE "$pattern"; then
		fail "starting repo output does not match /$pattern/:"
		printf '%s\n' "$out" | tail -20 | sed 's/^/    /'
	else
		echo "  start:    fails as intended (exit $code, matched /$pattern/)"
	fi

	# 2. The patched repository must pass, with protected files restored.
	fixed="$WORK/$id-fixed"
	cp -R "$dir/repo" "$fixed"
	if ! patch_out=$(patch -p1 -s -d "$fixed" <"$dir/solution.patch" 2>&1); then
		fail "solution.patch does not apply:"
		printf '%s\n' "$patch_out" | sed 's/^/    /'
		continue
	fi
	for p in "${protected[@]}"; do
		cp "$dir/repo/$p" "$fixed/$p"
	done
	out=$(cd "$fixed" && run_with_timeout "$timeout_seconds" "${cmd[@]}" 2>&1)
	code=$?
	if [ $code -ne 0 ]; then
		fail "patched repo fails test_command (exit $code):"
		printf '%s\n' "$out" | tail -30 | sed 's/^/    /'
	else
		echo "  solution: passes"
	fi

	# 3. Go code must be gofmt-clean after the patch.
	if [ "$lang" = go ]; then
		unformatted=$(cd "$fixed" && gofmt -l .)
		if [ -n "$unformatted" ]; then
			fail "gofmt -l reports: $unformatted"
		else
			echo "  gofmt:    clean"
		fi
	fi
done

echo
echo "tasks: ${#ids[@]} (go $n_go, python $n_py; easy $n_easy, medium $n_medium, hard $n_hard)"
if [ $# -eq 0 ]; then
	[ ${#ids[@]} -eq 15 ] || fail "want 15 tasks, have ${#ids[@]}"
	[ $n_go -eq 8 ] && [ $n_py -eq 7 ] || fail "want 8 Go and 7 Python tasks"
	[ $n_easy -eq 5 ] && [ $n_medium -eq 6 ] && [ $n_hard -eq 4 ] || fail "want 5 easy, 6 medium, 4 hard tasks"
fi
if [ $failures -gt 0 ]; then
	echo "validate: $failures problem(s)"
	exit 1
fi
echo "validate: all checks passed"
