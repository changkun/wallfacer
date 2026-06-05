<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted, nextTick } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { api } from '../api/client';
import { renderMarkdown, stripFirstHeading } from '../lib/markdown';

interface DocEntry {
  slug: string;
  title: string;
  category: string;
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

// Group entries by category preserving server order. Labels match the golden
// docs-mode vocabulary ('User Guide' / 'Technical Reference').
const groups = computed(() => {
  const map = new Map<string, DocEntry[]>();
  for (const e of entries.value) {
    if (!map.has(e.category)) map.set(e.category, []);
    map.get(e.category)!.push(e);
  }
  return Array.from(map.entries()).map(([category, items]) => ({
    category,
    label: category === 'guide' ? 'User Guide' : category === 'internals' ? 'Technical Reference' : category,
    items,
  }));
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
    const headers: Record<string, string> = { Accept: 'text/markdown' };
    const key = window.__WALLFACER__?.serverApiKey;
    if (key) headers['Authorization'] = `Bearer ${key}`;
    const res = await fetch(`/api/docs/${encodeURI(slug)}`, { credentials: 'same-origin', headers });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const md = await res.text();
    articleHtml.value = renderMarkdown(stripFirstHeading(md));
    // Reveal the body before querying it: the rendered markdown lives behind
    // v-else of articleLoading, so rewriteLinks/buildToc need it mounted.
    articleLoading.value = false;
    await nextTick();
    if (articleEl.value) articleEl.value.scrollTop = 0;
    rewriteLinks();
    buildToc();
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
        <div class="local-docs-eyebrow">Documentation</div>
        <div v-if="loading" class="local-docs-state">Loading…</div>
        <div v-else-if="error" class="local-docs-state local-docs-error">{{ error }}</div>
        <nav v-else>
          <div v-for="g in groups" :key="g.category" class="local-docs-group">
            <div class="local-docs-group-h">{{ g.label }}</div>
            <ul class="local-docs-list">
              <li v-for="e in g.items" :key="e.slug">
                <a
                  :href="`/docs/${e.slug}`"
                  class="local-docs-link"
                  :class="{ 'is-active': e.slug === activeSlug }"
                  @click.prevent="selectSlug(e.slug)"
                >
                  <span v-if="isIndexSlug(e.slug)" class="local-docs-glyph">&#9776;</span>
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
                class="local-docs-prevnext-link"
                @click.prevent="selectSlug(prevDoc.slug)"
              >&larr; {{ prevDoc.order }}. {{ prevDoc.title }}</a>
              <span v-else />
              <a
                v-if="nextDoc"
                :href="`/docs/${nextDoc.slug}`"
                class="local-docs-prevnext-link"
                @click.prevent="selectSlug(nextDoc.slug)"
              >{{ nextDoc.order }}. {{ nextDoc.title }} &rarr;</a>
            </nav>
          </template>
        </div>

        <aside v-if="tocItems.length" class="local-docs-toc" aria-label="On this page">
          <div class="local-docs-toc-title">Contents</div>
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
.local-docs-screen {
  flex: 1;
  background: var(--bg);
  overflow: hidden;
  display: flex;
  font-family: var(--font-sans);
  color: var(--text);
}
.local-docs-inner {
  display: grid;
  grid-template-columns: 200px 1fr;
  gap: 0;
  width: 100%;
  margin: 0 auto;
}

.local-docs-nav {
  border-right: 1px solid var(--border);
  background: var(--bg-sunken, var(--bg));
  padding: 1.25rem 1rem 1.5rem;
  overflow-y: auto;
  height: 100%;
}
.local-docs-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  color: var(--accent);
  text-transform: uppercase;
  letter-spacing: 0.15em;
  margin-bottom: 0.75rem;
  padding: 0 0.25rem;
}
.local-docs-group { margin-bottom: 1.25rem; }
.local-docs-group-h {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  padding: 0 0.5rem;
  margin-bottom: 0.25rem;
}
.local-docs-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 1px; }
.local-docs-link {
  display: flex;
  align-items: center;
  padding: 6px 10px;
  font-size: 13px;
  color: var(--text-secondary);
  border-radius: 6px;
  text-decoration: none;
  transition: background 0.1s, color 0.1s;
  line-height: 1.35;
}
.local-docs-link:hover { background: var(--bg-input); color: var(--text); }
.local-docs-link.is-active {
  background: var(--bg-input);
  color: var(--text);
  font-weight: 600;
}
.local-docs-badge {
  display: inline-block;
  width: 16px;
  height: 16px;
  line-height: 16px;
  text-align: center;
  border-radius: 50%;
  background: var(--bg-raised);
  color: var(--text-muted);
  font-size: 9px;
  font-weight: 700;
  margin-right: 6px;
  flex-shrink: 0;
}
.local-docs-glyph {
  font-size: 10px;
  margin-right: 6px;
  flex-shrink: 0;
  color: var(--text-muted);
}
.local-docs-link-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.local-docs-article {
  position: relative;
  padding: 2rem 2.25rem 5rem;
  overflow-y: auto;
  height: 100%;
  background: var(--bg);
}
/* Full-width reading surface. The nav handles wayfinding; the content fills
   the pane so long code blocks, tables, and diagrams aren't squeezed. */
.local-docs-wrap { max-width: none; }
/* Reserve room on the right for the floating TOC so prose doesn't run under it. */
.local-docs-wrap.has-toc { padding-right: 220px; }

.local-docs-article-head {
  margin-bottom: 0.5rem;
}
.local-docs-article-head h1 {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 400;
  font-size: 42px;
  line-height: 1.05;
  letter-spacing: -0.02em;
  margin: 0 0 8px;
  color: var(--ink);
}

.local-docs-body {
  max-width: none;
  font-size: 15px;
  line-height: 1.72;
  color: var(--ink);
}
.local-docs-state {
  padding: 1rem;
  font-size: 13px;
  color: var(--text-muted);
}
.local-docs-error { color: var(--err); }

/* Lead paragraph + drop cap on the first body paragraph (the title lives in
   a separate header, so there is no adjacent h1 to key off). */
.local-docs-body :deep(> p:first-of-type) {
  font-size: 17px;
  color: var(--ink-2);
  line-height: 1.6;
  margin-bottom: 28px;
  max-width: 620px;
}
.local-docs-body :deep(> p:first-of-type::first-letter) {
  font-family: var(--font-serif);
  font-style: italic;
  font-size: 3em;
  line-height: 0.9;
  float: left;
  padding: 4px 10px 0 0;
  color: var(--accent);
}

.local-docs-body :deep(h2) {
  font-size: 22px;
  font-weight: 600;
  margin: 34px 0 8px;
  color: var(--ink);
  letter-spacing: -0.005em;
}
.local-docs-body :deep(h3) {
  font-size: 16px;
  font-weight: 600;
  margin: 24px 0 8px;
  color: var(--ink);
}
.local-docs-body :deep(p) { margin: 0 0 14px; }
.local-docs-body :deep(ul), .local-docs-body :deep(ol) {
  padding-left: 1.25rem;
  margin: 0 0 0.85rem;
}
.local-docs-body :deep(li) { margin-bottom: 0.25rem; }
.local-docs-body :deep(a) {
  color: var(--accent);
  text-decoration: none;
  border-bottom: 1px solid var(--accent-glow);
}
.local-docs-body :deep(a:hover) { border-bottom-color: var(--accent); }
.local-docs-body :deep(code) {
  font-family: var(--font-mono);
  font-size: 0.92em;
  background: var(--bg-raised);
  padding: 1px 5px;
  border-radius: 4px;
  border: 1px solid var(--border);
}
.local-docs-body :deep(pre) {
  font-family: var(--font-mono);
  font-size: 13px;
  background: var(--bg-raised);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 16px;
  overflow-x: auto;
  line-height: 1.6;
  margin: 0 0 1rem;
}
.local-docs-body :deep(pre code) {
  background: none;
  border: none;
  padding: 0;
  font-size: inherit;
}
.local-docs-body :deep(blockquote) {
  margin: 0 0 16px;
  padding: 4px 0 4px 16px;
  border-left: 2px solid var(--accent);
  color: var(--ink-2);
  font-size: 15px;
  line-height: 1.6;
}
.local-docs-body :deep(table) {
  border-collapse: collapse;
  width: 100%;
  font-size: 13px;
  margin: 0 0 1rem;
}
.local-docs-body :deep(th), .local-docs-body :deep(td) {
  padding: 6px 10px;
  border: 1px solid var(--border);
  text-align: left;
  vertical-align: top;
}
.local-docs-body :deep(th) {
  background: var(--bg-raised);
  font-weight: 600;
}
.local-docs-body :deep(hr) {
  border: 0;
  border-top: 1px solid var(--border);
  margin: 1.5rem 0;
}

/* Prev/next ordered-doc navigation. */
.local-docs-prevnext {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 32px;
  padding-top: 16px;
  border-top: 1px solid var(--border);
  font-size: 13px;
}
.local-docs-prevnext-link {
  color: var(--accent);
  text-decoration: none;
}
.local-docs-prevnext-link:hover { text-decoration: underline; }

/* Floating table of contents, anchored top-right inside the content pane. */
.local-docs-toc {
  position: absolute;
  top: 2rem;
  right: 2.25rem;
  width: 180px;
  max-height: calc(100% - 4rem);
  overflow-y: auto;
  padding-left: 14px;
  border-left: 1px solid var(--border);
}
.local-docs-toc-title {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
  margin-bottom: 8px;
}
.local-docs-toc-link {
  display: block;
  font-size: 11.5px;
  line-height: 1.4;
  color: var(--text-muted);
  text-decoration: none;
  padding: 3px 0;
}
.local-docs-toc-link.is-h3 { padding-left: 10px; }
.local-docs-toc-link:hover { color: var(--text); }
.local-docs-toc-link.is-active { color: var(--accent); }

@media (max-width: 720px) {
  .local-docs-inner { grid-template-columns: 1fr; }
  .local-docs-nav { border-right: none; border-bottom: 1px solid var(--border); height: auto; max-height: 200px; }
  .local-docs-article { padding: 1.25rem 1rem 2rem; }
  .local-docs-toc { display: none; }
}
</style>
