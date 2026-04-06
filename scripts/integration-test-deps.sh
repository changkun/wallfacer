#!/bin/bash
#
# End-to-end integration test for dependent task execution with commit verification.
#
# Creates a DAG of tasks:
#   a: create test.md
#   b,c,d,e,f,g: update test.md to '0','1','2','3','4','5' (all depend on a)
#   h: delete test.md (depends on b,c,d,e,f,g)
#
# Enables autopilot, autotest, autosubmit (not ideation or push).
# Verifies each task produces one commit and test.md is deleted at the end.
# Reports the commit order at the end.
#
# Requires:
#   - A running wallfacer server (default: http://localhost:8080)
#   - The server's workspace pointed at a fresh git repo or empty directory
#   - Valid credentials configured in the server (.env)
#
# Usage:
#   # Scenario 1: fresh git repo
#   WORKSPACE=$(mktemp -d) && git -C "$WORKSPACE" init -b main && git -C "$WORKSPACE" commit --allow-empty -m "init"
#   # Start wallfacer with: WALLFACER_WORKSPACES=$WORKSPACE wallfacer run
#   sh scripts/integration-test-deps.sh "$WORKSPACE"
#
#   # Scenario 2: empty directory (no git)
#   WORKSPACE=$(mktemp -d)
#   # Start wallfacer with: WALLFACER_WORKSPACES=$WORKSPACE wallfacer run
#   sh scripts/integration-test-deps.sh "$WORKSPACE"
#
set -euo pipefail

# Bypass RTK filtering so curl returns raw JSON.
export RTK_DISABLED=1

WORKSPACE="${1:?usage: $0 <workspace-path>}"
BASE_URL="${WALLFACER_URL:-http://localhost:8080}"
API_KEY="${WALLFACER_SERVER_API_KEY:-}"
TIMEOUT="${WALLFACER_TEST_TIMEOUT:-300}"
FAILURES=0

pass() { printf "  \033[32mPASS\033[0m %s\n" "$1"; }
fail() { printf "  \033[31mFAIL\033[0m %s\n" "$1"; FAILURES=$((FAILURES + 1)); }
section() { printf "\n\033[1m%s\033[0m\n" "$1"; }
step() { printf "  ... %s\n" "$1"; }

api() {
    local method="$1" path="$2"
    shift 2
    local url="${BASE_URL}${path}"
    if [ -n "$API_KEY" ]; then
        curl -sf -X "$method" -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" "$url" "$@"
    else
        curl -sf -X "$method" -H "Content-Type: application/json" "$url" "$@"
    fi
}

# Wait for all tasks to reach a terminal state.
wait_all_done() {
    local elapsed=0
    while [ "$elapsed" -lt "$TIMEOUT" ]; do
        local pending
        pending=$(api GET "/api/tasks" | jq '[.[] | select(.status != "done" and .status != "failed" and .status != "cancelled")] | length')
        if [ "$pending" = "0" ]; then
            return 0
        fi
        sleep 5
        elapsed=$((elapsed + 5))
    done
    return 1
}

# --- Preflight ---
section "preflight"

if ! api GET "/api/debug/health" >/dev/null 2>&1; then
    echo "ERROR: cannot reach wallfacer at $BASE_URL"
    echo "Start the server first: WALLFACER_WORKSPACES=$WORKSPACE wallfacer run"
    exit 1
fi
pass "server reachable at $BASE_URL"

if [ -d "$WORKSPACE/.git" ]; then
    pass "workspace is a git repo: $WORKSPACE"
else
    step "workspace is not a git repo: $WORKSPACE"
fi

# --- Configure workspace ---
section "setup"
step "setting workspace to $WORKSPACE"
api PUT "/api/workspaces" -d "{\"workspaces\":[\"$WORKSPACE\"]}" >/dev/null
sleep 2
pass "workspace set"

# Set max parallel tasks to 3.
step "setting max parallel tasks to 3"
api PUT "/api/env" -d '{"max_parallel_tasks": 3}' >/dev/null
pass "max parallel tasks: 3"

# Enable automation (autopilot, autotest, autosubmit) but not ideation/push.
step "enabling automation"
api PUT "/api/config" -d '{
    "autopilot": true,
    "autotest": true,
    "autosubmit": true,
    "autosync": true,
    "autopush": false
}' >/dev/null
pass "autopilot, autotest, autosubmit, autosync enabled"

# --- Create task DAG ---
section "create tasks"
step "creating 8 tasks with dependency DAG"

