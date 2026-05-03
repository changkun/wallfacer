<script setup lang="ts">
import { useHead } from '@unhead/vue';
import DefaultLayout from '../layouts/DefaultLayout.vue';
import { useT } from '../i18n';
import { useReveal } from '../composables/useReveal';

const t = useT();
useReveal();
useHead({ title: 'Docs — Wallfacer', meta: [{ name: 'description', content: 'Everything you need to get started and go deep with Wallfacer.' }] });
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

      <section class="section">
        <div class="section-container">
          <div class="cap-grid">
            <router-link to="/docs/getting-started" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.getting-started')"></h3>
              <p v-html="t('wf.docs.getting-started.desc')"></p>
            </router-link>
            <router-link to="/docs/autonomy-spectrum" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.autonomy')"></h3>
              <p v-html="t('wf.docs.autonomy.desc')"></p>
            </router-link>
            <router-link to="/docs/exploring-ideas" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.exploring')"></h3>
              <p v-html="t('wf.docs.exploring.desc')"></p>
            </router-link>
            <router-link to="/docs/designing-specs" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.specs')"></h3>
              <p v-html="t('wf.docs.specs.desc')"></p>
            </router-link>
            <router-link to="/docs/board-and-tasks" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.board')"></h3>
              <p v-html="t('wf.docs.board.desc')"></p>
            </router-link>
            <router-link to="/docs/automation" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.automation')"></h3>
              <p v-html="t('wf.docs.automation.desc')"></p>
            </router-link>
            <router-link to="/docs/oversight-and-analytics" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.oversight')"></h3>
              <p v-html="t('wf.docs.oversight.desc')"></p>
            </router-link>
            <router-link to="/docs/workspaces" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.workspaces')"></h3>
              <p v-html="t('wf.docs.workspaces.desc')"></p>
            </router-link>
            <router-link to="/docs/refinement-and-ideation" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.refinement')"></h3>
              <p v-html="t('wf.docs.refinement.desc')"></p>
            </router-link>
            <router-link to="/docs/circuit-breakers" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.circuit')"></h3>
              <p v-html="t('wf.docs.circuit.desc')"></p>
            </router-link>
            <router-link to="/docs/configuration" class="cap-item cap-link">
              <h3 v-html="t('wf.docs.config')"></h3>
              <p v-html="t('wf.docs.config.desc')"></p>
            </router-link>
            <router-link to="/docs/usage" class="cap-item cap-link">
              <h3>Usage Guide</h3>
              <p>Day-to-day workflows, keyboard shortcuts, and tips.</p>
            </router-link>
            <router-link to="/docs/agents-and-flows" class="cap-item cap-link">
              <h3>Agents &amp; Flows</h3>
              <p>Configure agent backends and multi-step execution flows.</p>
            </router-link>
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
