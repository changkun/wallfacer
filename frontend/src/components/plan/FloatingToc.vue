<script setup lang="ts">
import { ref, onUnmounted, watch, nextTick } from 'vue';

const props = defineProps<{
  // Element whose `<h1>`/`<h2>`/`<h3>`/`<h4>` are surfaced as the TOC.
  bodyEl: HTMLElement | null;
  // Bumped by the parent each time the rendered HTML changes so we re-scan.
  contentKey: string;
}>();

const emit = defineEmits<{
  // True while the panel occupies the top-right corner. The pinned TOC never
  // scrolls, so the parent reserves a right gutter to keep body text from
  // sliding under it. False when there are no headings or it is collapsed to
  // the reveal tab, so the parent can reclaim the width.
  (e: 'reserve', value: boolean): void;
}>();

interface Entry {
  id: string;
  text: string;
  level: number;
}

const entries = ref<Entry[]>([]);
const activeId = ref<string>('');
let observer: IntersectionObserver | null = null;
let idSeq = 0;

// Collapsed state persists so hiding the TOC sticks across specs and reloads.
const COLLAPSE_KEY = 'wallfacer-spec-toc-collapsed';
const collapsed = ref(loadCollapsed());
function loadCollapsed(): boolean {
  try { return localStorage.getItem(COLLAPSE_KEY) === '1'; } catch { return false; }
}
function setCollapsed(v: boolean) {
  collapsed.value = v;
  try { localStorage.setItem(COLLAPSE_KEY, v ? '1' : '0'); } catch { /* ignore */ }
}

function rebuild() {
  observer?.disconnect();
  observer = null;
  entries.value = [];
  activeId.value = '';
  if (!props.bodyEl) return;
  const headings = Array.from(
    props.bodyEl.querySelectorAll('h1, h2, h3, h4'),
  ) as HTMLElement[];
  if (headings.length === 0) return;

  const built: Entry[] = [];
  for (const h of headings) {
    if (!h.id) {
      h.id = 'spec-toc-' + (++idSeq);
    }
    built.push({
      id: h.id,
      text: h.textContent ?? '',
      level: Number.parseInt(h.tagName.slice(1), 10) || 1,
    });
  }
  entries.value = built;

  observer = new IntersectionObserver(
    (items) => {
      for (const item of items) {
        if (item.isIntersecting) {
          activeId.value = (item.target as HTMLElement).id;
        }
      }
    },
    { rootMargin: '-80px 0px -55% 0px', threshold: 0 },
  );
  for (const h of headings) observer.observe(h);
}

watch(
  () => [props.bodyEl, props.contentKey],
  () => {
    void nextTick(rebuild);
  },
  { immediate: true },
);

// Mirror the panel's footprint to the parent so it can reserve the gutter.
watch(
  () => entries.value.length > 0 && !collapsed.value,
  (occupies) => emit('reserve', occupies),
  { immediate: true },
);

onUnmounted(() => observer?.disconnect());

function jumpTo(ev: Event, id: string) {
  ev.preventDefault();
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
}
</script>

<template>
  <template v-if="entries.length > 0">
    <!-- Collapsed: a small tab that brings the TOC back. -->
    <button
      v-if="collapsed"
      type="button"
      class="floating-toc__reveal"
      title="Show contents"
      aria-label="Show contents"
      @click="setCollapsed(false)"
    >☰</button>
    <nav v-else class="floating-toc" aria-label="On this page">
      <div class="floating-toc__head">
        <span class="floating-toc__title">Contents</span>
        <button
          type="button"
          class="floating-toc__collapse"
          title="Hide contents"
          aria-label="Hide contents"
          @click="setCollapsed(true)"
        >×</button>
      </div>
      <a
        v-for="e in entries"
        :key="e.id"
        :href="'#' + e.id"
        class="floating-toc__entry"
        :class="['floating-toc__entry--l' + e.level, { 'floating-toc__entry--active': e.id === activeId }]"
        :title="e.text"
        @click="jumpTo($event, e.id)"
      >{{ e.text }}</a>
    </nav>
  </template>
</template>

<style scoped>
.floating-toc {
  position: absolute;
  top: 72px;
  right: 12px;
  width: 180px;
  max-height: calc(100% - 88px);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding: 6px 8px;
  font-size: 11px;
  color: var(--ink-3);
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: 4px;
  opacity: 0.85;
  pointer-events: auto;
  z-index: 2;
}

.floating-toc:hover { opacity: 1; }

.floating-toc__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}

.floating-toc__title {
  font-weight: 600;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--ink-3);
}

.floating-toc__collapse {
  border: none;
  background: transparent;
  color: var(--ink-3);
  cursor: pointer;
  font-size: 13px;
  line-height: 1;
  padding: 0 2px;
  border-radius: 3px;
}
.floating-toc__collapse:hover { color: var(--ink); background: var(--bg-hover); }

/* Collapsed tab: same top-right anchor as the panel, shrunk to an icon. */
.floating-toc__reveal {
  position: absolute;
  top: 72px;
  right: 12px;
  width: 26px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  color: var(--ink-3);
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: 4px;
  cursor: pointer;
  opacity: 0.85;
  z-index: 2;
}
.floating-toc__reveal:hover { opacity: 1; color: var(--ink); }

.floating-toc__entry {
  display: block;
  padding: 1px 0;
  color: var(--ink-3);
  text-decoration: none;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.4;
}

.floating-toc__entry--l1 { padding-left: 0; }
.floating-toc__entry--l2 { padding-left: 8px; }
.floating-toc__entry--l3 { padding-left: 16px; }
.floating-toc__entry--l4 { padding-left: 24px; }

.floating-toc__entry:hover { color: var(--ink); }

.floating-toc__entry--active {
  color: var(--ink);
  font-weight: 500;
}

@media (max-width: 1100px) {
  .floating-toc,
  .floating-toc__reveal { display: none; }
}
</style>
