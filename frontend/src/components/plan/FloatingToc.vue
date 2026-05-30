<script setup lang="ts">
import { ref, onUnmounted, watch, nextTick } from 'vue';

const props = defineProps<{
  // Element whose `<h1>`/`<h2>`/`<h3>`/`<h4>` are surfaced as the TOC.
  bodyEl: HTMLElement | null;
  // Bumped by the parent each time the rendered HTML changes so we re-scan.
  contentKey: string;
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

onUnmounted(() => observer?.disconnect());

function jumpTo(ev: Event, id: string) {
  ev.preventDefault();
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
}
</script>

<template>
  <nav v-if="entries.length > 0" class="floating-toc" aria-label="On this page">
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

<style scoped>
.floating-toc {
  position: absolute;
  top: 80px;
  right: 16px;
  width: 200px;
  max-height: calc(100% - 100px);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding: 8px 6px;
  font-size: 11px;
  color: var(--ink-3);
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  pointer-events: auto;
  z-index: 1;
}

.floating-toc__entry {
  padding: 3px 6px;
  border-left: 2px solid transparent;
  color: var(--ink-3);
  text-decoration: none;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.4;
}

.floating-toc__entry--l1 { padding-left: 6px; font-weight: 500; }
.floating-toc__entry--l2 { padding-left: 14px; }
.floating-toc__entry--l3 { padding-left: 22px; }
.floating-toc__entry--l4 { padding-left: 30px; }

.floating-toc__entry:hover { color: var(--ink); }

.floating-toc__entry--active {
  color: var(--accent);
  border-left-color: var(--accent);
}

@media (max-width: 1100px) {
  .floating-toc { display: none; }
}
</style>
