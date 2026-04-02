// --- Task refinement via sandbox agent ---
//
// Flow:
//   1. User clicks "Start Refinement" → call the task refine action for the task id.
//   2. Sandbox agent runs read-only, produces a spec
//   3. Live logs stream while the refine job is running.
//   4. On completion, result appears in an editable textarea
//   5. User optionally edits and clicks "Apply as Prompt"

let refineTaskId = null;
let refineLogsAbort = null; // AbortController for the live log stream

// updateRefineUI re-renders the refinement panel based on the current task state.
// Called whenever the modal opens or the task object changes via SSE.
function updateRefineUI(task) {
  if (!task || task.status !== "backlog") return;

  const job = task.current_refinement;

  const startBtn = document.getElementById("refine-start-btn");
  const cancelBtn = document.getElementById("refine-cancel-btn");
  const running = document.getElementById("refine-running");
  const resultSec = document.getElementById("refine-result-section");
  const errorSec = document.getElementById("refine-error-section");
  const resultTA = document.getElementById("refine-result-prompt");
  const errorMsg = document.getElementById("refine-error-msg");

  // Determine UI state from job status.
  if (!job) {
    showRefineIdle(startBtn, cancelBtn, running, resultSec, errorSec);
    return;
  }

  if (job.status === "running") {
    startBtn.classList.add("hidden");
    cancelBtn.classList.remove("hidden");
    running.classList.remove("hidden");
    resultSec.classList.add("hidden");
    errorSec.classList.add("hidden");
    const idleDesc = document.getElementById("refine-idle-desc");
    if (idleDesc) idleDesc.classList.add("hidden");
    const instrSec = document.getElementById("refine-instructions-section");
    if (instrSec) instrSec.classList.add("hidden");

    // Attach log stream if this is the active task and not already streaming.
    if (refineTaskId === task.id && !refineLogsAbort) {
      startRefineLogStream(task.id);
    }
    return;
  }

  if (job.status === "done") {
    startBtn.classList.remove("hidden");
    cancelBtn.classList.add("hidden");
    running.classList.add("hidden");
    resultSec.classList.remove("hidden");
    errorSec.classList.add("hidden");
    const idleDesc = document.getElementById("refine-idle-desc");
    if (idleDesc) idleDesc.classList.add("hidden");
    const instrSec = document.getElementById("refine-instructions-section");
    if (instrSec) instrSec.classList.add("hidden");
    stopRefineLogStream();

    // Only populate the textareas if this is the first population for this job.
    if (resultTA.dataset.jobId !== job.id) {
      resultTA.value = job.result || "";
      resultTA.dataset.jobId = job.id;
      const goalTA = document.getElementById("refine-result-goal");
      if (goalTA) goalTA.value = job.goal || "";
    }

    // Show the dismiss button so user can skip applying the refinement.
    const dismissBtn = document.getElementById("refine-dismiss-btn");
    if (dismissBtn) dismissBtn.classList.remove("hidden");
    return;
  }

  if (job.status === "failed") {
    showRefineIdle(startBtn, cancelBtn, running, resultSec, errorSec);
    errorSec.classList.remove("hidden");
    errorMsg.textContent =
      "Refinement failed: " + (job.error || "unknown error");
    stopRefineLogStream();
    return;
  }
}

function showRefineIdle(startBtn, cancelBtn, running, resultSec, errorSec) {
  startBtn.classList.remove("hidden");
  cancelBtn.classList.add("hidden");
  running.classList.add("hidden");
  resultSec.classList.add("hidden");
  errorSec.classList.add("hidden");
  const idleDesc = document.getElementById("refine-idle-desc");
  if (idleDesc) idleDesc.classList.remove("hidden");
  const instrSec = document.getElementById("refine-instructions-section");
  if (instrSec) instrSec.classList.remove("hidden");
  const dismissBtn = document.getElementById("refine-dismiss-btn");
  if (dismissBtn) dismissBtn.classList.add("hidden");
}