batch_resp=$(api POST "/api/tasks/batch" -d '{
    "tasks": [
        {"ref": "a", "prompt": "Create a file called test.md with the content: hello"},
        {"ref": "b", "prompt": "Update test.md: replace its entire content with exactly the single character: 0", "depends_on_refs": ["a"]},
        {"ref": "c", "prompt": "Update test.md: replace its entire content with exactly the single character: 1", "depends_on_refs": ["a"]},
        {"ref": "d", "prompt": "Update test.md: replace its entire content with exactly the single character: 2", "depends_on_refs": ["a"]},
        {"ref": "e", "prompt": "Update test.md: replace its entire content with exactly the single character: 3", "depends_on_refs": ["a"]},
        {"ref": "f", "prompt": "Update test.md: replace its entire content with exactly the single character: 4", "depends_on_refs": ["a"]},
        {"ref": "g", "prompt": "Update test.md: replace its entire content with exactly the single character: 5", "depends_on_refs": ["a"]},
        {"ref": "h", "prompt": "Delete the file test.md completely. Remove it from the repository.", "depends_on_refs": ["b","c","d","e","f","g"]}
    ]
}')

task_count=$(echo "$batch_resp" | jq '.tasks | length')
if [ "$task_count" = "8" ]; then
    pass "8 tasks created"
else
    fail "expected 8 tasks, got $task_count"
    echo "    response: $(echo "$batch_resp" | jq -c .)"
    exit 1
fi

# Show the ref-to-id mapping.
echo "$batch_resp" | jq -r '.ref_to_id | to_entries[] | "      \(.key) -> \(.value[:8])"'

task_a_id=$(echo "$batch_resp" | jq -r '.ref_to_id.a')
task_h_id=$(echo "$batch_resp" | jq -r '.ref_to_id.h')

# --- Execute ---
section "execute"

# Start task a. Autopilot promotes b-g when a completes, then h when b-g complete.
step "starting task a (autopilot handles the rest)"
api PATCH "/api/tasks/$task_a_id" -d '{"status":"in_progress"}' >/dev/null
pass "task a in_progress"

step "waiting for all 8 tasks to finish (timeout: ${TIMEOUT}s)"
if wait_all_done; then
    pass "all tasks reached terminal state"
else
    fail "timeout: some tasks still pending"
    api GET "/api/tasks" | jq -r '.[] | "      \(.id[:8]) status=\(.status) \(.title // .prompt[:50])"'
fi

# --- Verify ---
section "verify"

# Check no tasks failed.
failed_tasks=$(api GET "/api/tasks" | jq -r '.[] | select(.status == "failed") | .id[:8] + " " + (.prompt[:60])')
if [ -z "$failed_tasks" ]; then
    pass "no failed tasks"
else
    fail "some tasks failed:"
    echo "$failed_tasks" | sed 's/^/      /'
fi

# Check each task finished as done.
done_count=$(api GET "/api/tasks" | jq '[.[] | select(.status == "done")] | length')
if [ "$done_count" = "8" ]; then
    pass "all 8 tasks are done"
else
    fail "$done_count/8 tasks are done"
    api GET "/api/tasks" | jq -r '.[] | "      \(.id[:8]) \(.status)"'
fi

# Check test.md is deleted.
if [ ! -f "$WORKSPACE/test.md" ]; then
    pass "test.md deleted in workspace"
else
    fail "test.md still exists (content: $(cat "$WORKSPACE/test.md"))"
fi

# Check git commits (if git repo).
if [ -d "$WORKSPACE/.git" ]; then
    section "commit history"

    commit_count=$(git -C "$WORKSPACE" log --oneline | wc -l | tr -d ' ')
    step "$commit_count total commits"

    # Each task should produce at least one commit (8 tasks + 1 initial = 9 minimum).
    if [ "$commit_count" -ge 9 ]; then
        pass "at least 9 commits ($commit_count total: 1 initial + 8 tasks)"
    else
        fail "expected >= 9 commits, got $commit_count"
    fi

    echo
    echo "    commit order (oldest first):"
    git -C "$WORKSPACE" log --oneline --reverse | sed 's/^/      /'
else
    section "commit history (skipped: not a git repo)"
fi

# --- Archive all done tasks ---
section "cleanup"
step "archiving all done tasks"
api POST "/api/tasks/archive-done" -d '{}' >/dev/null
sleep 2

archived_count=$(api GET "/api/tasks?include_archived=true" | jq '[.[] | select(.archived == true)] | length')
if [ "$archived_count" = "8" ]; then
    pass "all 8 tasks archived"
else
    fail "expected 8 archived, got $archived_count"
fi

# Check no containers remain for these tasks.
container_count=$(api GET "/api/containers" | jq 'length')
if [ "$container_count" = "0" ] || [ "$container_count" = "null" ]; then
    pass "no containers remaining"
else
    step "$container_count container(s) still present (may be from other work)"
fi

# --- Summary ---
echo
if [ "$FAILURES" -eq 0 ]; then
    printf "\033[32mAll integration checks passed.\033[0m\n"
else
    printf "\033[31m%d check(s) failed.\033[0m\n" "$FAILURES"
    exit 1
fi
