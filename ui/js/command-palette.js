// --- Command palette ---

let _commandPaletteOpen = false;
let _commandPaletteRows = [];
let _commandPaletteActiveIndex = -1;
let _commandPaletteQuery = '';
let _commandPaletteTaskRows = [];
let _commandPaletteActiveTaskId = '';
let _commandPaletteServerTimer = null;
let _commandPaletteServerSeq = 0;

function _getPaletteElements() {
  return {
    root: document.getElementById('command-palette'),
    panel: document.getElementById('command-palette-panel'),
    input: document.getElementById('command-palette-input'),
    results: document.getElementById('command-palette-results'),
    hint: document.getElementById('command-palette-hint-keys'),
  };
}

function _toTaskId(task) {
  return task && task.id ? String(task.id) : '';
}

function _shortTaskId(task) {
  const id = _toTaskId(task);
  return id ? id.slice(0, 8) : '';
}

function _getTaskTitle(task) {
  return (task && (task.title || task.prompt)) ? (task.title || task.prompt) : '';
}

function _resolveTaskById(taskId) {
  if (!taskId) return null;
  for (const task of tasks) {
    if (task.id === taskId) return task;
  }
  return null;
}

function _hasWorktree(task) {
  return !!(task && task.worktree_paths && Object.keys(task.worktree_paths).length > 0);
}

function _archiveTask(taskId) {
  if (!taskId || typeof api !== 'function') return Promise.resolve();
  return api(`/api/tasks/${encodeURIComponent(taskId)}/archive`, { method: 'POST' })
    .then(function() {
      if (typeof fetchTasks === 'function') fetchTasks();
    })
    .catch(function(e) {
      if (typeof showAlert === 'function') {
        showAlert('Error archiving task: ' + (e && e.message ? e.message : e));
      }
    });
}

function commandPaletteFuzzyMatch(text, query) {
  const hay = String(text || '').toLowerCase();
  const needle = String(query || '').trim().toLowerCase();

  if (!needle) return { matched: true, score: Number.MAX_SAFE_INTEGER };

  const exact = hay.indexOf(needle);
  if (exact !== -1) {
    return { matched: true, score: 10_000 - exact };
  }

  let position = 0;
  let gap = 0;
  for (let i = 0; i < needle.length; i++) {
    const ch = needle[i];
    const next = hay.indexOf(ch, position);
    if (next === -1) return { matched: false, score: 0 };
    gap += next - position;
    position = next + 1;
  }

  return { matched: true, score: 1_000 - gap };
}

function commandPaletteMatchTask(task, query) {
  const q = String(query || '').trim();
  if (!q) return { matched: true, score: Number.MAX_SAFE_INTEGER };

  const candidates = [
    _getTaskTitle(task),
    task && task.prompt,
    _shortTaskId(task),
  ];

  let bestMatch = null;
  for (const field of candidates) {
    const result = commandPaletteFuzzyMatch(field, q);
    if (!result.matched) continue;
    if (!bestMatch || result.score > bestMatch.score) bestMatch = result;
  }

  return bestMatch;
}

function commandPaletteSearchTasks(query, sourceTasks) {
  const q = String(query || '').trim();
  const available = Array.isArray(sourceTasks) ? sourceTasks : tasks;
  if (!available || !available.length) return [];

  if (!q) return available.slice();

  const matches = [];
  for (const task of available) {
    const match = commandPaletteMatchTask(task, q);
    if (match && match.matched) matches.push({ task, score: match.score });
  }

  matches.sort(function(a, b) {
    if (b.score !== a.score) return b.score - a.score;
    return _getTaskTitle(a.task).localeCompare(_getTaskTitle(b.task));
  });

  return matches.map(({ task }) => task);
}

function _buildTaskRow(task, isRemote) {
  return {
    type: 'task',
    id: _toTaskId(task),
    taskObj: task,
    title: _getTaskTitle(task) || '(untitled)',
    prompt: task && task.prompt ? task.prompt : '',
    idHint: _shortTaskId(task),
    status: task && task.status ? task.status : '',
    snippet: task && task.snippet ? task.snippet : '',
    isRemote: !!isRemote,
    execute: function() {
      return openModal(_toTaskId(task));
    },
  };
}

