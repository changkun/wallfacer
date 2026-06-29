<script setup lang="ts">
// GitHub connect/disconnect settings tab (spec: github-integration component 1).
// Owns the Disconnected / Connecting / Connected / token-expired states from the
// umbrella state matrix. Connect opens the brokered "Latere AI" install + grant
// flow; until the ../auth broker ships, the endpoint reports it unavailable and
// the tab surfaces that rather than erroring.
import { onMounted, ref } from 'vue';
import { useGithubStore } from '../../stores/github';

const github = useGithubStore();
const connecting = ref(false);

onMounted(() => github.fetchStatus());

async function onConnect() {
  connecting.value = true;
  try {
    await github.connect();
  } finally {
    connecting.value = false;
  }
}
</script>

<template>
  <div class="gh-settings">
    <h2 class="gh-h">GitHub</h2>

    <!-- Disconnected -->
    <div v-if="!github.connected" class="gh-block">
      <p class="gh-muted">
        Connect a GitHub App installation to browse and open pull requests and
        issues from inside the workspace.
      </p>
      <button
        class="gh-btn gh-btn--primary"
        :disabled="connecting || !github.status.can_connect"
        @click="onConnect"
      >
        {{ connecting ? 'Connecting…' : 'Connect GitHub' }}
      </button>
      <p v-if="!github.status.can_connect" class="gh-hint">
        The GitHub connect flow is not available in this deployment yet.
      </p>
      <p v-if="github.error" class="gh-error">{{ github.error }}</p>
    </div>

    <!-- Connected -->
    <div v-else class="gh-block">
      <div class="gh-row"><span class="gh-label">Signed in as</span><span>@{{ github.status.login }}</span></div>
      <div v-if="github.status.account" class="gh-row">
        <span class="gh-label">Installed on</span><span>{{ github.status.account }}</span>
      </div>
      <div v-if="github.status.permissions?.length" class="gh-row">
        <span class="gh-label">Permissions</span><span>{{ github.status.permissions.join(', ') }}</span>
      </div>
      <div class="gh-actions">
        <button class="gh-btn" @click="github.disconnect()">Disconnect</button>
      </div>
      <p v-if="github.error" class="gh-error">{{ github.error }}</p>
    </div>
  </div>
</template>

<style scoped>
.gh-settings { max-width: 36rem; }
.gh-h { font-size: 1.05rem; font-weight: 600; margin: 0 0 0.75rem; }
.gh-block { display: flex; flex-direction: column; gap: 0.6rem; }
.gh-muted { color: var(--text-muted, #888); margin: 0; }
.gh-row { display: flex; gap: 0.75rem; }
.gh-label { width: 8rem; color: var(--text-muted, #888); }
.gh-actions { margin-top: 0.5rem; }
.gh-btn {
  align-self: flex-start;
  padding: 0.4rem 0.9rem;
  border: 1px solid var(--border, #444);
  border-radius: 6px;
  background: transparent;
  color: inherit;
  cursor: pointer;
}
.gh-btn--primary { background: var(--accent, #2563eb); color: #fff; border-color: transparent; }
.gh-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.gh-hint { color: var(--text-muted, #888); font-size: 0.85rem; margin: 0; }
.gh-error { color: var(--danger, #dc2626); font-size: 0.85rem; margin: 0; }
</style>
