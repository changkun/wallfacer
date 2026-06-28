<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue';
import { api } from '../api/client';
// Wallfacer-native theming for the Excalidraw island (maps Excalidraw's CSS
// variables onto wallfacer tokens; hides Excalidraw's external Help links).
import '../styles/whiteboard.css';

// WhiteboardPage hosts an Excalidraw drawing canvas as a peer view. Excalidraw
// is a React component with no Vue port, so a single React root is mounted into
// one container element. React, react-dom, and @excalidraw/excalidraw are
// imported dynamically inside onMounted (never at module scope) for two reasons:
//   1. SSG safety — vite-ssg prerenders this route without a DOM; a module-scope
//      React/Excalidraw import would execute browser-only code server-side.
//   2. Code splitting — the React + Excalidraw chunk (~1.5MB gzipped) is fetched
//      only when this route is first opened, keeping the entry bundle React-free.
// The scene persists per workspace via GET/PUT /api/whiteboard as opaque JSON.

const SAVE_DEBOUNCE_MS = 1500;
// Excalidraw libraries (reusable shape sets) are global to the user, not per
// workspace, and are persisted in localStorage. The embedded component does not
// persist them itself, so without this the "Add to Excalidraw" library import —
// which round-trips back through a fresh tab and the addLibrary URL hash — would
// never reach the working session.
const LIBRARY_KEY = 'wallfacer-whiteboard-library';

