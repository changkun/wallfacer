import { ref, watchEffect } from 'vue';

type Theme = 'light' | 'dark' | 'auto';

const STORAGE_KEY = 'wallfacer-theme';

function systemPrefersDark(): boolean {
  if (typeof window === 'undefined') return false;
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

function resolve(t: Theme): 'light' | 'dark' {
  if (t === 'auto') return systemPrefersDark() ? 'dark' : 'light';
  return t;
}

function loadTheme(): Theme {
  if (typeof window === 'undefined') return 'auto';
  try { return (localStorage.getItem(STORAGE_KEY) as Theme) || 'auto'; } catch { return 'auto'; }
}

const theme = ref<Theme>(loadTheme());

export function useTheme() {
  watchEffect(() => {
    if (typeof document === 'undefined') return;
    const resolved = resolve(theme.value);
    document.documentElement.setAttribute('data-theme', resolved);
    try { localStorage.setItem(STORAGE_KEY, theme.value); } catch { /* SSR */ }
  });

  function cycle() {
    const order: Theme[] = ['light', 'dark', 'auto'];
    const idx = order.indexOf(theme.value);
    theme.value = order[(idx + 1) % order.length];
  }

  return { theme, cycle, resolve };
}
