/* global populateSandboxSelects, openTemplatesPicker, api, Routes,
   switchMode, clearWorkspaceIsNew, showAlert, DEFAULT_TASK_TIMEOUT */
//
// Board empty-state composer: when Board mode opens with zero tasks in
// the current workspace group, render a task-creation composer in place
// of the empty columns. Submits through the shared POST /api/tasks
// pipeline — no new routes, no new fields.
//
// Lifecycle:
//   BoardComposer.sync() — called whenever the task list changes, e.g.
//     from render.js. Mounts the composer when the board is empty and
//     the user has not yet dismissed it this session; unmounts when the
//     task list becomes non-empty.
//   BoardComposer.dismissForSession() — flips the "do not remount" flag
//     so a later archive of the sole task does not resurrect the
//     composer.
//
// Advanced disclosure state is kept in a module-scope variable, so it
// persists across mode switches in the same session but not across
// page reloads — matching the parent spec's non-goal list.

var BoardComposer = (function () {
  var _root = null;
  var _dismissedForSession = false;
  var _advancedOpen = false;
  var _submitting = false;

  function _prefersReducedMotion() {
    if (
      typeof window === "undefined" ||
      typeof window.matchMedia !== "function"
    ) {
      return false;
    }
    try {
      return !!window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    } catch (_e) {
      return false;
    }
  }

  function _slot() {
    return document.getElementById("board-empty-composer");
  }

  function _buildComposer() {
    var el = document.createElement("div");
    el.className = "board-composer spec-chat-composer";
    el.innerHTML =
      '<label class="board-composer__label" for="board-composer-prompt">' +
      "What should the agent work on?" +
      "</label>" +
      '<textarea id="board-composer-prompt" class="spec-chat-composer__input board-composer__input" ' +
      'rows="4" placeholder="Describe the task (Markdown supported)..."></textarea>' +
      '<div class="spec-chat-composer__bar board-composer__bar">' +
      '<div class="spec-chat-composer__actions">' +
      '<button type="button" class="spec-chat-composer__action board-composer__advanced-toggle" ' +
      'aria-expanded="false">' +
      '<span class="board-composer__chevron">\u25BE</span> Advanced' +
      "</button>" +
      "</div>" +
      '<div class="spec-chat-composer__right">' +
      '<button type="button" class="spec-chat-composer__send board-composer__submit">' +
      "Create \u27A4" +
      "</button>" +
      "</div>" +
      "</div>" +
      '<div class="board-composer__advanced hidden">' +
      '<label class="board-composer__field">' +
      "<span>Sandbox</span>" +
      '<select id="board-composer-sandbox" class="select" data-sandbox-select="true">' +
      '<option value="">Default</option>' +
      "</select>" +
      "</label>" +
      '<label class="board-composer__field">' +
      "<span>Timeout (min)</span>" +
      '<input type="number" id="board-composer-timeout" class="field" min="1" max="600" />' +
      "</label>" +
      '<label class="board-composer__field board-composer__field--wide">' +
      "<span>Goal (optional)</span>" +
      '<input type="text" id="board-composer-goal" class="field" ' +
      'placeholder="What does success look like?" />' +
      "</label>" +
      '<div class="board-composer__field board-composer__field--wide">' +
      '<button type="button" class="btn-icon board-composer__templates">' +
      "Insert from template" +
      "</button>" +
      "</div>" +
      "</div>" +
      '<p class="board-composer__bridge">' +
      "Planning something larger? Start a chat in " +
      '<button type="button" class="board-composer__plan-link">Plan</button> \u2192' +
      "</p>";
    return el;
  }

  function _wireComposer(el) {
    var toggle = el.querySelector(".board-composer__advanced-toggle");
    var advanced = el.querySelector(".board-composer__advanced");
    var chevron = el.querySelector(".board-composer__chevron");
    function applyAdvanced() {
      if (!advanced || !toggle) return;
      if (_advancedOpen) {
        advanced.classList.remove("hidden");
        toggle.setAttribute("aria-expanded", "true");
        if (chevron) chevron.textContent = "\u25B4";
      } else {
        advanced.classList.add("hidden");
        toggle.setAttribute("aria-expanded", "false");
        if (chevron) chevron.textContent = "\u25BE";
      }
    }
    applyAdvanced();
    if (toggle) {
      toggle.addEventListener("click", function () {
        _advancedOpen = !_advancedOpen;
        applyAdvanced();
      });
    }

    var timeoutInput = el.querySelector("#board-composer-timeout");
    if (timeoutInput) {
      timeoutInput.value =
        typeof DEFAULT_TASK_TIMEOUT !== "undefined" ? DEFAULT_TASK_TIMEOUT : 60;
    }

    if (typeof populateSandboxSelects === "function") {
      populateSandboxSelects();
    }

    var submit = el.querySelector(".board-composer__submit");
    var prompt = el.querySelector("#board-composer-prompt");
    if (submit) {
      submit.addEventListener("click", function () {
        _submit(el);
      });
    }
    if (prompt) {
      prompt.addEventListener("keydown", function (e) {
        if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
          e.preventDefault();
          _submit(el);
        }
      });
    }

    var planLink = el.querySelector(".board-composer__plan-link");
    if (planLink) {
      planLink.addEventListener("click", function () {
        if (typeof switchMode === "function") {
          // Explicit user action: persist the choice per the
          // default-mode-resolution spec.
          switchMode("spec", { persist: true });
        }
      });
    }

    var templatesBtn = el.querySelector(".board-composer__templates");
    if (templatesBtn && typeof openTemplatesPicker === "function") {
      templatesBtn.addEventListener("click", function () {
        openTemplatesPicker(function (body) {
          if (prompt) {
            prompt.value = body;
            prompt.focus();
          }
        });
      });
    }
  }

  function _submit(el) {
    if (_submitting) return;
    var prompt = el.querySelector("#board-composer-prompt");
    var text = (prompt && prompt.value ? prompt.value : "").trim();
    if (!text) {
      if (prompt) {
        prompt.focus();
        prompt.classList.add("board-composer__input--error");
        setTimeout(function () {
          prompt.classList.remove("board-composer__input--error");
        }, 1500);
      }
      return;
    }
    var timeoutEl = el.querySelector("#board-composer-timeout");
    var timeoutVal =
      timeoutEl && timeoutEl.value ? parseInt(timeoutEl.value, 10) : 0;
    var timeout =
      timeoutVal > 0
        ? timeoutVal
        : typeof DEFAULT_TASK_TIMEOUT !== "undefined"
          ? DEFAULT_TASK_TIMEOUT
          : 60;
    var sandboxEl = el.querySelector("#board-composer-sandbox");
    var sandbox = sandboxEl ? sandboxEl.value : "";
    var goalEl = el.querySelector("#board-composer-goal");
    var goal = goalEl ? goalEl.value.trim() : "";

    // Body shape mirrors the existing task-create flow (see tasks.js
    // createTask). Fields not surfaced in the composer use sensible
    // defaults that match the wider UI.
    var body = {
      prompt: text,
      goal: goal,
      timeout: timeout,
      mount_worktrees: true,
      sandbox: sandbox,
      sandbox_by_activity: {},
      tags: [],
      max_cost_usd: 0,
      max_input_tokens: 0,
    };

    _submitting = true;
    var submitBtn = el.querySelector(".board-composer__submit");
    if (submitBtn) {
      submitBtn.disabled = true;
      submitBtn.textContent = "Creating\u2026";
    }
    api(Routes.tasks.create(), {
      method: "POST",
      body: JSON.stringify(body),
    })
      .then(function () {
        _dismissedForSession = true;
        if (typeof clearWorkspaceIsNew === "function") {
          clearWorkspaceIsNew();
        }
        _animateOutAndUnmount(el);
      })
      .catch(function (err) {
        if (typeof showAlert === "function") {
          showAlert("Error creating task: " + err.message);
        }
      })
      .finally(function () {
        _submitting = false;
        if (submitBtn) {
          submitBtn.disabled = false;
          submitBtn.textContent = "Create \u27A4";
        }
      });
  }

  function _animateOutAndUnmount(el) {
    if (_prefersReducedMotion()) {
      unmount();
      return;
    }
    el.classList.add("board-composer--submitting");
    setTimeout(function () {
      unmount();
    }, 260);
  }

  function mount() {
    if (_dismissedForSession) return;
    var slot = _slot();
    if (!slot) return;
    if (_root) return; // already mounted
    _root = _buildComposer();
    slot.appendChild(_root);
    _wireComposer(_root);
    // Focus the prompt after layout so the caret is ready for typing.
    var prompt = _root.querySelector("#board-composer-prompt");
    if (prompt && typeof prompt.focus === "function") {
      setTimeout(function () {
        try {
          prompt.focus();
        } catch (_e) {
          // Focus errors are benign in headless test contexts.
        }
      }, 0);
    }
  }

  function unmount() {
    if (!_root) return;
    if (_root.parentNode) _root.parentNode.removeChild(_root);
    _root = null;
  }

  // sync reconciles the mounted state with the current task count. Call
  // after every render so the composer appears/disappears in lock-step
  // with the board.
  function sync(taskCount) {
    var count = typeof taskCount === "number" ? taskCount : 0;
    if (count === 0 && !_dismissedForSession) {
      mount();
    } else if (count > 0) {
      _dismissedForSession = true;
      unmount();
    }
  }

  function isMounted() {
    return !!_root;
  }

  function dismissForSession() {
    _dismissedForSession = true;
    unmount();
  }

  // Exposed for tests: reset module-scope flags.
  function __resetForTests() {
    _dismissedForSession = false;
    _advancedOpen = false;
    _submitting = false;
    unmount();
  }

  return {
    sync: sync,
    mount: mount,
    unmount: unmount,
    isMounted: isMounted,
    dismissForSession: dismissForSession,
    __resetForTests: __resetForTests,
  };
})();
