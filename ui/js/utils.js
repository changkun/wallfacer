// --- Utility helpers ---

var _alertDismiss = null;
function showAlert(message) {
  document.getElementById("alert-message").textContent = message;
  const modal = document.getElementById("alert-modal");
  modal.classList.remove("hidden");
  modal.classList.add("flex");
  document.getElementById("alert-ok-btn").focus();
  if (_alertDismiss) _alertDismiss();
  _alertDismiss = bindModalDismiss(modal, closeAlert);
}

function closeAlert() {
  const modal = document.getElementById("alert-modal");
  modal.classList.add("hidden");
  modal.classList.remove("flex");
  if (_alertDismiss) {
    _alertDismiss();
    _alertDismiss = null;
  }
}

function showConfirm(message) {
  return new Promise(function (resolve) {
    document.getElementById("confirm-message").textContent = message;
    var modal = document.getElementById("confirm-modal");
    modal.classList.remove("hidden");
    modal.classList.add("flex");
    var confirmBtn = document.getElementById("confirm-ok-btn");
    var cancelBtn = document.getElementById("confirm-cancel-btn");
    confirmBtn.focus();

    var cleanup;
    function close(result) {
      modal.classList.add("hidden");
      modal.classList.remove("flex");
      if (cleanup) {
        cleanup();
        cleanup = null;
      }
      resolve(result);
    }
    confirmBtn.onclick = function () {
      close(true);
    };
    cancelBtn.onclick = function () {
      close(false);
    };
    cleanup = bindModalDismiss(modal, function () {
      close(false);
    });
  });
}

function showPrompt(message, defaultValue) {
  return new Promise(function (resolve) {
    document.getElementById("prompt-message").textContent = message;
    var input = document.getElementById("prompt-input");
    input.value = defaultValue || "";
    var modal = document.getElementById("prompt-modal");
    modal.classList.remove("hidden");
    modal.classList.add("flex");
    input.focus();
    input.select();

    var cleanup;
    function close(result) {
      modal.classList.add("hidden");
      modal.classList.remove("flex");
      if (cleanup) {
        cleanup();
        cleanup = null;
      }
      resolve(result);
    }
    document.getElementById("prompt-ok-btn").onclick = function () {
      close(input.value);
    };
    document.getElementById("prompt-cancel-btn").onclick = function () {
      close(null);
    };
    cleanup = bindModalDismiss(modal, function () {
      close(null);
    });
    input.onkeydown = function (e) {
      if (e.key === "Enter") {
        e.preventDefault();
        close(input.value);
      }
    };
  });
}

// escapeHtml, timeAgo, formatTimeout → moved to js/lib/formatting.js.
// createModalStateController, openModalPanel, closeModalPanel,
// bindModalDismiss → moved to js/lib/modal.js.

function bindModalBackdropClose(modal, onClose) {
  bindModalDismiss(modal, onClose);
}

function loadJsonEndpoint(url, onSuccess, setState, options) {
  setState("loading");
  var request =
    typeof apiGet === "function"
      ? apiGet(url, options || {})
      : fetch(url, options || {}).then(function (res) {
          return res.json();
        });
  return request
    .then(function (data) {
      onSuccess(data);
    })
    .catch(function (err) {
      setState("error", String(err));
    });
}

function createHoverRow(cells) {
  var tr = document.createElement("tr");
  tr.style.cssText =
    "border-bottom: 1px solid var(--border); transition: background 0.1s;";
  tr.addEventListener("mouseenter", function () {
    tr.style.background = "var(--bg-raised)";
  });
  tr.addEventListener("mouseleave", function () {
    tr.style.background = "";
  });

  cells.forEach(function (cell) {
    var td = document.createElement("td");
    td.style.cssText = cell.style || "padding:6px 10px;";
    if (cell.html != null) {
      td.innerHTML = cell.html;
    } else {
      td.textContent = cell.text != null ? cell.text : "";
    }
    tr.appendChild(td);
  });
  return tr;
}

function appendRowsToTbody(tbodyOrId, rows) {
  var tbody =
    typeof tbodyOrId === "string"
      ? document.getElementById(tbodyOrId)
      : tbodyOrId;
  if (!tbody) return;
  tbody.innerHTML = "";
  (rows || []).forEach(function (row) {
    tbody.appendChild(createHoverRow(row));
  });
}

function appendNoDataRow(tbodyOrId, colSpan, message) {
  var tbody =
    typeof tbodyOrId === "string"
      ? document.getElementById(tbodyOrId)
      : tbodyOrId;
  if (!tbody) return;
  var emptyRow = document.createElement("tr");
  emptyRow.innerHTML =
    '<td colspan="' +
    colSpan +
    '" style="padding:12px 10px;text-align:center;color:var(--text-muted);font-size:12px;">' +
    escapeHtml(message || "No data") +
    "</td>";
  tbody.appendChild(emptyRow);
}

function closeFirstVisibleModal(modals) {
  for (var i = 0; i < modals.length; i++) {
    var item = modals[i];
    if (!item) continue;
    var modal = document.getElementById(item.id);
    if (!modal || modal.classList.contains("hidden")) continue;
    if (typeof item.close === "function") item.close();
    return true;
  }
  return false;
}

// taskDisplayPrompt returns the prompt text that should be shown to users.
// For brainstorm runner cards we show the generated execution prompt once it
// exists so the card/modal reflects the actual synthesized instructions.
function taskDisplayPrompt(task) {
  if (task && task.kind === "idea-agent" && task.execution_prompt)
    return task.execution_prompt;
  return task ? task.prompt : "";
}

function getTaskAccessibleTitle(task) {
  if (!task) return "";
  if (task.title) return task.title;
  if (task.prompt)
    return task.prompt.length > 60
      ? task.prompt.slice(0, 60) + "\u2026"
      : task.prompt;
  return task.id || "";
}

function formatTaskStatusLabel(status) {
  return String(status || "").replace(/_/g, " ");
}

function announceBoardStatus(message) {
  var announcer = document.getElementById("board-announcer");
  if (!announcer) return;
  announcer.textContent = "";
  announcer.textContent = message || "";
}

// --- Mobile column navigation ---

function scrollToColumn(wrapperId) {
  const el = document.getElementById(wrapperId);
  if (!el) return;
  el.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "start" });
}

// Keep the mobile nav active pill in sync with the visible column.
(function initMobileColNav() {
  function setup() {
    const board = document.getElementById("board");
    const nav = document.getElementById("mobile-col-nav");
    if (!board || !nav) return;

    const colWrapperIds = [
      "col-wrapper-backlog",
      "col-wrapper-in_progress",
      "col-wrapper-waiting",
      "col-wrapper-done",
      "col-wrapper-cancelled",
    ];

    const observer = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (!entry.isIntersecting) return;
          const id = entry.target.id;
          nav.querySelectorAll(".mobile-col-btn").forEach(function (btn) {
            btn.classList.toggle("active", btn.dataset.col === id);
          });
        });
      },
      {
        root: board,
        threshold: 0.5,
      },
    );

    colWrapperIds.forEach(function (id) {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", setup);
  } else {
    setup();
  }
})();