// startRefinement is called by the "Start" button.
async function startRefinement() {
  if (!getOpenModalTaskId()) return;

  // If refinement is already running, ignore the click.
  const currentTask = tasks.find((t) => t.id === getOpenModalTaskId());
  if (
    currentTask &&
    currentTask.current_refinement &&
    currentTask.current_refinement.status === "running"
  )
    return;

  refineTaskId = getOpenModalTaskId();

  // Clear prior log output and reset mode.
  refineRawLogBuffer = "";
  refineLogsMode = "pretty";
  setRefineLogsMode("pretty");
  const logsEl = document.getElementById("refine-logs");
  if (logsEl) logsEl.innerHTML = "";

  // Clear prior result textarea job-id so result gets populated fresh.
  const resultTA = document.getElementById("refine-result-prompt");
  if (resultTA) delete resultTA.dataset.jobId;

  try {
    const userInstructions =
      document.getElementById("refine-user-instructions")?.value.trim() || "";
    const updatedTask = await api(task(getOpenModalTaskId()).refine(), {
      method: "POST",
      body: JSON.stringify({ user_instructions: userInstructions }),
    });
    // Immediately show the running state from the 202 response.
    // SSE will also deliver updates, but this avoids a visual gap.
    if (updatedTask) {
      updateRefineUI(updatedTask);
      // Merge the updated task into the local tasks array so the card
      // re-renders with the "refining…" badge and disabled Start button.
      var idx = tasks.findIndex(function (t) {
        return t.id === updatedTask.id;
      });
      if (idx !== -1) tasks[idx] = updatedTask;
      scheduleRender();
    }
  } catch (e) {
    const errorSec = document.getElementById("refine-error-section");
    const errorMsg = document.getElementById("refine-error-msg");
    if (errorSec) errorSec.classList.remove("hidden");
    if (errorMsg)
      errorMsg.textContent = "Failed to start refinement: " + e.message;
  }
}

// cancelRefinement is called by the "Cancel" button.
async function cancelRefinement() {
  if (!refineTaskId) return;
  stopRefineLogStream();
  try {
    await api(task(refineTaskId).refine(), { method: "DELETE" });
  } catch (e) {
    // Ignore — SSE will reflect the updated state.
  }
}

// --- Refine log render scheduler ---
var scheduleRefineLogRender = createRAFScheduler(function () {
  renderRefineLogs();
});

