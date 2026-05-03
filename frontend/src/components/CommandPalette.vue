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
    <div
      v-if="modelValue"
      class="modal-overlay command-palette"
      @click="onOverlayClick"
      @keydown="onKeydown"
    >
      <div class="command-palette-panel">
        <div class="command-palette-header">
          <span class="command-palette-label">
            <strong>Command palette</strong>
          </span>
          <span class="command-palette-hints">&#8984;/ Ctrl+K</span>
        </div>
        <input
          ref="inputRef"
          v-model="query"
          type="text"
          class="command-palette-input"
          placeholder="Search commands"
          autocomplete="off"
        />
        <div class="command-palette-results">
          <div v-if="filtered.length" class="command-palette-section">
            <div class="command-palette-section-title">Navigation</div>
            <button
              v-for="(cmd, i) in filtered"
              :key="cmd.path"
              type="button"
              class="command-palette-row"
              :class="{ active: i === activeIndex }"
              @click="select(cmd)"
              @mouseenter="activeIndex = i"
            >
              <div class="command-palette-row-title">
                <span class="cp-row-icon">{{ cmd.icon }}</span>
                {{ cmd.label }}
              </div>
              <div class="command-palette-row-meta">{{ cmd.path }}</div>
            </button>
          </div>
          <div v-else class="command-palette-empty">No results</div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.cp-row-icon {
  display: inline-block;
  width: 18px;
  text-align: center;
  margin-right: 6px;
  opacity: 0.85;
}
</style>
