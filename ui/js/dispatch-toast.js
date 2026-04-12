// Dispatch-complete toast — shown after a successful /api/specs/dispatch
// call. Renders at bottom-right with a "View on Board →" action that
// switches to Board mode without persisting the preference, scrolls the
// Backlog column, and pulse-highlights the newly created task cards.
//
// The toast element is appended directly to <body> so it survives mode
// transitions (the "View on Board" action swaps the main container).
// It uses a minimal one-toast-at-a-time model — rapidly chained
// dispatches replace the previous toast rather than stacking.

var _dispatchToastEl = null;
var _dispatchToastTimer = null;

function _dismissDispatchToast() {
  if (_dispatchToastTimer) {
    clearTimeout(_dispatchToastTimer);
    _dispatchToastTimer = null;
  }
  if (_dispatchToastEl && _dispatchToastEl.parentNode) {
    _dispatchToastEl.parentNode.removeChild(_dispatchToastEl);
  }
  _dispatchToastEl = null;
}

function _highlightDispatchedTasks(taskIds) {
  if (!Array.isArray(taskIds) || taskIds.length === 0) return;
  var backlogCol = document.getElementById("col-backlog");
  var firstCard = null;
  for (var i = 0; i < taskIds.length; i++) {
    var card = document.querySelector('[data-task-id="' + taskIds[i] + '"]');
    if (!card) continue;
    card.classList.add("task-card--just-created");
    if (!firstCard) firstCard = card;
    (function (c) {
      setTimeout(function () {
        c.classList.remove("task-card--just-created");
      }, 1200);
    })(card);
  }
  if (firstCard && typeof firstCard.scrollIntoView === "function") {
    firstCard.scrollIntoView({ behavior: "smooth", block: "nearest" });
  } else if (backlogCol && typeof backlogCol.scrollTo === "function") {
    backlogCol.scrollTo({ top: 0, behavior: "smooth" });
  }
}

function showDispatchCompleteToast(taskIds) {
  _dismissDispatchToast();

  var toast = document.createElement("div");
  toast.className = "dispatch-toast";
  toast.setAttribute("role", "status");
  toast.setAttribute("aria-live", "polite");

  var text = document.createElement("span");
  text.className = "dispatch-toast__text";
  var n = Array.isArray(taskIds) ? taskIds.length : 0;
  text.textContent =
    "Dispatched " + n + " task" + (n === 1 ? "" : "s") + " to the Board.";
  toast.appendChild(text);

  var viewBtn = document.createElement("button");
  viewBtn.type = "button";
  viewBtn.className = "dispatch-toast__view";
  viewBtn.textContent = "View on Board \u2192";
  viewBtn.addEventListener("click", function () {
    _dismissDispatchToast();
    if (typeof switchMode === "function") {
      // Programmatic switch — never persists to localStorage per the
      // default-mode-resolution spec.
      switchMode("board");
    }
    // Give the board a frame to render before scrolling/highlighting.
    var run = function () {
      _highlightDispatchedTasks(taskIds);
    };
    if (typeof requestAnimationFrame === "function") {
      requestAnimationFrame(function () {
        requestAnimationFrame(run);
      });
    } else {
      setTimeout(run, 16);
    }
  });
  toast.appendChild(viewBtn);

  var closeBtn = document.createElement("button");
  closeBtn.type = "button";
  closeBtn.className = "dispatch-toast__close";
  closeBtn.setAttribute("aria-label", "Dismiss");
  closeBtn.textContent = "\u2715";
  closeBtn.addEventListener("click", _dismissDispatchToast);
  toast.appendChild(closeBtn);

  document.body.appendChild(toast);
  _dispatchToastEl = toast;
  _dispatchToastTimer = setTimeout(_dismissDispatchToast, 8000);
  return toast;
}