function _localTaskRowsForQuery(query) {
  return commandPaletteSearchTasks(query, tasks).map(function(task) {
    return _buildTaskRow(task, false);
  });
}

function commandPaletteTaskActions(task) {
  if (!task || !task.id) return [];

  const actions = [];

  if (task.status === 'backlog') {
    const refineStatus = task.current_refinement && task.current_refinement.status;
    const refineBlocked = refineStatus === 'running' || refineStatus === 'done';
    if (!refineBlocked) {
      actions.push({
        id: 'start-task',
        label: 'Start',
        hint: 'move to in progress',
        execute: function() { return updateTaskStatus(task.id, 'in_progress'); },
      });
    }
  }

  if (task.status === 'waiting') {
    actions.push({
      id: 'run-test',
      label: 'Run test',
      hint: 'start test agent',
      execute: function() { return quickTestTask(task.id); },
    });
    actions.push({
      id: 'mark-done',
      label: 'Mark done',
      hint: 'complete and commit',
      execute: function() { return quickDoneTask(task.id); },
    });
  }

  if (task.status === 'failed' && task.session_id) {
    actions.push({
      id: 'resume-task',
      label: 'Resume',
      hint: 'resume failed task in same session',
      execute: function() { return quickResumeTask(task.id, task.timeout || 15); },
    });
  }

  if (task.status === 'failed' || task.status === 'done' || task.status === 'waiting' || task.status === 'cancelled') {
    actions.push({
      id: 'retry-task',
      label: 'Retry',
      hint: 'reset to backlog',
      execute: function() { return quickRetryTask(task.id); },
    });
  }

  if ((task.status === 'done' || task.status === 'cancelled') && !task.archived) {
    actions.push({
      id: 'archive-task',
      label: 'Archive',
      hint: 'move task to archive',
      execute: function() { return _archiveTask(task.id); },
    });
  }

  if (task.status === 'waiting' || task.status === 'failed') {
    actions.push({
      id: 'sync-task',
      label: 'Sync with default',
      hint: 'rebase worktree',
      execute: function() { return syncTask(task.id); },
    });
  }

  actions.push({
    id: 'open-task',
    label: 'Open task',
    hint: 'open task modal',
    execute: function() { return openModal(task.id); },
  });

  if (task.status !== 'backlog') {
    actions.push({
      id: 'open-task-testing',
      label: 'Open testing',
      hint: 'switch modal to testing tab',
      execute: function() {
        return Promise.resolve()
          .then(function() { return openModal(task.id); })
          .then(function() { return setRightTab('testing'); });
      },
    });

    actions.push({
      id: 'open-task-changes',
      label: 'Open changes',
      hint: _hasWorktree(task) ? 'switch to changes tab' : 'no worktree available',
      execute: function() {
        return Promise.resolve()
          .then(function() { return openModal(task.id); })
          .then(function() { return setRightTab('changes'); });
      },
    });

    if (task.turns > 0) {
      actions.push({
        id: 'open-task-spans',
        label: 'Open flamegraph',
        hint: 'switch to spans tab',
        execute: function() {
          return Promise.resolve()
            .then(function() { return openModal(task.id); })
            .then(function() { return setRightTab('spans'); });
        },
      });

      actions.push({
        id: 'open-task-timeline',
        label: 'Open timeline',
        hint: 'switch to timeline tab',
        execute: function() {
          return Promise.resolve()
            .then(function() { return openModal(task.id); })
            .then(function() { return setRightTab('timeline'); });
        },
      });
    }
  }

  return actions;
}

function _buildActionRow(action) {
  return {
    type: 'action',
    id: 'action-' + action.id + ':' + action.label,
    actionId: action.id,
    label: action.label,
    hint: action.hint || '',
    execute: action.execute,
  };
}

function _taskRowsFromRows(rows) {
  return rows.filter(function(row) { return row && row.type === 'task'; });
}

function _findSelectedTaskFromRows() {
  if (_commandPaletteActiveTaskId) {
    const selected = _commandPaletteTaskRows.find(function(row) {
      return row.id === _commandPaletteActiveTaskId;
    });
    if (selected) {
      return _resolveTaskById(selected.id) || selected.taskObj;
    }
  }

  const firstTask = _taskRowsFromRows(_commandPaletteRows)[0];
  if (firstTask) {
    return _resolveTaskById(firstTask.id) || firstTask.taskObj;
  }

  return null;
}

