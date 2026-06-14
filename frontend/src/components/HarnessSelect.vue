<script setup lang="ts">
// A logo+text harness picker. Native <select> can't render SVG, so this is
// a custom listbox: a trigger button showing the current selection and a
// dropdown of options, each a HarnessBadge. Keyboard- and click-outside-
// aware, with ARIA listbox roles.
import { ref, computed, nextTick, onBeforeUnmount } from 'vue';
import HarnessBadge from './HarnessBadge.vue';

const props = withDefaults(
  defineProps<{
    modelValue: string;
    options: string[];
    // Label for the empty ('') value — the "use the agent's default" choice.
    defaultLabel?: string;
    ariaLabel?: string;
  }>(),
  { defaultLabel: 'Default (agent)', ariaLabel: 'Harness override' },
);
const emit = defineEmits<{ 'update:modelValue': [string] }>();

const open = ref(false);
const root = ref<HTMLElement | null>(null);
const menuRef = ref<HTMLUListElement | null>(null);
const activeIndex = ref(0);

// All selectable values: '' (default) followed by each harness id.
const values = computed(() => ['', ...props.options]);

function onDocPointer(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node)) close();
}

function openMenu() {
  open.value = true;
  activeIndex.value = Math.max(0, values.value.indexOf(props.modelValue));
  document.addEventListener('mousedown', onDocPointer);
  // Move focus into the listbox so arrow-key navigation works immediately.
  nextTick(() => menuRef.value?.focus());
}

function close() {
  open.value = false;
  document.removeEventListener('mousedown', onDocPointer);
}

function toggle() {
  open.value ? close() : openMenu();
}

function select(v: string) {
  emit('update:modelValue', v);
  close();
}

function onTriggerKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    if (!open.value) openMenu();
  } else if (e.key === 'Escape') {
    close();
  }
}

function onListKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    activeIndex.value = (activeIndex.value + 1) % values.value.length;
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    activeIndex.value = (activeIndex.value - 1 + values.value.length) % values.value.length;
  } else if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    select(values.value[activeIndex.value]);
    focusTrigger();
  } else if (e.key === 'Escape') {
    e.preventDefault();
    close();
    focusTrigger();
  }
}

const triggerRef = ref<HTMLButtonElement | null>(null);
function focusTrigger() {
  nextTick(() => triggerRef.value?.focus());
}

onBeforeUnmount(() => document.removeEventListener('mousedown', onDocPointer));
</script>

<template>
  <div ref="root" class="harness-select">
    <button
      ref="triggerRef"
      type="button"
      class="harness-select__trigger composer__select"
      role="combobox"
      :aria-label="ariaLabel"
      aria-haspopup="listbox"
      :aria-expanded="open"
      @click="toggle"
      @keydown="onTriggerKeydown"
    >
      <HarnessBadge v-if="modelValue" :harness="modelValue" :size="15" />
      <span v-else class="harness-select__default">{{ defaultLabel }}</span>
      <svg class="harness-select__caret" width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <path d="M2 3.5 L5 6.5 L8 3.5" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </button>

    <ul
      v-if="open"
      class="harness-select__menu"
      role="listbox"
      tabindex="-1"
      @keydown="onListKeydown"
      ref="menuRef"
    >
      <li
        v-for="(v, i) in values"
        :key="v || '__default'"
        class="harness-select__opt"
        :class="{ 'is-active': i === activeIndex, 'is-selected': v === modelValue }"
        role="option"
        :aria-selected="v === modelValue"
        @mouseenter="activeIndex = i"
        @click="select(v)"
      >
        <HarnessBadge v-if="v" :harness="v" :size="15" />
        <span v-else>{{ defaultLabel }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.harness-select {
  position: relative;
  display: inline-block;
}
.harness-select__trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  min-width: 132px;
}
.harness-select__default {
  font-weight: 600;
}
.harness-select__caret {
  margin-left: auto;
  color: var(--text-muted);
}
.harness-select__menu {
  position: absolute;
  z-index: 30;
  top: calc(100% + 4px);
  left: 0;
  min-width: 100%;
  margin: 0;
  padding: 4px;
  list-style: none;
  background: var(--bg-card, var(--bg-input));
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
  outline: none;
}
.harness-select__opt {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  white-space: nowrap;
  font-size: 12px;
}
.harness-select__opt.is-active {
  background: var(--bg-hover, rgba(127, 127, 127, 0.14));
}
.harness-select__opt.is-selected {
  font-weight: 700;
}
</style>
