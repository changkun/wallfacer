import { defineStore } from 'pinia';
import { computed, ref, watch } from 'vue';

export type Theme = 'light' | 'dark' | 'auto';
export type Locale = 'en' | 'zh';

const THEME_KEY = 'latere-theme';
const LOCALE_KEY = 'latere-lang';

function hasStorage(): boolean {
  try { return typeof localStorage !== 'undefined' && typeof localStorage.getItem === 'function'; }
  catch { return false; }
}

function readTheme(): Theme {
  if (!hasStorage()) return 'auto';
  const v = localStorage.getItem(THEME_KEY);
  return v === 'light' || v === 'dark' || v === 'auto' ? v : 'auto';
}

function readLocale(): Locale {
  if (!hasStorage()) return 'en';
  const v = localStorage.getItem(LOCALE_KEY);
  if (v === 'en' || v === 'zh') return v;
  if (typeof navigator !== 'undefined' && navigator.language.toLowerCase().startsWith('zh')) return 'zh';
  return 'en';
}

function resolveTheme(t: Theme): 'light' | 'dark' {
  if (t !== 'auto') return t;
  if (typeof window === 'undefined') return 'dark';
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyTheme(t: Theme) {
  if (typeof document === 'undefined') return;
  document.documentElement.setAttribute('data-theme', resolveTheme(t));
}

function applyLocale(l: Locale) {
  if (typeof document === 'undefined') return;
  document.documentElement.setAttribute('lang', l);
  document.cookie = `${LOCALE_KEY}=${l};path=/;max-age=31536000;SameSite=Lax`;
}

const mediaQuery = typeof window !== 'undefined' && window.matchMedia
  ? window.matchMedia('(prefers-color-scheme: dark)')
  : null;

export const usePrefsStore = defineStore('prefs', () => {
  const theme = ref<Theme>(readTheme());
  const locale = ref<Locale>(readLocale());

  applyTheme(theme.value);
  applyLocale(locale.value);

  watch(theme, (t) => {
    if (hasStorage()) localStorage.setItem(THEME_KEY, t);
    applyTheme(t);
  });
  watch(locale, (l) => {
    if (hasStorage()) localStorage.setItem(LOCALE_KEY, l);
    applyLocale(l);
  });

  if (mediaQuery) {
    const onOSChange = () => { if (theme.value === 'auto') applyTheme('auto'); };
    if (mediaQuery.addEventListener) mediaQuery.addEventListener('change', onOSChange);
    else mediaQuery.addListener(onOSChange);
  }

  function toggleTheme() {
    theme.value = theme.value === 'light' ? 'dark' : theme.value === 'dark' ? 'auto' : 'light';
  }
  function setTheme(t: Theme) { theme.value = t; }
  function setLocale(l: Locale) { locale.value = l; }

  const themeIcon = computed(() => theme.value === 'light' ? '☀' : theme.value === 'dark' ? '☾' : '◐');

  return { theme, locale, themeIcon, toggleTheme, setTheme, setLocale };
});