function _setActiveRow(index) {
  _commandPaletteActiveIndex = index;

  const els = _getPaletteElements();
  const rowEls = (els.results && typeof els.results.querySelectorAll === 'function')
    ? els.results.querySelectorAll('.command-palette-row')
    : [];

  let next = -1;
  for (let i = 0; i < rowEls.length; i++) {
    const rowEl = rowEls[i];
    const isActive = i === index;
    rowEl.classList.toggle('active', isActive);
    if (isActive) {
      next = i;
      const row = _commandPaletteRows[i];
      if (row && row.type === 'task') {
        _commandPaletteActiveTaskId = row.id;
      }
    }
  }

  if (next === -1) _commandPaletteActiveTaskId = '';

  if (els.hint) {
    els.hint.textContent = _commandPaletteRows.length
      ? '↑↓ navigate • Enter run • Esc close'
      : 'Esc close';
  }
}

function _updateContextActionsForActiveRow(taskRows) {
  const active = _commandPaletteRows[_commandPaletteActiveIndex];
  if (!active || active.type !== 'task' || !active.id) return;
  const selectedTaskRow = (taskRows || []).find(function(row) {
    return row.id === active.id;
  });
  const selectedTask = _resolveTaskById(active.id) || (selectedTaskRow && selectedTaskRow.taskObj);
  if (!selectedTask) return;
  _updateContextActions(taskRows, selectedTask);
}

function moveCommandPaletteActiveRow(delta) {
  const flatRows = _commandPaletteRows;
  if (!flatRows.length) {
    _commandPaletteActiveIndex = -1;
    return;
  }

  if (_commandPaletteActiveIndex < 0) {
    _commandPaletteActiveIndex = delta > 0 ? 0 : flatRows.length - 1;
    _setActiveRow(_commandPaletteActiveIndex);
    _updateContextActionsForActiveRow(_commandPaletteTaskRows);
    return;
  }

  const next = _commandPaletteActiveIndex + delta;
  if (next < 0) {
    _setActiveRow(flatRows.length - 1);
    _updateContextActionsForActiveRow(_commandPaletteTaskRows);
    return;
  }
  if (next >= flatRows.length) {
    _setActiveRow(0);
    _updateContextActionsForActiveRow(_commandPaletteTaskRows);
    return;
  }

  _setActiveRow(next);
  _updateContextActionsForActiveRow(_commandPaletteTaskRows);
}

function commandPaletteMoveDown() { moveCommandPaletteActiveRow(1); }
function commandPaletteMoveUp() { moveCommandPaletteActiveRow(-1); }

function executeCommandPaletteActiveRow() {
  const row = _commandPaletteRows[_commandPaletteActiveIndex];
  if (!row || typeof row.execute !== 'function') return;
  closeCommandPalette();
  return row.execute();
}

function _groupWithTitle(title, rows) {
  return {
    title,
    rows,
  };
}

function _flattenRows(groups) {
  const all = [];
  groups.forEach(function(group) {
    if (!group || !group.rows || !group.rows.length) return;
    for (const row of group.rows) {
      all.push(row);
    }
  });
  return all;
}

