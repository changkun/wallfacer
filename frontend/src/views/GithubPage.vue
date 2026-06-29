<script setup lang="ts">
// The /github workspace page (spec: github-integration component 3). Composes
// the repo selector (component 2), the PRs/Issues tabs, and the master-detail
// list/detail. Owns the Disconnected call-to-action, Loading, Empty, and Error
// states from the umbrella matrix; connect itself lives in the Settings tab.
import { onMounted } from 'vue';
import { useGithubStore, type GithubState } from '../stores/github';
import RepoPicker from '../components/github/RepoPicker.vue';

const github = useGithubStore();

onMounted(async () => {
  await github.fetchStatus();
});

const states: GithubState[] = ['open', 'closed', 'all'];
</script>

<template>
  <div class="gh-page">
    <!-- Disconnected: link to the Settings connect flow -->
    <div v-if="!github.connected" class="gh-empty">
      <h2>GitHub not connected</h2>
      <p>Connect GitHub to browse pull requests and issues.</p>
      <RouterLink class="gh-link" to="/settings?tab=github">Open GitHub settings →</RouterLink>
    </div>

    <template v-else>
      <header class="gh-header">
        <RepoPicker />
        <button v-if="github.hasRepo" class="gh-refresh" title="Refresh" @click="github.refresh()">↻</button>
      </header>

      <!-- No repo selected: the picker renders its first-run state above -->
      <template v-if="github.hasRepo">
        <nav class="gh-tabs">
          <button :class="{ active: github.tab === 'pulls' }" @click="github.setTab('pulls')">Pull Requests</button>
          <button :class="{ active: github.tab === 'issues' }" @click="github.setTab('issues')">Issues</button>
          <span class="gh-spacer" />
          <select :value="github.stateFilter" class="gh-state" @change="github.setStateFilter(($event.target as HTMLSelectElement).value as GithubState)">
            <option v-for="s in states" :key="s" :value="s">{{ s }}</option>
          </select>
        </nav>

        <div class="gh-body">
          <!-- List pane (master) -->
          <ul class="gh-list">
            <li v-if="github.loading" class="gh-muted">Loading…</li>
            <template v-else-if="github.tab === 'pulls'">
              <li v-if="github.pulls.length === 0" class="gh-muted">No {{ github.stateFilter }} pull requests</li>
              <li v-for="p in github.pulls" :key="p.number">
                <button class="gh-row" @click="github.openDetail(p.number)">
                  <span class="gh-num">#{{ p.number }}</span>
                  <span class="gh-rowtitle">{{ p.title }}</span>
                  <span class="gh-by">@{{ p.author }}</span>
                </button>
              </li>
            </template>
            <template v-else>
              <li v-if="github.issues.length === 0" class="gh-muted">No {{ github.stateFilter }} issues</li>
              <li v-for="i in github.issues" :key="i.number">
                <button class="gh-row" @click="github.openDetail(i.number)">
                  <span class="gh-num">#{{ i.number }}</span>
                  <span class="gh-rowtitle">{{ i.title }}</span>
                  <span class="gh-by">@{{ i.author }}</span>
                </button>
              </li>
            </template>
          </ul>

          <!-- Detail pane -->
          <section class="gh-detail">
            <div v-if="!github.detail" class="gh-muted gh-detail-empty">Select an item to view details.</div>
            <article v-else>
              <h3 class="gh-detail-title">
                <span class="gh-num">#{{ github.detail.number }}</span> {{ github.detail.title }}
              </h3>
              <p class="gh-detail-meta">@{{ github.detail.author }} · {{ github.detail.state }}</p>
              <p v-if="github.detail.body" class="gh-detail-body">{{ github.detail.body }}</p>
              <a v-if="github.detail.html_url" class="gh-link" :href="github.detail.html_url" target="_blank" rel="noopener">Open on GitHub ↗</a>
              <div v-if="github.detail.comments?.length" class="gh-comments">
                <div v-for="(c, idx) in github.detail.comments" :key="idx" class="gh-comment">
                  <span class="gh-by">@{{ c.author }}</span>
                  <p>{{ c.body }}</p>
                </div>
              </div>
            </article>
          </section>
        </div>
      </template>

      <p v-if="github.error" class="gh-error">{{ github.error }}</p>
    </template>
  </div>
</template>

<style scoped>
.gh-page { display: flex; flex-direction: column; height: 100%; padding: 1rem; gap: 0.75rem; }
.gh-empty { max-width: 30rem; margin: 4rem auto; text-align: center; }
.gh-header { display: flex; align-items: center; gap: 0.75rem; }
.gh-refresh { margin-left: auto; background: transparent; border: 1px solid var(--border, #444); border-radius: 6px; cursor: pointer; color: inherit; padding: 0.2rem 0.5rem; }
.gh-tabs { display: flex; align-items: center; gap: 0.5rem; border-bottom: 1px solid var(--border, #333); }
.gh-tabs button { background: transparent; border: none; color: var(--text-muted, #888); padding: 0.4rem 0.6rem; cursor: pointer; border-bottom: 2px solid transparent; }
.gh-tabs button.active { color: inherit; border-bottom-color: var(--accent, #2563eb); }
.gh-spacer { flex: 1; }
.gh-state { background: var(--surface, #161616); color: inherit; border: 1px solid var(--border, #444); border-radius: 6px; padding: 0.2rem 0.4rem; }
.gh-body { display: grid; grid-template-columns: minmax(16rem, 24rem) 1fr; gap: 1rem; flex: 1; min-height: 0; }
.gh-list { list-style: none; margin: 0; padding: 0; overflow: auto; border-right: 1px solid var(--border, #2a2a2a); }
.gh-row { display: flex; gap: 0.5rem; width: 100%; text-align: left; background: transparent; border: none; color: inherit; padding: 0.45rem 0.5rem; cursor: pointer; border-radius: 6px; }
.gh-row:hover { background: var(--hover, rgba(255, 255, 255, 0.06)); }
.gh-num { color: var(--text-muted, #888); }
.gh-rowtitle { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.gh-by { color: var(--text-muted, #888); font-size: 0.85rem; }
.gh-detail { overflow: auto; }
.gh-detail-empty { margin-top: 2rem; }
.gh-detail-title { margin: 0 0 0.25rem; }
.gh-detail-meta { color: var(--text-muted, #888); margin: 0 0 0.75rem; }
.gh-detail-body { white-space: pre-wrap; }
.gh-comments { margin-top: 1rem; display: flex; flex-direction: column; gap: 0.75rem; }
.gh-comment { border-top: 1px solid var(--border, #2a2a2a); padding-top: 0.5rem; }
.gh-comment p { white-space: pre-wrap; margin: 0.25rem 0 0; }
.gh-link { color: var(--accent, #3b82f6); }
.gh-muted { color: var(--text-muted, #888); padding: 0.5rem; }
.gh-error { color: var(--danger, #dc2626); font-size: 0.85rem; }
</style>
