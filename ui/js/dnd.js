// --- Drag and Drop ---

function initSortable() {
  const backlog = document.getElementById('col-backlog');
  const inProgress = document.getElementById('col-in_progress');

  // Backlog: can reorder (sort) and drag out to In Progress
  Sortable.create(backlog, {
    group: { name: 'kanban', pull: true, put: false },
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
    group: { name: 'kanban', pull: false, put: true },
    animation: 150,
    ghostClass: 'sortable-ghost',
    chosenClass: 'sortable-chosen',
    onAdd: function(evt) {
      const id = evt.item.dataset.id;
      updateTaskStatus(id, 'in_progress');
    },
  });

  // Waiting, Done, and Cancelled: no drag interaction
  Sortable.create(document.getElementById('col-waiting'), {
    group: { name: 'kanban', pull: false, put: false },
    animation: 150,
    sort: false,
  });
  Sortable.create(document.getElementById('col-done'), {
    group: { name: 'kanban', pull: false, put: false },
    animation: 150,
    sort: false,
  });
}
