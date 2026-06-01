<script setup lang="ts">
import { computed, ref } from 'vue';
import { useHead } from '@unhead/vue';
import DefaultLayout from '../layouts/DefaultLayout.vue';
import { useT } from '../i18n';
import { useMermaid } from '../composables/useMermaid';
import { useToc } from '../composables/useToc';
import { renderMarkdown, stripFirstHeading } from '../lib/markdown';
import { docIndex } from '../data/docs';

const props = defineProps<{ slug: string }>();
const t = useT();

const entry = computed(() => docIndex.find(e => e.slug === props.slug));
const sidebarCollapsed = ref(false);

// Ordered prev/next within the doc list (docIndex is the canonical order).
const docPos = computed(() => docIndex.findIndex(e => e.slug === props.slug));
const prevDoc = computed(() => (docPos.value > 0 ? docIndex[docPos.value - 1] : null));
const nextDoc = computed(() => (docPos.value >= 0 && docPos.value < docIndex.length - 1 ? docIndex[docPos.value + 1] : null));

// Floating table of contents from the rendered article's h2 headings.
const { entries: tocEntries, activeId } = useToc('.docs-article');

const docFiles = import.meta.glob('../../../docs/guide/*.md', { query: '?raw', import: 'default', eager: true }) as Record<string, string>;

const html = computed(() => {
  if (!entry.value) return '';
  const key = Object.keys(docFiles).find(k => k.includes(`/${props.slug}.md`));
  if (!key) return '';
  let rendered = renderMarkdown(stripFirstHeading(docFiles[key]));
  rendered = rendered.replace(/href="([^"]*\.md)"/g, (_match: string, href: string) => {
    if (href.startsWith('http')) return `href="${href}"`;
    if (href.includes('../')) {
      const clean = href.replace(/\.\.\//g, '');
      return `href="https://github.com/changkun/wallfacer/blob/main/docs/${clean}" target="_blank" rel="noopener"`;
    }
    const s = href.replace(/\.md$/, '');
    return `href="/docs/${s}"`;
  });
  return rendered;
});

const articleHtml = computed(() => {
  if (!entry.value) return '';
  return `<h1>${entry.value.title}</h1>${html.value}`;
});

useMermaid('.docs-article', html);

useHead(computed(() => ({
  title: entry.value ? `${entry.value.title} — Wallfacer Docs` : '404',
})));
</script>

<template>
  <DefaultLayout>
    <div class="wallfacer-page">
      <template v-if="entry">
        <div class="docs-layout">
          <aside class="docs-sidebar" :class="{ collapsed: sidebarCollapsed }">
            <div class="docs-sidebar-header">
              <router-link to="/" class="docs-back" v-html="t('wf.docs.back')" />
              <button class="docs-sidebar-toggle" @click="sidebarCollapsed = !sidebarCollapsed" aria-label="Toggle navigation">
                <svg class="docs-toggle-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9"/></svg>
              </button>
            </div>
            <nav class="docs-nav">
              <router-link
                v-for="doc in docIndex"
                :key="doc.slug"
                :to="`/docs/${doc.slug}`"
                class="docs-nav-link"
                :class="{ active: doc.slug === slug }"
              >{{ doc.title }}</router-link>
            </nav>
          </aside>
          <main class="docs-main">
            <article class="docs-article prose" v-html="articleHtml"></article>
            <!-- Prev/next ordered-doc navigation. -->
            <nav v-if="prevDoc || nextDoc" class="docs-prevnext">
              <router-link v-if="prevDoc" :to="`/docs/${prevDoc.slug}`" class="docs-prevnext__link docs-prevnext__prev">
                <span class="docs-prevnext__dir">← Previous</span>
                <span class="docs-prevnext__title">{{ prevDoc.title }}</span>
              </router-link>
              <span v-else />
              <router-link v-if="nextDoc" :to="`/docs/${nextDoc.slug}`" class="docs-prevnext__link docs-prevnext__next">
                <span class="docs-prevnext__dir">Next →</span>
                <span class="docs-prevnext__title">{{ nextDoc.title }}</span>
              </router-link>
            </nav>
          </main>
          <!-- Floating table of contents for the current doc. -->
          <aside v-if="tocEntries.length" class="docs-toc">
            <div class="docs-toc__label">On this page</div>
            <a
              v-for="e in tocEntries"
              :key="e.id"
              :href="`#${e.id}`"
              class="docs-toc__link"
              :class="{ active: e.id === activeId }"
            >{{ e.text }}</a>
          </aside>
        </div>
      </template>
      <template v-else>
        <div class="section" style="text-align:center;padding-top:120px;">
          <h1>404</h1>
          <p style="margin-top:16px;color:var(--text-secondary)">Doc page not found.</p>
          <p style="margin-top:24px"><router-link to="/docs" style="text-decoration:underline">Back to docs</router-link></p>
        </div>
      </template>
    </div>
  </DefaultLayout>
</template>
