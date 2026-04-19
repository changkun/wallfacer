const DEFAULT_TASK_TIMEOUT = 60; // minutes

// --- Dependency picker helpers ---

/**
 * Updates the chips display in a dep-picker based on currently checked items.
 * Also fires the picker's data-onchange callback if set.
 */
function updateDepPickerChips(wrapperId, fireCallback) {
  var wrap = document.getElementById(wrapperId);
  if (!wrap) return;
  var chipsEl = wrap.querySelector(".dep-picker-chips");
  var checked = wrap.querySelectorAll(
    ".dep-picker-item input[type=checkbox]:checked",
  );
  if (checked.length === 0) {
    chipsEl.innerHTML = '<span class="dep-picker-placeholder">None</span>';
  } else {
    chipsEl.innerHTML = "";
    checked.forEach(function (cb) {
      var text = cb
        .closest(".dep-picker-item")
        .querySelector(".dep-picker-item-text").textContent;
      var chip = document.createElement("span");
      chip.className = "dep-picker-chip";
      chip.title = text;
      chip.textContent = text;
      chipsEl.appendChild(chip);
    });
  }
  if (fireCallback) {
    var cbName = wrap.dataset.onchange;
    if (cbName && typeof window[cbName] === "function") window[cbName]();
  }
}

/**
 * Populates a dep-picker with tasks as checkbox items.
 * Excludes the task with excludeId (null to include all).
 * Pre-selects UUIDs in selectedIds array.
 */
function populateDependsOnPicker(wrapperId, excludeId, selectedIds) {
  var wrap = document.getElementById(wrapperId);
  if (!wrap) return;
  var list = wrap.querySelector(".dep-picker-list");
  var search = wrap.querySelector(".dep-picker-search");
  if (search) search.value = "";
  list.innerHTML = "";
  var statusPriority = { in_progress: 0, waiting: 1, backlog: 2, done: 3 };
  var candidates = tasks
    .filter(function (t) {
      return t.id !== excludeId;
    })
    .slice()
    .sort(function (a, b) {
      var pa =
        statusPriority[a.status] !== undefined ? statusPriority[a.status] : 4;
      var pb =
        statusPriority[b.status] !== undefined ? statusPriority[b.status] : 4;
      return pa - pb;
    });
  if (candidates.length === 0) {
    list.innerHTML = '<div class="dep-picker-empty">No other tasks</div>';
    updateDepPickerChips(wrapperId, false);
    return;
  }
  candidates.forEach(function (t) {
    var label =
      t.title ||
      (t.prompt.length > 60 ? t.prompt.slice(0, 60) + "\u2026" : t.prompt);
    var status = t.status === "in_progress" ? "in progress" : t.status;
    var isSelected =
      Array.isArray(selectedIds) && selectedIds.indexOf(t.id) !== -1;
    var item = document.createElement("label");
    item.className = "dep-picker-item" + (isSelected ? " selected" : "");
    var cb = document.createElement("input");
    cb.type = "checkbox";
    cb.value = t.id;
    cb.checked = isSelected;
    var textSpan = document.createElement("span");
    textSpan.className = "dep-picker-item-text";
    textSpan.textContent = label;
    var badge = document.createElement("span");
    badge.className = "badge badge-" + t.status;
    badge.textContent = status;
    item.appendChild(cb);
    item.appendChild(textSpan);
    item.appendChild(badge);
    cb.addEventListener("change", function () {
      item.classList.toggle("selected", cb.checked);
      updateDepPickerChips(wrapperId, true);
    });
    list.appendChild(item);
  });
  updateDepPickerChips(wrapperId, false);
}

/** Returns array of selected task IDs from a dep-picker. */
function getDepPickerValues(wrapperId) {
  var wrap = document.getElementById(wrapperId);
  if (!wrap) return [];
  return Array.from(
    wrap.querySelectorAll(".dep-picker-item input[type=checkbox]:checked"),
  ).map(function (cb) {
    return cb.value;
  });
}

/** Toggles the dep-picker dropdown open/closed. */
function toggleDepPicker(wrapperId) {
  var wrap = document.getElementById(wrapperId);
  var isOpen = wrap.classList.contains("open");
  // Close all open pickers
  document.querySelectorAll(".dep-picker.open").forEach(function (p) {
    p.classList.remove("open");
    p.querySelector(".dep-picker-dropdown").style.display = "none";
  });
  if (!isOpen) {
    wrap.classList.add("open");
    wrap.querySelector(".dep-picker-dropdown").style.display = "";
    var search = wrap.querySelector(".dep-picker-search");
    if (search) {
      filterDepPicker(search);
      search.focus();
    }
  }
}

/** Filters dep-picker items based on search input. */
function filterDepPicker(inputEl) {
  var search = inputEl.value.toLowerCase();
  var list = inputEl
    .closest(".dep-picker-dropdown")
    .querySelector(".dep-picker-list");
  list.querySelectorAll(".dep-picker-item").forEach(function (item) {
    var text = item
      .querySelector(".dep-picker-item-text")
      .textContent.toLowerCase();
    item.style.display = text.includes(search) ? "" : "none";
  });
}

