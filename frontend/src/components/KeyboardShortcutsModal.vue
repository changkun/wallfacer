<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();

// The bindings, grouped by where they apply. Each row is the key chord and
// what it does; the template renders one card of rows per group.
const GROUPS: { title: string; rows: { keys: string[]; label: string }[] }[] = [
  {
    title: 'Global',
    rows: [
      { keys: ['n'], label: 'New task' },
      { keys: ['/'], label: 'Focus search' },
      { keys: ['Ctrl', 'K'], label: 'Command palette' },
      { keys: ['Ctrl', '`'], label: 'Toggle terminal' },
      { keys: ['e'], label: 'Toggle Explorer' },
      { keys: ['p'], label: 'Switch to Plan mode' },
      { keys: ['Ctrl', ','], label: 'Open settings' },
      { keys: ['?'], label: 'Show this help' },
      { keys: ['Esc'], label: 'Close modal / cancel' },
    ],
  },
  {
    title: 'New task form',
    rows: [
      { keys: ['Ctrl', 'Enter'], label: 'Save task' },
      { keys: ['Esc'], label: 'Cancel' },
    ],
  },
  {
    title: 'Card navigation',
    rows: [
      { keys: ['Enter', 'Space'], label: 'Open task' },
      { keys: ['Arrow keys'], label: 'Navigate cards' },
      { keys: ['s'], label: 'Start backlog task' },
      { keys: ['d'], label: 'Done (waiting task)' },
      { keys: ['Esc'], label: 'Blur card' },
    ],
  },
];

function close() { emit('update:modelValue', false); }
function onOverlayClick(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) close();
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.modelValue) close();
}
onMounted(() => document.addEventListener('keydown', onKey));
onUnmounted(() => document.removeEventListener('keydown', onKey));
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
      @click="onOverlayClick"
    >
      <div class="dialog shortcuts" role="dialog" aria-modal="true" aria-label="Keyboard shortcuts">
        <div class="dialog-head">
          <div class="dialog-head__main">
            <h3 class="dialog-title">Keyboard shortcuts</h3>
          </div>
          <button type="button" class="icon-btn" aria-label="Close" @click="close">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"></line><line x1="18" y1="6" x2="6" y2="18"></line></svg>
          </button>
        </div>
        <div class="dialog-body">
          <section v-for="g in GROUPS" :key="g.title" class="card compact shortcuts__group">
            <div class="card-head"><span class="eyebrow">{{ g.title }}</span></div>
            <div class="rows">
              <div v-for="r in g.rows" :key="r.label" class="row">
                <span class="row-main shortcuts__label">{{ r.label }}</span>
                <span class="row-end shortcuts__keys">
                  <template v-for="(k, i) in r.keys" :key="k">
                    <span v-if="i > 0" class="shortcuts__plus" aria-hidden="true">+</span>
                    <kbd class="key">{{ k }}</kbd>
                  </template>
                </span>
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.shortcuts__group + .shortcuts__group {
  margin-top: 12px;
}
.shortcuts__label {
  font-size: var(--fs-base);
  color: var(--ink);
}
.shortcuts__keys {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.shortcuts__plus {
  font-size: var(--fs-10);
  color: var(--ink-4);
}
</style>
