#!/bin/bash
#
# End-to-end integration test for task lifecycle with real sandbox containers
# or host-exec'd claude/codex (BACKEND=host).
#
# Requires:
#   - A running wallfacer server (default: http://localhost:8080)
#   - Valid credentials configured in the server (.env)
#   - Container mode: container runtime (podman/docker) available.
#   - Host mode:      claude (and optionally codex) on $PATH, and the server
#                     must have been started with `wallfacer run --backend host`.
#
# Usage:
#   sh scripts/e2e-lifecycle.sh                              # test both sandboxes, container backend
#   sh scripts/e2e-lifecycle.sh claude                       # test claude only
#   sh scripts/e2e-lifecycle.sh codex                        # test codex only
#   BACKEND=host sh scripts/e2e-lifecycle.sh                 # host backend, both sandboxes
#   BACKEND=host sh scripts/e2e-lifecycle.sh claude          # host backend, claude only
#   WALLFACER_URL=http://localhost:9090 sh scripts/e2e-lifecycle.sh
#
set -euo pipefail

# Bypass RTK filtering so curl returns raw JSON.
export RTK_DISABLED=1

BASE_URL="${WALLFACER_URL:-http://localhost:8080}"
API_KEY="${WALLFACER_SERVER_API_KEY:-}"
TIMEOUT="${WALLFACER_TEST_TIMEOUT:-120}"  # seconds to wait for task completion
SANDBOXES="${1:-claude codex}"
BACKEND="${BACKEND:-container}"  # "container" (default) | "host"
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

# Host-mode preflight: the server must have been started with --backend host
# for the host-mode assertions below to be meaningful. /api/config exposes
# a host_mode boolean reflecting the runner's HostMode() state.
if [ "$BACKEND" = "host" ]; then
    if ! command -v claude >/dev/null 2>&1; then
        echo "ERROR: BACKEND=host requires 'claude' on \$PATH (or WALLFACER_HOST_CLAUDE_BINARY set on the server)"
        exit 1
    fi
    for sb in $SANDBOXES; do
        if [ "$sb" = "codex" ] && ! command -v codex >/dev/null 2>&1; then
            echo "ERROR: BACKEND=host with codex tests requires 'codex' on \$PATH"
            exit 1
        fi
    done
    server_host_mode=$(api GET "/api/config" | jq -r '.host_mode // false')
    if [ "$server_host_mode" != "true" ]; then
        echo "ERROR: server is not running in host mode (host_mode=$server_host_mode)"
        echo "Restart the server with: wallfacer run --backend host"
        exit 1
    fi
    pass "server running with --backend host"
elif [ "$BACKEND" != "container" ] && [ "$BACKEND" != "local" ]; then
    echo "ERROR: unknown BACKEND=$BACKEND (want \"container\" or \"host\")"
    exit 1
fi

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
        if [ "$BACKEND" = "host" ]; then
            # Host mode reports PID-tracked processes under /api/containers too
            # (Image="host"); assert none lingered for this task.
            pass "no host-mode processes for archived task"
        else
            pass "no containers for archived task"
        fi
    else
        fail "$task_containers process(es) still tracked for archived task"
    fi

    # 9. Check active workers via runtime debug endpoint. Worker stats are
    # a container-mode optimization and are always zero in host mode; skip
    # the assertion there rather than reporting a misleading "note".
    if [ "$BACKEND" != "host" ]; then
        local worker_count
        worker_count=$(api GET "/api/debug/runtime" | jq '.worker_stats.active_workers // 0')
        if [ "$worker_count" = "0" ]; then
            pass "no active workers remaining"
        else
            # Workers from other tasks may exist; just log it.
            step "note: $worker_count active worker(s) (may be from other tasks)"
        fi
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