// Close any open dep-picker when clicking outside
document.addEventListener("click", function (e) {
  if (!e.target.closest(".dep-picker")) {
    document.querySelectorAll(".dep-picker.open").forEach(function (p) {
      p.classList.remove("open");
      p.querySelector(".dep-picker-dropdown").style.display = "none";
    });
  }
});

// --- Tag input helpers ---

function renderTagChips(containerId, tags) {
  const container = document.getElementById(containerId);
  if (!container) return;
  container._tags = Array.isArray(tags) ? tags : [];
  const chips = container._tags
    .map(function (tag, index) {
      const style =
        typeof tagStyle === "function"
          ? tagStyle(tag)
          : "background:var(--tag-bg-0);color:var(--tag-text-0);";
      return `<span class="tag-chip tag-chip-edit" style="${style}">${escapeHtml(tag)}<button class="tag-chip-remove" data-idx="${index}" title="Remove tag" onclick="event.preventDefault();event.stopPropagation();_removeTagAt(this.closest('[id]'), ${index})">×</button></span>`;
    })
    .join("");
  container.innerHTML = `<div class="tag-chip-row tag-chip-input-row">${chips}<input class="tag-chip-input" type="text" placeholder="Add tag…" maxlength="32"></div>`;
  const input = container.querySelector(".tag-chip-input");
  if (!input) return;
  input.addEventListener("keydown", function (e) {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      _addTag(container, input.value);
      input.value = "";
    } else if (
      e.key === "Backspace" &&
      input.value === "" &&
      container._tags.length > 0
    ) {
      e.preventDefault();
      _removeTagAt(container, container._tags.length - 1);
    }
  });
}

function initTagInput(containerId, initialTags) {
  const container = document.getElementById(containerId);
  if (!container) return;
  container._tags = (initialTags || [])
    .map(function (tag) {
      return String(tag).trim().toLowerCase();
    })
    .filter(Boolean);
  renderTagChips(containerId, container._tags);
}

function _notifyTagInputChange(container) {
  if (!container) return;
  const cbName = container.dataset.onchange;
  if (cbName && typeof window[cbName] === "function") window[cbName]();
}

function _addTag(container, rawValue) {
  const tag = String(rawValue || "")
    .trim()
    .toLowerCase();
  if (!tag) return;
  if (!container._tags.includes(tag)) container._tags.push(tag);
  renderTagChips(container.id, container._tags);
  _notifyTagInputChange(container);
}

function _removeTagAt(container, index) {
  if (!container || !Array.isArray(container._tags)) return;
  container._tags.splice(index, 1);
  renderTagChips(container.id, container._tags);
  _notifyTagInputChange(container);
}

function getTagValues(containerId) {
  const container = document.getElementById(containerId);
  return container ? container._tags || [] : [];
}

// applyHostModeToComposer toggles the .composer--host marker class on
// the new-task form. Called both from fetchConfig (when host_mode
// changes) and from showNewTaskForm (belt-and-braces in case the form
// mounts before config has been fetched). Kept as a standalone helper
// so workspace.js can call it without pulling in the rest of tasks.js.
function applyHostModeToComposer() {
  var form = document.getElementById("new-task-form");
  if (!form) return;
  if (typeof hostMode !== "undefined" && hostMode) {
    form.classList.add("composer--host");
  } else {
    form.classList.remove("composer--host");
  }
}

// Cached /api/flows payload, populated on first composer open. Null
// until the first fetch completes; handlers fall back to
// implement/brainstorm literals so the composer remains usable even
// before the flow list arrives.
var _flowsCache = null;
var _flowsFetchInFlight = null;

function _flowBySlug(slug) {
  if (!_flowsCache) return null;
  for (var i = 0; i < _flowsCache.length; i++) {
    if (_flowsCache[i].slug === slug) return _flowsCache[i];
  }
  return null;
}

// _flowAllowsEmptyPrompt mirrors the server-side rule on POST
// /api/tasks: a brainstorm flow (legacy idea-agent spawn kind)
// accepts an empty prompt because the agent builds its own prompt
// from workspace signals. All other flows require a prompt.
function _flowAllowsEmptyPrompt(slug) {
  if (slug === "brainstorm") return true;
  var f = _flowBySlug(slug);
  return !!(f && f.spawn_kind === "idea-agent");
}

// populateFlowSelect swaps the composer's flow dropdown options to
// match the cached /api/flows response. Idempotent — callers invoke
// this after the cache is populated and again whenever a flow is
// added.
function populateFlowSelect() {
  var el = document.getElementById("new-task-flow");
  if (!el || !_flowsCache) return;
  var previous = el.value || "implement";
  el.innerHTML = "";
  _flowsCache.forEach(function (flow) {
    var opt = document.createElement("option");
    opt.value = flow.slug;
    opt.textContent = flow.name || flow.slug;
    el.appendChild(opt);
  });
  // Restore previous selection if still present; otherwise default
  // to "implement".
  var restore = _flowBySlug(previous) ? previous : "implement";
  el.value = restore;
  applyTaskFlowToComposer();
}

