import { defineStore } from 'pinia';
import { computed, ref, watch } from 'vue';

export type Theme = 'light' | 'dark' | 'auto';
export type Locale = 'en' | 'zh';

// Color palettes are a separate axis from light/dark: a palette defines the
// hues, the theme picks the mode. `clay` is the default and needs no
// data-palette attribute (its tokens are tokens.css's :root values); the
// others override in palettes.css. Keep this list, PALETTES, palettes.css,
// and the index.html no-flash script in sync.
export type PaletteName = 'clay' | 'indigo' | 'amber' | 'rose' | 'copper';

export interface PaletteInfo {
  name: PaletteName;
  label: string;
  // Representative swatches for picker UIs: [light accent, light canvas,
  // dark accent, dark canvas].
  swatches: [string, string, string, string];
}

export const PALETTES: PaletteInfo[] = [
  { name: 'clay', label: 'Clay', swatches: ['#c45a33', '#f4f1ea', '#e07a51', '#15140f'] },
  { name: 'indigo', label: 'Indigo', swatches: ['#5b5bd6', '#fafafa', '#7c6cf0', '#0b0b0f'] },
  { name: 'amber', label: 'Amber', swatches: ['#d97706', '#fafaf7', '#f59e0b', '#12100d'] },
  { name: 'rose', label: 'Rose', swatches: ['#c22a56', '#fafaf8', '#e5487f', '#110e0f'] },
  { name: 'copper', label: 'Copper', swatches: ['#9a5b2e', '#faf8f4', '#c88a4e', '#121009'] },
];

// Theme key matches the legacy ui/ so both UIs read/write the same
// preference. Without this, cloud and local mode (and the legacy and Vue
// surfaces during the migration) end up with disagreeing themes when
// opened in different tabs.
const THEME_KEY = 'wallfacer-theme';
const PALETTE_KEY = 'wallfacer-palette';
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

function readPalette(): PaletteName {
  if (!hasStorage()) return 'clay';
  const v = localStorage.getItem(PALETTE_KEY);
  return PALETTES.some((p) => p.name === v) ? (v as PaletteName) : 'clay';
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
  // SSG (vite-ssg) prerenders without `window`. Defaulting to 'dark' here
  // bakes `<html data-theme="dark">` into the static HTML and forces every
  // first paint to dark, which then flips on hydration. Default to light
  // so the prerendered page matches the no-attribute :root tokens.
  if (typeof window === 'undefined') return 'light';
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyTheme(t: Theme) {
  if (typeof document === 'undefined') return;
  document.documentElement.setAttribute('data-theme', resolveTheme(t));
}

function applyPalette(p: PaletteName) {
  if (typeof document === 'undefined') return;
  // The default palette carries no attribute so the SSG-prerendered page
  // (no attribute) matches the default first paint exactly.
  if (p === 'clay') document.documentElement.removeAttribute('data-palette');
  else document.documentElement.setAttribute('data-palette', p);
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
  const palette = ref<PaletteName>(readPalette());
  const locale = ref<Locale>(readLocale());

  applyTheme(theme.value);
  applyPalette(palette.value);
  applyLocale(locale.value);

  watch(theme, (t) => {
    if (hasStorage()) localStorage.setItem(THEME_KEY, t);
    applyTheme(t);
  });
  watch(palette, (p) => {
    if (hasStorage()) localStorage.setItem(PALETTE_KEY, p);
    applyPalette(p);
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
  function setPalette(p: PaletteName) { palette.value = p; }
  function setLocale(l: Locale) { locale.value = l; }

  const themeIcon = computed(() => theme.value === 'light' ? '☀' : theme.value === 'dark' ? '☾' : '◐');

  return { theme, palette, locale, themeIcon, toggleTheme, setTheme, setPalette, setLocale };
});