// renderRefineLogs re-renders the refine log area from refineRawLogBuffer.
function renderRefineLogs() {
  const logsEl = document.getElementById("refine-logs");
  if (!logsEl) return;
  // Read scroll state before mutating the DOM to avoid a forced synchronous layout
  // between the read and the subsequent innerHTML write.
  const atBottom =
    logsEl.scrollHeight - logsEl.scrollTop - logsEl.clientHeight < 80;
  if (refineLogsMode === "pretty") {
    logsEl.innerHTML = renderPrettyLogs(refineRawLogBuffer);
  } else {
    logsEl.textContent = refineRawLogBuffer.replace(
      /\x1b\[[0-9;]*[a-zA-Z]/g,
      "",
    );
  }
  if (atBottom) {
    // Defer the scroll-to-bottom to the next frame so the browser can batch
    // the layout triggered by the innerHTML write with the scroll update.
    requestAnimationFrame(function () {
      logsEl.scrollTop = logsEl.scrollHeight;
    });
  }
}

// setRefineLogsMode switches between 'pretty' and 'raw' and re-renders.
function setRefineLogsMode(mode) {
  refineLogsMode = mode;
  ["pretty", "raw"].forEach(function (m) {
    const tab = document.getElementById("refine-logs-tab-" + m);
    if (tab) tab.classList.toggle("active", m === mode);
  });
  renderRefineLogs();
}

// startRefineLogStream opens a streaming fetch to the refine/logs endpoint
// and accumulates chunks into refineRawLogBuffer for pretty/raw rendering.
function startRefineLogStream(taskId) {
  if (refineLogsAbort) return; // already streaming
  refineLogsAbort = new AbortController();

  const decoder = new TextDecoder();

  fetch(withAuthToken(task(taskId).refineLogs()), {
    signal: refineLogsAbort.signal,
    headers: withBearerHeaders(),
  })
    .then(async (resp) => {
      if (resp.status === 204) {
        // Container already done.
        refineLogsAbort = null;
        return;
      }
      if (!resp.ok || !resp.body) {
        refineLogsAbort = null;
        return;
      }
      const reader = resp.body.getReader();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        refineRawLogBuffer += decoder.decode(value, { stream: true });
        scheduleRefineLogRender();
      }
      refineLogsAbort = null;
      // Log stream ended — container exited. Refresh the task so the
      // refinement result appears without waiting for the SSE event.
      if (refineTaskId) {
        fetchTasks().then(function () {
          var openId = getOpenModalTaskId();
          if (openId) {
            var t = tasks.find(function (x) {
              return x.id === openId;
            });
            if (t) {
              updateRefineUI(t);
              renderRefineHistory(t);
            }
          }
        });
      }
    })
    .catch((err) => {
      if (err.name !== "AbortError") {
        console.warn("refine log stream error:", err);
      }
      refineLogsAbort = null;
    });
}

// stopRefineLogStream aborts any active log stream.
function stopRefineLogStream() {
  if (refineLogsAbort) {
    refineLogsAbort.abort();
    refineLogsAbort = null;
  }
}

// resetRefinePanel resets the panel state when the modal closes or switches tasks.
function resetRefinePanel() {
  refineTaskId = null;
  stopRefineLogStream();
  // Reset all sub-elements individually to avoid errors when elements are absent.
  const startBtn = document.getElementById("refine-start-btn");
  const cancelBtn = document.getElementById("refine-cancel-btn");
  const running = document.getElementById("refine-running");
  const resultSec = document.getElementById("refine-result-section");
  const errorSec = document.getElementById("refine-error-section");
  if (startBtn) startBtn.classList.remove("hidden");
  if (cancelBtn) cancelBtn.classList.add("hidden");
  if (running) running.classList.add("hidden");
  if (resultSec) resultSec.classList.add("hidden");
  if (errorSec) errorSec.classList.add("hidden");
  const idleDesc = document.getElementById("refine-idle-desc");
  if (idleDesc) idleDesc.classList.remove("hidden");
  const instrSec = document.getElementById("refine-instructions-section");
  if (instrSec) instrSec.classList.remove("hidden");
  const instrTA = document.getElementById("refine-user-instructions");
  if (instrTA) instrTA.value = "";
  const resultTA = document.getElementById("refine-result-prompt");
  if (resultTA) delete resultTA.dataset.jobId;
  const dismissBtn = document.getElementById("refine-dismiss-btn");
  if (dismissBtn) dismissBtn.classList.add("hidden");
  const applyBtn = document.getElementById("refine-apply-btn");
  if (applyBtn) {
    applyBtn.disabled = false;
    applyBtn.textContent = "Apply as Prompt";
  }
  refineRawLogBuffer = "";
  refineLogsMode = "pretty";
  ["pretty", "raw"].forEach(function (m) {
    const tab = document.getElementById("refine-logs-tab-" + m);
    if (tab) tab.classList.toggle("active", m === "pretty");
  });
  const logsEl = document.getElementById("refine-logs");
  if (logsEl) logsEl.innerHTML = "";
}

