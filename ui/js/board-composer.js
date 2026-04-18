/* global populateSandboxSelects, openTemplatesPicker, api, Routes,
   switchMode, clearWorkspaceIsNew, showAlert, DEFAULT_TASK_TIMEOUT,
   attachMentionAutocomplete */
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
    // Outer wrap mirrors the chat-first `.spec-chat-stream` card (border,
    // radius, soft shadow, bg-card); the inner `.spec-chat-composer`
    // uses its native Slack-style inset look (bg-input, smaller border)
    // with zero board-specific overrides. The hint, composer, and
    // advanced panel are all children of the wrap; the bridge line sits
    // outside the card as a sibling.
    var wrap = document.createElement("div");
    wrap.className = "board-composer-wrap";
    wrap.innerHTML =
      '<p class="spec-chat-empty-hint spec-chat-empty-hint--visible board-composer-wrap__hint">' +
      'Type <span class="spec-chat-empty-hint__cmd">@</span> to reference a file' +
      "</p>" +
      '<div class="board-composer spec-chat-composer">' +
      '<textarea id="board-composer-prompt" class="spec-chat-composer__input" ' +
      'rows="3" placeholder="Describe a task for the agent..."></textarea>' +
      '<div class="spec-chat-composer__bar">' +
      '<div class="spec-chat-composer__actions">' +
      '<button type="button" class="spec-chat-composer__action board-composer__at" ' +
      'title="Mention a file">@</button>' +
      '<button type="button" class="spec-chat-composer__action board-composer__advanced-toggle" ' +
      'aria-expanded="false" title="Advanced options">\u25BE</button>' +
      "</div>" +
      '<div class="spec-chat-composer__right">' +
      '<button type="button" class="spec-chat-composer__send board-composer__submit" ' +
      'title="Create task">\u27A4</button>' +
      "</div>" +
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
      "</div>";
    var bridge = document.createElement("p");
    bridge.className = "board-composer__bridge";
    bridge.innerHTML =
      "Planning something larger? Start a chat in " +
      '<button type="button" class="board-composer__plan-link">Plan</button> \u2192';
    // Return a host element containing both the card and the bridge so
    // _root refers to a single element for mount/unmount/animation.
    var host = document.createElement("div");
    host.className = "board-composer-host";
    host.appendChild(wrap);
    host.appendChild(bridge);
    return host;
  }

  function _wireComposer(el) {
    var toggle = el.querySelector(".board-composer__advanced-toggle");
    var advanced = el.querySelector(".board-composer__advanced");
    function applyAdvanced() {
      if (!advanced || !toggle) return;
      if (_advancedOpen) {
        advanced.classList.remove("hidden");
        toggle.setAttribute("aria-expanded", "true");
        toggle.textContent = "\u25B4";
      } else {
        advanced.classList.add("hidden");
        toggle.setAttribute("aria-expanded", "false");
        toggle.textContent = "\u25BE";
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

    // Templates picker — only surfaced from inside the advanced panel,
    // since Board mode doesn't have a live slash-command system the way
    // Plan mode does and a top-level `/` button would promise a UX that
    // doesn't exist.
    var templatesBtn = el.querySelector(".board-composer__templates");
    if (templatesBtn && typeof openTemplatesPicker === "function") {
      templatesBtn.addEventListener("click", function () {
        openTemplatesPicker(function (body) {
          if (prompt) {
            prompt.value = body;
            prompt.focus();
          }
        }, templatesBtn);
      });
    }

    // `@` action: inserts an `@` at the cursor and re-fires the input
    // event so the mention autocomplete widget opens — matches
    // planning-chat.js's at-button behaviour.
    var atBtn = el.querySelector(".board-composer__at");
    if (atBtn && prompt) {
      atBtn.addEventListener("click", function () {
        var pos =
          prompt.selectionStart != null
            ? prompt.selectionStart
            : prompt.value.length;
        var before = prompt.value.substring(0, pos);
        var after = prompt.value.substring(pos);
        var needsSpace = before.length > 0 && !/\s$/.test(before);
        var insert = needsSpace ? " @" : "@";
        prompt.value = before + insert + after;
        var newPos = pos + insert.length;
        prompt.focus();
        prompt.setSelectionRange(newPos, newPos);
        prompt.dispatchEvent(new Event("input", { bubbles: true }));
      });
    }

    // @-mention file autocomplete, matching the Plan-mode chat composer.
    if (prompt && typeof attachMentionAutocomplete === "function") {
      attachMentionAutocomplete(prompt, { priorityPrefix: "specs/" });
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

  function _liftTarget() {
    // The Backlog "+ New Task" button occupies roughly the footprint of
    // the first card about to land; it's a better-looking landing spot
    // than the raw column top which has no visible anchor.
    var btn = document.getElementById("new-task-btn");
    if (btn) return btn.getBoundingClientRect();
    var col = document.getElementById("col-backlog");
    if (col) return col.getBoundingClientRect();
    return null;
  }

  function _animateOutAndUnmount(el) {
    var board = document.getElementById("board");
    var target = _liftTarget();
    if (_prefersReducedMotion() || !board || !target) {
      unmount();
      return;
    }
    var src = el.getBoundingClientRect();
    if (!src.width || !src.height) {
      unmount();
      return;
    }

    // Pin the composer to its current viewport rect on document.body so
    // the slot goes :empty and the #board:has(...) rule disengages —
    // the board columns reappear behind us and get faded in via the
    // board--columns-entering class. Reparenting avoids clipping and
    // stacking-context surprises from the grid cell.
    el.style.position = "fixed";
    el.style.left = src.left + "px";
    el.style.top = src.top + "px";
    el.style.width = src.width + "px";
    el.style.height = src.height + "px";
    el.style.maxWidth = src.width + "px";
    el.style.margin = "0";
    el.style.transformOrigin = "top left";
    el.style.willChange = "transform, opacity";
    el.style.pointerEvents = "none";
    el.style.zIndex = "50";
    document.body.appendChild(el);
    board.classList.add("board--columns-entering");

    // Compress the interior content first: textarea shrinks to a
    // one-line preview while chrome (label, bar, advanced, bridge)
    // fades out. This makes the outer translate+scale read as a card
    // "settling" into the Backlog rather than a box collapsing.
    var inner = el.querySelectorAll(
      ".board-composer__label," +
        ".board-composer__bar," +
        ".board-composer__advanced," +
        ".board-composer__bridge",
    );
    for (var i = 0; i < inner.length; i++) {
      inner[i].style.transition =
        "opacity 180ms cubic-bezier(0.3, 0, 0.8, 0.15)";
      inner[i].style.opacity = "0";
    }
    var textarea = el.querySelector(".board-composer__input");
    if (textarea) {
      textarea.style.transition =
        "opacity 180ms cubic-bezier(0.3, 0, 0.8, 0.15)";
      textarea.style.opacity = "0.2";
    }

    var dx = target.left - src.left;
    var dy = target.top - src.top;
    var scaleX = Math.max(0.2, target.width / src.width);
    var scaleY = Math.max(0.08, target.height / src.height);

    requestAnimationFrame(function () {
      el.style.transition =
        "transform 260ms cubic-bezier(0.05, 0.7, 0.1, 1)," +
        " opacity 200ms ease 120ms";
      el.style.transform =
        "translate(" +
        dx +
        "px, " +
        dy +
        "px) scale(" +
        scaleX +
        ", " +
        scaleY +
        ")";
      el.style.opacity = "0";
    });

    setTimeout(function () {
      board.classList.remove("board--columns-entering");
      if (el.parentNode) el.parentNode.removeChild(el);
      if (_root === el) _root = null;
    }, 380);
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
