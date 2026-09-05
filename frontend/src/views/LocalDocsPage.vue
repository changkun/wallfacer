<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted, nextTick } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { api, authHeaders } from '../api/client';
import { renderMarkdown, stripFirstHeading } from '../lib/markdown';
import { enhanceMermaid, watchThemeReinit } from '../lib/mermaidRender';

interface DocEntry {
  slug: string;
  title: string;
  category: string;
  section?: string;
  order: number;
}

interface TocItem {
  id: string;
  text: string;
  level: number;
}

const route = useRoute();
const router = useRouter();

const entries = ref<DocEntry[]>([]);
const loading = ref(true);
const error = ref<string | null>(null);
const articleEl = ref<HTMLElement | null>(null);
const bodyEl = ref<HTMLElement | null>(null);

// Slug from path: /docs (no slug → first guide), /docs/:slug or /docs/:cat/:name
const activeSlug = computed<string>(() => {
  const m = route.path.match(/^\/docs\/(.+)$/);
  if (m) return m[1];
  if (entries.value.length) return entries.value[0].slug;
  return 'guide/usage';
});

const activeEntry = computed(() => entries.value.find(e => e.slug === activeSlug.value));

// Index slugs render with a hamburger glyph instead of a step number.
const INDEX_SLUGS = new Set(['guide/usage', 'internals/internals']);
function isIndexSlug(slug: string) {
  return INDEX_SLUGS.has(slug);
}

// Group entries into sidebar sections preserving server order. Guide entries
// carry a `section` (Get Started / Use Wallfacer / Operate) derived from the
// reading-order headings; entries without one fall back to a category label.
const groups = computed(() => {
  const map = new Map<string, DocEntry[]>();
  for (const e of entries.value) {
    const label = e.section
      || (e.category === 'guide' ? 'User Guide' : e.category === 'internals' ? 'Technical Reference' : e.category);
    if (!map.has(label)) map.set(label, []);
    map.get(label)!.push(e);
  }
  return Array.from(map.entries()).map(([label, items]) => ({ label, items }));
});

// Prev/next within the active doc's category, ordered by entry.order and
// excluding the index page (matches the old _appendDocNav).
const orderedSiblings = computed(() => {
  const cat = activeSlug.value.startsWith('internals/') ? 'internals' : 'guide';
  return entries.value
    .filter(e => e.category === cat && e.order && !isIndexSlug(e.slug))
    .sort((a, b) => a.order - b.order);
});
const prevDoc = computed(() => {
  const idx = orderedSiblings.value.findIndex(e => e.slug === activeSlug.value);
  return idx > 0 ? orderedSiblings.value[idx - 1] : null;
});
const nextDoc = computed(() => {
  const idx = orderedSiblings.value.findIndex(e => e.slug === activeSlug.value);
  return idx >= 0 && idx < orderedSiblings.value.length - 1 ? orderedSiblings.value[idx + 1] : null;
});

const articleHtml = ref('');
const articleLoading = ref(false);

// Floating table of contents (h2/h3), with scroll-spy active state.
const tocItems = ref<TocItem[]>([]);
const activeTocId = ref('');
let tocObserver: IntersectionObserver | null = null;

function teardownToc() {
  tocObserver?.disconnect();
  tocObserver = null;
  tocItems.value = [];
  activeTocId.value = '';
}

function buildToc() {
  teardownToc();
  if (!bodyEl.value) return;
  const headings = Array.from(bodyEl.value.querySelectorAll<HTMLElement>('h2[id], h3[id]'));
  if (headings.length < 2) return;
  tocItems.value = headings.map(h => ({
    id: h.id,
    text: h.textContent || '',
    level: parseInt(h.tagName.slice(1), 10),
  }));
  tocObserver = new IntersectionObserver(
    items => {
      items.forEach(item => {
        if (item.isIntersecting) activeTocId.value = (item.target as HTMLElement).id;
      });
    },
    { rootMargin: '-80px 0px -60% 0px', threshold: 0 },
  );
  headings.forEach(h => tocObserver!.observe(h));
  activeTocId.value = headings[0].id;
}

function scrollToHeading(e: Event, id: string) {
  e.preventDefault();
  const el = bodyEl.value?.querySelector<HTMLElement>(`#${CSS.escape(id)}`);
  if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
  activeTocId.value = id;
}

