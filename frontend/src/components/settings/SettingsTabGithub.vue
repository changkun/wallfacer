<script setup lang="ts">
// GitHub settings tab. wallfacer does not connect GitHub itself -- connections
// are managed centrally at auth.latere.ai (the connectors hub). This tab only
// reflects state derived from the latere.ai sign-in:
//   - not signed in       -> prompt to sign in via latere.ai
//   - signed in, connected -> show the borrowed connection + a manage link
//   - signed in, not connected -> link to connect GitHub at auth.latere.ai
import { computed, onMounted } from 'vue';
import { useGithubStore } from '../../stores/github';
import { useAuthStore } from '../../stores/auth';

const github = useGithubStore();
const auth = useAuthStore();

const signedIn = computed(() => !!auth.me);

onMounted(() => {
  if (signedIn.value) void github.fetchStatus();
});
</script>

<template>
  <div class="gh-settings">
    <h2 class="gh-h">GitHub</h2>

    <!-- Not signed in via latere.ai -->
    <div v-if="!signedIn" class="gh-block">
      <p class="gh-muted">
        Sign in via latere.ai to use GitHub in wallfacer. GitHub is connected
        once in your latere.ai account and shared across latere products.
      </p>
      <button class="gh-btn gh-btn--primary" @click="auth.login()">Sign in via latere.ai</button>
    </div>

    <!-- Signed in, GitHub connected (borrowed from latere.ai) -->
    <div v-else-if="github.connected" class="gh-block">
      <div class="gh-row"><span class="gh-label">Connected as</span><span>@{{ github.status.login }}</span></div>
      <p class="gh-muted">GitHub is connected through your latere.ai account.</p>
      <a class="gh-link" :href="github.manageUrl" target="_blank" rel="noopener">Manage connections at latere.ai ↗</a>
    </div>

    <!-- Signed in, GitHub not connected -->
    <div v-else class="gh-block">
      <p class="gh-muted">GitHub is not connected to your latere.ai account yet.</p>
      <a class="gh-link gh-link--btn" :href="github.manageUrl" target="_blank" rel="noopener">Connect GitHub at latere.ai ↗</a>
      <p v-if="github.error" class="gh-error">{{ github.error }}</p>
    </div>
  </div>
</template>

<style scoped>
.gh-settings { max-width: 36rem; }
.gh-h { font-size: 1.05rem; font-weight: 600; margin: 0 0 0.75rem; }
.gh-block { display: flex; flex-direction: column; align-items: flex-start; gap: 0.6rem; }
.gh-muted { color: var(--text-muted, #888); margin: 0; }
.gh-row { display: flex; gap: 0.75rem; }
.gh-label { width: 8rem; color: var(--text-muted, #888); }
.gh-btn {
  padding: 0.4rem 0.9rem;
  border: 1px solid var(--border, #444);
  border-radius: 6px;
  background: transparent;
  color: inherit;
  cursor: pointer;
}
.gh-btn--primary { background: var(--accent, #2563eb); color: #fff; border-color: transparent; }
.gh-link { color: var(--accent, #3b82f6); text-decoration: none; }
.gh-link--btn {
  padding: 0.4rem 0.9rem;
  border: 1px solid var(--border, #444);
  border-radius: 6px;
}
.gh-error { color: var(--danger, #dc2626); font-size: 0.85rem; margin: 0; }
</style>
