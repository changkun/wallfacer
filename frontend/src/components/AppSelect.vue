<script setup lang="ts" generic="T extends string | number">
// App-native single-select to replace the browser <select>. A trigger button
// shows the current option's label; clicking opens a listbox of options.
// Keyboard- and click-outside-aware with ARIA listbox roles. Values keep their
// type (string or number), so callers bind a plain v-model — no .number needed.
import { ref, computed, nextTick, onBeforeUnmount } from 'vue';

interface Option<V> {
  value: V;
  label: string;
  disabled?: boolean;
}

const props = withDefaults(
  defineProps<{
    modelValue: T;
    options: Option<T>[];
    ariaLabel?: string;
    /** Native tooltip text on the trigger. */
    title?: string;
    /** Shown on the trigger when no option matches modelValue. */
    placeholder?: string;
    disabled?: boolean;
    /** Extra class(es) applied to the trigger button. */
    triggerClass?: string;
    /** Stretch the control to fill its container. */
    block?: boolean;
  }>(),
  { ariaLabel: 'Select', placeholder: 'Select…', triggerClass: '', block: false },
);
const emit = defineEmits<{ 'update:modelValue': [T] }>();

const open = ref(false);
const root = ref<HTMLElement | null>(null);
const menuRef = ref<HTMLUListElement | null>(null);
const triggerRef = ref<HTMLButtonElement | null>(null);
const activeIndex = ref(0);

const selectedLabel = computed(() => {
  const m = props.options.find((o) => o.value === props.modelValue);
  return m ? m.label : '';
});

function onDocPointer(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node)) close();
}

function openMenu() {
  if (props.disabled) return;
  open.value = true;
  activeIndex.value = Math.max(0, props.options.findIndex((o) => o.value === props.modelValue));
  document.addEventListener('mousedown', onDocPointer);
  nextTick(() => menuRef.value?.focus());
}

function close() {
  open.value = false;
  document.removeEventListener('mousedown', onDocPointer);
}

function toggle() {
  open.value ? close() : openMenu();
}

function select(opt: Option<T>) {
  if (opt.disabled) return;
  emit('update:modelValue', opt.value);
  close();
}

function focusTrigger() {
  nextTick(() => triggerRef.value?.focus());
}

function moveActive(delta: number) {
  const n = props.options.length;
  if (n === 0) return;
  let i = activeIndex.value;
  for (let step = 0; step < n; step++) {
    i = (i + delta + n) % n;
    if (!props.options[i].disabled) break;
  }
  activeIndex.value = i;
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
    moveActive(1);
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    moveActive(-1);
  } else if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    const opt = props.options[activeIndex.value];
    if (opt) select(opt);
    focusTrigger();
  } else if (e.key === 'Escape') {
    e.preventDefault();
    close();
    focusTrigger();
  }
}

onBeforeUnmount(() => document.removeEventListener('mousedown', onDocPointer));
</script>

<template>
  <div ref="root" class="app-select" :class="{ 'app-select--block': block }">
    <button
      ref="triggerRef"
      type="button"
      class="app-select__trigger"
      :class="triggerClass"
      role="combobox"
      :aria-label="ariaLabel"
      :title="title"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :disabled="disabled"
      @click="toggle"
      @keydown="onTriggerKeydown"
    >
      <span class="app-select__value" :class="{ 'app-select__value--placeholder': !selectedLabel }">
        {{ selectedLabel || placeholder }}
      </span>
      <svg class="app-select__caret" width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <path d="M2 3.5 L5 6.5 L8 3.5" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </button>

    <ul
      v-if="open"
      ref="menuRef"
      class="app-select__menu"
      role="listbox"
      tabindex="-1"
      @keydown="onListKeydown"
    >
      <li
        v-for="(opt, i) in options"
        :key="String(opt.value)"
        class="app-select__opt"
        :class="{ 'is-active': i === activeIndex, 'is-selected': opt.value === modelValue, 'is-disabled': opt.disabled }"
        role="option"
        :aria-selected="opt.value === modelValue"
        @mouseenter="activeIndex = i"
        @click="select(opt)"
      >
        {{ opt.label }}
      </li>
    </ul>
  </div>
</template>

<style scoped>
.app-select {
  position: relative;
  display: inline-block;
}
.app-select--block {
  display: block;
}
.app-select__trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: var(--bg-input);
  border: 1px solid var(--border);
  color: var(--text-secondary);
  border-radius: 4px;
  padding: 4px 8px;
  font-size: 12px;
  font-family: inherit;
  cursor: pointer;
  text-align: left;
}
.app-select__trigger:focus-visible {
  outline: none;
  border-color: var(--accent);
}
.app-select__trigger:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.app-select__value {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.app-select__value--placeholder {
  color: var(--text-muted);
}
.app-select__caret {
  flex-shrink: 0;
  margin-left: auto;
  color: var(--text-muted);
}
.app-select__menu {
  position: absolute;
  z-index: 40;
  top: calc(100% + 4px);
  left: 0;
  min-width: 100%;
  max-height: 280px;
  overflow-y: auto;
  margin: 0;
  padding: 4px;
  list-style: none;
  background: var(--bg-card, var(--bg-input));
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
  outline: none;
}
.app-select__opt {
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  white-space: nowrap;
  font-size: 12px;
  color: var(--text);
}
.app-select__opt.is-active {
  background: var(--bg-hover, rgba(127, 127, 127, 0.14));
}
.app-select__opt.is-selected {
  font-weight: 700;
}
.app-select__opt.is-disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
