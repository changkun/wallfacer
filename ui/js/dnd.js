// --- Drag and Drop ---

function initSortable() {
  const backlog = document.getElementById('col-backlog');
  const inProgress = document.getElementById('col-in_progress');

  // Backlog: can drag out
  Sortable.create(backlog, {
    group: { name: 'kanban', pull: true, put: false },
    animation: 150,
    ghostClass: 'sortable-ghost',
    chosenClass: 'sortable-chosen',
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

  // Waiting, Done, and Failed: no drag interaction
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
  Sortable.create(document.getElementById('col-failed'), {
    group: { name: 'kanban', pull: false, put: false },
    animation: 150,
    sort: false,
  });
}