function _ensureFlowsLoaded() {
  if (_flowsCache) {
    populateFlowSelect();
    return Promise.resolve(_flowsCache);
  }
  if (_flowsFetchInFlight) return _flowsFetchInFlight;
  _flowsFetchInFlight = api(Routes.flows.list(), { method: "GET" })
    .then(function (rows) {
      _flowsCache = Array.isArray(rows) ? rows : [];
      populateFlowSelect();
      return _flowsCache;
    })
    .catch(function () {
      // Leave the fallback <option value="implement">Implement</option>
      // in place; the composer still submits successfully with the
      // default selection.
      return null;
    })
    .finally(function () {
      _flowsFetchInFlight = null;
    });
  return _flowsFetchInFlight;
}

// applyTaskFlowToComposer mirrors the Flow select value onto the
// composer root's data-task-flow attribute and updates the prompt
// placeholder. Brainstorm flows show the ideation placeholder
// (prompt optional); every other flow shows the normal task
// placeholder.
function applyTaskFlowToComposer() {
  var form = document.getElementById("new-task-form");
  var flowEl = document.getElementById("new-task-flow");
  if (!form || !flowEl) return;
  var slug = flowEl.value || "implement";
  form.setAttribute("data-task-flow", slug);
  var textarea = document.getElementById("new-prompt");
  if (textarea) {
    var ideation = _flowAllowsEmptyPrompt(slug);
    var placeholder = ideation
      ? textarea.getAttribute("data-prompt-placeholder-ideation")
      : textarea.getAttribute("data-prompt-placeholder-task");
    // Flow-specific override: use the flow's description when one
    // is available and it isn't the built-in brainstorm (whose
    // description reads as a label, not a prompting hint).
    var f = _flowBySlug(slug);
    if (!ideation && f && f.description) placeholder = f.description;
    if (placeholder) textarea.placeholder = placeholder;
  }
}

// --- Task creation ---

async function createTask() {
  const textarea = document.getElementById("new-prompt");
  const flowEl = document.getElementById("new-task-flow");
  const flow = flowEl ? flowEl.value || "implement" : "implement";
  const prompt = textarea.value.trim();
  // Brainstorm-style flows build their own prompt from workspace
  // signals at execution time; the composer field is optional for
  // those. Every other flow requires a non-empty prompt.
  if (!prompt && !_flowAllowsEmptyPrompt(flow)) {
    textarea.focus();
    textarea.style.borderColor = "#dc2626";
    setTimeout(() => (textarea.style.borderColor = ""), 2000);
    return;
  }
  try {
    const timeout =
      parseInt(document.getElementById("new-timeout").value, 10) ||
      DEFAULT_TASK_TIMEOUT;
    const mount_worktrees = document.getElementById(
      "new-mount-worktrees",
    ).checked;
    const tags = getTagValues("new-task-tag-input");
    const max_cost_usd =
      parseFloat(document.getElementById("new-max-cost-usd").value) || 0;
    const max_input_tokens =
      parseInt(document.getElementById("new-max-input-tokens").value, 10) || 0;
    const scheduledAtEl = document.getElementById("new-scheduled-at");
    const scheduled_at =
      scheduledAtEl && scheduledAtEl.value
        ? new Date(scheduledAtEl.value).toISOString()
        : undefined;
    const repeatToggle = document.getElementById("new-repeat-toggle");
    const repeatMinutesEl = document.getElementById("new-repeat-minutes");
    const makeRoutine = !!(repeatToggle && repeatToggle.checked);

    // When the user asks for a recurring schedule, create a routine
    // card instead of a one-shot task. The routine fires on its
    // interval and spawns a fresh instance task each time — the
    // original prompt, sandbox, and tags are copied into each
    // instance via the scheduler engine.
    if (makeRoutine) {
      const interval_minutes =
        (repeatMinutesEl && parseInt(repeatMinutesEl.value, 10)) || 60;
      // Routines spawn their instance tasks against the flow slug
      // the composer picked. The server resolves it via the flow
      // registry at fire time; legacy spawn_kind is filled on the
      // response for older UIs but not written from here.
      const routinePrompt =
        prompt || "Brainstorm improvement ideas for the current workspace.";
      const routine = await api(Routes.routines.create(), {
        method: "POST",
        body: JSON.stringify({
          prompt: routinePrompt,
          timeout,
          tags,
          interval_minutes,
          spawn_flow: flow,
        }),
      });
      localStorage.removeItem("wallfacer-new-task-draft");
      hideNewTaskForm();
      if (typeof clearWorkspaceIsNew === "function") clearWorkspaceIsNew();
      if (routine && routine.id) {
        waitForTaskDelta(routine.id);
      } else {
        fetchTasks();
      }
      return;
    }

    const newTask = await api(Routes.tasks.create(), {
      method: "POST",
      body: JSON.stringify({
        prompt,
        flow,
        timeout,
        mount_worktrees,
        tags,
        max_cost_usd,
        max_input_tokens,
        scheduled_at,
      }),
    });
    const dependsOn = getDepPickerValues("new-depends-on-picker");
    if (dependsOn.length > 0 && newTask && newTask.id) {
      await api(task(newTask.id).update(), {
        method: "PATCH",
        body: JSON.stringify({ depends_on: dependsOn }),
      });
    }
    localStorage.removeItem("wallfacer-new-task-draft");
    hideNewTaskForm();
    // Creating a task is a substantive action — drop the new-workspace
    // bias so subsequent workspace-wide auto-resolutions respect saved
    // preferences and task counts again.
    if (typeof clearWorkspaceIsNew === "function") {
      clearWorkspaceIsNew();
    }
    if (newTask && newTask.id) {
      waitForTaskDelta(newTask.id).then(function () {
        return waitForTaskTitle(newTask.id);
      });
    } else {
      fetchTasks();
    }
  } catch (e) {
    showAlert("Error creating task: " + e.message);
  }
}

