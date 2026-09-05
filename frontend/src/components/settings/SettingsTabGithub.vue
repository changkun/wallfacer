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
  <div class="card compact" data-settings-tab="github">
    <div class="card-head">
      <span class="eyebrow">GitHub</span>
      <span v-if="signedIn && github.connected" class="pill pill-ok gh-state">connected</span>
      <span v-else-if="signedIn" class="pill pill-neutral gh-state">not connected</span>
    </div>
    <div class="rows">
      <div v-if="!signedIn" class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Sign in via latere.ai</span>
          <span class="set-row__help">GitHub is connected once in your latere.ai account and shared across latere products.</span>
        </div>
        <div class="set-row__end">
          <button class="btn sm" @click="auth.login()">Sign in via latere.ai</button>
        </div>
      </div>
      <template v-else-if="github.connected">
        <div class="set-row">
          <div class="set-row__main">
            <span class="set-row__label">Connected as</span>
            <span class="set-row__help">GitHub is connected through your latere.ai account.</span>
          </div>
          <div class="set-row__end"><span class="mono">@{{ github.status.login }}</span></div>
        </div>
        <div class="set-row">
          <div class="set-row__main"><span class="set-row__label">Connections</span></div>
          <div class="set-row__end"><a class="link" :href="github.manageUrl" target="_blank" rel="noopener">Manage at latere.ai ↗</a></div>
        </div>
      </template>
      <div v-else class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Connect GitHub</span>
          <span class="set-row__help">GitHub is not connected to your latere.ai account yet.</span>
          <span v-if="github.error" class="set-status set-status--warn">{{ github.error }}</span>
        </div>
        <div class="set-row__end">
          <a class="btn sm" :href="github.manageUrl" target="_blank" rel="noopener">Connect at latere.ai ↗</a>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.gh-state { margin-left: auto; }
</style>