function _buildTaskListSections(groups) {
  const els = _getPaletteElements();
  if (!els.results) return;

  els.results.innerHTML = '';
  _commandPaletteRows = _flattenRows(groups);

  if (!_commandPaletteRows.length) {
    _commandPaletteActiveIndex = -1;
    _commandPaletteActiveTaskId = '';
    const section = document.createElement('section');
    section.className = 'command-palette-section';
    const title = document.createElement('div');
    title.className = 'command-palette-section-title';
    title.textContent = 'No matches';
    section.appendChild(title);
    const empty = document.createElement('div');
    empty.className = 'command-palette-empty';
    empty.textContent = 'No results';
    section.appendChild(empty);
    els.results.appendChild(section);
    if (els.hint) els.hint.textContent = 'Esc close';
    return;
  }

  for (const group of groups) {
    if (!group || !group.rows) continue;

    const section = document.createElement('section');
    section.className = 'command-palette-section';

    const heading = document.createElement('div');
    heading.className = 'command-palette-section-title';
    heading.textContent = group.title;
    section.appendChild(heading);

    if (!group.rows.length) {
      const empty = document.createElement('div');
      empty.className = 'command-palette-empty';
      empty.textContent = 'No entries';
      section.appendChild(empty);
      els.results.appendChild(section);
      continue;
    }

    for (const row of group.rows) {
      const index = _commandPaletteRows.indexOf(row);
      const rowEl = document.createElement('button');
      rowEl.type = 'button';
      rowEl.className = 'command-palette-row ' + (row.type === 'task' ? 'command-palette-row-task' : 'command-palette-row-action');

      if (row.type === 'task') {
        const title = row.title || '(untitled)';
        const status = row.status ? `<span class="command-palette-task-badge">${row.status}</span>` : '';
        const idLabel = row.idHint ? `<span class="command-palette-task-id">${row.idHint}</span>` : '';
        const snippet = row.snippet ? `<div class="command-palette-task-snippet">${escapeHtml(row.snippet)}</div>` : '';
        const trimmedPrompt = row.prompt ? row.prompt.slice(0, 180) : '';
        const promptSnippet = !row.snippet && trimmedPrompt
          ? '<div class="command-palette-task-snippet">' + escapeHtml(trimmedPrompt) + '</div>'
          : '';
        rowEl.innerHTML =
          '<div class="command-palette-row-title">' + escapeHtml(title) + '</div>' +
          '<div class="command-palette-row-meta">' + idLabel + status + '</div>' +
          (snippet || promptSnippet);
      } else {
        rowEl.innerHTML =
          '<div class="command-palette-row-title">' + escapeHtml(row.label) + '</div>' +
          (row.hint ? ('<div class="command-palette-row-hint">' + escapeHtml(row.hint) + '</div>') : '');
      }

      rowEl.addEventListener('click', function() {
        _setActiveRow(index);
        executeCommandPaletteActiveRow();
      });

      section.appendChild(rowEl);
    }

    els.results.appendChild(section);
  }

  if (_commandPaletteActiveIndex < 0 || _commandPaletteActiveIndex >= _commandPaletteRows.length) {
    _setActiveRow(0);
  } else {
    _setActiveRow(_commandPaletteActiveIndex);
  }
}

function _updateContextActions(taskRows, selectedTaskOverride) {
  const groups = [_groupWithTitle('Task Targets', taskRows)];

  const selectedTask = selectedTaskOverride || _findSelectedTaskFromRows();

  if (selectedTask) {
    _commandPaletteActiveTaskId = selectedTask.id;
    const actionRows = commandPaletteTaskActions(_resolveTaskById(selectedTask.id) || selectedTask)
      .map(_buildActionRow);
    if (actionRows.length) {
      groups.push(_groupWithTitle('Context Actions', actionRows));
    }
  } else {
    _commandPaletteActiveTaskId = '';
  }

  _buildTaskListSections(groups);
}

function _searchLocal(query) {
  const taskRows = _localTaskRowsForQuery(query);
  _commandPaletteTaskRows = taskRows;
  _commandPaletteActiveTaskId = '';
  _commandPaletteActiveIndex = Math.min(_commandPaletteActiveIndex, taskRows.length - 1);
  _updateContextActions(taskRows);
}

function _searchRemote(query, seq) {
  const trimmed = String(query || '').trim().slice(1).trim();
  if (trimmed.length < 2) {
    _commandPaletteTaskRows = [];
    _buildTaskListSections([_groupWithTitle('Task Targets', [])]);
    return;
  }

  const els = _getPaletteElements();
  if (els.results) {
    els.results.innerHTML =
      '<div class="command-palette-section">' +
      '<div class="command-palette-section-title">Task Targets</div>' +
      '<div class="command-palette-empty">Searching…</div>' +
      '</div>';
  }

  fetch('/api/tasks/search?q=' + encodeURIComponent(trimmed))
    .then(function(resp) { return resp.ok ? resp.json() : Promise.reject(resp.status); })
    .then(function(results) {
      if (seq !== _commandPaletteServerSeq) return;
      const rows = (results || []).map(function(result) {
        const local = _resolveTaskById(result.id);
        const taskObj = local || {
          id: result.id,
          title: result.title || '',
          status: result.status || '',
          prompt: result.snippet || '',
          worktree_paths: local ? local.worktree_paths : null,
          turns: local ? local.turns : 0,
          session_id: local ? local.session_id : null,
          timeout: local ? local.timeout : 15,
          current_refinement: local ? local.current_refinement : null,
        };
        const row = _buildTaskRow(taskObj, true);
        row.snippet = result.snippet || '';
        return row;
      });

      _commandPaletteTaskRows = rows;
      if (!rows.length) {
        _commandPaletteActiveIndex = -1;
        _buildTaskListSections([_groupWithTitle('Task Targets', rows)]);
        return;
      }

      _commandPaletteActiveTaskId = rows[0].id;
      _updateContextActions(rows);
    })
    .catch(function() {
      if (seq !== _commandPaletteServerSeq) return;
      _commandPaletteTaskRows = [];
      _buildTaskListSections([_groupWithTitle('Task Targets', [])]);
    });
}