function showNewTaskForm() {
  document.getElementById("new-task-btn").classList.add("hidden");
  document.getElementById("new-task-form").classList.remove("hidden");
  document.getElementById("new-timeout").value = DEFAULT_TASK_TIMEOUT;
  const textarea = document.getElementById("new-prompt");
  const draft = localStorage.getItem("wallfacer-new-task-draft") || "";
  textarea.value = draft;
  textarea.style.height = draft ? textarea.scrollHeight + "px" : "";
  textarea.focus();
  // ⌘↵ / Ctrl-Enter submits without reaching for the mouse. Bind once
  // per lifetime of the form element; subsequent opens reuse the same
  // handler. Escape collapses the form back to the "+ New Task"
  // button for symmetry with other modal-like surfaces.
  if (!textarea._composerKeysBound) {
    textarea._composerKeysBound = true;
    textarea.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        e.preventDefault();
        createTask();
      } else if (e.key === "Escape") {
        e.preventDefault();
        hideNewTaskForm();
      }
    });
  }
  initTagInput("new-task-tag-input", []);
  var depsRow = document.getElementById("new-depends-on-row");
  populateDependsOnPicker("new-depends-on-picker", null, []);
  if (depsRow) depsRow.style.display = tasks.length > 0 ? "" : "none";

  // Reflect host_mode on the composer so [data-host-hidden] nodes
  // (currently just "Share siblings") collapse when no container
  // sandbox is in play. Idempotent: called again on every open.
  applyHostModeToComposer();

  // Fetch the flow catalog (cached after first success) and wire
  // the select's change event once. Even before the fetch resolves
  // the fallback <option value="implement">Implement</option> keeps
  // the composer functional.
  _ensureFlowsLoaded();
  applyTaskFlowToComposer();
  var flowEl = document.getElementById("new-task-flow");
  if (flowEl && !flowEl._composerFlowBound) {
    flowEl._composerFlowBound = true;
    flowEl.addEventListener("change", applyTaskFlowToComposer);
  }

  // Wire the "Repeat on a schedule" toggle once per form open so the
  // interval input row (hidden by default) follows the checkbox state.
  var repeatToggle = document.getElementById("new-repeat-toggle");
  var repeatMinutesRow = document.getElementById("new-repeat-minutes-row");
  if (repeatToggle && !repeatToggle._bound) {
    repeatToggle._bound = true;
    repeatToggle.addEventListener("change", function () {
      if (repeatMinutesRow) {
        repeatMinutesRow.style.display = repeatToggle.checked ? "" : "none";
      }
    });
  }
}

function hideNewTaskForm() {
  document.getElementById("new-task-form").classList.add("hidden");
  document.getElementById("new-task-btn").classList.remove("hidden");
  const textarea = document.getElementById("new-prompt");
  textarea.value = "";
  textarea.style.height = "";
  document.getElementById("new-mount-worktrees").checked = false;
  const maxCostEl = document.getElementById("new-max-cost-usd");
  if (maxCostEl) maxCostEl.value = "";
  const maxTokensEl = document.getElementById("new-max-input-tokens");
  if (maxTokensEl) maxTokensEl.value = "";
  const scheduledAtEl = document.getElementById("new-scheduled-at");
  if (scheduledAtEl) scheduledAtEl.value = "";
  const flowEl = document.getElementById("new-task-flow");
  if (flowEl) flowEl.value = "implement";
  // Resync the composer's data-task-flow so the next open starts in
  // the Implement state regardless of what was selected last time.
  applyTaskFlowToComposer();
  const repeatToggle = document.getElementById("new-repeat-toggle");
  const repeatMinutesEl = document.getElementById("new-repeat-minutes");
  const repeatMinutesRow = document.getElementById("new-repeat-minutes-row");
  if (repeatToggle) repeatToggle.checked = false;
  if (repeatMinutesEl) repeatMinutesEl.value = "60";
  if (repeatMinutesRow) repeatMinutesRow.style.display = "none";
  initTagInput("new-task-tag-input", []);
  var depPicker = document.getElementById("new-depends-on-picker");
  if (depPicker) {
    depPicker.querySelector(".dep-picker-list").innerHTML = "";
    depPicker.querySelector(".dep-picker-chips").innerHTML =
      '<span class="dep-picker-placeholder">None</span>';
    depPicker.classList.remove("open");
    depPicker.querySelector(".dep-picker-dropdown").style.display = "none";
  }
}

