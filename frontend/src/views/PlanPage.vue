<script setup lang="ts">
import { ref, computed, onMounted, nextTick, onUnmounted } from 'vue';
import { api } from '../api/client';

// ── Types ──────────────────────────────────────────────────────────

interface SpecMeta {
  title: string;
  status: string;
  depends_on: string[];
  affects: string[];
  effort: string;
  created: string;
  updated: string;
  author: string;
  dispatched_task_id: string | null;
}

interface SpecNode {
  path: string;
  spec: SpecMeta;
  children: string[];
  is_leaf: boolean;
  depth: number;
}

interface SpecTreeResponse {
  nodes: SpecNode[];
  progress: Record<string, { complete: number; total: number }>;
}

interface ChatMessage {
  role: 'user' | 'assistant';
  content: string;
  timestamp?: string;
}

// ── Auth helper (mirrors useSse pattern) ───────────────────────────

function addAuthParam(url: string): string {
  if (typeof window !== 'undefined' && window.__WALLFACER__?.serverApiKey) {
    const sep = url.includes('?') ? '&' : '?';
    return url + sep + 'token=' + encodeURIComponent(window.__WALLFACER__.serverApiKey);
  }
  return url;
}

// ── Spec tree state ────────────────────────────────────────────────

const nodes = ref<SpecNode[]>([]);
const collapsed = ref<Set<string>>(new Set());
const selectedPath = ref<string | null>(null);
const treeLoading = ref(true);

const selectedNode = computed(() =>
  nodes.value.find(n => n.path === selectedPath.value) ?? null,
);

const sortedNodes = computed(() => {
  return [...nodes.value].sort((a, b) => a.path.localeCompare(b.path));
});

// Determine which nodes are visible based on collapse state.
const visibleNodes = computed(() => {
  const hidden = new Set<string>();
  for (const node of sortedNodes.value) {
    if (hidden.has(node.path)) continue;
    if (collapsed.value.has(node.path) && node.children.length > 0) {
      const queue = [...node.children];
      while (queue.length) {
        const p = queue.shift()!;
        hidden.add(p);
        const child = nodes.value.find(n => n.path === p);
        if (child) queue.push(...child.children);
      }
    }
  }
  return sortedNodes.value.filter(n => !hidden.has(n.path));
});

function toggleCollapse(path: string) {
  const next = new Set(collapsed.value);
  if (next.has(path)) {
    next.delete(path);
  } else {
    next.add(path);
  }
  collapsed.value = next;
}

function selectSpec(path: string) {
  selectedPath.value = path;
  loadSpecContent(path);
}

// ── Spec content (loaded via file explorer API) ────────────────────

const specContent = ref<string>('');
const specContentLoading = ref(false);

async function loadSpecContent(path: string) {
  specContentLoading.value = true;
  specContent.value = '';
  try {
    const resp = await api<{ content: string }>(
      'GET',
      `/api/explorer/file?path=${encodeURIComponent(path)}`,
    );
    specContent.value = resp.content ?? '';
  } catch {
    specContent.value = '(Could not load spec content)';
  }
  specContentLoading.value = false;
}

async function fetchTree() {
  try {
    const resp = await api<SpecTreeResponse>('GET', '/api/specs/tree');
    nodes.value = resp.nodes ?? [];
  } catch (e) {
    console.error('spec tree:', e);
  }
  treeLoading.value = false;
}

// ── Chat state ─────────────────────────────────────────────────────

const messages = ref<ChatMessage[]>([]);
const chatInput = ref('');
const sending = ref(false);
const messagesEl = ref<HTMLElement | null>(null);
let streamEs: EventSource | null = null;

function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) {
      messagesEl.value.scrollTop = messagesEl.value.scrollHeight;
    }
  });
}

async function loadMessages() {
  try {
    const msgs = await api<ChatMessage[]>('GET', '/api/planning/messages');
    messages.value = msgs ?? [];
    scrollToBottom();
  } catch {
    // No conversation yet.
  }
}

function closeStream() {
  if (streamEs) {
    streamEs.close();
    streamEs = null;
  }
}

function openStream() {
  closeStream();
  let buffer = '';

  const es = new EventSource(addAuthParam('/api/planning/messages/stream'), {
    withCredentials: true,
  });
  streamEs = es;

  es.onmessage = (ev) => {
    const text = ev.data as string;
    if (!text) return;

    buffer += text;

    const last = messages.value[messages.value.length - 1];
    if (last && last.role === 'assistant') {
      last.content = buffer;
    } else {
      messages.value.push({ role: 'assistant', content: buffer });
    }
    scrollToBottom();
  };

  es.onerror = () => {
    // Stream ended (normal after response completes) or error.
    closeStream();
  };
}