// dismissRefinement clears the refinement result without applying it, allowing
// the task to be started with the original prompt.
async function dismissRefinement() {
  if (!getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  try {
    await api(task(taskId).refineDismiss(), { method: "POST" });
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error dismissing refinement: " + e.message);
  }
}

// applyRefinement POSTs the (possibly edited) spec as the new task prompt.
async function applyRefinement() {
  if (!getOpenModalTaskId()) return;
  const resultTA = document.getElementById("refine-result-prompt");
  const newPrompt = resultTA ? resultTA.value.trim() : "";
  if (!newPrompt) {
    showAlert("The refined prompt cannot be empty.");
    return;
  }
  const goalTA = document.getElementById("refine-result-goal");
  const newGoal = goalTA ? goalTA.value.trim() : "";

  const taskId = getOpenModalTaskId();
  const applyBtn = document.getElementById("refine-apply-btn");
  if (applyBtn) {
    applyBtn.disabled = true;
    applyBtn.textContent = "Applying…";
  }
  try {
    // Save settings changes and apply refinement in parallel.
    const sandbox = document.getElementById("modal-edit-sandbox")?.value || "";
    const sandboxByActivity = collectSandboxByActivity("modal-edit-sandbox-");
    const timeout =
      parseInt(document.getElementById("modal-edit-timeout")?.value, 10) ||
      DEFAULT_TASK_TIMEOUT;
    const mountWorktrees =
      document.getElementById("modal-edit-mount-worktrees")?.checked || false;
    await Promise.all([
      api(task(taskId).update(), {
        method: "PATCH",
        body: JSON.stringify({
          sandbox,
          sandbox_by_activity: sandboxByActivity,
          timeout,
          mount_worktrees: mountWorktrees,
        }),
      }),
      api(task(taskId).refineApply(), {
        method: "POST",
        body: JSON.stringify({ prompt: newPrompt, goal: newGoal }),
      }),
    ]);

    await waitForTaskDelta(taskId);
    openModal(taskId);
  } catch (e) {
    showAlert("Error applying refinement: " + e.message);
    if (applyBtn) {
      applyBtn.disabled = false;
      applyBtn.textContent = "Apply as Prompt";
    }
  }
}

// diffTextLines produces a line-level diff between `before` and `after` texts.
// Returns an array of {op: 'eq'|'ins'|'del', text: string} objects in document order.
// Uses a longest-common-subsequence backtrack — no external dependencies.
function diffTextLines(before, after) {
  const a = before.split("\n");
  const b = after.split("\n");
  const m = a.length,
    n = b.length;
  // Build LCS DP table (O(m*n) — prompt-sized inputs are well within budget).
  const dp = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      dp[i][j] =
        a[i - 1] === b[j - 1]
          ? dp[i - 1][j - 1] + 1
          : Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }
  // Backtrack from dp[m][n] to recover operations in reverse order.
  const ops = [];
  let i = m,
    j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && a[i - 1] === b[j - 1]) {
      ops.push({ op: "eq", text: a[i - 1] });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      ops.push({ op: "ins", text: b[j - 1] });
      j--;
    } else {
      ops.push({ op: "del", text: a[i - 1] });
      i--;
    }
  }
  return ops.reverse();
}

// renderTextDiff populates `container` with a visual line-level diff of before→after.
// Uses the same diff-add / diff-del CSS classes as modal-diff.js for palette consistency.
function renderTextDiff(container, before, after) {
  const ops = diffTextLines(before, after);
  const lines = ops.map((op) => {
    const prefix = op.op === "ins" ? "+" : op.op === "del" ? "-" : " ";
    const cls =
      op.op === "ins"
        ? "diff-line diff-add"
        : op.op === "del"
          ? "diff-line diff-del"
          : "diff-line text-gray-400";
    return `<span class="${cls}">${escapeHtml(prefix + op.text)}</span>`;
  });
  container.innerHTML = `<div class="font-mono text-xs whitespace-pre-wrap">${lines.join("\n")}</div>`;
}

