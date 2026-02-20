// --- API client ---

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (!res.ok && res.status !== 204) {
    const text = await res.text();
    throw new Error(text);
  }
  if (res.status === 204) return null;
  return res.json();
}

// --- Tasks SSE stream ---

function startTasksStream() {
  if (tasksSource) tasksSource.close();
  const url = showArchived ? '/api/tasks/stream?include_archived=true' : '/api/tasks/stream';
  tasksSource = new EventSource(url);
  tasksSource.onmessage = function(e) {
    tasksRetryDelay = 1000;
    try {
      tasks = JSON.parse(e.data);
      render();
    } catch (err) {
      console.error('tasks SSE parse error:', err);
    }
  };
  tasksSource.onerror = function() {
    if (tasksSource.readyState === EventSource.CLOSED) {
      tasksSource = null;
      setTimeout(startTasksStream, tasksRetryDelay);
      tasksRetryDelay = Math.min(tasksRetryDelay * 2, 30000);
    }
  };
}

async function fetchTasks() {
  const url = showArchived ? '/api/tasks?include_archived=true' : '/api/tasks';
  tasks = await api(url);
  render();
}

function toggleShowArchived() {
  showArchived = document.getElementById('show-archived-toggle').checked;
  localStorage.setItem('wallfacer-show-archived', showArchived ? 'true' : 'false');
  startTasksStream();
}