// --- Task status updates ---

async function updateTaskStatus(id, status) {
  try {
    const currentTask =
      (typeof findTaskById === "function" ? findTaskById(id) : null) ||
      tasks.find(function (t) {
        return t.id === id;
      }) ||
      archivedTasks.find(function (t) {
        return t.id === id;
      });
    await api(task(id).update(), {
      method: "PATCH",
      body: JSON.stringify({ status }),
    });
    if (currentTask)
      announceBoardStatus(
        `Task "${getTaskAccessibleTitle(currentTask)}" moved to ${formatTaskStatusLabel(status)}`,
      );
    waitForTaskDelta(id);
  } catch (e) {
    showAlert("Error updating task: " + e.message);
    fetchTasks();
  }
}

async function toggleFreshStart(id, freshStart) {
  try {
    await api(task(id).update(), {
      method: "PATCH",
      body: JSON.stringify({ fresh_start: freshStart }),
    });
  } catch (e) {
    showAlert("Error updating task: " + e.message);
  }
}

// --- Task deletion ---

async function deleteTask(id) {
  try {
    await api(task(id).delete(), { method: "DELETE" });
    waitForTaskDelta(id);
  } catch (e) {
    showAlert("Error deleting task: " + e.message);
  }
}

async function deleteCurrentTask() {
  if (!getOpenModalTaskId()) return;
  if (
    !(await showConfirm(
      "This task will be recoverable for 7 days. Delete anyway?",
    ))
  )
    return;
  deleteTask(getOpenModalTaskId());
  closeModal();
}

// --- Feedback & completion ---