async function sendMessage() {
  const text = chatInput.value.trim();
  if (!text || sending.value) return;

  sending.value = true;
  chatInput.value = '';

  messages.value.push({ role: 'user', content: text });
  scrollToBottom();

  // Open the SSE stream before POST so we don't miss early tokens.
  openStream();

  try {
    const body: Record<string, string> = { message: text };
    if (selectedPath.value) {
      body.focused_spec = selectedPath.value;
    }
    await api('POST', '/api/planning/messages', body);
  } catch (e) {
    console.error('send message:', e);
    messages.value.push({
      role: 'assistant',
      content: '(Error sending message)',
    });
  }
  sending.value = false;
}

function handleKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Enter' && !ev.shiftKey) {
    ev.preventDefault();
    sendMessage();
  }
}

// ── Status badge helpers ───────────────────────────────────────────

function statusColor(status: string): string {
  const map: Record<string, string> = {
    drafted: 'var(--info)',
    validated: 'var(--ok)',
    complete: 'var(--ok)',
    stale: 'var(--warn)',
    archived: 'var(--ink-4)',
    vague: 'var(--ink-3)',
  };
  return map[status] ?? 'var(--ink-3)';
}

// ── Lifecycle ──────────────────────────────────────────────────────

onMounted(async () => {
  await fetchTree();
  await loadMessages();
});

onUnmounted(() => {
  closeStream();
});
</script>

<template>
  <div class="plan-page">
    <!-- Left pane: spec tree -->
    <aside class="tree-pane">
      <div class="pane-header">
        <span class="pane-title">Specs</span>
      </div>
      <div class="tree-body">
        <div v-if="treeLoading" class="tree-empty">Loading...</div>
        <div v-else-if="!nodes.length" class="tree-empty">No specs found</div>
        <div
          v-for="node in visibleNodes"
          :key="node.path"
          class="tree-row"
          :class="{ selected: selectedPath === node.path }"
          :style="{ paddingLeft: 12 + node.depth * 16 + 'px' }"
          @click="selectSpec(node.path)"
        >
          <span
            v-if="node.children.length > 0"
            class="tree-chevron"
            :class="{ open: !collapsed.has(node.path) }"
            @click.stop="toggleCollapse(node.path)"
          >&#9656;</span>
          <span v-else class="tree-chevron-spacer" />
          <span class="tree-label">{{ node.spec.title || node.path.split('/').pop() }}</span>
          <span
            class="tree-badge"
            :style="{ color: statusColor(node.spec.status), borderColor: statusColor(node.spec.status) }"
          >{{ node.spec.status }}</span>
        </div>
      </div>
    </aside>

    <!-- Center pane: focused spec -->
    <main class="spec-pane">
      <div v-if="!selectedNode" class="spec-empty">
        Select a spec from the tree to view its details.
      </div>
      <template v-else>
        <div class="spec-header">
          <h2 class="spec-title">{{ selectedNode.spec.title }}</h2>
          <div class="spec-meta">
            <span
              class="spec-status"
              :style="{ color: statusColor(selectedNode.spec.status), borderColor: statusColor(selectedNode.spec.status) }"
            >{{ selectedNode.spec.status }}</span>
            <span v-if="selectedNode.spec.effort" class="spec-effort">{{ selectedNode.spec.effort }}</span>
            <span v-if="selectedNode.spec.author" class="spec-author">{{ selectedNode.spec.author }}</span>
          </div>
          <div v-if="selectedNode.spec.depends_on?.length" class="spec-deps">
            <span class="spec-deps-label">Depends on:</span>
            <span
              v-for="dep in selectedNode.spec.depends_on"
              :key="dep"
              class="spec-dep"
              @click="selectSpec(dep)"
            >{{ dep.split('/').pop() }}</span>
          </div>
        </div>
        <div class="spec-body">
          <div v-if="specContentLoading" class="spec-empty">Loading content...</div>
          <pre v-else class="spec-content">{{ specContent }}</pre>
        </div>
      </template>
    </main>

    <!-- Right pane: planning chat -->
    <aside class="chat-pane">
      <div class="pane-header">
        <span class="pane-title">Planning Chat</span>
      </div>
      <div ref="messagesEl" class="chat-messages">
        <div v-if="!messages.length" class="chat-empty">
          No messages yet. Start a conversation about the focused spec.
        </div>
        <div
          v-for="(msg, i) in messages"
          :key="i"
          class="chat-bubble"
          :class="msg.role"
        >
          <div class="bubble-role">{{ msg.role === 'user' ? 'You' : 'Assistant' }}</div>
          <div class="bubble-content">{{ msg.content }}</div>
        </div>
      </div>
      <div class="chat-input-area">
        <textarea
          v-model="chatInput"
          class="chat-input"
          placeholder="Ask about this spec..."
          rows="2"
          :disabled="sending"
          @keydown="handleKeydown"
        />
        <button
          class="chat-send"
          :disabled="!chatInput.trim() || sending"
          @click="sendMessage"
        >Send</button>
      </div>
    </aside>
  </div>
