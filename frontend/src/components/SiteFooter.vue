<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { usePrefsStore, type Theme, type Locale } from '../stores/prefs';
import { useT } from '../i18n';

const t = useT();
const prefs = usePrefsStore();
const { theme, locale } = storeToRefs(prefs);

const themes: { v: Theme; label: string }[] = [
  { v: 'light', label: '☀' },
  { v: 'dark',  label: '☾' },
  { v: 'auto',  label: '◐' },
];
const locales: { v: Locale; label: string }[] = [
  { v: 'en', label: 'EN' },
  { v: 'zh', label: '中' },
];
</script>

<template>
  <footer class="site-footer">
    <div class="footer-container">
      <div class="footer-brand">
        <a href="https://latere.ai" class="logo-link" target="_blank" rel="noopener">
          <svg class="logo-icon" width="28" height="28" viewBox="0 0 32 32" fill="none">
            <path d="M16 8c-5.5 0.2-9.5 3-9.3 7.2 0.3 5.2 5.8 7.6 9.3 4.8 3.5-2.8 4.2-7.2 2-9.8-2.8-3.2-7.5-1.5-8.5 2.2-0.8 3 1.2 6.5 4.5 7.2 4 0.8 7.5-1.8 8.5-4.5 1.2-3.5-0.5-7.8-4.5-8.5"
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" fill="none"/>
          </svg>
          <span class="logo-text">Latere.ai</span>
        </a>
        <p class="footer-tagline" v-html="t('footer.tagline')" />
      </div>

      <div class="footer-cols">
        <div class="footer-col">
          <h4 class="footer-col-title" v-html="t('footer.products')" />
          <router-link to="/"><span class="wallfacer-brand">Wallfacer</span></router-link>
          <a href="https://cella.latere.ai/"><span class="cella-brand">Cella</span></a>
        </div>
        <div class="footer-col">
          <h4 class="footer-col-title" v-html="t('footer.company')" />
          <a href="https://latere.ai/about" v-html="t('footer.team')" />
          <a href="https://latere.ai/blog" v-html="t('footer.blog')" />
          <a href="https://latere.ai/podcast" v-html="t('footer.podcast')" />
          <a href="mailto:contact@latere.ai" v-html="t('footer.contact')" />
        </div>
        <div class="footer-col">
          <h4 class="footer-col-title" v-html="t('footer.legal')" />
          <a href="https://latere.ai/legal/privacy" v-html="t('footer.privacy')" />
          <a href="https://latere.ai/legal/terms" v-html="t('footer.terms')" />
          <a href="https://latere.ai/legal/impressum">Impressum</a>
        </div>
      </div>

      <div class="footer-extra">
        <div class="footer-prefs">
          <div class="footer-seg" role="group" aria-label="Theme">
            <button
              v-for="opt in themes"
              :key="opt.v"
              type="button"
              class="footer-seg-btn"
              :class="{ 'is-active': theme === opt.v }"
              @click="prefs.setTheme(opt.v)">{{ opt.label }}</button>
          </div>
          <div class="footer-seg" role="group" aria-label="Language">
            <button
              v-for="opt in locales"
              :key="opt.v"
              type="button"
              class="footer-seg-btn"
              :class="{ 'is-active': locale === opt.v }"
              @click="prefs.setLocale(opt.v)">{{ opt.label }}</button>
          </div>
        </div>
        <div class="footer-social">
          <a href="https://www.linkedin.com/company/latere-ai/about/" target="_blank" rel="noopener" title="LinkedIn">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433a2.062 2.062 0 01-2.063-2.065 2.064 2.064 0 112.063 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z"/></svg>
          </a>
          <a href="https://x.com/LatereAI" target="_blank" rel="noopener" title="X">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z"/></svg>
          </a>
          <a href="https://github.com/latere-ai" target="_blank" rel="noopener" title="GitHub">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"/></svg>
          </a>
        </div>
      </div>
    </div>

    <div class="footer-bottom">
      <p v-html="t('footer.rights')" />
    </div>
  </footer>
</template>