// toggleRefineDiff shows or hides the inline diff for a refine history session card.
// The diff is rendered lazily on first click; subsequent clicks only toggle visibility.
function toggleRefineDiff(sessionIndex) {
  const container = document.getElementById("refine-diff-" + sessionIndex);
  const btn = document.getElementById("refine-diff-btn-" + sessionIndex);
  if (!container) return;
  if (!container.dataset.rendered) {
    const currentTask = tasks.find((t) => t.id === getOpenModalTaskId());
    if (!currentTask || !currentTask.refine_sessions) return;
    const session = currentTask.refine_sessions[sessionIndex];
    if (!session || !session.start_prompt || !session.result_prompt) return;
    renderTextDiff(container, session.start_prompt, session.result_prompt);
    container.dataset.rendered = "true";
  }
  const isHidden = container.classList.contains("hidden");
  container.classList.toggle("hidden");
  if (btn) btn.textContent = isHidden ? "Hide diff" : "Show diff";
}

// renderRefineHistory populates the history section from task.refine_sessions.
function renderRefineHistory(task) {
  const section = document.getElementById("refine-history-section");
  const list = document.getElementById("refine-history-list");
  const sessions = task.refine_sessions || [];
  if (sessions.length === 0) {
    section.classList.add("hidden");
    return;
  }
  section.classList.remove("hidden");
  list.innerHTML = sessions
    .map((s, i) => {
      const date = new Date(s.created_at).toLocaleString();
      const previewPrompt = s.start_prompt || "";
      const resultPrompt = s.result_prompt || "";
      const sandboxResult = s.result || "";
      return `<details class="refine-history-entry">
      <summary class="refine-history-summary">
        <span class="text-xs text-v-muted">#${i + 1} · ${escapeHtml(date)}</span>
      </summary>
      <div style="padding:8px 0 0 0;">
        <div class="text-xs text-v-muted" style="margin-bottom:4px;">Starting prompt:</div>
        <pre class="code-block text-xs" style="white-space:pre-wrap;word-break:break-word;opacity:0.7;">${escapeHtml(previewPrompt)}</pre>
        ${
          sandboxResult
            ? `
        <details style="margin-top:8px;">
          <summary class="text-xs text-v-muted" style="cursor:pointer;">Sandbox spec (before editing)</summary>
          <pre class="code-block text-xs" style="white-space:pre-wrap;word-break:break-word;margin-top:4px;opacity:0.8;">${escapeHtml(sandboxResult)}</pre>
        </details>
        `
            : ""
        }
        ${
          resultPrompt
            ? `
        <div class="text-xs text-v-muted" style="margin-top:8px;margin-bottom:4px;">Applied prompt:</div>
        <pre class="code-block text-xs" style="white-space:pre-wrap;word-break:break-word;">${escapeHtml(resultPrompt)}</pre>
        <button class="btn btn-ghost text-xs" style="margin-top:6px;" onclick="revertToHistoryPrompt(${i})">Revert to this version</button>
        <button id="refine-diff-btn-${i}" class="btn btn-ghost text-xs" style="margin-top:6px;margin-left:6px;" onclick="toggleRefineDiff(${i})">Show diff</button>
        <div id="refine-diff-${i}" class="hidden" style="margin-top:8px;"></div>
        `
            : ""
        }
      </div>
    </details>`;
    })
    .join("");
}

// revertToHistoryPrompt loads a previous session's applied prompt into the result textarea.
function revertToHistoryPrompt(sessionIndex) {
  const currentTask = tasks.find((t) => t.id === getOpenModalTaskId());
  if (!currentTask || !currentTask.refine_sessions) return;
  const session = currentTask.refine_sessions[sessionIndex];
  if (!session || !session.result_prompt) return;

  const resultTA = document.getElementById("refine-result-prompt");
  if (!resultTA) return;

  // Show the result section so the user can apply.
  document.getElementById("refine-result-section").classList.remove("hidden");
  resultTA.value = session.result_prompt;
  delete resultTA.dataset.jobId;
}