async function loadIndex() {
  loading.value = true;
  error.value = null;
  try {
    entries.value = await api<DocEntry[]>('GET', '/api/docs');
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function loadDoc(slug: string) {
  articleLoading.value = true;
  articleHtml.value = '';
  teardownToc();
  try {
    const headers: Record<string, string> = { Accept: 'text/markdown', ...authHeaders() };
    const res = await fetch(`/api/docs/${encodeURI(slug)}`, { credentials: 'same-origin', headers });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const md = await res.text();
    // baseDir is the doc's category dir (e.g. "guide" from "guide/board-and-tasks")
    // so doc-relative image src resolves under /api/docs-asset/<baseDir>/.
    const baseDir = slug.includes('/') ? slug.slice(0, slug.lastIndexOf('/')) : '';
    articleHtml.value = renderMarkdown(stripFirstHeading(md), baseDir);
    // Reveal the body before querying it: the rendered markdown lives behind
    // v-else of articleLoading, so rewriteLinks/buildToc need it mounted.
    articleLoading.value = false;
    await nextTick();
    if (articleEl.value) articleEl.value.scrollTop = 0;
    rewriteLinks();
    buildToc();
    // Render ```mermaid fences (placeholders emitted by the markdown renderer)
    // into SVG. Idempotent and a no-op when the doc has no diagrams.
    void enhanceMermaid(bodyEl.value);
  } catch (e) {
    articleHtml.value = `<p class="docs-error">Failed to load: ${e instanceof Error ? e.message : String(e)}</p>`;
    articleLoading.value = false;
  }
}

function rewriteLinks() {
  if (!bodyEl.value) return;
  const links = bodyEl.value.querySelectorAll<HTMLAnchorElement>('a[href]');
  links.forEach(a => {
    const href = a.getAttribute('href') || '';
    if (!href || href.startsWith('http') || href.startsWith('#') || href.startsWith('mailto:')) return;
    if (href.endsWith('.md')) {
      // Map a relative .md link into the in-app router.
      const cur = activeSlug.value;
      const lastSlash = cur.lastIndexOf('/');
      const baseDir = lastSlash > 0 ? cur.slice(0, lastSlash) : '';
      let target = href.replace(/\.md$/, '');
      if (target.startsWith('./')) target = target.slice(2);
      while (target.startsWith('../')) {
        target = target.slice(3);
      }
      const resolved = target.includes('/') ? target : (baseDir ? `${baseDir}/${target}` : target);
      a.setAttribute('href', `/docs/${resolved}`);
      a.addEventListener('click', (e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        void router.push(`/docs/${resolved}`);
      });
    }
  });
}

function selectSlug(slug: string) {
  void router.push(`/docs/${slug}`);
}

onMounted(async () => {
  // Re-color mermaid diagrams when the theme toggles (idempotent global watcher).
  watchThemeReinit();
  await loadIndex();
  await loadDoc(activeSlug.value);
});

watch(() => route.path, async (p) => {
  if (!p.startsWith('/docs')) return;
  await loadDoc(activeSlug.value);
});

onUnmounted(() => teardownToc());
</script>

<template>
  <div class="local-docs-screen">
    <div class="local-docs-inner">
      <aside class="local-docs-nav" aria-label="Documentation index">
        <div class="eyebrow local-docs-eyebrow">Documentation</div>
        <div v-if="loading" class="local-docs-state">Loading…</div>
        <div v-else-if="error" class="local-docs-state local-docs-error">{{ error }}</div>
        <nav v-else>
          <div v-for="g in groups" :key="g.label" class="local-docs-group">
            <div class="eyebrow local-docs-group-h">{{ g.label }}</div>
            <ul class="local-docs-list">
              <li v-for="e in g.items" :key="e.slug">
                <a
                  :href="`/docs/${e.slug}`"
                  class="local-docs-link"
                  :class="{ 'is-active': e.slug === activeSlug }"
                  @click.prevent="selectSlug(e.slug)"
                >
                  <span v-if="isIndexSlug(e.slug)" class="local-docs-glyph" aria-hidden="true">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="7" x2="20" y2="7"></line><line x1="4" y1="12" x2="20" y2="12"></line><line x1="4" y1="17" x2="20" y2="17"></line></svg>
                  </span>
                  <span v-else-if="e.order" class="local-docs-badge">{{ e.order }}</span>
                  <span class="local-docs-link-text">{{ e.title }}</span>
                </a>
              </li>
            </ul>
          </div>
        </nav>
      </aside>

      <main ref="articleEl" class="local-docs-article">
        <div class="local-docs-wrap" :class="{ 'has-toc': tocItems.length }">
          <header v-if="activeEntry" class="local-docs-article-head">
            <h1>{{ activeEntry.title }}</h1>
          </header>
          <div v-if="articleLoading" class="local-docs-state">Loading…</div>
          <template v-else>
            <div ref="bodyEl" class="local-docs-body prose-content" v-html="articleHtml" />
            <nav v-if="prevDoc || nextDoc" class="local-docs-prevnext">
              <a
                v-if="prevDoc"
                :href="`/docs/${prevDoc.slug}`"
                class="link local-docs-prevnext-link"
                @click.prevent="selectSlug(prevDoc.slug)"
              >&larr; {{ prevDoc.order }}. {{ prevDoc.title }}</a>
              <span v-else />
              <a
                v-if="nextDoc"
                :href="`/docs/${nextDoc.slug}`"
                class="link local-docs-prevnext-link"
                @click.prevent="selectSlug(nextDoc.slug)"
              >{{ nextDoc.order }}. {{ nextDoc.title }} &rarr;</a>
            </nav>
          </template>
        </div>

        <aside v-if="tocItems.length" class="local-docs-toc" aria-label="On this page">
          <div class="eyebrow local-docs-toc-title">Contents</div>
          <a
            v-for="t in tocItems"
            :key="t.id"
            :href="`#${t.id}`"
            class="local-docs-toc-link"
            :class="[`is-h${t.level}`, { 'is-active': t.id === activeTocId }]"
            @click="scrollToHeading($event, t.id)"
          >{{ t.text }}</a>
        </aside>
      </main>
    </div>
  </div>
</template>

<style scoped>
/* The local docs: a 260px nav of tree rows on the sunk surface, then the
   reading column at 76ch as .prose-content, shared with the focused spec, with
   a contents rail on the right when the article has headings. */
.local-docs-screen {
  flex: 1;
  min-height: 0;
  background: var(--bg);
  overflow: hidden;
  display: flex;
  font-family: var(--font-sans);
  color: var(--ink);
}
.local-docs-inner {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  width: 100%;
  min-height: 0;
}

.local-docs-nav {
  border-right: 1px solid var(--rule);
  background: var(--bg-sunk);
  padding: 14px 8px 24px;
  overflow-y: auto;
  min-height: 0;
}
.local-docs-eyebrow {
  display: block;
  padding: 4px 10px 10px;
  color: var(--accent);
}
.local-docs-group {
  margin-bottom: 14px;
}
.local-docs-group-h {
  display: block;
  padding: 6px 10px 4px;
}
.local-docs-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.local-docs-link {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
  padding: 0 10px;
  border-radius: var(--r-row);
  font-size: var(--fs-base);
  font-weight: 500;
  line-height: 1.35;
  color: var(--ink-2);
  text-decoration: none;
  transition: background var(--dur-hover), color var(--dur-hover);
}
.local-docs-link:hover {
  background: color-mix(in srgb, var(--ink) 5%, transparent);
  color: var(--ink);
}
.local-docs-link.is-active {
  background: var(--bg-card);
  color: var(--ink);
  font-weight: 600;
  box-shadow: var(--sh-card);
}
.local-docs-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--tint-neutral);
  color: var(--ink-3);
  font: 600 var(--fs-9) / 1 var(--font-mono);
  flex-shrink: 0;
}
.local-docs-glyph {
  display: inline-flex;
  width: 18px;
  justify-content: center;
  flex-shrink: 0;
  color: var(--ink-3);
}
.local-docs-link-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.local-docs-article {
  position: relative;
  padding: 32px 40px 80px;
  overflow-y: auto;
  min-height: 0;
  background: var(--bg);
}
.local-docs-wrap {
  max-width: 76ch;
}
.local-docs-wrap.has-toc {
  margin-right: 220px;
}
.local-docs-article-head {
  margin-bottom: 8px;
}
.local-docs-article-head h1 {
  font-family: var(--font-display);
  font-weight: 600;
  font-size: 38px;
  line-height: 1.08;
  letter-spacing: -0.02em;
  margin: 0 0 12px;
  color: var(--ink);
}
.local-docs-state {
  padding: 16px 10px;
  font-size: var(--fs-base);
  color: var(--ink-3);
}
.local-docs-error {
  color: var(--err);
}

