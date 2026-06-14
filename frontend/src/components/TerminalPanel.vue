<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, nextTick, computed } from 'vue';
import { withAuthToken } from '../api/client';
import { useDockStore } from '../stores/dock';
import type { DockRegion } from '../lib/dock/types';

// The terminal is a dockable panel. DockWorkspace mounts this component once and
// passes the mount element its body should teleport into (the current region, or
// the maximize overlay). Teleport preserves the component instance across moves,
// so the xterm instance and WebSocket survive being docked to another edge — the
// load-bearing constraint in specs/local/dockable-panel-workspace.md. Only the
// DOM renderer is loaded; a canvas/WebGL addon would lose its context on
// re-parent and must not be added.
const props = defineProps<{ target: HTMLElement | null }>();

const dock = useDockStore();
const open = computed(() => dock.isOpen('terminal'));
const isMaximized = computed(() => dock.maximized === 'terminal');
const currentRegion = computed<DockRegion | null>(() => dock.regionOf('terminal'));

const termContainer = ref<HTMLElement | null>(null);
const sessions = ref<Record<string, { label: string; buffer: Uint8Array[] }>>({});
const activeId = ref<string | null>(null);
const sessionsOrder = ref<string[]>([]);
const tabCounter = ref(0);
const SESSION_BUFFER_LIMIT = 100_000;

const DOCK_TARGETS: { region: DockRegion; label: string; glyph: string }[] = [
  { region: 'left', label: 'Dock left', glyph: '⊣' },
  { region: 'bottom', label: 'Dock bottom', glyph: '⊥' },
  { region: 'top', label: 'Dock top', glyph: '⊤' },
  { region: 'right', label: 'Dock right', glyph: '⊢' },
];

let term: import('@xterm/xterm').Terminal | null = null;
let fitAddon: import('@xterm/addon-fit').FitAddon | null = null;
let ws: WebSocket | null = null;
let resizeObserver: ResizeObserver | null = null;
let themeObserver: MutationObserver | null = null;
let initialized = false;
let reconnectDelay = 1000;
let reconnectTimer: number | null = null;

// VSCode-style ANSI palettes (dark matches legacy ui/js/terminal.js; light is
// the VSCode light+ palette so colours stay legible on a light surface).
const darkAnsi = {
  black: '#3c3c3c', red: '#f14c4c', green: '#23d18b', yellow: '#f5f543',
  blue: '#3b8eea', magenta: '#d670d6', cyan: '#29b8db', white: '#cccccc',
  brightBlack: '#666666', brightRed: '#f14c4c', brightGreen: '#23d18b', brightYellow: '#f5f543',
  brightBlue: '#3b8eea', brightMagenta: '#d670d6', brightCyan: '#29b8db', brightWhite: '#e5e5e5',
};
const lightAnsi = {
  black: '#000000', red: '#cd3131', green: '#00a000', yellow: '#949800',
  blue: '#0451a5', magenta: '#bc05bc', cyan: '#0598bc', white: '#555555',
  brightBlack: '#7a766e', brightRed: '#cd3131', brightGreen: '#14ce14', brightYellow: '#b5ba00',
  brightBlue: '#0451a5', brightMagenta: '#bc05bc', brightCyan: '#0598bc', brightWhite: '#2a2720',
};

function getCssVar(name: string): string {
  if (typeof getComputedStyle === 'undefined') return '';
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function isDarkTheme(): boolean {
  return typeof document !== 'undefined' && document.documentElement.dataset.theme === 'dark';
}

function buildTermTheme() {
  const dark = isDarkTheme();
  return {
    background: getCssVar('--terminal-bg') || (dark ? '#1b1916' : '#faf8f3'),
    foreground: getCssVar('--terminal-fg') || (dark ? '#f4f1ea' : '#2a2720'),
    cursor: getCssVar('--accent') || '#c45a33',
    selectionBackground: dark ? 'rgba(244, 241, 234, 0.18)' : 'rgba(27, 25, 22, 0.16)',
    ...(dark ? darkAnsi : lightAnsi),
  };
}

function getWsUrl(cols: number, rows: number): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  // withAuthToken uses & since the URL already carries a query string.
  return withAuthToken(`${proto}//${location.host}/api/terminal/ws?cols=${cols}&rows=${rows}`);
}

const tabs = computed(() => sessionsOrder.value.map(id => ({
  id,
  label: sessions.value[id]?.label ?? 'Shell',
  active: id === activeId.value,
})));

function refit() {
  try { fitAddon?.fit(); } catch { /* hidden */ }
}

