// --- Drag and Drop ---

function initSortable() {
  const backlog = document.getElementById('col-backlog');
  const inProgress = document.getElementById('col-in_progress');

  // Backlog: can reorder (sort) and drag out to In Progress
  Sortable.create(backlog, {
    group: { name: 'taskboard', pull: true, put: false },
    animation: 150,
    ghostClass: 'sortable-ghost',
    chosenClass: 'sortable-chosen',
    onSort: function() {
      // Persist new position order after drag-reorder within backlog.
      const cards = backlog.querySelectorAll('.card[data-id]');
      cards.forEach((card, idx) => {
        const id = card.dataset.id;
        api(`/api/tasks/${id}`, { method: 'PATCH', body: JSON.stringify({ position: idx }) });
      });
    },
  });

  // In Progress: can receive from backlog
  Sortable.create(inProgress, {
    group: { name: 'taskboard', pull: false, put: true },
    animation: 150,
    ghostClass: 'sortable-ghost',
    chosenClass: 'sortable-chosen',
    onAdd: function(evt) {
      const id = evt.item.dataset.id;
      const task = tasks.find(t => t.id === id);
      const refineStatus = task && task.current_refinement && task.current_refinement.status;
      if (refineStatus === 'running' || refineStatus === 'done') {
        // Return card to backlog and alert user.
        backlog.insertBefore(evt.item, backlog.children[evt.oldIndex] || null);
        if (refineStatus === 'running') {
          showAlert('Refinement is in progress. Please wait for it to complete before starting.');
        } else {
          showAlert('This task has a refined prompt awaiting review. Open the task to apply or dismiss it before starting.');
        }
        return;
      }
      updateTaskStatus(id, 'in_progress');
    },
  });

  // Waiting, Done, and Cancelled: no drag interaction
  Sortable.create(document.getElementById('col-waiting'), {
    group: { name: 'taskboard', pull: false, put: false },
    animation: 150,
    sort: false,
  });
  Sortable.create(document.getElementById('col-done'), {
    group: { name: 'taskboard', pull: false, put: false },
    animation: 150,
    sort: false,
  });
}
