// --- Task-stream state module ---
//
// Pure reducer functions for the tasks SSE stream. Every function here takes
// an explicit state object and returns a new state object — no globals are
// read or written. Callers are responsible for committing the returned state
// back to their own globals.
//
// State shape:
//   { tasks: Task[], archivedTasks: Task[], archivedPage: ArchivedPage }
//
// ArchivedPage shape:
//   { loadState: 'idle'|'loading-before'|'loading-after',
//     hasMoreBefore: boolean, hasMoreAfter: boolean }

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function sortArchivedByUpdatedDesc(items) {
  return items.sort(function(a, b) {
    const ad = new Date(a.updated_at).getTime();
    const bd = new Date(b.updated_at).getTime();
    if (bd !== ad) return bd - ad;
    if (a.id === b.id) return 0;
    return a.id > b.id ? -1 : 1;
  });
}

function cloneArchivedPage(page) {
  return {
    loadState: page && page.loadState ? page.loadState : 'idle',
    hasMoreBefore: !!(page && page.hasMoreBefore),
    hasMoreAfter: !!(page && page.hasMoreAfter),
  };
}

// ---------------------------------------------------------------------------
// Public: state factories and reducers
// ---------------------------------------------------------------------------

/**
 * Creates a fresh (or cloned) tasks state object.
 * @param {object} [state] - optional seed; missing fields default to empty.
 */
function createTasksState(state) {
  const source = state || {};
  return {
    tasks: Array.isArray(source.tasks) ? source.tasks.slice() : [],
    archivedTasks: Array.isArray(source.archivedTasks) ? source.archivedTasks.slice() : [],
    archivedPage: cloneArchivedPage(source.archivedPage),
  };
}

/**
 * Trims the archived window to at most pageSize * 3 entries, discarding from
 * the direction opposite to `direction` and marking the pagination flag.
 */
function trimArchivedWindowState(state, direction, pageSize) {
  const next = createTasksState(state);
  const size = Math.max(1, pageSize || 20);
  const maxItems = size * 3;
  if (next.archivedTasks.length <= maxItems) return next;
  const overflow = next.archivedTasks.length - maxItems;
  if (direction === 'before') {
    next.archivedTasks = next.archivedTasks.slice(overflow);
    next.archivedPage.hasMoreAfter = true;
    return next;
  }
  next.archivedTasks = next.archivedTasks.slice(0, maxItems);
  next.archivedPage.hasMoreBefore = true;
  return next;
}

/**
 * Replaces the active task list from a full SSE snapshot payload.
 * Archived tasks and pagination are left untouched (handled separately).
 * @param {object} state
 * @param {Task[]} snapshot
 */
function applyTasksSnapshot(state, snapshot) {
  const next = createTasksState(state);
  next.tasks = Array.isArray(snapshot) ? snapshot.slice() : [];
  return next;
}

/**
 * Removes the identified task from both active and archived arrays.
 * @param {object} state
 * @param {{id: string}} payload
 */
function applyTaskDeleted(state, payload) {
  const id = payload && payload.id;
  const next = createTasksState(state);
  next.tasks = next.tasks.filter(function(t) { return t.id !== id; });
  next.archivedTasks = next.archivedTasks.filter(function(t) { return t.id !== id; });
  return next;
}

/**
 * Merges a single updated task into state, routing it to active or archived
 * based on its `archived` flag and the caller's `showArchived` option.
 *
 * Returns `{ state, previousTask }` where `previousTask` is the pre-update
 * record (used for status-change announcements).
 *
 * @param {object} state
 * @param {Task} task
 * @param {{ showArchived?: boolean, pageSize?: number }} [opts]
 * @returns {{ state: object, previousTask: Task|null }}
 */
function applyTaskUpdated(state, task, opts) {
  const options = opts || {};
  const next = createTasksState(state);
  const showArchivedTasks = !!options.showArchived;
  const pageSize = Math.max(1, options.pageSize || 20);
  const previousTask = next.tasks.find(function(t) { return t.id === task.id; }) ||
    next.archivedTasks.find(function(t) { return t.id === task.id; }) ||
    null;

  next.tasks = next.tasks.filter(function(t) { return t.id !== task.id; });
  next.archivedTasks = next.archivedTasks.filter(function(t) { return t.id !== task.id; });

  if (task.archived) {
    if (showArchivedTasks) {
      next.archivedTasks.unshift(task);
      sortArchivedByUpdatedDesc(next.archivedTasks);
      return {
        state: trimArchivedWindowState(next, 'after', pageSize),
        previousTask,
      };
    }
    return { state: next, previousTask };
  }

  next.tasks.push(task);
  return { state: next, previousTask };
}

/**
 * Merges a paginated archived-tasks API response into the existing window.
 * Deduplicates by ID and keeps the window sorted descending by updated_at.
 *
 * @param {object} state
 * @param {{ tasks: Task[], has_more_before?: boolean, has_more_after?: boolean }} resp
 * @param {'initial'|'before'|'after'} [direction]
 * @param {number} [pageSize]
 */
function mergeArchivedTasksPage(state, resp, direction, pageSize) {
  const dir = direction || 'initial';
  const page = resp && Array.isArray(resp.tasks) ? resp.tasks : [];
  const next = createTasksState(state);

  if (dir === 'initial') {
    next.archivedTasks = page.slice();
  } else if (page.length > 0) {
    const seen = new Set(next.archivedTasks.map(function(t) { return t.id; }));
    const additions = page.filter(function(t) { return !seen.has(t.id); });
    if (additions.length > 0) {
      if (dir === 'before') {
        next.archivedTasks = next.archivedTasks.concat(additions);
      } else {
        next.archivedTasks = additions.concat(next.archivedTasks);
      }
      sortArchivedByUpdatedDesc(next.archivedTasks);
      const trimmed = trimArchivedWindowState(next, dir, pageSize);
      next.archivedTasks = trimmed.archivedTasks;
      next.archivedPage = trimmed.archivedPage;
    }
  }

  next.archivedPage.hasMoreBefore = !!(resp && resp.has_more_before);
  next.archivedPage.hasMoreAfter = !!(resp && resp.has_more_after);
  return next;
}

/**
 * Builds the SSE stream URL, appending the auth token and, when present, the
 * last received event ID so the server can replay only missed deltas.
 *
 * @param {string} baseUrl
 * @param {string|null|undefined} eventId
 */
function buildTasksStreamUrl(baseUrl, eventId) {
  baseUrl = withAuthToken(baseUrl);
  if (eventId === null || typeof eventId === 'undefined') return baseUrl;
  const sep = baseUrl.includes('?') ? '&' : '?';
  return baseUrl + sep + 'last_event_id=' + encodeURIComponent(eventId);
}
