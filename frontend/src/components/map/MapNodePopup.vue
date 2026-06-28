<script setup lang="ts">
// A floating, draggable, resizable popup that renders a spec's markdown.
// Opened by double-clicking a spec node on the Map. Content is fetched from
// the explorer file endpoint (the same source SpecFocusedView streams), trying
// each configured workspace until one resolves the workspace-relative path.
import { ref, reactive, computed, watch, onMounted } from 'vue';
import { authHeaders } from '../../api/client';
import { renderMarkdown } from '../../lib/markdown';

const props = defineProps<{
  path: string; // workspace-relative spec path, e.g. specs/local/foo.md
  title: string;
  workspaces: string[];
}>();
const emit = defineEmits<{ close: []; discuss: [] }>();

const content = ref('');
const loading = ref(true);
const error = ref(false);

// Drop the leading YAML frontmatter block so the popup shows the spec body, not
// a raw `title: … depends_on: …` dump (the explorer endpoint returns the file
// verbatim, frontmatter included).
function stripFrontmatter(src: string): string {
  if (!src.startsWith('---')) return src;
  const close = src.indexOf('\n---', 3);
  if (close === -1) return src;
  const nl = src.indexOf('\n', close + 1);
  return nl === -1 ? '' : src.slice(nl + 1);
}
const rendered = computed(() => (content.value ? renderMarkdown(stripFrontmatter(content.value)) : ''));

async function load() {
  loading.value = true;
  error.value = false;
  for (const ws of props.workspaces) {
    const abs = ws + '/' + props.path;
    const url =
      '/api/explorer/file?path=' + encodeURIComponent(abs) + '&workspace=' + encodeURIComponent(ws);
    try {
      const res = await fetch(url, { headers: authHeaders(), credentials: 'same-origin' });
      if (res.ok) {
        content.value = await res.text();
        loading.value = false;
        return;
      }
    } catch {
      /* try the next workspace */
    }
  }
  error.value = true;
  loading.value = false;
}
onMounted(load);
watch(() => props.path, load);

// --- drag the popup by its header (CSS `resize` handles sizing) ---
const geom = reactive({ x: Math.max(40, window.innerWidth / 2 - 320), y: 90 });
let dragStart: { px: number; py: number; ox: number; oy: number } | null = null;
function onDragDown(ev: PointerEvent) {
  dragStart = { px: ev.clientX, py: ev.clientY, ox: geom.x, oy: geom.y };
  window.addEventListener('pointermove', onDragMove);
  window.addEventListener('pointerup', onDragUp);
}
function onDragMove(ev: PointerEvent) {
  if (!dragStart) return;
  geom.x = Math.max(0, Math.min(window.innerWidth - 120, dragStart.ox + (ev.clientX - dragStart.px)));
  geom.y = Math.max(0, Math.min(window.innerHeight - 60, dragStart.oy + (ev.clientY - dragStart.py)));
}
function onDragUp() {
  dragStart = null;
  window.removeEventListener('pointermove', onDragMove);
  window.removeEventListener('pointerup', onDragUp);
}
</script>

<template>
  <div class="map-popup" :style="{ left: geom.x + 'px', top: geom.y + 'px' }" role="dialog">
    <header class="map-popup__header" @pointerdown="onDragDown">
      <span class="map-popup__title">{{ title }}</span>
      <span class="map-popup__actions">
        <button type="button" class="map-popup__btn" @pointerdown.stop @click="emit('discuss')">
          Refine / discuss
        </button>
        <button type="button" class="map-popup__close" aria-label="Close" @pointerdown.stop @click="emit('close')">
          ✕
        </button>
      </span>
    </header>
    <div class="map-popup__body">
      <p v-if="loading" class="map-popup__muted">Loading…</p>
      <p v-else-if="error" class="map-popup__muted">Couldn't load this spec.</p>
      <div v-else class="prose-content" v-html="rendered"></div>
    </div>
  </div>
</template>

<style scoped>
.map-popup {
  position: fixed;
  z-index: 50;
  width: 640px;
  height: 560px;
  min-width: 320px;
  min-height: 200px;
  max-width: 96vw;
  max-height: 92vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-card, #fff);
  border: 1px solid var(--rule-2, #c7c0af);
  border-radius: var(--r-lg, 10px);
  box-shadow: var(--sh-4, 0 12px 40px rgba(0, 0, 0, 0.22));
  overflow: hidden; /* required for the resize handle below */
  resize: both; /* native bottom-right resize grip */
}
.map-popup__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 10px 8px 14px;
  background: var(--bg-elevated, #faf8f3);
  border-bottom: 1px solid var(--rule, #d9d3c5);
  cursor: grab;
  user-select: none;
  flex: 0 0 auto;
}
.map-popup__title {
  font-weight: 600;
  font-size: var(--fs-md, 13px);
  color: var(--ink, #1b1916);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.map-popup__actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
}
.map-popup__btn {
  border: 1px solid var(--rule, #d9d3c5);
  background: var(--bg-card, #fff);
  color: var(--ink-2, #4c4842);
  cursor: pointer;
  font-size: var(--fs-10, 11px);
  padding: 3px 8px;
  border-radius: var(--r-sm, 4px);
}
.map-popup__btn:hover {
  background: var(--bg-hover, rgba(31, 29, 26, 0.045));
}
.map-popup__close {
  border: none;
  background: transparent;
  color: var(--ink-3, #6b6760);
  cursor: pointer;
  font-size: 13px;
  padding: 2px 6px;
  border-radius: var(--r-sm, 4px);
}
.map-popup__close:hover {
  background: var(--bg-hover, rgba(31, 29, 26, 0.045));
  color: var(--ink, #1b1916);
}
.map-popup__body {
  flex: 1 1 auto;
  overflow: auto;
  padding: 14px 18px;
}
.map-popup__muted {
  color: var(--ink-3, #6b6760);
  font-size: var(--fs-md, 13px);
}
</style>
