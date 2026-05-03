<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { useAuthStore } from '../stores/auth';
import { useT } from '../i18n';

const auth = useAuthStore();
const t = useT();

const mobileNavOpen = ref(false);
const userMenuOpen = ref(false);

function toggleMobileNav() { mobileNavOpen.value = !mobileNavOpen.value; }
function toggleUserMenu() { userMenuOpen.value = !userMenuOpen.value; }

function onClickAway(e: MouseEvent) {
  const target = e.target as HTMLElement;
  if (!target.closest('.nav-user-menu')) userMenuOpen.value = false;
}

onMounted(() => {
  if (typeof document !== 'undefined') document.addEventListener('click', onClickAway);
  if (!auth.loaded) auth.fetchMe();
});
onUnmounted(() => {
  if (typeof document !== 'undefined') document.removeEventListener('click', onClickAway);
});
</script>

<template>
  <header class="site-header">
    <nav class="nav-container">
      <!-- Logo -->
      <router-link to="/" class="logo-link">
        <svg class="logo-icon" width="20" height="20" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" style="image-rendering:pixelated;"><rect x="0" y="0" width="6" height="3" fill="#d97757"/><rect x="7" y="0" width="9" height="3" fill="#c4623f"/><rect x="0" y="4" width="4" height="3" fill="#a84e2e"/><rect x="5" y="4" width="6" height="3" fill="#d97757"/><rect x="12" y="4" width="4" height="3" fill="#c4623f"/><rect x="0" y="8" width="7" height="3" fill="#c4623f"/><rect x="8" y="8" width="8" height="3" fill="#a84e2e"/><rect x="0" y="12" width="3" height="4" fill="#d97757"/><rect x="4" y="12" width="6" height="4" fill="#a84e2e"/><rect x="11" y="12" width="5" height="4" fill="#d97757"/></svg>
        <span class="logo-text wallfacer-brand">Wallfacer</span>
      </router-link>

      <!-- Nav links -->
      <div class="nav-links" :class="{ 'open': mobileNavOpen }">
        <router-link to="/" class="nav-link" v-html="t('nav.home')" />
        <router-link to="/install" class="nav-link" v-html="t('nav.download')" />
        <router-link to="/docs" class="nav-link" v-html="t('nav.docs')" />
        <router-link to="/pricing" class="nav-link" v-html="t('nav.pricing')" />
        <a href="https://github.com/changkun/wallfacer" class="nav-link nav-icon-link" target="_blank" rel="noopener" title="GitHub"><svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/></svg></a>

        <!-- User menu -->
        <template v-if="auth.me">
          <div class="nav-user-menu">
            <button class="nav-user-toggle" @click="toggleUserMenu" aria-label="User menu">
              <span class="nav-user-name">{{ auth.me.email }}</span>
              <svg class="nav-user-chevron" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9"/></svg>
            </button>
            <div v-show="userMenuOpen" class="nav-user-dropdown">
              <a v-if="auth.me.auth_url" :href="auth.me.auth_url + '/me'" class="nav-user-dropdown-item" v-html="t('nav.me')" />
              <a v-if="auth.me.auth_url" :href="auth.me.auth_url + '/admin'" class="nav-user-dropdown-item" v-html="t('nav.admin')" />
              <div class="nav-user-dropdown-divider"></div>
              <a href="/logout" class="nav-user-dropdown-item" v-html="t('nav.logout')" />
            </div>
          </div>
        </template>
        <template v-else-if="auth.loaded">
          <a href="/login" class="nav-link nav-login" v-html="t('nav.login')" />
        </template>
      </div>

      <!-- Mobile toggle -->
      <button class="nav-mobile-toggle" @click="toggleMobileNav" aria-label="Menu">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>
      </button>
    </nav>
  </header>
</template>
