// Sidebar Board unread-dot logic — shows a small accent dot on the
// sidebar Board nav button when tasks arrive while the user is in
// another mode (typically Plan). Session-only: a reload treats all
// existing tasks as "seen" so the dot never shows on a cold open.
//
// Wire-up:
//   - initBoardUnreadSeen(taskIds): call after the first task snapshot to
//     seed the seen-set.
//   - noteBoardNewTask(taskId): call when a task-updated event represents
//     a brand-new task (previousTask === null).
//   - clearBoardUnreadDot(): call whenever Board mode is entered.

var _boardSeenTaskIds = new Set();
var _boardSeenInitialized = false;

function _boardUnreadDotEl() {
  if (typeof document === "undefined") return null;
  return document.getElementById("sidebar-board-unread-dot");
}

function _setBoardUnreadDot(visible) {
  var el = _boardUnreadDotEl();
  if (!el) return;
  if (visible) {
    el.removeAttribute("hidden");
  } else {
    el.setAttribute("hidden", "");
  }
}

function initBoardUnreadSeen(taskIds) {
  _boardSeenTaskIds = new Set();
  if (Array.isArray(taskIds)) {
    for (var i = 0; i < taskIds.length; i++) {
      if (taskIds[i]) _boardSeenTaskIds.add(taskIds[i]);
    }
  }
  _boardSeenInitialized = true;
  // A fresh snapshot always starts with a clean slate — any unread dot
  // from a previous workspace no longer applies.
  _setBoardUnreadDot(false);
}

function noteBoardNewTask(taskId) {
  if (!taskId) return;
  // Until the initial snapshot has populated the seen-set we treat
  // everything as "already seen" (avoids a stray dot on cold open).
  if (!_boardSeenInitialized) {
    _boardSeenTaskIds.add(taskId);
    return;
  }
  if (_boardSeenTaskIds.has(taskId)) return;
  var mode = typeof getCurrentMode === "function" ? getCurrentMode() : "board";
  if (mode === "board") {
    // User is already looking at the Board — count as seen, no dot.
    _boardSeenTaskIds.add(taskId);
    return;
  }
  _setBoardUnreadDot(true);
}

function clearBoardUnreadDot() {
  if (typeof tasks !== "undefined" && Array.isArray(tasks)) {
    for (var i = 0; i < tasks.length; i++) {
      if (tasks[i] && tasks[i].id) _boardSeenTaskIds.add(tasks[i].id);
    }
  }
  _setBoardUnreadDot(false);
}

function _boardUnreadDotVisibleForTests() {
  var el = _boardUnreadDotEl();
  if (!el) return false;
  return !el.hasAttribute("hidden");
}
