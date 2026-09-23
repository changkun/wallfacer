// The ramp and surfaces as concrete color strings for code that paints
// outside CSS: the canvas charts. Read from the computed style of the root so
// a palette or theme change is one call away; watchPalette re-invokes a
// callback when either root attribute changes.
export interface ChartPalette {
  ink: string;
  ink2: string;
  ink3: string;
  ink4: string;
  bg: string;
  bgCard: string;
  bgSunk: string;
  rule: string;
  rule2: string;
  accent: string;
  ok: string;
  warn: string;
  run: string;
  err: string;
  purple: string;
}

const TOKENS: Record<keyof ChartPalette, string> = {
  ink: '--ink', ink2: '--ink-2', ink3: '--ink-3', ink4: '--ink-4',
  bg: '--bg', bgCard: '--bg-card', bgSunk: '--bg-sunk',
  rule: '--rule', rule2: '--rule-2', accent: '--accent',
  ok: '--ok', warn: '--warn', run: '--run', err: '--err', purple: '--purple',
};

export function chartPalette(): ChartPalette {
  const out = {} as ChartPalette;
  const cs = typeof getComputedStyle === 'function' && typeof document !== 'undefined'
    ? getComputedStyle(document.documentElement)
    : null;
  for (const key of Object.keys(TOKENS) as (keyof ChartPalette)[]) {
    out[key] = cs ? cs.getPropertyValue(TOKENS[key]).trim() : '';
  }
  return out;
}

// Calls fn whenever the theme or palette attribute on <html> changes. Returns
// the disposer.
export function watchPalette(fn: () => void): () => void {
  if (typeof MutationObserver === 'undefined' || typeof document === 'undefined') return () => {};
  const obs = new MutationObserver(fn);
  obs.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'data-palette'] });
  return () => obs.disconnect();
}
