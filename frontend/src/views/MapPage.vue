<script setup lang="ts">
import { computed } from 'vue';
import { useTaskStore } from '../stores/tasks';

const store = useTaskStore();

const nodes = computed(() => store.tasks
  .filter(t => !t.archived)
  .map(t => ({
    id: t.id,
    title: t.title || (t.prompt || '').slice(0, 60) || t.id.slice(0, 8),
    status: t.status,
    deps: t.depends_on || [],
  })));
</script>

<template>
  <div class="map-page">
    <header class="page-header">
      <span class="page-eyebrow">Workspace</span>
      <h1 class="page-title">Dependency map</h1>
    </header>

    <div class="map-body">
      <p v-if="nodes.length === 0" class="map-empty">
        No tasks yet. Create a task on the board and connect it via dependencies.
      </p>
      <ul v-else class="map-list">
        <li v-for="n in nodes" :key="n.id" class="map-row">
          <span class="badge" :class="`badge-${n.status}`">{{ n.status.replace(/_/g, ' ') }}</span>
          <span class="map-title">{{ n.title }}</span>
          <span class="map-id mono">{{ n.id.slice(0, 8) }}</span>
          <span v-if="n.deps.length" class="map-deps">
            ← {{ n.deps.length }} {{ n.deps.length === 1 ? 'dep' : 'deps' }}
          </span>
        </li>
      </ul>
      <p class="map-footer">
        Dependency graph visualisation is coming soon. Tasks above are listed in
        creation order with their declared dependencies.
      </p>
    </div>
  </div>
</template>

<style scoped>
.map-page {
  flex: 1;
  overflow-y: auto;
  padding: 1.75rem 2rem 2rem;
  background: var(--bg);
  color: var(--text);
  font-family: var(--font-sans);
}
.page-header { display: flex; flex-direction: column; gap: 0.25rem; max-width: 1100px; margin: 0 auto 1.25rem; }
.page-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  color: var(--accent);
  text-transform: uppercase;
  letter-spacing: 0.15em;
}
.page-title { font-size: 28px; font-weight: 600; margin: 0; line-height: 1.15; letter-spacing: -0.025em; }

.map-body { max-width: 1100px; margin: 0 auto; }
.map-empty, .map-footer {
  font-size: 13px;
  color: var(--text-muted);
  padding: 24px;
  text-align: center;
  border: 1px dashed var(--border);
  border-radius: 10px;
  background: var(--bg-card);
}
.map-footer { margin-top: 1rem; }
.map-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 6px; }
.map-row {
  display: grid;
  grid-template-columns: 100px 1fr auto auto;
  gap: 12px;
  align-items: center;
  padding: 10px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-card);
  font-size: 13px;
}
.map-title { color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.map-id { font-size: 11px; color: var(--text-muted); }
.map-deps { font-size: 11px; color: var(--text-muted); }
</style>
