<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue';

const termContainer = ref<HTMLElement | null>(null);
let term: import('@xterm/xterm').Terminal | null = null;
let fitAddon: import('@xterm/addon-fit').FitAddon | null = null;
let ws: WebSocket | null = null;

function getWsUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  let url = `${proto}//${location.host}/api/terminal/ws?cols=80&rows=24`;
  if (window.__WALLFACER__?.serverApiKey) {
    url += '&token=' + encodeURIComponent(window.__WALLFACER__.serverApiKey);
  }
  return url;
}

async function init() {
  if (!termContainer.value) return;
  const { Terminal } = await import('@xterm/xterm');
  const { FitAddon } = await import('@xterm/addon-fit');
  await import('@xterm/xterm/css/xterm.css');

  term = new Terminal({
    fontSize: 13,
    fontFamily: 'var(--font-mono, "JetBrains Mono", monospace)',
    cursorBlink: true,
    theme: {
      background: '#1b1916',
      foreground: '#f4f1ea',
      cursor: '#f4f1ea',
    },
  });
  fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  term.open(termContainer.value);
  fitAddon.fit();

  ws = new WebSocket(getWsUrl());
  ws.binaryType = 'arraybuffer';

  ws.onopen = () => {
    const msg = JSON.stringify({
      type: 'create_session',
      cols: term!.cols,
      rows: term!.rows,
    });
    ws!.send(msg);
  };

  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      try {
        const msg = JSON.parse(ev.data);
        if (msg.type === 'sessions' || msg.type === 'session_created') return;
      } catch { /* not JSON, write raw */ }
      term!.write(ev.data);
    } else {
      term!.write(new Uint8Array(ev.data));
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

  const ro = new ResizeObserver(() => fitAddon?.fit());
  ro.observe(termContainer.value);
}

onMounted(async () => {
  await nextTick();
  init();
});

onUnmounted(() => {
  ws?.close();
  term?.dispose();
});
</script>

<template>
  <div class="terminal-page">
    <header class="page-header">
      <h1>Terminal</h1>
    </header>
    <div ref="termContainer" class="terminal-container" />
  </div>
</template>

<style scoped>
.terminal-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.page-header {
  padding: 12px 20px;
  border-bottom: 1px solid var(--rule);
  flex-shrink: 0;
}
.page-header h1 { margin: 0; font-size: 15px; font-weight: 600; }
.terminal-container {
  flex: 1;
  padding: 4px;
  background: #1b1916;
}
</style>