// Use window.localStorage (the DOM Storage) explicitly rather than the bare
// global, which on Node 22 resolves to an unrelated experimental webstorage stub
// in tests.
function loadLibrary(): unknown[] | null {
  try {
    const raw = window.localStorage.getItem(LIBRARY_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch { return null; }
}
function saveLibrary(items: unknown): void {
  try { window.localStorage.setItem(LIBRARY_KEY, JSON.stringify(items ?? [])); } catch { /* quota / disabled */ }
}

const rootEl = ref<HTMLDivElement | null>(null);
const status = ref<'loading' | 'ready' | 'error'>('loading');
const saveState = ref<'idle' | 'saving' | 'saved' | 'error'>('idle');

// Imperative (non-reactive) handles for the React island and save lifecycle.
// They are plain locals, not refs, because Vue reactivity must not wrap the
// React root or Excalidraw internals.
let reactRoot: { unmount: () => void } | null = null;
let serialize: ((elements: unknown, appState: unknown, files: unknown, type: string) => string) | null = null;
let themeObserver: MutationObserver | null = null;
let saveTimer: ReturnType<typeof setTimeout> | null = null;
let pendingScene: unknown = null;
let disposed = false;
// Excalidraw imperative API (captured via the excalidrawAPI prop), used to push
// cross-tab library updates into the live instance.
let excalApi: { updateLibrary: (opts: { libraryItems: unknown; merge?: boolean }) => void } | null = null;
let onWindowStorage: ((e: StorageEvent) => void) | null = null;
let onDocEscape: ((e: KeyboardEvent) => void) | null = null;

// resolvedTheme reads the app's resolved theme from <html data-theme>, which the
// prefs store keeps current (including OS 'auto' resolution). Excalidraw's theme
// is driven from this so the canvas always matches the rest of the UI.
function resolvedTheme(): 'light' | 'dark' {
  if (typeof document === 'undefined') return 'light';
  return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
}

async function flushSave(): Promise<void> {
  if (pendingScene == null) return;
  const scene = pendingScene;
  pendingScene = null;
  saveState.value = 'saving';
  try {
    await api('PUT', '/api/whiteboard', scene);
    if (!disposed) saveState.value = 'saved';
  } catch {
    if (!disposed) saveState.value = 'error';
  }
}

function scheduleSave(elements: unknown, appState: unknown, files: unknown): void {
  if (disposed || !serialize) return;
  // serializeAsJSON strips transient app state (collaborators, sizing, …) so the
  // persisted blob is stable and free of session-only fields.
  pendingScene = JSON.parse(serialize(elements, appState, files, 'local'));
  if (saveTimer) clearTimeout(saveTimer);
  saveTimer = setTimeout(() => { void flushSave(); }, SAVE_DEBOUNCE_MS);
}

onMounted(async () => {
  // Load the saved scene first; an empty body (no scene yet) returns null.
  let initialData: unknown = null;
  try {
    initialData = await api('GET', '/api/whiteboard');
  } catch {
    status.value = 'error';
    return;
  }
  if (disposed || !rootEl.value) return;

  // Merge the persisted (global) library into the initial data so saved /
  // imported shape sets are available. The library is separate from the scene
  // blob stored per workspace.
  const library = loadLibrary();
  if (library) {
    initialData = (initialData && typeof initialData === 'object')
      ? { ...(initialData as object), libraryItems: library }
      : { libraryItems: library };
  }

  // Dynamic, client-only imports: React island + Excalidraw + its stylesheet.
  let createRoot: typeof import('react-dom/client')['createRoot'];
  // buildScene constructs the Excalidraw React tree (with the trimmed menu and
  // welcome screen) using the current theme; it is rebuilt on each render.
  let buildScene: (() => unknown) | null = null;
  try {
    const [react, reactDom, excalidraw] = await Promise.all([
      import('react'),
      import('react-dom/client'),
      import('@excalidraw/excalidraw'),
      import('@excalidraw/excalidraw/index.css'),
    ]);
    createRoot = reactDom.createRoot;
    serialize = excalidraw.serializeAsJSON as typeof serialize;

    const { Excalidraw, MainMenu, WelcomeScreen } = excalidraw;
    // Permissive createElement wrapper. Composing Excalidraw's React children
    // against React's JSX types under vue-tsc adds only noise; this island never
    // type-checks as JSX, so element construction is kept untyped here.
    const h = react.createElement as unknown as
      (type: unknown, props?: unknown, ...children: unknown[]) => unknown;

    // Trimmed main menu: local scene / export / canvas actions only. Excalidraw's
    // default menu items that are external to wallfacer — the Excalidraw+ promo,
    // GitHub and social links, live collaboration, and sign-up — are dropped by
    // supplying our own MainMenu, which replaces the default entirely.
    const menu = () => h(MainMenu, null,
      h(MainMenu.DefaultItems.SearchMenu),
      h(MainMenu.DefaultItems.CommandPalette),
      h(MainMenu.Separator),
      h(MainMenu.DefaultItems.LoadScene),
      h(MainMenu.DefaultItems.SaveAsImage),
      h(MainMenu.DefaultItems.Export),
      h(MainMenu.Separator),
      h(MainMenu.DefaultItems.ChangeCanvasBackground),
      h(MainMenu.Separator),
      h(MainMenu.DefaultItems.ClearCanvas),
      h(MainMenu.DefaultItems.Help),
    );
    // Branding-free welcome screen: keep the onboarding hints, drop the
    // Excalidraw logo and the sign-up / Excalidraw+ center menu.
    const welcome = () => h(WelcomeScreen, null,
      h(WelcomeScreen.Hints.MenuHint),
      h(WelcomeScreen.Hints.ToolbarHint),
      h(WelcomeScreen.Hints.HelpHint),
      h(WelcomeScreen.Center, null,
        h(WelcomeScreen.Center.Heading, null,
          'Sketch, diagram, and brainstorm on an infinite canvas.'),
      ),
    );

    // initialData keeps a stable reference so Excalidraw consumes it once (at
    // mount); theme changes re-render with a new theme prop without resetting
    // the canvas.
    buildScene = () => h(Excalidraw, {
      initialData,
      theme: resolvedTheme(),
      onChange: (elements: unknown, appState: unknown, files: unknown) =>
        scheduleSave(elements, appState, files),
      // Persist library changes (incl. the "Add to Excalidraw" import) so they
      // survive reloads and reach other tabs.
      onLibraryChange: (items: unknown) => saveLibrary(items),
      excalidrawAPI: (apiObj: unknown) => { excalApi = apiObj as typeof excalApi; },
      // Theme is driven by the app (<html data-theme>), so hide Excalidraw's
      // own theme toggle to avoid a second, conflicting control.
      UIOptions: { canvasActions: { toggleTheme: false } },
    }, menu(), welcome());
  } catch {
    status.value = 'error';
    return;
  }
  if (disposed || !rootEl.value || !buildScene) return;

  const root = createRoot(rootEl.value);
  reactRoot = root;

  const render = () => { root.render(buildScene!() as never); };
  render();
  status.value = 'ready';

  // Mirror app theme changes (store toggle or OS change) onto the canvas by
  // watching the data-theme attribute the prefs store maintains.
  themeObserver = new MutationObserver(render);
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme'],
  });

  // Cross-tab library sync: the "Add to Excalidraw" flow imports into a separate
  // tab and writes localStorage; reflect that into this live instance.
  onWindowStorage = (e) => {
    if (e.key !== LIBRARY_KEY || !excalApi || !e.newValue) return;
    try { excalApi.updateLibrary({ libraryItems: JSON.parse(e.newValue), merge: false }); }
    catch { /* malformed payload — ignore */ }
  };
  window.addEventListener('storage', onWindowStorage);

  // Excalidraw binds its dialog Escape handler to the (portaled) modal element,
  // which can miss focus in this embed, so Escape sometimes fails to close a
  // dialog (e.g. Help). As a fallback, close any open Excalidraw dialog on
  // Escape. When none is open this is a no-op, leaving Excalidraw's own Escape
  // (deselect, exit tool) untouched.
  onDocEscape = (e) => {
    if (e.key !== 'Escape') return;
    const closeBtn = document.querySelector<HTMLElement>(
      '.excalidraw-modal-container .Dialog__close',
    );
    if (closeBtn) { e.preventDefault(); e.stopPropagation(); closeBtn.click(); }
  };
  document.addEventListener('keydown', onDocEscape, true);
});

onBeforeUnmount(() => {
  disposed = true;
  if (saveTimer) clearTimeout(saveTimer);
  // Best-effort flush of any pending edits before the React root is torn down.
  void flushSave();
  if (themeObserver) themeObserver.disconnect();
  if (onWindowStorage) window.removeEventListener('storage', onWindowStorage);
  if (onDocEscape) document.removeEventListener('keydown', onDocEscape, true);
  if (reactRoot) reactRoot.unmount();
});
</script>

<template>
  <div class="wb-container">
    <div class="wb-mount">
      <div ref="rootEl" class="wb-canvas"></div>
      <div v-if="status === 'loading'" class="wb-overlay">Loading whiteboard…</div>
      <div v-else-if="status === 'error'" class="wb-overlay">
        Couldn't load the whiteboard.
      </div>
    </div>
  </div>
</template>

<style scoped>
.wb-container {
  display: flex;
  flex: 1;
  min-height: 0;
  min-width: 0;
  flex-direction: column;
  background: var(--bg);
}
.wb-mount {
  position: relative;
  flex: 1;
  min-height: 0;
  min-width: 0;
}
/* Excalidraw renders into an absolutely-positioned fill so it gets a concrete
   pixel size from the flex parent. */
.wb-canvas {
  position: absolute;
  inset: 0;
}
.wb-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 13px;
  pointer-events: none;
}
</style>
