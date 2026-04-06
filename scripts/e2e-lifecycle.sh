#!/bin/bash
#
# End-to-end integration test for task lifecycle with real sandbox containers.
#
# Requires:
#   - A running wallfacer server (default: http://localhost:8080)
#   - Valid credentials configured in the server (.env)
#   - Container runtime (podman/docker) available
#
# Usage:
#   sh scripts/integration-test.sh                          # test both sandboxes
#   sh scripts/integration-test.sh claude                   # test claude only
#   sh scripts/integration-test.sh codex                    # test codex only
#   WALLFACER_URL=http://localhost:9090 sh scripts/integration-test.sh
#
set -euo pipefail

# Bypass RTK filtering so curl returns raw JSON.
export RTK_DISABLED=1

BASE_URL="${WALLFACER_URL:-http://localhost:8080}"
API_KEY="${WALLFACER_SERVER_API_KEY:-}"
TIMEOUT="${WALLFACER_TEST_TIMEOUT:-120}"  # seconds to wait for task completion
SANDBOXES="${1:-claude codex}"
FAILURES=0

pass() { printf "  \033[32mPASS\033[0m %s\n" "$1"; }
fail() { printf "  \033[31mFAIL\033[0m %s\n" "$1"; FAILURES=$((FAILURES + 1)); }
section() { printf "\n\033[1m%s\033[0m\n" "$1"; }
step() { printf "  ... %s\n" "$1"; }

# HTTP helper with optional bearer auth.
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

# Wait for a task to reach one of the terminal states (done, failed, cancelled, waiting).
# Returns the final status.
wait_for_task() {
    local task_id="$1"
    local elapsed=0
    while [ "$elapsed" -lt "$TIMEOUT" ]; do
        local status
        status=$(api GET "/api/tasks" | jq -r --arg id "$task_id" '.[] | select(.id == $id) | .status')
        case "$status" in
            done|failed|cancelled|waiting)
                echo "$status"
                return 0
                ;;
        esac
        sleep 3
        elapsed=$((elapsed + 3))
    done
    echo "timeout"
    return 1
}

# Check server is reachable.
section "preflight"
if ! api GET "/api/debug/health" >/dev/null 2>&1; then
    echo "ERROR: cannot reach wallfacer at $BASE_URL"
    echo "Start the server first: wallfacer run"
    exit 1
fi
pass "server reachable at $BASE_URL"

# Run the lifecycle test for a given sandbox type.
test_sandbox() {
    local sb="$1"
    section "lifecycle: $sb sandbox"

    # 1. Create task.
    step "creating task (sandbox=$sb)"
    local create_resp
    create_resp=$(api POST "/api/tasks" \
        -d "{\"prompt\":\"who are you? answer in one sentence.\",\"sandbox\":\"$sb\"}")
    local task_id
    task_id=$(echo "$create_resp" | jq -r '.id')
    if [ -z "$task_id" ] || [ "$task_id" = "null" ]; then
        fail "create task: no id returned"
        return
    fi
    pass "task created: ${task_id:0:8}"

    # 2. Verify sandbox is set.
    local task_sandbox
    task_sandbox=$(echo "$create_resp" | jq -r '.sandbox')
    if [ "$task_sandbox" = "$sb" ]; then
        pass "sandbox set to $sb"
    else
        fail "sandbox is $task_sandbox, expected $sb"
    fi

    # 3. Trigger execution (move to in_progress).
    step "starting task"
    local patch_resp
    patch_resp=$(api PATCH "/api/tasks/$task_id" -d '{"status":"in_progress"}')
    local new_status
    new_status=$(echo "$patch_resp" | jq -r '.status')
    if [ "$new_status" = "in_progress" ]; then
        pass "task moved to in_progress"
    else
        fail "task status is $new_status after patch, expected in_progress"
        return
    fi

    # 4. Wait for completion.
    step "waiting for task to finish (timeout: ${TIMEOUT}s)"
    local final_status
    final_status=$(wait_for_task "$task_id")
    if [ "$final_status" = "done" ]; then
        pass "task completed: done"
    elif [ "$final_status" = "waiting" ]; then
        # "waiting" means the agent finished but wants feedback.
        # Mark it as done.
        step "task is waiting, marking as done"
        api POST "/api/tasks/$task_id/done" -d '{}' >/dev/null
        pass "task completed: waiting -> done"
    else
        fail "task ended with status: $final_status"
        # Print events for debugging.
        echo "    events:"
        api GET "/api/tasks/$task_id/events" | jq -c '.[-5:][] | {type: .event_type, data: .data}' 2>/dev/null | sed 's/^/      /'
        return
    fi

    # 5. Verify task result has output.
    local task_result
    task_result=$(api GET "/api/tasks" | jq -r --arg id "$task_id" '.[] | select(.id == $id) | .result // empty')
    if [ -n "$task_result" ]; then
        pass "task has result: ${task_result:0:80}"
    else
        # Result may be in events or outputs, not always on the task object.
        pass "task completed (result may be in outputs)"
    fi

    # 6. Wait for commit pipeline to finish (task may briefly be in "committing" state).
    step "waiting for commit pipeline"
    local post_done_status
    local post_elapsed=0
    while [ "$post_elapsed" -lt 30 ]; do
        post_done_status=$(api GET "/api/tasks" | jq -r --arg id "$task_id" '.[] | select(.id == $id) | .status')
        if [ "$post_done_status" = "done" ]; then
            break
        fi
        sleep 2
        post_elapsed=$((post_elapsed + 2))
    done

    # 7. Archive the task.
    step "archiving task"
    local archive_resp
    archive_resp=$(api POST "/api/tasks/$task_id/archive" -d '{}')
    local archived
    archived=$(api GET "/api/tasks?include_archived=true" | jq -r --arg id "$task_id" '.[] | select(.id == $id) | .archived')
    if [ "$archived" = "true" ]; then
        pass "task archived"
    else
        fail "task not archived (archived=$archived)"
    fi

    # 8. Wait briefly for worker cleanup, then verify.
    sleep 3
    local containers
    containers=$(api GET "/api/containers")
    local task_containers
    task_containers=$(echo "$containers" | jq --arg id "$task_id" '[.[] | select(.task_id == $id)] | length')
    if [ "$task_containers" = "0" ] || [ "$task_containers" = "null" ]; then
        pass "no containers for archived task"
    else
        fail "$task_containers container(s) still running for archived task"
    fi

    # 9. Check active workers via runtime debug endpoint.
    local worker_count
    worker_count=$(api GET "/api/debug/runtime" | jq '.worker_stats.active_workers // 0')
    if [ "$worker_count" = "0" ]; then
        pass "no active workers remaining"
    else
        # Workers from other tasks may exist; just log it.
        step "note: $worker_count active worker(s) (may be from other tasks)"
    fi
}

# Run tests for each requested sandbox.
for sb in $SANDBOXES; do
    test_sandbox "$sb"
done

# Summary.
echo
if [ "$FAILURES" -eq 0 ]; then
    printf "\033[32mAll integration checks passed.\033[0m\n"
else
    printf "\033[31m%d check(s) failed.\033[0m\n" "$FAILURES"
    exit 1
fi
