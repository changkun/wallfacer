<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, nextTick, computed } from 'vue';
import { useUiStore } from '../stores/ui';
import { api } from '../api/client';

const ui = useUiStore();

const termContainer = ref<HTMLElement | null>(null);
const panelHeight = ref(260);
const dragging = ref(false);
const sessions = ref<Record<string, { label: string; buffer: Uint8Array[] }>>({});
const activeId = ref<string | null>(null);
const sessionsOrder = ref<string[]>([]);
const tabCounter = ref(0);
const containerPickerOpen = ref(false);
const containerList = ref<{ id: string; name: string; state: string; task_title?: string }[]>([]);
const SESSION_BUFFER_LIMIT = 100_000;

let term: import('@xterm/xterm').Terminal | null = null;
let fitAddon: import('@xterm/addon-fit').FitAddon | null = null;
let ws: WebSocket | null = null;
let resizeObserver: ResizeObserver | null = null;
let themeObserver: MutationObserver | null = null;
let initialized = false;
let reconnectDelay = 1000;
let reconnectTimer: number | null = null;

// VSCode-style dark ANSI palette (matches legacy ui/js/terminal.js).
const darkAnsi = {
  black: '#3c3c3c', red: '#f14c4c', green: '#23d18b', yellow: '#f5f543',
  blue: '#3b8eea', magenta: '#d670d6', cyan: '#29b8db', white: '#cccccc',
  brightBlack: '#666666', brightRed: '#f14c4c', brightGreen: '#23d18b', brightYellow: '#f5f543',
  brightBlue: '#3b8eea', brightMagenta: '#d670d6', brightCyan: '#29b8db', brightWhite: '#e5e5e5',
};

function getCssVar(name: string): string {
  if (typeof getComputedStyle === 'undefined') return '';
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function buildTermTheme() {
  return {
    background: getCssVar('--terminal-bg') || '#1b1916',
    foreground: getCssVar('--terminal-fg') || '#f4f1ea',
    cursor: getCssVar('--accent') || '#c45a33',
    selectionBackground: 'rgba(244, 241, 234, 0.18)',
    ...darkAnsi,
  };
}

function getWsUrl(cols: number, rows: number): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  let url = `${proto}//${location.host}/api/terminal/ws?cols=${cols}&rows=${rows}`;
  if (typeof window !== 'undefined' && window.__WALLFACER__?.serverApiKey) {
    url += '&token=' + encodeURIComponent(window.__WALLFACER__.serverApiKey);
  }
  return url;
}

const tabs = computed(() => sessionsOrder.value.map(id => ({
  id,
  label: sessions.value[id]?.label ?? 'Shell',
  active: id === activeId.value,
})));

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
  try { fitAddon.fit(); } catch { /* hidden */ }

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
    if (ui.showTerminal) { try { fitAddon?.fit(); } catch { /* ignore */ } }
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
    try { fitAddon?.fit(); } catch { /* ignore */ }
    term.focus();
    return;
  }
  const cols = term.cols || 80;
  const rows = term.rows || 24;
  ws = new WebSocket(getWsUrl(cols, rows));
  ws.binaryType = 'arraybuffer';

  ws.onopen = () => {
    reconnectDelay = 1000;
    try { fitAddon?.fit(); } catch { /* ignore */ }
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
    let msg: { type: string; sessions?: { id: string; active?: boolean; container?: string }[]; session?: string };
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
      ui.closeTerminal();
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

function handleSessionsList(list: { id: string; active?: boolean; container?: string }[]) {
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
      let label: string;
      if (s.container) {
        label = s.container.length > 24 ? s.container.slice(0, 24) + '…' : s.container;
      } else {
        tabCounter.value += 1;
        label = `Shell ${tabCounter.value}`;
      }
      sessions.value[s.id] = { label, buffer: [] };
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

async function toggleContainerPicker() {
  if (containerPickerOpen.value) { containerPickerOpen.value = false; return; }
  try {
    const containers = await api<{ id: string; name: string; state: string; task_title?: string }[]>('GET', '/api/containers');
    containerList.value = containers ?? [];
  } catch {
    containerList.value = [];
  }
  containerPickerOpen.value = true;
}

function attachToContainer(name: string) {
  containerPickerOpen.value = false;
  if (ws?.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'create_session', container: name }));
  }
  deferFocus();
}

function onPickerOutsideClick(e: MouseEvent) {
  const t = e.target as HTMLElement;
  if (!t.closest('.terminal-container-picker') && !t.closest('.terminal-container-btn-trigger')) {
    containerPickerOpen.value = false;
  }
}

watch(() => ui.showTerminal, async (open) => {
  if (open) {
    await nextTick();
    if (!initialized) await init();
    setTimeout(() => {
      try { fitAddon?.fit(); } catch { /* ignore */ }
      term?.focus();
    }, 60);
  }
});

watch(containerPickerOpen, (open) => {
  if (open) document.addEventListener('mousedown', onPickerOutsideClick);
  else document.removeEventListener('mousedown', onPickerOutsideClick);
});

onMounted(async () => {
  if (ui.showTerminal) {
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
  document.removeEventListener('mousedown', onPickerOutsideClick);
});

function onHandleDown(e: MouseEvent) {
  dragging.value = true;
  e.preventDefault();
  const startY = e.clientY;
  const startH = panelHeight.value;
  function onMove(ev: MouseEvent) {
    const dy = startY - ev.clientY;
    panelHeight.value = Math.min(Math.max(startH + dy, 120), Math.round(window.innerHeight * 0.8));
  }
  function onUp() {
    dragging.value = false;
    document.removeEventListener('mousemove', onMove);
    document.removeEventListener('mouseup', onUp);
    try { fitAddon?.fit(); } catch { /* ignore */ }
  }
  document.addEventListener('mousemove', onMove);
  document.addEventListener('mouseup', onUp);
}
</script>

<template>
  <div
    v-show="ui.showTerminal"
    class="status-bar-panel-resize"
    :class="{ 'status-bar-panel-resize--active': dragging }"
    role="separator"
    aria-orientation="horizontal"
    aria-label="Resize terminal panel"
    @mousedown="onHandleDown"
  ></div>
  <div
    v-show="ui.showTerminal"
    class="status-bar-panel"
    role="region"
    aria-label="Terminal panel"
    :style="{ height: panelHeight + 'px' }"
  >
    <div class="terminal-tab-bar" :hidden="tabs.length === 0">
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
      <button
        type="button"
        class="terminal-tab-add terminal-container-btn-trigger"
        title="Attach to container"
        aria-label="Attach to running container"
        tabindex="-1"
        @mousedown.prevent
        @click="toggleContainerPicker"
      >▢</button>
    </div>
    <div ref="termContainer" id="terminal-canvas" />

    <div
      v-if="containerPickerOpen"
      class="terminal-container-picker"
    >
      <div
        v-if="containerList.filter(c => c.state === 'running').length === 0"
        class="terminal-container-picker__empty"
      >No running containers</div>
      <button
        v-for="c in containerList.filter(x => x.state === 'running')"
        :key="c.id || c.name"
        type="button"
        class="terminal-container-picker__item"
        :data-container-name="c.name"
        @mousedown.prevent
        @click="attachToContainer(c.name)"
      >{{ c.task_title || c.name }}<span v-if="c.id" class="terminal-container-picker__id"> @ {{ c.id.slice(0, 8) }}</span></button>
    </div>
  </div>
</template>

<style scoped>
.terminal-container-picker__id { color: var(--text-muted); font-size: 11px; }
</style>
