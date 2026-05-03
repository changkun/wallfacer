<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue';
import { useRouter } from 'vue-router';

interface Command {
  label: string;
  path: string;
  icon: string;
}

const commands: Command[] = [
  { label: 'Board', path: '/', icon: '☰' },
  { label: 'Agents', path: '/agents', icon: '◆' },
  { label: 'Flows', path: '/flows', icon: '→' },
  { label: 'Terminal', path: '/terminal', icon: '▸' },
  { label: 'Analytics', path: '/analytics', icon: '▪' },
];

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  navigate: [path: string];
}>();

const router = useRouter();
const query = ref('');
const activeIndex = ref(0);
const inputRef = ref<HTMLInputElement | null>(null);

const filtered = computed(() => {
  const q = query.value.toLowerCase();
  if (!q) return commands;
  return commands.filter((c) => {
    const label = c.label.toLowerCase();
    let qi = 0;
    for (let ci = 0; ci < label.length && qi < q.length; ci++) {
      if (label[ci] === q[qi]) qi++;
    }
    return qi === q.length;
  });
});

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      query.value = '';
      activeIndex.value = 0;
      nextTick(() => inputRef.value?.focus());
    }
  },
);

watch(filtered, () => {
  activeIndex.value = 0;
});

function close() {
  emit('update:modelValue', false);
}

function select(cmd: Command) {
  emit('navigate', cmd.path);
  router.push(cmd.path);
  close();
}

function onKeydown(e: KeyboardEvent) {
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault();
      activeIndex.value = (activeIndex.value + 1) % filtered.value.length;
      break;
    case 'ArrowUp':
      e.preventDefault();
      activeIndex.value =
        (activeIndex.value - 1 + filtered.value.length) % filtered.value.length;
      break;
    case 'Enter':
      e.preventDefault();
      if (filtered.value.length > 0) {
        select(filtered.value[activeIndex.value]);
      }
      break;
    case 'Escape':
      e.preventDefault();
      close();
      break;
  }
}

function onOverlayClick(e: MouseEvent) {
  if (e.target === e.currentTarget) close();
}
</script>

<template>
  <Teleport to="body">
    <div v-if="modelValue" class="cp-overlay" @click="onOverlayClick" @keydown="onKeydown">
      <div class="cp-dialog">
        <input
          ref="inputRef"
          v-model="query"
          class="cp-input"
          type="text"
          placeholder="Type a command..."
        />
        <ul v-if="filtered.length" class="cp-list">
          <li
            v-for="(cmd, i) in filtered"
            :key="cmd.path"
            class="cp-item"
            :class="{ active: i === activeIndex }"
            @click="select(cmd)"
            @mouseenter="activeIndex = i"
          >
            <span class="cp-icon">{{ cmd.icon }}</span>
            <span class="cp-label">{{ cmd.label }}</span>
            <span class="cp-path">{{ cmd.path }}</span>
          </li>
        </ul>
        <div v-else class="cp-empty">No results</div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.cp-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 20vh;
}

.cp-dialog {
  width: 100%;
  max-width: 480px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-pop);
  overflow: hidden;
}

.cp-input {
  width: 100%;
  padding: 12px 16px;
  border: none;
  border-bottom: 1px solid var(--rule);
  background: var(--bg);
  color: var(--ink);
  font-family: var(--font-sans);
  font-size: 14px;
  outline: none;
}

.cp-input::placeholder {
  color: var(--ink-4);
}

.cp-list {
  list-style: none;
  margin: 0;
  padding: 4px;
  max-height: 300px;
  overflow-y: auto;
}

.cp-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-radius: var(--r-sm);
  cursor: pointer;
  font-size: 13px;
  color: var(--ink-2);
}

.cp-item:hover,
.cp-item.active {
  background: var(--bg-hover);
  color: var(--ink);
}

.cp-icon {
  flex-shrink: 0;
  width: 20px;
  text-align: center;
  font-size: 14px;
}

.cp-label {
  flex: 1;
  font-weight: 500;
}

.cp-path {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-3);
}

.cp-empty {
  padding: 16px;
  text-align: center;
  font-size: 12px;
  color: var(--ink-3);
}
</style>