async function init() {
  if (initialized || !termContainer.value) return;
  initialized = true;
  const { Terminal } = await import('@xterm/xterm');
  const { FitAddon } = await import('@xterm/addon-fit');
  await import('@xterm/xterm/css/xterm.css');

  term = new Terminal({
    cursorBlink: true,
    fontSize: 12,
    fontFamily: '"JetBrains Mono", "SF Mono", Menlo, Monaco, "Courier New", monospace',
    theme: buildTermTheme(),
    macOptionIsMeta: true,
  });
  fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.open(termContainer.value);
  refit();

  term.onData((data) => {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'input', data: btoa(data) }));
    }
  });
  term.onResize(({ cols, rows }) => {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'resize', cols, rows }));
    }
  });

  // Inject Cmd+Backspace / Cmd+K → Ctrl+U / Ctrl+K (kill line) like the legacy UI.
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown') return true;
    if (e.metaKey && e.key === 'Backspace') {
      ws?.readyState === WebSocket.OPEN && ws.send(JSON.stringify({ type: 'input', data: btoa('\x15') }));
      e.preventDefault(); return false;
    }
    if (e.metaKey && e.key === 'k') {
      ws?.readyState === WebSocket.OPEN && ws.send(JSON.stringify({ type: 'input', data: btoa('\x0b') }));
      e.preventDefault(); return false;
    }
    return true;
  });

  resizeObserver = new ResizeObserver(() => {
    if (open.value) refit();
  });
  resizeObserver.observe(termContainer.value);

  themeObserver = new MutationObserver(() => {
    if (term) term.options.theme = buildTermTheme();
  });
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });

  connect();
}

function connect() {
  if (!term) return;
  if (ws && ws.readyState === WebSocket.OPEN) {
    refit();
    term.focus();
    return;
  }
  const cols = term.cols || 80;
  const rows = term.rows || 24;
  ws = new WebSocket(getWsUrl(cols, rows));
  ws.binaryType = 'arraybuffer';

  ws.onopen = () => {
    reconnectDelay = 1000;
    refit();
    term?.focus();
  };

  ws.onmessage = (event) => {
    if (event.data instanceof ArrayBuffer) {
      const bytes = new Uint8Array(event.data);
      term?.write(bytes);
      if (activeId.value) {
        const buf = sessions.value[activeId.value]?.buffer;
        if (buf) {
          buf.push(bytes);
          let total = 0;
          for (const b of buf) total += b.length;
          while (total > SESSION_BUFFER_LIMIT && buf.length > 0) {
            total -= buf[0].length;
            buf.shift();
          }
        }
      }
      return;
    }
    let msg: { type: string; sessions?: { id: string; active?: boolean }[]; session?: string };
    try { msg = JSON.parse(event.data); } catch { return; }
    switch (msg.type) {
      case 'sessions': handleSessionsList(msg.sessions ?? []); break;
      case 'session_switched': if (msg.session) handleSessionSwitched(msg.session); break;
      case 'session_closed': if (msg.session) handleSessionClosed(msg.session); break;
      case 'session_exited': if (msg.session) handleSessionExited(msg.session); break;
    }
  };

  ws.onclose = (event) => {
    ws = null;
    if (event.code !== 1000) {
      term?.write('\r\n\x1b[33mDisconnected. Reconnecting…\x1b[0m\r\n');
      scheduleReconnect();
    } else {
      dock.closeTerminal();
      sessions.value = {};
      sessionsOrder.value = [];
      activeId.value = null;
      tabCounter.value = 0;
    }
  };
  ws.onerror = () => { /* logged by browser; onclose retries */ };
}

function scheduleReconnect() {
  if (reconnectTimer) return;
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = null;
    if (term) term.clear();
    connect();
    reconnectDelay = Math.min(reconnectDelay * 2, 30_000);
  }, reconnectDelay);
}

function clearTermScreen() {
  if (!term) return;
  term.clear();
  term.write('\x1b[2J\x1b[H');
}

function handleSessionsList(list: { id: string; active?: boolean }[]) {
  const ids: Record<string, true> = {};
  for (const s of list) ids[s.id] = true;
  // Drop sessions no longer on the server.
  for (const id of Object.keys(sessions.value)) {
    if (!ids[id]) {
      delete sessions.value[id];
      sessionsOrder.value = sessionsOrder.value.filter(x => x !== id);
    }
  }
  // Add new sessions.
  for (const s of list) {
    if (!sessions.value[s.id]) {
      tabCounter.value += 1;
      sessions.value[s.id] = { label: `Shell ${tabCounter.value}`, buffer: [] };
      sessionsOrder.value.push(s.id);
    }
    if (s.active && activeId.value !== s.id) {
      const prev = activeId.value;
      activeId.value = s.id;
      if (term && prev) {
        clearTermScreen();
        const buf = sessions.value[s.id]?.buffer ?? [];
        for (const b of buf) term.write(b);
      }
    } else if (s.active) {
      activeId.value = s.id;
    }
  }
  if (activeId.value) deferFocus();
}