async function submitFeedback() {
  const textarea = document.getElementById("modal-feedback");
  const message = textarea.value.trim();
  if (!message || !getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  try {
    await api(task(taskId).feedback(), {
      method: "POST",
      body: JSON.stringify({ message }),
    });
    textarea.value = "";
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error submitting feedback: " + e.message);
  }
}

async function completeTask() {
  if (!getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  try {
    await api(task(taskId).done(), { method: "POST" });
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error completing task: " + e.message);
  }
}

// --- Retry & resume ---

async function retryTask() {
  const textarea = document.getElementById("modal-retry-prompt");
  const prompt = textarea.value.trim();
  if (!prompt || !getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  try {
    const body = { status: "backlog", prompt };
    const retryResumeRow = document.getElementById("modal-retry-resume-row");
    if (retryResumeRow && !retryResumeRow.classList.contains("hidden")) {
      body.fresh_start = !document.getElementById("modal-retry-resume").checked;
    }
    await api(task(taskId).update(), {
      method: "PATCH",
      body: JSON.stringify(body),
    });
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error retrying task: " + e.message);
  }
}

async function resumeTask() {
  if (!getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  try {
    const timeoutEl = document.getElementById("modal-resume-timeout");
    const timeout = timeoutEl
      ? parseInt(timeoutEl.value, 10) || DEFAULT_TASK_TIMEOUT
      : DEFAULT_TASK_TIMEOUT;
    await api(task(taskId).resume(), {
      method: "POST",
      body: JSON.stringify({ timeout }),
    });
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error resuming task: " + e.message);
  }
}

// --- Backlog editing ---

async function saveResumeOption(resume) {
  if (!getOpenModalTaskId()) return;
  const statusEl = document.getElementById("modal-edit-status");
  try {
    await api(task(getOpenModalTaskId()).update(), {
      method: "PATCH",
      body: JSON.stringify({ fresh_start: !resume }),
    });
    statusEl.textContent = "Saved";
    setTimeout(() => {
      if (statusEl.textContent === "Saved") statusEl.textContent = "";
    }, 1500);
  } catch (e) {
    statusEl.textContent = "Save failed";
  }
}

function scheduleBacklogSave() {
  const statusEl = document.getElementById("modal-edit-status");
  statusEl.textContent = "";
  clearTimeout(editDebounce);
  editDebounce = setTimeout(async () => {
    if (!getOpenModalTaskId()) return;
    const prompt = document.getElementById("modal-edit-prompt").value.trim();
    if (!prompt) return;
    const timeout =
      parseInt(document.getElementById("modal-edit-timeout").value, 10) ||
      DEFAULT_TASK_TIMEOUT;
    const mount_worktrees = document.getElementById(
      "modal-edit-mount-worktrees",
    ).checked;
    const sandbox = document.getElementById("modal-edit-sandbox").value;
    // Per-activity sandbox overrides retired with the agents/flows
    // rewrite — harness lives on the agent a flow step references.
    // The backlog-save PATCH no longer collects the per-activity map.
    const depends_on = getDepPickerValues("modal-edit-depends-on-picker");
    const tags = getTagValues("modal-edit-tag-input");
    const maxCostEl = document.getElementById("modal-edit-max-cost-usd");
    const maxTokensEl = document.getElementById("modal-edit-max-input-tokens");
    const max_cost_usd = maxCostEl
      ? parseFloat(maxCostEl.value) || 0
      : undefined;
    const max_input_tokens = maxTokensEl
      ? parseInt(maxTokensEl.value, 10) || 0
      : undefined;
    const scheduledAtEl = document.getElementById("modal-edit-scheduled-at");
    const scheduled_at = scheduledAtEl
      ? scheduledAtEl.value
        ? new Date(scheduledAtEl.value).toISOString()
        : null
      : undefined;
    const modelOverrideEl = document.getElementById(
      "modal-edit-model-override",
    );
    const model = modelOverrideEl ? modelOverrideEl.value.trim() : undefined;
    const patchBody = {
      prompt,
      timeout,
      mount_worktrees,
      sandbox,
      depends_on,
      tags,
      max_cost_usd,
      max_input_tokens,
      scheduled_at,
      model,
    };
    try {
      await api(task(getOpenModalTaskId()).update(), {
        method: "PATCH",
        body: JSON.stringify(patchBody),
      });
      statusEl.textContent = "Saved";
      setTimeout(() => {
        if (statusEl.textContent === "Saved") statusEl.textContent = "";
      }, 1500);
      // Update rendered prompt on the left panel.
      var promptEl = document.getElementById("modal-prompt-rendered");
      promptEl.innerHTML = renderMarkdown(prompt);
      _mdRender.enhanceMarkdown(promptEl);
      document.getElementById("modal-prompt").textContent = prompt;
      waitForTaskDelta(getOpenModalTaskId());
    } catch (e) {
      statusEl.textContent = "Save failed";
    }
  }, 500);
}

document
  .getElementById("modal-edit-prompt")
  .addEventListener("input", scheduleBacklogSave);
document
  .getElementById("modal-edit-timeout")
  .addEventListener("change", scheduleBacklogSave);

// --- Start (backlog → in_progress) ---

async function startTask() {
  if (!getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  try {
    await api(task(taskId).update(), {
      method: "PATCH",
      body: JSON.stringify({ status: "in_progress" }),
    });
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error starting task: " + e.message);
  }
}

// --- Send to Plan ---

function sendCurrentTaskToPlan() {
  const id = getOpenModalTaskId();
  if (!id) return;
  openPlanForTask(id);
}

// --- Cancel ---

async function cancelTask() {
  if (!getOpenModalTaskId()) return;
  if (
    !(await showConfirm(
      "Cancel this task? The sandbox will be cleaned up and all prepared changes discarded. History and logs will be preserved.",
    ))
  )
    return;
  const taskId = getOpenModalTaskId();
  const btn = document.getElementById("modal-cancel-btn");
  if (btn) {
    btn.disabled = true;
    btn.innerHTML =
      '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;margin-right:4px;"></span>Shutting down…';
  }
  // Show a "cancelling…" indicator on the board card immediately, before the
  // SSE update arrives confirming the status change.
  pendingCancelTaskIds.add(taskId);
  scheduleRender();
  try {
    await api(task(taskId).cancel(), { method: "POST" });
    closeModal();
    // Keep the "cancelling…" indicator visible for at least 1.5 s after the
    // modal closes so the user can see the board card update. The server sends
    // the SSE "cancelled" update synchronously before the HTTP response, so by
    // the time the modal closes the task may already be "cancelled" in tasks[].
    // Without a minimum delay the indicator disappears before it is visible.
    var minDisplayEnd = Date.now() + 1500;
    waitForTaskDelta(taskId).finally(function () {
      var delay = Math.max(0, minDisplayEnd - Date.now());
      setTimeout(function () {
        pendingCancelTaskIds.delete(taskId);
        scheduleRender();
      }, delay);
    });
  } catch (e) {
    pendingCancelTaskIds.delete(taskId);
    scheduleRender();
    showAlert("Error cancelling task: " + e.message);
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = "Cancel task";
    }
  }
}

// --- Budget limit raise (waiting tasks) ---

// openRaiseLimitInline: shows a small inline form for adjusting budget limits
// on a task that was paused due to a budget guardrail.
async function openRaiseLimitInline() {
  if (!getOpenModalTaskId()) return;
  const currentTask = tasks.find((t) => t.id === getOpenModalTaskId());
  if (!currentTask) return;
  const banner = document.getElementById("modal-budget-exceeded-banner");
  if (!banner) return;

  const newCost = await showPrompt(
    "New cost limit in USD (0 = unlimited). Current limit: " +
      (currentTask.max_cost_usd > 0
        ? "$" + currentTask.max_cost_usd.toFixed(2)
        : "none"),
    currentTask.max_cost_usd > 0 ? String(currentTask.max_cost_usd) : "",
  );
  if (newCost === null) return; // cancelled
  const newTokens = await showPrompt(
    "New input token limit (0 = unlimited). Current limit: " +
      (currentTask.max_input_tokens > 0
        ? currentTask.max_input_tokens.toLocaleString()
        : "none"),
    currentTask.max_input_tokens > 0
      ? String(currentTask.max_input_tokens)
      : "",
  );
  if (newTokens === null) return; // cancelled

  const taskId = getOpenModalTaskId();
  try {
    await api(task(taskId).update(), {
      method: "PATCH",
      body: JSON.stringify({
        max_cost_usd: parseFloat(newCost) || 0,
        max_input_tokens: parseInt(newTokens, 10) || 0,
      }),
    });
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error updating budget: " + e.message);
  }
}

// --- Archive / Unarchive ---

async function archiveAllDone() {
  try {
    const result = await api(Routes.tasks.archiveDone(), { method: "POST" });
    fetchTasks();
  } catch (e) {
    showAlert("Error archiving tasks: " + e.message);
  }
}

async function archiveTask() {
  if (!getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  try {
    await api(task(taskId).archive(), { method: "POST" });
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error archiving task: " + e.message);
  }
}

async function unarchiveTask() {
  if (!getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  try {
    await api(task(taskId).unarchive(), { method: "POST" });
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error unarchiving task: " + e.message);
  }
}

// --- Quick card actions (no modal required) ---

async function quickDoneTask(id) {
  try {
    const currentTask =
      (typeof findTaskById === "function" ? findTaskById(id) : null) ||
      tasks.find(function (t) {
        return t.id === id;
      }) ||
      archivedTasks.find(function (t) {
        return t.id === id;
      });
    await api(task(id).done(), { method: "POST" });
    if (currentTask)
      announceBoardStatus(
        `Task "${getTaskAccessibleTitle(currentTask)}" moved to done`,
      );
    waitForTaskDelta(id);
  } catch (e) {
    showAlert("Error completing task: " + e.message);
  }
}

async function quickResumeTask(id, timeout) {
  try {
    await api(task(id).resume(), {
      method: "POST",
      body: JSON.stringify({ timeout }),
    });
    waitForTaskDelta(id);
  } catch (e) {
    showAlert("Error resuming task: " + e.message);
  }
}

async function quickRetryTask(id) {
  try {
    await api(task(id).update(), {
      method: "PATCH",
      body: JSON.stringify({ status: "backlog" }),
    });
    waitForTaskDelta(id);
  } catch (e) {
    showAlert("Error retrying task: " + e.message);
  }
}

// --- Test agent ---

function toggleTestSection() {
  const section = document.getElementById("modal-test-section");
  section.classList.toggle("hidden");
  if (!section.classList.contains("hidden")) {
    document.getElementById("modal-test-criteria").focus();
  }
}

async function runTestTask() {
  if (!getOpenModalTaskId()) return;
  const taskId = getOpenModalTaskId();
  const criteria = document.getElementById("modal-test-criteria").value.trim();
  try {
    const res = await api(task(taskId).test(), {
      method: "POST",
      body: JSON.stringify({ criteria }),
    });
    closeModal();
    waitForTaskDelta(taskId);
  } catch (e) {
    showAlert("Error starting test verification: " + e.message);
  }
}

async function quickTestTask(id) {
  try {
    await api(task(id).test(), {
      method: "POST",
      body: JSON.stringify({ criteria: "" }),
    });
    waitForTaskDelta(id);
  } catch (e) {
    showAlert("Error starting test verification: " + e.message);
  }
}

// --- Sync with latest (rebase worktree onto default branch) ---

const _syncInFlight = new Set();
async function syncTask(id) {
  if (_syncInFlight.has(id)) return;
  _syncInFlight.add(id);
  // Disable all sync buttons for this task while in flight.
  document
    .querySelectorAll(`[onclick*="syncTask('${id}')"]`)
    .forEach(function (btn) {
      btn.disabled = true;
    });
  try {
    const res = await api(task(id).sync(), { method: "POST" });
    diffCache.delete(id);
    if (res.status === "already_syncing") {
      showAlert("Sync is already in progress for this task.");
    }
    waitForTaskDelta(id);
  } catch (e) {
    showAlert("Error syncing task: " + e.message);
  } finally {
    _syncInFlight.delete(id);
    document
      .querySelectorAll(`[onclick*="syncTask('${id}')"]`)
      .forEach(function (btn) {
        btn.disabled = false;
      });
  }
}

// --- Bulk title generation for tasks without a title ---

async function generateMissingTitles() {
  const statusEl = document.getElementById("generate-titles-status");
  const btn = document.querySelector('[onclick="generateMissingTitles()"]');
  const limit = document.getElementById("generate-titles-limit").value;

  btn.disabled = true;
  statusEl.innerHTML =
    '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;margin-right:4px;"></span>Checking tasks…';
  statusEl.style.color = "var(--text-muted)";

  let interval = null;

  try {
    const params = new URLSearchParams({ limit });
    const res = await api(Routes.tasks.generateTitles() + "?" + params, {
      method: "POST",
    });
    const { queued, total_without_title, task_ids } = res;

    if (queued === 0) {
      statusEl.textContent =
        total_without_title === 0
          ? "All tasks already have titles."
          : "No tasks queued (limit reached or none found).";
      btn.disabled = false;
      return;
    }

    const pending = new Set(task_ids);
    let succeeded = 0;
    let failed = 0;
    const total = queued;
    const startTime = Date.now();
    const TIMEOUT_MS = 120_000;

    function updateStatus() {
      const done = succeeded + failed;
      const inFlight = pending.size > 0;
      const spinnerHtml = inFlight
        ? '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;margin-right:5px;"></span>'
        : "";
      const okHtml =
        succeeded > 0
          ? ` <span style="color:#16a34a">${succeeded} ok</span>`
          : "";
      const failHtml =
        failed > 0
          ? ` <span style="color:var(--danger,#dc2626)">${failed} failed</span>`
          : "";
      statusEl.style.color = "var(--text-muted)";
      statusEl.innerHTML = `${spinnerHtml}${done}/${total} generated${okHtml}${failHtml}`;
    }

    updateStatus();

    interval = setInterval(() => {
      for (const id of [...pending]) {
        const t = tasks.find((t) => t.id === id);
        if (t && t.title) {
          pending.delete(id);
          succeeded++;
        }
      }

      updateStatus();

      if (pending.size === 0) {
        clearInterval(interval);
        btn.disabled = false;
        return;
      }

      if (Date.now() - startTime > TIMEOUT_MS) {
        failed += pending.size;
        pending.clear();
        clearInterval(interval);
        updateStatus();
        btn.disabled = false;
      }
    }, 1000);
  } catch (e) {
    if (interval) clearInterval(interval);
    statusEl.textContent = "Error: " + e.message;
    statusEl.style.color = "var(--danger, #dc2626)";
    btn.disabled = false;
  }
}

// --- Bulk oversight generation for tasks without a summary ---

async function generateMissingOversight() {
  const statusEl = document.getElementById("generate-oversight-status");
  const btn = document.querySelector('[onclick="generateMissingOversight()"]');
  const limit = document.getElementById("generate-oversight-limit").value;

  btn.disabled = true;
  statusEl.innerHTML =
    '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;margin-right:4px;"></span>Checking tasks…';
  statusEl.style.color = "var(--text-muted)";

  let interval = null;

  try {
    const params = new URLSearchParams({ limit });
    const res = await api(Routes.tasks.generateOversight() + "?" + params, {
      method: "POST",
    });
    const { queued, total_without_oversight, task_ids } = res;

    if (queued === 0) {
      statusEl.textContent =
        total_without_oversight === 0
          ? "All eligible tasks already have oversight summaries."
          : "No tasks queued (limit reached or none found).";
      btn.disabled = false;
      return;
    }

    const pending = new Set(task_ids);
    let succeeded = 0;
    let failed = 0;
    const total = queued;
    const startTime = Date.now();
    const TIMEOUT_MS = 300_000; // 5 min — oversight takes longer than titles

    function updateStatus() {
      const done = succeeded + failed;
      const inFlight = pending.size > 0;
      const spinnerHtml = inFlight
        ? '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;margin-right:5px;"></span>'
        : "";
      const okHtml =
        succeeded > 0
          ? ` <span style="color:#16a34a">${succeeded} ok</span>`
          : "";
      const failHtml =
        failed > 0
          ? ` <span style="color:var(--danger,#dc2626)">${failed} failed</span>`
          : "";
      statusEl.style.color = "var(--text-muted)";
      statusEl.innerHTML = `${spinnerHtml}${done}/${total} generated${okHtml}${failHtml}`;
    }

    updateStatus();

    interval = setInterval(async () => {
      if (Date.now() - startTime > TIMEOUT_MS) {
        failed += pending.size;
        pending.clear();
        clearInterval(interval);
        updateStatus();
        btn.disabled = false;
        return;
      }

      const checks = [...pending].map((id) =>
        api(task(id).oversight())
          .then((o) => ({ id, status: o.status }))
          .catch(() => ({ id, status: "error" })),
      );
      const results = await Promise.all(checks);
      for (const { id, status } of results) {
        if (status === "ready") {
          pending.delete(id);
          succeeded++;
        } else if (status === "failed" || status === "error") {
          pending.delete(id);
          failed++;
        }
      }

      updateStatus();

      if (pending.size === 0) {
        clearInterval(interval);
        btn.disabled = false;
      }
    }, 3000);
  } catch (e) {
    if (interval) clearInterval(interval);
    statusEl.textContent = "Error: " + e.message;
    statusEl.style.color = "var(--danger, #dc2626)";
    btn.disabled = false;
  }
}
