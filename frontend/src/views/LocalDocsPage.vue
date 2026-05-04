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

const route = useRoute();
const router = useRouter();

const entries = ref<DocEntry[]>([]);
const loading = ref(true);
const error = ref<string | null>(null);
const articleEl = ref<HTMLElement | null>(null);

// Slug from path: /docs (no slug → first guide), /docs/:slug or /docs/:cat/:name
const activeSlug = computed<string>(() => {
  const m = route.path.match(/^\/docs\/(.+)$/);
  if (m) return m[1];
  if (entries.value.length) return entries.value[0].slug;
  return 'guide/usage';
});

const activeEntry = computed(() => entries.value.find(e => e.slug === activeSlug.value));

// Group entries by category preserving server order.
const groups = computed(() => {
  const map = new Map<string, DocEntry[]>();
  for (const e of entries.value) {
    if (!map.has(e.category)) map.set(e.category, []);
    map.get(e.category)!.push(e);
  }
  return Array.from(map.entries()).map(([category, items]) => ({
    category,
    label: category === 'guide' ? 'User Manual' : category === 'internals' ? 'Internals' : category,
    items,
  }));
});

const articleHtml = ref('');
const articleLoading = ref(false);

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
  try {
    const headers: Record<string, string> = { Accept: 'text/markdown' };
    const key = window.__WALLFACER__?.serverApiKey;
    if (key) headers['Authorization'] = `Bearer ${key}`;
    const res = await fetch(`/api/docs/${encodeURI(slug)}`, { credentials: 'same-origin', headers });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const md = await res.text();
    articleHtml.value = renderMarkdown(stripFirstHeading(md));
    await nextTick();
    if (articleEl.value) articleEl.value.scrollTop = 0;
    rewriteLinks();
  } catch (e) {
    articleHtml.value = `<p class="docs-error">Failed to load: ${e instanceof Error ? e.message : String(e)}</p>`;
  } finally {
    articleLoading.value = false;
  }
}

function rewriteLinks() {
  if (!articleEl.value) return;
  const links = articleEl.value.querySelectorAll<HTMLAnchorElement>('a[href]');
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

let themeObserver: MutationObserver | null = null;
onMounted(() => {
  themeObserver = new MutationObserver(() => {
    // Re-render to refresh any theme-dependent inline styles in the markdown.
  });
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
});
onUnmounted(() => themeObserver?.disconnect());
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
                >{{ e.title }}</a>
              </li>
            </ul>
          </div>
        </nav>
      </aside>

      <main ref="articleEl" class="local-docs-article">
        <header v-if="activeEntry" class="local-docs-article-head">
          <span class="local-docs-article-eyebrow">{{ activeEntry.category === 'guide' ? 'User Manual' : 'Internals' }}</span>
          <h1>{{ activeEntry.title }}</h1>
        </header>
        <div v-if="articleLoading" class="local-docs-state">Loading…</div>
        <div v-else class="local-docs-body prose-content" v-html="articleHtml" />
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
  grid-template-columns: 240px 1fr;
  gap: 0;
  width: 100%;
  max-width: 1200px;
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
  display: block;
  padding: 6px 10px;
  font-size: 13px;
  color: var(--text-secondary);
  border-radius: 6px;
  text-decoration: none;
  transition: background 0.1s, color 0.1s;
  line-height: 1.35;
}
.local-docs-link:hover { background: var(--bg-hover, rgba(0,0,0,0.04)); color: var(--text); }
.local-docs-link.is-active {
  background: var(--bg-card);
  color: var(--accent);
  font-weight: 500;
  box-shadow: var(--shadow);
}

.local-docs-article {
  padding: 2rem 2.25rem 3rem;
  overflow-y: auto;
  height: 100%;
  background: var(--bg);
}
.local-docs-article-head {
  margin-bottom: 1.25rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--border);
}
.local-docs-article-eyebrow {
  display: block;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  color: var(--accent);
  text-transform: uppercase;
  letter-spacing: 0.15em;
  margin-bottom: 0.4rem;
}
.local-docs-article-head h1 {
  font-family: var(--font-sans);
  font-size: 28px;
  font-weight: 600;
  margin: 0;
  letter-spacing: -0.025em;
  line-height: 1.15;
  color: var(--text);
}

.local-docs-body {
  max-width: 760px;
  font-size: 14px;
  line-height: 1.7;
  color: var(--text);
}
.local-docs-state {
  padding: 1rem;
  font-size: 13px;
  color: var(--text-muted);
}
.local-docs-error { color: var(--err); }

/* Markdown body styling: keep it minimal and let prose-content carry the rest. */
.local-docs-body :deep(h2) {
  font-size: 19px;
  font-weight: 600;
  margin: 1.75rem 0 0.5rem;
  color: var(--text);
  letter-spacing: -0.015em;
}
.local-docs-body :deep(h3) {
  font-size: 15px;
  font-weight: 600;
  margin: 1.25rem 0 0.4rem;
  color: var(--text);
}
.local-docs-body :deep(p) { margin: 0 0 0.85rem; }
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
  font-size: 12.5px;
  background: var(--bg-raised);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 12px 14px;
  overflow-x: auto;
  line-height: 1.5;
  margin: 0 0 1rem;
}
.local-docs-body :deep(pre code) {
  background: none;
  border: none;
  padding: 0;
  font-size: inherit;
}
.local-docs-body :deep(blockquote) {
  margin: 0 0 1rem;
  padding: 0.5rem 0.875rem;
  border-left: 3px solid var(--accent);
  background: var(--accent-subtle);
  color: var(--text-secondary);
  border-radius: 0 8px 8px 0;
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

@media (max-width: 720px) {
  .local-docs-inner { grid-template-columns: 1fr; }
  .local-docs-nav { border-right: none; border-bottom: 1px solid var(--border); height: auto; max-height: 200px; }
  .local-docs-article { padding: 1.25rem 1rem 2rem; }
}
</style>
