<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { dependencyCandidates, filterCandidates } from '../lib/depPicker';

// A searchable multi-select for task dependencies. Replaces the plain
// <select multiple>: selected deps show as removable chips on the trigger, the
// dropdown lists candidate tasks (status-sorted) with a live search and
// per-task status badges, and clicking outside closes it. Mirrors the legacy
// ui/js/tasks.js dep-picker.
const props = defineProps<{ modelValue: string[]; excludeId?: string }>();
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>();

const store = useTaskStore();
const open = ref(false);
const search = ref('');
const root = ref<HTMLElement | null>(null);
const searchInput = ref<HTMLInputElement | null>(null);

const candidates = computed(() => dependencyCandidates(store.tasks, props.excludeId));
const filtered = computed(() => filterCandidates(candidates.value, search.value));

const selectedChips = computed(() =>
  props.modelValue
    .map((id) => candidates.value.find((c) => c.id === id))
    .filter((c): c is NonNullable<typeof c> => !!c),
);

function isSelected(id: string): boolean {
  return props.modelValue.includes(id);
}

function toggle(id: string) {
  const next = isSelected(id)
    ? props.modelValue.filter((x) => x !== id)
    : [...props.modelValue, id];
  emit('update:modelValue', next);
}

function remove(id: string, e: Event) {
  e.stopPropagation();
  emit('update:modelValue', props.modelValue.filter((x) => x !== id));
}

async function toggleOpen() {
  open.value = !open.value;
  if (open.value) {
    search.value = '';
    await nextTick();
    searchInput.value?.focus();
  }
}

function onDocClick(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) {
    open.value = false;
  }
}

function statusLabel(status: string): string {
  return status === 'in_progress' ? 'in progress' : status;
}

onMounted(() => document.addEventListener('click', onDocClick));
onUnmounted(() => document.removeEventListener('click', onDocClick));
</script>

<template>
  <div ref="root" class="dep-picker" :class="{ open }">
    <button type="button" class="dep-picker-trigger" @click="toggleOpen">
      <span class="dep-picker-chips">
        <span v-if="selectedChips.length === 0" class="dep-picker-placeholder">No dependencies</span>
        <span v-for="chip in selectedChips" :key="chip.id" class="dep-picker-chip">
          {{ chip.label }}
          <button type="button" class="tag-chip-remove" title="Remove dependency" @click="remove(chip.id, $event)">×</button>
        </span>
      </span>
      <svg class="dep-picker-chevron" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9" /></svg>
    </button>
    <div v-show="open" class="dep-picker-dropdown">
      <input
        ref="searchInput"
        v-model="search"
        type="text"
        class="dep-picker-search"
        placeholder="Search tasks…"
        @click.stop
      />
      <div class="dep-picker-list">
        <div v-if="filtered.length === 0" class="dep-picker-empty">No other tasks</div>
        <label
          v-for="c in filtered"
          :key="c.id"
          class="dep-picker-item"
          :class="{ selected: isSelected(c.id) }"
        >
          <input type="checkbox" :checked="isSelected(c.id)" @change="toggle(c.id)" />
          <span class="dep-picker-item-text">{{ c.label }}</span>
          <span :class="`badge badge-${c.status}`">{{ statusLabel(c.status) }}</span>
        </label>
      </div>
    </div>
  </div>
</template>