/* The lead paragraph reads larger, then the shared prose takes over. */
.local-docs-body :deep(> p:first-of-type) {
  font-size: var(--fs-lg);
  color: var(--ink-2);
  line-height: 1.6;
  margin-bottom: 24px;
}

/* Prev/next ordered-doc navigation. */
.local-docs-prevnext {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-top: 32px;
  padding-top: 16px;
  border-top: 1px solid var(--rule);
  font-size: var(--fs-base);
}

/* Contents rail, anchored top-right inside the article pane. */
.local-docs-toc {
  position: absolute;
  top: 36px;
  right: 40px;
  width: 180px;
  max-height: calc(100% - 72px);
  overflow-y: auto;
  padding-left: 14px;
  border-left: 1px solid var(--rule);
}
.local-docs-toc-title {
  display: block;
  margin-bottom: 8px;
}
.local-docs-toc-link {
  display: block;
  padding: 3px 0;
  font-size: var(--fs-10);
  line-height: 1.4;
  color: var(--ink-3);
  text-decoration: none;
  transition: color var(--dur-hover);
}
.local-docs-toc-link.is-h3 {
  padding-left: 10px;
}
.local-docs-toc-link:hover {
  color: var(--ink);
}
.local-docs-toc-link.is-active {
  color: var(--accent);
}

@media (max-width: 900px) {
  .local-docs-wrap.has-toc {
    margin-right: 0;
  }
  .local-docs-toc {
    display: none;
  }
}
@media (max-width: 720px) {
  .local-docs-inner {
    grid-template-columns: 1fr;
  }
  .local-docs-nav {
    border-right: none;
    border-bottom: 1px solid var(--rule);
    max-height: 200px;
  }
  .local-docs-article {
    padding: 20px 16px 40px;
  }
}
</style>