function handleSessionSwitched(id: string) {
  activeId.value = id;
  if (term) {
    clearTermScreen();
    const buf = sessions.value[id]?.buffer ?? [];
    for (const b of buf) term.write(b);
    deferFocus();
  }
}

function handleSessionClosed(id: string) {
  delete sessions.value[id];
  sessionsOrder.value = sessionsOrder.value.filter(x => x !== id);
}

function handleSessionExited(id: string) {
  if (id === activeId.value) term?.write('\r\n\x1b[33mSession ended.\x1b[0m\r\n');
  handleSessionClosed(id);
}

function deferFocus() {
  setTimeout(() => term?.focus(), 0);
}

function newSession() {
  if (ws?.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'create_session' }));
  }
  deferFocus();
}

function switchSession(id: string) {
  if (id === activeId.value) { deferFocus(); return; }
  if (ws?.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'switch_session', session: id }));
  }
}

function closeSession(id: string) {
  if (ws?.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'close_session', session: id }));
  }
}

// Dock controls (header). Moving regions re-parents the live xterm via Teleport.
function dockTo(region: DockRegion) { dock.dockTo('terminal', region); }
function toggleMaximize() { dock.toggleMaximize('terminal'); }
function closePanel() { dock.closeTerminal(); }

watch(open, async (isOpen) => {
  if (!isOpen) return;
  await nextTick();
  if (!initialized) {
    await init();
  } else if (!ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) {
    // Panel was reopened after the last session was closed (server cleanly
    // hung up the WS). Wipe the stale screen and request a fresh PTY — the
    // server auto-creates an initial session on every new connection.
    clearTermScreen();
    connect();
  }
  setTimeout(() => { refit(); term?.focus(); }, 60);
});

// The panel moved to a different region (or maximize toggled): the xterm node is
// re-parented by Teleport, so reflow it to the new size and refocus.
watch(() => props.target, async () => {
  if (!open.value) return;
  await nextTick();
  setTimeout(() => { refit(); term?.focus(); }, 60);
});

onMounted(async () => {
  if (open.value) {
    await nextTick();
    await init();
  }
});

onUnmounted(() => {
  resizeObserver?.disconnect();
  themeObserver?.disconnect();
  if (reconnectTimer) clearTimeout(reconnectTimer);
  ws?.close();
  term?.dispose();
});
</script>

<template>
  <Teleport :to="props.target" :disabled="!props.target">
    <div
      v-show="open"
      class="dock-panel terminal-panel"
      role="region"
      aria-label="Terminal panel"
    >
      <div class="terminal-tab-bar">
        <div id="terminal-tab-list">
          <div
            v-for="tab in tabs"
            :key="tab.id"
            class="terminal-tab"
            :aria-selected="tab.active ? 'true' : 'false'"
            :data-session-id="tab.id"
            @mousedown.prevent
            @click="switchSession(tab.id)"
          >
            <span class="terminal-tab__label">{{ tab.label }}</span>
            <button
              type="button"
              class="terminal-tab__close"
              aria-label="Close session"
              tabindex="-1"
              @mousedown.prevent
              @click.stop="closeSession(tab.id)"
            >×</button>
          </div>
        </div>
        <button
          type="button"
          class="terminal-tab-add"
          title="New session"
          aria-label="New terminal session"
          tabindex="-1"
          @mousedown.prevent
          @click="newSession"
        >+</button>
        <div class="dock-panel__controls" @mousedown.prevent>
          <button
            v-for="d in DOCK_TARGETS"
            :key="d.region"
            type="button"
            class="dock-panel__btn"
            :class="{ 'dock-panel__btn--active': currentRegion === d.region && !isMaximized }"
            :title="d.label"
            :aria-label="d.label"
            tabindex="-1"
            @click="dockTo(d.region)"
          >{{ d.glyph }}</button>
          <button
            type="button"
            class="dock-panel__btn"
            :class="{ 'dock-panel__btn--active': isMaximized }"
            :title="isMaximized ? 'Restore' : 'Maximize'"
            :aria-label="isMaximized ? 'Restore terminal' : 'Maximize terminal'"
            tabindex="-1"
            @click="toggleMaximize"
          >{{ isMaximized ? '❐' : '⛶' }}</button>
          <button
            type="button"
            class="dock-panel__btn dock-panel__btn--close"
            title="Close terminal"
            aria-label="Close terminal"
            tabindex="-1"
            @click="closePanel"
          >×</button>
        </div>
      </div>
      <div ref="termContainer" id="terminal-canvas" />
    </div>
  </Teleport>
</template>