function _updatePaletteFromInput() {
  const els = _getPaletteElements();
  const query = els.input ? els.input.value : '';
  _commandPaletteQuery = query;
  _commandPaletteRows = [];
  _commandPaletteTaskRows = [];
  _commandPaletteActiveIndex = -1;

  if (query.trim().startsWith('@')) {
    const seq = ++_commandPaletteServerSeq;
    clearTimeout(_commandPaletteServerTimer);
    _commandPaletteServerTimer = setTimeout(function() {
      _searchRemote(query, seq);
    }, 220);
    return;
  }

  _searchLocal(query);
}

function openCommandPalette() {
  const els = _getPaletteElements();
  if (!els.root || !els.input) return;

  _commandPaletteOpen = true;
  _commandPaletteActiveIndex = 0;
  _commandPaletteTaskRows = [];
  _commandPaletteRows = [];
  _commandPaletteActiveTaskId = '';

  els.root.classList.remove('hidden');
  els.input.value = _commandPaletteQuery || '';
  _updatePaletteFromInput();
  els.input.focus();
  els.input.setSelectionRange(els.input.value.length, els.input.value.length);
}

function closeCommandPalette() {
  const els = _getPaletteElements();
  if (!els.root) return;

  _commandPaletteOpen = false;
  els.root.classList.add('hidden');
  _commandPaletteRows = [];
  _commandPaletteTaskRows = [];
  _commandPaletteActiveIndex = -1;
  _commandPaletteActiveTaskId = '';
  _commandPaletteQuery = '';
  _commandPaletteServerSeq++;
}

function isCommandPaletteOpen() {
  return !!_commandPaletteOpen;
}

function commandPaletteToggle() {
  if (isCommandPaletteOpen()) {
    closeCommandPalette();
  } else {
    openCommandPalette();
  }
}

function commandPaletteHandleGlobalKey(e) {
  const tag = document.activeElement && document.activeElement.tagName;

  if (e.key === 'k' && (e.ctrlKey || e.metaKey)) {
    if (tag !== 'INPUT' && tag !== 'TEXTAREA' && !(document.activeElement && document.activeElement.isContentEditable)) {
      e.preventDefault();
      e.stopImmediatePropagation();
      openCommandPalette();
      return;
    }
  }

  if (!isCommandPaletteOpen()) return;

  if (e.key === 'Escape') {
    e.preventDefault();
    e.stopImmediatePropagation();
    closeCommandPalette();
    return;
  }

  if (e.key === 'ArrowDown') {
    e.preventDefault();
    commandPaletteMoveDown();
    return;
  }

  if (e.key === 'ArrowUp') {
    e.preventDefault();
    commandPaletteMoveUp();
    return;
  }

  if (e.key === 'Enter') {
    e.preventDefault();
    executeCommandPaletteActiveRow();
  }
}

function commandPaletteHandlePaletteInput() {
  _updatePaletteFromInput();
}

(function initCommandPalette() {
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initCommandPalette);
    return;
  }

  const els = _getPaletteElements();
  if (!els.root) return;

  document.addEventListener('keydown', commandPaletteHandleGlobalKey);

  if (els.input) {
    els.input.addEventListener('input', commandPaletteHandlePaletteInput);
    els.input.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') {
        e.preventDefault();
        closeCommandPalette();
      }
    });
  }

  els.root.addEventListener('click', function() {
    closeCommandPalette();
  });

  if (els.panel) {
    els.panel.addEventListener('click', function(e) {
      e.stopPropagation();
    });
  }
})();
