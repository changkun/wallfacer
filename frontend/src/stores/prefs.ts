import { defineStore } from 'pinia';
import { ref, watch } from 'vue';

export type Theme = 'light' | 'dark' | 'auto';

// Color palettes are a separate axis from light/dark: a palette defines the
// hues, the theme picks the mode. `clay` (clay accent on a neutral canvas) is
// the default and needs no data-palette attribute (its tokens are tokens.css's
// :root values); the others override in palettes.css. `paper` is the original
// cream canvas. Keep this list, PALETTES, palettes.css, and the index.html
// no-flash script in sync.
export type PaletteName = 'clay' | 'paper' | 'indigo' | 'amber' | 'rose' | 'copper';

export interface PaletteInfo {
  name: PaletteName;
  label: string;
  // Representative swatches for picker UIs: [light accent, light canvas,
  // dark accent, dark canvas].
  swatches: [string, string, string, string];
}

export const PALETTES: PaletteInfo[] = [
  { name: 'clay', label: 'Clay', swatches: ['#c45a33', '#fafaf9', '#e07a51', '#17171a'] },
  { name: 'paper', label: 'Paper', swatches: ['#c45a33', '#f4f1ea', '#e07a51', '#15140f'] },
  { name: 'indigo', label: 'Indigo', swatches: ['#5b5bd6', '#fafafa', '#7c6cf0', '#0b0b0f'] },
  { name: 'amber', label: 'Amber', swatches: ['#d97706', '#fafaf7', '#f59e0b', '#12100d'] },
  { name: 'rose', label: 'Rose', swatches: ['#c22a56', '#fafaf8', '#e5487f', '#110e0f'] },
  { name: 'copper', label: 'Copper', swatches: ['#9a5b2e', '#faf8f4', '#c88a4e', '#121009'] },
];

// The inline script in index.html reads the same keys to paint the first
// frame in the stored theme and palette.
const THEME_KEY = 'wallfacer-theme';
const PALETTE_KEY = 'wallfacer-palette';

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

function resolveTheme(t: Theme): 'light' | 'dark' {
  if (t !== 'auto') return t;
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyTheme(t: Theme) {
  document.documentElement.setAttribute('data-theme', resolveTheme(t));
}

function applyPalette(p: PaletteName) {
  // The default palette carries no attribute, matching the first paint the
  // inline script in index.html produces.
  if (p === 'clay') document.documentElement.removeAttribute('data-palette');
  else document.documentElement.setAttribute('data-palette', p);
}

const mediaQuery = typeof window !== 'undefined' && window.matchMedia
  ? window.matchMedia('(prefers-color-scheme: dark)')
  : null;

export const usePrefsStore = defineStore('prefs', () => {
  const theme = ref<Theme>(readTheme());
  const palette = ref<PaletteName>(readPalette());

  applyTheme(theme.value);
  applyPalette(palette.value);

  watch(theme, (t) => {
    if (hasStorage()) localStorage.setItem(THEME_KEY, t);
    applyTheme(t);
  });
  watch(palette, (p) => {
    if (hasStorage()) localStorage.setItem(PALETTE_KEY, p);
    applyPalette(p);
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

  return { theme, palette, toggleTheme, setTheme, setPalette };
});
