<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, nextTick } from 'vue';
import { useUiStore } from '../stores/ui';

const ui = useUiStore();

const termContainer = ref<HTMLElement | null>(null);
const handleEl = ref<HTMLElement | null>(null);
const panelHeight = ref(260);
const dragging = ref(false);

let term: import('@xterm/xterm').Terminal | null = null;
let fitAddon: import('@xterm/addon-fit').FitAddon | null = null;
let ws: WebSocket | null = null;
let resizeObserver: ResizeObserver | null = null;
let initialized = false;

function getWsUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  let url = `${proto}//${location.host}/api/terminal/ws?cols=80&rows=24`;
  if (typeof window !== 'undefined' && window.__WALLFACER__?.serverApiKey) {
    url += '&token=' + encodeURIComponent(window.__WALLFACER__.serverApiKey);
  }
  return url;
}

async function init() {
  if (initialized || !termContainer.value) return;
  initialized = true;
  const { Terminal } = await import('@xterm/xterm');
  const { FitAddon } = await import('@xterm/addon-fit');
  await import('@xterm/xterm/css/xterm.css');

  term = new Terminal({
    fontSize: 12,
    fontFamily: 'var(--font-mono, ui-monospace, "SF Mono", "JetBrains Mono", monospace)',
    cursorBlink: true,
    theme: { background: '#1b1916', foreground: '#f4f1ea', cursor: '#f4f1ea' },
  });
  fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.open(termContainer.value);
  try { fitAddon.fit(); } catch { /* hidden */ }

  ws = new WebSocket(getWsUrl());
  ws.binaryType = 'arraybuffer';
  ws.onopen = () => {
    if (!term) return;
    ws!.send(JSON.stringify({ type: 'create_session', cols: term.cols, rows: term.rows }));
  };
  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      try {
        const msg = JSON.parse(ev.data);
        if (msg.type === 'sessions' || msg.type === 'session_created') return;
      } catch { /* not JSON */ }
      term?.write(ev.data);
    } else {
      term?.write(new Uint8Array(ev.data));
    }
  };
  ws.onerror = () => term?.write('\r\n\x1b[31mWebSocket error\x1b[0m\r\n');
  ws.onclose = () => term?.write('\r\n\x1b[33mDisconnected\x1b[0m\r\n');

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

  resizeObserver = new ResizeObserver(() => {
    if (ui.showTerminal) {
      try { fitAddon?.fit(); } catch { /* ignore */ }
    }
  });
  resizeObserver.observe(termContainer.value);
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

onMounted(async () => {
  if (ui.showTerminal) {
    await nextTick();
    await init();
  }
});

onUnmounted(() => {
  resizeObserver?.disconnect();
  ws?.close();
  term?.dispose();
});

// --- Resize handle ---
function onHandleDown(e: MouseEvent) {
  dragging.value = true;
  e.preventDefault();
  const startY = e.clientY;
  const startH = panelHeight.value;
  function onMove(ev: MouseEvent) {
    const dy = startY - ev.clientY;
    const next = Math.min(Math.max(startH + dy, 120), Math.round(window.innerHeight * 0.8));
    panelHeight.value = next;
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
    ref="handleEl"
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
    <div class="terminal-tab-bar">
      <div id="terminal-tab-list">
        <div class="terminal-tab" aria-selected="true">
          <span>shell</span>
        </div>
      </div>
      <button
        type="button"
        class="terminal-tab-add"
        title="Close terminal"
        aria-label="Close terminal"
        @click="ui.closeTerminal()"
      >×</button>
    </div>
    <div ref="termContainer" id="terminal-canvas" />
  </div>
</template>
