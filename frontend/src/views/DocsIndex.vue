<script setup lang="ts">
import { computed } from 'vue';
import { useHead } from '@unhead/vue';
import DefaultLayout from '../layouts/DefaultLayout.vue';
import { docIndex } from '../data/docs';
import { useT } from '../i18n';
import { useReveal } from '../composables/useReveal';

const t = useT();
useReveal();
useHead({ title: 'Wallfacer Docs', meta: [{ name: 'description', content: 'Everything you need to get started and go deep with Wallfacer.' }] });

// Cards derive from the generated docIndex (single source: the Reading
// Order in docs/guide/usage.md), grouped by section in reading order.
const sections = computed(() => {
  const order: string[] = [];
  const bySection = new Map<string, typeof docIndex>();
  for (const entry of docIndex) {
    if (!bySection.has(entry.section)) {
      order.push(entry.section);
      bySection.set(entry.section, []);
    }
    bySection.get(entry.section)!.push(entry);
  }
  return order.map((name) => ({ name, entries: bySection.get(name)! }));
});
</script>

<template>
  <DefaultLayout>
    <div class="wallfacer-page">
      <section class="hero hero-compact">
        <div class="hero-container">
          <h1 class="hero-title" v-html="t('wf.docs.title')"></h1>
          <p class="hero-sub" v-html="t('wf.docs.sub')"></p>
        </div>
      </section>

      <section v-for="section in sections" :key="section.name" class="section">
        <div class="section-container">
          <span class="section-label">{{ section.name }}</span>
          <div class="cap-grid">
            <router-link
              v-for="entry in section.entries"
              :key="entry.slug"
              :to="`/docs/${entry.slug}`"
              class="cap-item cap-link"
            >
              <h3>{{ entry.title }}</h3>
              <p>{{ entry.desc }}</p>
            </router-link>
          </div>
        </div>
      </section>

      <section class="section">
        <div class="section-container">
          <span class="section-label">More</span>
          <div class="cap-grid">
            <a href="https://github.com/changkun/wallfacer" class="cap-item cap-link" target="_blank" rel="noopener">
              <h3 v-html="t('wf.docs.github')"></h3>
              <p v-html="t('wf.docs.github.desc')"></p>
            </a>
          </div>
        </div>
      </section>
    </div>
  </DefaultLayout>
</template>
