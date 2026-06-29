<script setup lang="ts">
// Repo picker (spec: github-integration component 2). Renders as the centered
// first-run picker when no repo is selected, and as a compact header dropdown
// once one is. Lists installation-granted repos with a search filter.
import { computed, onMounted, ref } from 'vue';
import { useGithubStore } from '../../stores/github';

const github = useGithubStore();
const open = ref(false);
const query = ref('');

onMounted(() => {
  if (github.repos.length === 0) void github.fetchRepos();
});

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return github.repos;
  return github.repos.filter(r => r.full_name.toLowerCase().includes(q));
});

async function choose(fullName: string) {
  open.value = false;
  query.value = '';
  await github.selectRepo(fullName);
}
</script>

<template>
  <!-- Selected: compact header dropdown trigger -->
  <div v-if="github.hasRepo" class="rp-compact">
    <button class="rp-trigger" @click="open = !open">
      {{ github.selectedRepo?.full_name }} <span class="rp-caret">▾</span>
    </button>
    <div v-if="open" class="rp-menu">
      <input v-model="query" class="rp-search" placeholder="Search repositories…" />
      <ul class="rp-list">
        <li v-for="r in filtered" :key="r.full_name">
          <button class="rp-item" @click="choose(r.full_name)">
            <span class="rp-name">{{ r.full_name }}</span>
            <span class="rp-meta">{{ r.default_branch }}</span>
          </button>
        </li>
      </ul>
    </div>
  </div>

  <!-- No repo selected: centered first-run picker -->
  <div v-else class="rp-firstrun">
    <h3 class="rp-title">Choose a repository</h3>
    <input v-model="query" class="rp-search" placeholder="Search granted repositories…" />
    <p v-if="github.loading" class="rp-muted">Loading repositories…</p>
    <p v-else-if="github.repos.length === 0" class="rp-muted">
      No repositories granted to the installation yet.
    </p>
    <ul v-else class="rp-list rp-list--block">
      <li v-for="r in filtered" :key="r.full_name">
        <button class="rp-item" @click="choose(r.full_name)">
          <span class="rp-name">{{ r.full_name }}</span>
          <span class="rp-meta">{{ r.default_branch }}<span v-if="r.private"> · private</span></span>
        </button>
      </li>
    </ul>
    <p v-if="github.error" class="rp-error">{{ github.error }}</p>
  </div>
</template>

<style scoped>
.rp-compact { position: relative; }
.rp-trigger {
  padding: 0.3rem 0.7rem; border: 1px solid var(--border, #444);
  border-radius: 6px; background: transparent; color: inherit; cursor: pointer;
}
.rp-caret { opacity: 0.6; }
.rp-menu {
  position: absolute; top: 100%; left: 0; margin-top: 4px; z-index: 20;
  min-width: 22rem; background: var(--surface, #1c1c1c);
  border: 1px solid var(--border, #444); border-radius: 8px; padding: 0.5rem;
}
.rp-firstrun { max-width: 32rem; margin: 3rem auto; text-align: left; }
.rp-title { font-size: 1.1rem; font-weight: 600; margin: 0 0 0.75rem; }
.rp-search {
  width: 100%; padding: 0.45rem 0.6rem; margin-bottom: 0.5rem;
  border: 1px solid var(--border, #444); border-radius: 6px;
  background: var(--surface, #161616); color: inherit;
}
.rp-list { list-style: none; margin: 0; padding: 0; max-height: 18rem; overflow: auto; }
.rp-list--block { max-height: 24rem; }
.rp-item {
  display: flex; justify-content: space-between; width: 100%; gap: 1rem;
  padding: 0.4rem 0.55rem; border: none; border-radius: 6px;
  background: transparent; color: inherit; cursor: pointer; text-align: left;
}
.rp-item:hover { background: var(--hover, rgba(255, 255, 255, 0.06)); }
.rp-meta { color: var(--text-muted, #888); font-size: 0.85rem; }
.rp-muted { color: var(--text-muted, #888); }
.rp-error { color: var(--danger, #dc2626); font-size: 0.85rem; }
</style>