</template>

<style scoped>
.plan-page {
  display: flex;
  height: 100%;
  overflow: hidden;
  font-family: var(--font-sans);
  color: var(--ink);
  background: var(--bg);
}

/* ── Left pane: tree ──────────────────────────── */

.tree-pane {
  width: 280px;
  min-width: 280px;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--rule);
  background: var(--bg-card);
}

.pane-header {
  padding: 10px 14px;
  border-bottom: 1px solid var(--rule);
  display: flex;
  align-items: center;
}

.pane-title {
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--ink-3);
}

.tree-body {
  flex: 1;
  overflow-y: auto;
}

.tree-empty {
  padding: 20px 14px;
  color: var(--ink-4);
  font-size: 12px;
  text-align: center;
}

.tree-row {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 5px 10px;
  cursor: pointer;
  font-size: 12px;
  line-height: 1.4;
  border-bottom: 1px solid transparent;
}

.tree-row:hover {
  background: var(--bg-hover);
}

.tree-row.selected {
  background: var(--bg-active);
}

.tree-chevron {
  display: inline-block;
  width: 14px;
  text-align: center;
  font-size: 10px;
  color: var(--ink-4);
  cursor: pointer;
  transition: transform 0.15s;
  flex-shrink: 0;
}

.tree-chevron.open {
  transform: rotate(90deg);
}

.tree-chevron-spacer {
  display: inline-block;
  width: 14px;
  flex-shrink: 0;
}

.tree-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tree-badge {
  flex-shrink: 0;
  font-size: 9px;
  font-weight: 500;
  padding: 1px 5px;
  border: 1px solid;
  border-radius: 2px;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}

/* ── Center pane: spec detail ─────────────────── */

.spec-pane {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-width: 0;
}

.spec-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--ink-4);
  font-size: 13px;
}

.spec-header {
  padding: 14px 20px;
  border-bottom: 1px solid var(--rule);
}

.spec-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.spec-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 6px;
  font-size: 11px;
}

.spec-status {
  padding: 1px 6px;
  border: 1px solid;
  border-radius: 2px;
  font-weight: 500;
  font-size: 10px;
  text-transform: uppercase;
}

.spec-effort {
  color: var(--ink-3);
  font-family: var(--font-mono);
}

.spec-author {
  color: var(--ink-3);
}

.spec-deps {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  font-size: 11px;
  flex-wrap: wrap;
}

.spec-deps-label {
  color: var(--ink-3);
}

.spec-dep {
  color: var(--accent);
  cursor: pointer;
  font-family: var(--font-mono);
  font-size: 10px;
}

.spec-dep:hover {
  text-decoration: underline;
}

.spec-body {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
}

.spec-content {
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  color: var(--ink-2);
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0;
  background: var(--bg-sunk);
  padding: 14px;
  border-radius: var(--r-sm);
}

/* ── Right pane: chat ─────────────────────────── */

.chat-pane {
  width: 320px;
  min-width: 320px;
  display: flex;
  flex-direction: column;
  border-left: 1px solid var(--rule);
  background: var(--bg-card);
}

.chat-messages {
  flex: 1;
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.chat-empty {
  color: var(--ink-4);
  font-size: 12px;
  text-align: center;
  padding: 20px 10px;
}

.chat-bubble {
  max-width: 90%;
  padding: 8px 12px;
  border-radius: var(--r-sm);
  font-size: 12px;
  line-height: 1.5;
  word-break: break-word;
}

.chat-bubble.user {
  align-self: flex-end;
  background: var(--accent);
  color: #fff;
}

.chat-bubble.assistant {
  align-self: flex-start;
  background: var(--bg-sunk);
  color: var(--ink);
}

.bubble-role {
  font-size: 9px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-bottom: 2px;
  opacity: 0.7;
}

.bubble-content {
  white-space: pre-wrap;
}

.chat-input-area {
  padding: 10px 12px;
  border-top: 1px solid var(--rule);
  display: flex;
  gap: 8px;
  align-items: flex-end;
}

.chat-input {
  flex: 1;
  font-family: var(--font-sans);
  font-size: 12px;
  padding: 8px 10px;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  background: var(--bg);
  color: var(--ink);
  resize: none;
  line-height: 1.4;
}

.chat-input:focus {
  outline: none;
  border-color: var(--accent);
}

.chat-input:disabled {
  opacity: 0.5;
}

.chat-send {
  padding: 6px 14px;
  font-size: 12px;
  font-weight: 500;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: var(--r-sm);
  cursor: pointer;
  white-space: nowrap;
}

.chat-send:hover:not(:disabled) {
  background: var(--accent-2);
}

.chat-send:disabled {
  opacity: 0.4;
  cursor: default;
}
</style>
