<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue';
import { storeToRefs } from 'pinia';
import { api } from '../../api/client';
import { renderMarkdown } from '../../lib/markdown';
import { startStreamingFetch, type StreamingFetchHandle } from '../../composables/useStreamingFetch';
import { parseActivity, hasActivity, type ActivityRow } from '../../lib/prettyNdjson';
import { enhanceMermaid } from '../../lib/mermaidRender';
import { usePlanningStore } from '../../stores/planning';
import type { PlanningMessage, PlanningThread } from '../../stores/planning';

defineProps<{ visible: boolean }>();
const emit = defineEmits<{ toggle: [] }>();

defineExpose({
  send(text: string) {
    void sendMessage(text);
  },
});

const planning = usePlanningStore();
const {
  threads, threadOrder, archivedThreads, activeThreadId,
  streaming, streamingThreadId, focusedSpecPath,
} = storeToRefs(planning);

// ── Composer state ─────────────────────────────────────────────────

const inputEl = ref<HTMLTextAreaElement | null>(null);
const messagesEl = ref<HTMLElement | null>(null);
const queueWrapEl = ref<HTMLElement | null>(null);

const inputText = ref<string>('');
const userScrolledUp = ref(false);

const SEND_MODE_KEY = 'wallfacer-chat-send-mode';
const sendMode = ref<'enter' | 'cmd-enter'>(
  ((typeof localStorage !== 'undefined' && localStorage.getItem(SEND_MODE_KEY)) as 'enter' | 'cmd-enter') || 'enter',
);

const isMac = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform);
const sendHint = computed(() => {
  const mod = isMac ? '⌘' : 'Ctrl';
  return sendMode.value === 'cmd-enter' ? `${mod}+Return to send` : 'Shift+Return for new line';
});

function toggleSendMode() {
  sendMode.value = sendMode.value === 'enter' ? 'cmd-enter' : 'enter';
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem(SEND_MODE_KEY, sendMode.value);
  }
}

// ── Messages render state ──────────────────────────────────────────

interface RenderedBubble {
  role: 'user' | 'assistant' | 'system';
  contentHtml: string;
  rawText: string;
  rawOutput?: string;
  timestamp?: string;
  planRound: number;
  reverted: boolean;
  activity: ActivityRow[];
  hasActivity: boolean;
  isStreaming: boolean;
  errorText?: string;
}

const renderedMessages = ref<RenderedBubble[]>([]);
const interruptedAt = ref<number>(-1);

function timeOf(ts?: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

function bubbleFromMessage(m: PlanningMessage): RenderedBubble {
  if (m.role === 'assistant') {
    if (m.raw_output) {
      const text = extractAssistantText(m.raw_output);
      const errorText = extractError(m.raw_output);
      const activity = parseActivity(m.raw_output);
      return {
        role: 'assistant',
        contentHtml: text ? renderMarkdown(text) : '',
        rawText: text,
        rawOutput: m.raw_output,
        timestamp: m.timestamp,
        planRound: m.plan_round ?? 0,
        reverted: false,
        activity,
        hasActivity: activity.length > 0,
        isStreaming: false,
        errorText,
      };
    }
    return {
      role: 'assistant',
      contentHtml: renderMarkdown(m.content ?? ''),
      rawText: m.content ?? '',
      timestamp: m.timestamp,
      planRound: m.plan_round ?? 0,
      reverted: false,
      activity: [],
      hasActivity: false,
      isStreaming: false,
    };
  }
  return {
    role: m.role,
    contentHtml: '',
    rawText: m.content ?? '',
    timestamp: m.timestamp,
    planRound: 0,
    reverted: false,
    activity: [],
    hasActivity: false,
    isStreaming: false,
  };
}

function extractAssistantText(raw: string): string {
  let text = '';
  for (const line of raw.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed || trimmed[0] !== '{') continue;
    try {
      const obj = JSON.parse(trimmed);
      if (obj.type === 'assistant' && obj.message?.content) {
        for (const block of obj.message.content) {
          if (block.type === 'text' && typeof block.text === 'string') {
            text += block.text;
          }
        }
      }
    } catch { /* skip malformed */ }
  }
  return text;
}

function extractError(raw: string): string {
  const lines = raw.split('\n');
  for (let i = lines.length - 1; i >= 0; i--) {
    const trimmed = lines[i].trim();
    if (!trimmed || trimmed[0] !== '{') continue;
    try {
      const obj = JSON.parse(trimmed);
      if (obj.type === 'result' && obj.is_error && obj.result) return String(obj.result);
    } catch { /* skip */ }
  }
  return '';
}

function activityIcon(kind: ActivityRow['kind']): string {
  switch (kind) {
    case 'tool': return '▶';
    case 'tool_result': return '✓';
    case 'thinking': return '🧠';
    default: return '·';
  }
}

// ── Streaming buffer ───────────────────────────────────────────────

let streamHandle: StreamingFetchHandle | null = null;

async function loadHistory() {
  if (!activeThreadId.value) {
    renderedMessages.value = [];
    return;
  }
  const fetched = activeThreadId.value;
  try {
    const msgs = await api<PlanningMessage[]>(
      'GET',
      '/api/planning/messages?thread=' + encodeURIComponent(fetched),
    );
    if (fetched !== activeThreadId.value) return;
    renderedMessages.value = (msgs ?? []).map(bubbleFromMessage);
    interruptedAt.value = -1;
    void scrollToBottom(true);
  } catch {
    renderedMessages.value = [];
  }
}

function appendSystem(text: string) {
  renderedMessages.value.push({
    role: 'system',
    contentHtml: '',
    rawText: text,
    planRound: 0,
    reverted: false,
    activity: [],
    hasActivity: false,
    isStreaming: false,
  });
  void scrollToBottom();
}

function startStreaming() {
  streaming.value = true;
  const bubble: RenderedBubble = {
    role: 'assistant',
    contentHtml: '',
    rawText: '',
    rawOutput: '',
    planRound: 0,
    reverted: false,
    activity: [],
    hasActivity: false,
    isStreaming: true,
  };
  renderedMessages.value.push(bubble);
  void scrollToBottom();

  const idx = renderedMessages.value.length - 1;
  let rawBuffer = '';
  let receivedContent = false;
  let retried = false;

  const connect = () => {
    const url =
      '/api/planning/messages/stream' +
      (streamingThreadId.value
        ? '?thread=' + encodeURIComponent(streamingThreadId.value)
        : '');
    streamHandle = startStreamingFetch({
      url,
      onChunk: (chunk: string) => {
        rawBuffer += chunk;
        const text = extractAssistantText(rawBuffer);
        const errorText = extractError(rawBuffer);
        const activity = parseActivity(rawBuffer);
        const hasAct = hasActivity(rawBuffer);
        if (!receivedContent && (text || hasAct)) receivedContent = true;
        if (receivedContent) {
          const updated: RenderedBubble = {
            ...renderedMessages.value[idx],
            rawText: text,
            contentHtml: text ? renderMarkdown(text) : '',
            rawOutput: rawBuffer,
            activity,
            hasActivity: hasAct,
            errorText: errorText || undefined,
          };
          renderedMessages.value.splice(idx, 1, updated);
        }
        void scrollToBottom();
      },
      onDone: (hadData: boolean) => {
        if (!hadData && !retried) {
          retried = true;
          setTimeout(connect, 500);
          return;
        }
        const text = extractAssistantText(rawBuffer);
        const errorText = extractError(rawBuffer);
        const activity = parseActivity(rawBuffer);
        const updated: RenderedBubble = {
          ...renderedMessages.value[idx],
          rawText: text,
          contentHtml: text ? renderMarkdown(text) : '',
          rawOutput: rawBuffer,
          activity,
          hasActivity: activity.length > 0,
          errorText: errorText || undefined,
          isStreaming: false,
        };
        renderedMessages.value.splice(idx, 1, updated);
        finishStreaming(false);
      },
      onError: () => {
        if (!retried) {
          retried = true;
          setTimeout(connect, 500);
          return;
        }
        finishStreaming(false);
      },
    });
  };
  connect();
}

function finishStreaming(interrupted: boolean) {
  if (streamHandle) {
    streamHandle.abort();
    streamHandle = null;
  }
  streaming.value = false;
  const finishedThread = streamingThreadId.value;
  streamingThreadId.value = '';
  if (interrupted) {
    interruptedAt.value = renderedMessages.value.length - 1;
  }
  drainNextQueued();
  if (!interrupted) {
    if (finishedThread && finishedThread !== activeThreadId.value) {
      const t = threads.value[finishedThread];
      if (t) t.unread = true;
    } else {
      // Refetch so the streaming bubble picks up its server-attributed
      // plan_round (per-message undo button).
      void loadHistory();
    }
  }
}

async function sendMessage(text: string, opts?: { threadID?: string }): Promise<void> {
  const targetId = opts?.threadID ?? activeThreadId.value;
  if (!targetId) {
    appendSystem('No active thread — create one first.');
    return;
  }
  if (streaming.value) {
    enqueue(text, targetId);
    return;
  }
  if (!opts?.threadID) {
    inputText.value = '';
    autoGrow();
  }

  if (targetId === activeThreadId.value) {
    renderedMessages.value.push(bubbleFromMessage({
      role: 'user',
      content: text,
      timestamp: new Date().toISOString(),
    }));
    userScrolledUp.value = false;
    void scrollToBottom(true);
  }

  const thread = threads.value[targetId];
  const body: Record<string, string> = { message: text, thread: targetId };
  if (thread?.mode === 'task') {
    if (thread.task_id) body.focused_task = thread.task_id;
  } else {
    body.focused_spec = focusedSpecPath.value || '';
  }

  try {
    const res = await fetch('/api/planning/messages', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        ...(window.__WALLFACER__?.serverApiKey
          ? { Authorization: 'Bearer ' + window.__WALLFACER__.serverApiKey }
          : {}),
      },
      body: JSON.stringify(body),
    });

    if (res.status === 409) {
      let conflictText = 'Agent is busy — try again shortly.';
      try {
        const j = await res.json();
        if (j?.error) conflictText = j.error;
      } catch { /* ignore */ }
      appendSystem(conflictText);
      inputEl.value?.focus();
      return;
    }
    if (!res.ok) {
      appendSystem('Error: ' + (await res.text()));
      inputEl.value?.focus();
      return;
    }
    streamingThreadId.value = targetId;
    startStreaming();
  } catch (e) {
    appendSystem('Error: ' + (e instanceof Error ? e.message : String(e)));
  }
}

async function onInterrupt() {
  if (!streaming.value) return;
  const url =
    '/api/planning/messages/interrupt' +
    (streamingThreadId.value
      ? '?thread=' + encodeURIComponent(streamingThreadId.value)
      : '');
  try {
    await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        ...(window.__WALLFACER__?.serverApiKey
          ? { Authorization: 'Bearer ' + window.__WALLFACER__.serverApiKey }
          : {}),
      },
    });
  } catch { /* swallow */ }
  finishStreaming(true);
}

async function clearHistory() {
  const url =
    '/api/planning/messages' +
    (activeThreadId.value
      ? '?thread=' + encodeURIComponent(activeThreadId.value)
      : '');
  try {
    await fetch(url, {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        ...(window.__WALLFACER__?.serverApiKey
          ? { Authorization: 'Bearer ' + window.__WALLFACER__.serverApiKey }
          : {}),
      },
    });
  } catch { /* swallow */ }
  renderedMessages.value = [];
}

// ── Queue ──────────────────────────────────────────────────────────

let queueSeq = 0;

function enqueue(text: string, threadID: string) {
  const t = threads.value[threadID];
  if (!t) return;
  if (t.queue.length === 0) t.enqueuedAt = Date.now();
  t.queue.push({ id: ++queueSeq, text });
}

const currentQueue = computed(() => {
  const t = activeThreadId.value ? threads.value[activeThreadId.value] : null;
  return t?.queue ?? [];
});

function removeFromQueue(id: number) {
  const t = activeThreadId.value ? threads.value[activeThreadId.value] : null;
  if (!t) return;
  t.queue = t.queue.filter(q => q.id !== id);
  if (t.queue.length === 0) t.enqueuedAt = 0;
}

function drainNextQueued() {
  if (streaming.value) return;
  let bestId: string | null = null;
  let bestTs = Infinity;
  for (const id of Object.keys(threads.value)) {
    const t = threads.value[id];
    if (!t || t.queue.length === 0) continue;
    if (t.enqueuedAt < bestTs) {
      bestTs = t.enqueuedAt;
      bestId = id;
    }
  }
  if (!bestId) return;
  const t = threads.value[bestId];
  const next = t.queue.shift();
  if (!next) return;
  t.enqueuedAt = t.queue.length > 0 ? Date.now() : 0;
  void sendMessage(next.text, { threadID: bestId });
}

// ── Keyboard handling ──────────────────────────────────────────────

const slashOpen = ref(false);
const slashItems = ref<{ name: string; description?: string }[]>([]);
const slashFiltered = ref<{ name: string; description?: string }[]>([]);
const slashIndex = ref(0);
const slashStart = ref(-1);

const mentionOpen = ref(false);
const mentionItems = ref<string[]>([]);
const mentionFiltered = ref<string[]>([]);
const mentionIndex = ref(0);
const mentionStart = ref(-1);

let commandsCache: { name: string; description?: string }[] | null = null;
async function fetchCommands() {
  if (commandsCache) return commandsCache;
  try {
    commandsCache = await api<{ name: string; description?: string }[]>('GET', '/api/planning/commands');
    return commandsCache ?? [];
  } catch {
    return [];
  }
}

let filesCache: string[] | null = null;
async function fetchFiles() {
  if (filesCache) return filesCache;
  try {
    const resp = await api<{ files: string[] }>('GET', '/api/files');
    filesCache = resp.files ?? [];
    return filesCache;
  } catch {
    return [];
  }
}

function autoGrow() {
  const el = inputEl.value;
  if (!el) return;
  el.style.height = 'auto';
  el.style.height = Math.min(el.scrollHeight, 200) + 'px';
}

async function onInput() {
  autoGrow();
  const el = inputEl.value;
  if (!el) return;
  const value = el.value;
  const pos = el.selectionStart ?? value.length;
  const before = value.slice(0, pos);

  // Slash detection: token starting with / at line start or after whitespace.
  const slashMatch = before.match(/(^|\s)\/([\w-]*)$/);
  if (slashMatch) {
    slashStart.value = before.lastIndexOf('/');
    const q = slashMatch[2].toLowerCase();
    if (slashItems.value.length === 0) slashItems.value = await fetchCommands();
    slashFiltered.value = slashItems.value.filter(c => c.name.toLowerCase().startsWith(q));
    slashIndex.value = 0;
    slashOpen.value = slashFiltered.value.length > 0;
    mentionOpen.value = false;
    return;
  }
  slashOpen.value = false;

  // Mention detection: token starting with @.
  const atMatch = before.match(/(^|\s)@([\w./-]*)$/);
  if (atMatch) {
    mentionStart.value = before.lastIndexOf('@');
    const q = atMatch[2].toLowerCase();
    if (mentionItems.value.length === 0) mentionItems.value = await fetchFiles();
    mentionFiltered.value = mentionItems.value
      .filter(f => f.toLowerCase().includes(q))
      .slice(0, 50);
    mentionIndex.value = 0;
    mentionOpen.value = mentionFiltered.value.length > 0;
    return;
  }
  mentionOpen.value = false;
}

function applySlash(cmd: { name: string }) {
  const el = inputEl.value;
  if (!el || slashStart.value < 0) return;
  const v = el.value;
  const pos = el.selectionStart ?? v.length;
  const inserted = '/' + cmd.name + ' ';
  el.value = v.slice(0, slashStart.value) + inserted + v.slice(pos);
  inputText.value = el.value;
  const newPos = slashStart.value + inserted.length;
  el.setSelectionRange(newPos, newPos);
  el.focus();
  slashOpen.value = false;
}

function applyMention(file: string) {
  const el = inputEl.value;
  if (!el || mentionStart.value < 0) return;
  const v = el.value;
  const pos = el.selectionStart ?? v.length;
  const inserted = '@' + file + ' ';
  el.value = v.slice(0, mentionStart.value) + inserted + v.slice(pos);
  inputText.value = el.value;
  const newPos = mentionStart.value + inserted.length;
  el.setSelectionRange(newPos, newPos);
  el.focus();
  mentionOpen.value = false;
}

function onKeydown(ev: KeyboardEvent) {
  if (slashOpen.value) {
    if (ev.key === 'ArrowDown') {
      ev.preventDefault();
      slashIndex.value = (slashIndex.value + 1) % slashFiltered.value.length;
      return;
    }
    if (ev.key === 'ArrowUp') {
      ev.preventDefault();
      slashIndex.value = (slashIndex.value - 1 + slashFiltered.value.length) % slashFiltered.value.length;
      return;
    }
    if (ev.key === 'Enter' || ev.key === 'Tab') {
      ev.preventDefault();
      const c = slashFiltered.value[slashIndex.value];
      if (c) applySlash(c);
      return;
    }
    if (ev.key === 'Escape') {
      ev.preventDefault();
      slashOpen.value = false;
      return;
    }
  }
  if (mentionOpen.value) {
    if (ev.key === 'ArrowDown') {
      ev.preventDefault();
      mentionIndex.value = (mentionIndex.value + 1) % mentionFiltered.value.length;
      return;
    }
    if (ev.key === 'ArrowUp') {
      ev.preventDefault();
      mentionIndex.value = (mentionIndex.value - 1 + mentionFiltered.value.length) % mentionFiltered.value.length;
      return;
    }
    if (ev.key === 'Enter' || ev.key === 'Tab') {
      ev.preventDefault();
      const f = mentionFiltered.value[mentionIndex.value];
      if (f) applyMention(f);
      return;
    }
    if (ev.key === 'Escape') {
      ev.preventDefault();
      mentionOpen.value = false;
      return;
    }
  }

  if (ev.key === 'Enter') {
    let shouldSend = false;
    if (sendMode.value === 'cmd-enter') {
      shouldSend = ev.metaKey || ev.ctrlKey;
    } else {
      shouldSend = !ev.shiftKey || ev.metaKey || ev.ctrlKey;
    }
    if (shouldSend) {
      ev.preventDefault();
      const text = inputText.value.trim();
      if (text) void sendMessage(text);
    }
  }
}

function insertChar(ch: '/' | '@') {
  const el = inputEl.value;
  if (!el) return;
  const pos = el.selectionStart ?? el.value.length;
  const v = el.value;
  el.value = v.slice(0, pos) + ch + v.slice(pos);
  inputText.value = el.value;
  el.setSelectionRange(pos + 1, pos + 1);
  el.focus();
  void onInput();
}

// ── Tabs ───────────────────────────────────────────────────────────

const renamingId = ref<string>('');
const renameDraft = ref<string>('');
const archiveMenuOpen = ref(false);

async function createThread() {
  try {
    const t = await api<PlanningThread>('POST', '/api/planning/threads', {});
    if (t?.id) {
      await planning.loadThreads();
      await switchToThread(t.id);
    }
  } catch (e) {
    appendSystem('Failed to create thread: ' + (e instanceof Error ? e.message : String(e)));
  }
}

async function switchToThread(id: string) {
  if (!id || id === activeThreadId.value) return;
  // Save scroll position of outgoing thread.
  const outgoing = activeThreadId.value ? threads.value[activeThreadId.value] : null;
  if (outgoing && messagesEl.value) outgoing.scrollTop = messagesEl.value.scrollTop;

  // Detach our local stream reader if leaving the in-flight thread.
  if (streaming.value && streamingThreadId.value !== id) {
    if (streamHandle) {
      streamHandle.abort();
      streamHandle = null;
    }
    streaming.value = false;
  }

  activeThreadId.value = id;
  const t = threads.value[id];
  if (t) {
    t.unread = false;
    t.lastViewedAt = Date.now();
  }
  // Server-side activate (fire and forget).
  api('POST', '/api/planning/threads/' + encodeURIComponent(id) + '/activate', {}).catch(() => {});
  await loadHistory();
}

function startRename(id: string) {
  const t = threads.value[id];
  if (!t) return;
  renamingId.value = id;
  renameDraft.value = t.name;
  void nextTick(() => {
    const inp = document.querySelector<HTMLInputElement>('.pcp-tab-rename');
    inp?.focus();
    inp?.select();
  });
}

async function commitRename() {
  const id = renamingId.value;
  if (!id) return;
  const newName = renameDraft.value.trim();
  const current = threads.value[id]?.name ?? '';
  if (!newName || newName === current) {
    renamingId.value = '';
    return;
  }
  try {
    await api('PATCH', '/api/planning/threads/' + encodeURIComponent(id), { name: newName });
    if (threads.value[id]) threads.value[id].name = newName;
  } catch { /* ignore */ }
  renamingId.value = '';
}

function cancelRename() {
  renamingId.value = '';
}

async function archiveThread(id: string) {
  if (!confirm('Archive this thread? You can restore it later.')) return;
  try {
    const res = await fetch('/api/planning/threads/' + encodeURIComponent(id) + '/archive', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        ...(window.__WALLFACER__?.serverApiKey
          ? { Authorization: 'Bearer ' + window.__WALLFACER__.serverApiKey }
          : {}),
      },
    });
    if (res.status === 409) {
      appendSystem('Thread is busy — interrupt it before archiving.');
      return;
    }
    if (!res.ok) {
      appendSystem('Archive failed: HTTP ' + res.status);
      return;
    }
    await planning.loadThreads();
    if (activeThreadId.value && activeThreadId.value !== id) await loadHistory();
    else if (threadOrder.value.length > 0) await switchToThread(threadOrder.value[0]);
    else renderedMessages.value = [];
  } catch (e) {
    appendSystem('Archive failed: ' + (e instanceof Error ? e.message : String(e)));
  }
}

async function unarchiveThread(id: string) {
  try {
    await api('POST', '/api/planning/threads/' + encodeURIComponent(id) + '/unarchive', {});
    await planning.loadThreads();
    await switchToThread(id);
    archiveMenuOpen.value = false;
  } catch (e) {
    appendSystem('Restore failed: ' + (e instanceof Error ? e.message : String(e)));
  }
}

// ── Per-bubble undo ────────────────────────────────────────────────

const latestRound = computed(() => {
  let max = -1;
  for (const m of renderedMessages.value) {
    if (m.role === 'assistant' && !m.reverted && m.planRound > max) max = m.planRound;
  }
  return max;
});

async function undoRound(bubble: RenderedBubble) {
  if (bubble.planRound <= 0 || bubble.planRound !== latestRound.value) return;
  const url =
    '/api/planning/undo' +
    (activeThreadId.value
      ? '?thread=' + encodeURIComponent(activeThreadId.value)
      : '');
  try {
    const res = await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        ...(window.__WALLFACER__?.serverApiKey
          ? { Authorization: 'Bearer ' + window.__WALLFACER__.serverApiKey }
          : {}),
      },
    });
    let body: { round?: number; summary?: string; files_reverted?: string[]; error?: string } = {};
    try { body = await res.json(); } catch { /* ignore */ }
    if (!res.ok) {
      let msg: string;
      const err = body.error ?? '';
      if (res.status === 409 && err.includes('revert conflict')) {
        msg = '⚠ Undo ran into a merge conflict — a concurrent thread edited the same spec. Resolve manually before retrying.';
      } else if (res.status === 409 && err.includes('stash pop conflict')) {
        msg = '⚠ Undo partially applied: your working-tree edits couldn\'t be reapplied cleanly.';
      } else if (res.status === 409) {
        msg = '⚠ Nothing to undo right now.';
      } else {
        msg = `Undo failed (HTTP ${res.status})${err ? ': ' + err : ''}`;
      }
      appendSystem(msg);
      return;
    }
    bubble.reverted = true;
    appendSystem(
      `↺ Undid round ${body.round ?? '?'}${body.summary ? ' — ' + body.summary : ''}`,
    );
    // Best-effort tree refresh.
    void planning.fetchTree();
  } catch (e) {
    appendSystem('Undo failed: ' + (e instanceof Error ? e.message : 'network error'));
  }
}

// ── Lifecycle ──────────────────────────────────────────────────────

function scrollToBottom(force = false) {
  void nextTick(() => {
    if (!messagesEl.value) return;
    if (force || !userScrolledUp.value) {
      messagesEl.value.scrollTop = messagesEl.value.scrollHeight;
    }
  });
}

function onScroll() {
  if (!messagesEl.value) return;
  userScrolledUp.value =
    messagesEl.value.scrollTop + messagesEl.value.clientHeight <
    messagesEl.value.scrollHeight - 40;
}

watch(activeThreadId, () => {
  void loadHistory();
});

// Whenever the rendered list changes, run the mermaid enhancer over the
// chat scroller. It's a no-op if there are no `.mermaid-block` placeholders.
watch(renderedMessages, () => {
  void nextTick(() => {
    if (messagesEl.value) void enhanceMermaid(messagesEl.value);
  });
}, { deep: true });

watch(messagesEl, (el) => {
  if (el) {
    el.addEventListener('scroll', onScroll);
  }
});

onMounted(async () => {
  await planning.loadThreads();
  await loadHistory();
});

onUnmounted(() => {
  if (streamHandle) streamHandle.abort();
});
</script>

<template>
  <aside v-show="visible" class="planning-chat-panel">
    <header class="pcp-header">
      <span class="pcp-title">Planning Chat</span>
      <div class="pcp-header-actions">
        <button
          type="button"
          class="pcp-clear"
          title="Clear conversation"
          @click="clearHistory"
        >Clear</button>
        <button
          type="button"
          class="pcp-fold"
          title="Hide chat (C)"
          @click="emit('toggle')"
        >✕</button>
      </div>
    </header>

    <div class="pcp-tabs" role="tablist" aria-label="Planning chat threads">
      <template v-for="id in threadOrder" :key="id">
        <component
          :is="id === activeThreadId ? 'div' : 'button'"
          class="pcp-tab"
          :class="{ 'pcp-tab--active': id === activeThreadId }"
          :role="id === activeThreadId ? 'tab' : undefined"
          :type="id === activeThreadId ? undefined : 'button'"
          @click="id === activeThreadId ? null : switchToThread(id)"
        >
          <input
            v-if="renamingId === id"
            v-model="renameDraft"
            class="pcp-tab-rename"
            type="text"
            @keydown.enter.prevent="commitRename"
            @keydown.escape.prevent="cancelRename"
            @blur="commitRename"
            @click.stop
          />
          <template v-else>
            <span class="pcp-tab-label">{{ threads[id]?.name }}</span>
            <span v-if="id !== activeThreadId && threads[id]?.unread" class="pcp-tab-unread" />
            <button
              v-if="id === activeThreadId"
              type="button"
              class="pcp-tab-pencil"
              title="Rename"
              @click.stop="startRename(id)"
            >✎</button>
            <button
              type="button"
              class="pcp-tab-close"
              title="Archive thread"
              @click.stop="archiveThread(id)"
            >×</button>
          </template>
        </component>
      </template>
      <span class="pcp-tab-newwrap">
        <button
          type="button"
          class="pcp-tab-new"
          title="New thread"
          @click="createThread"
        >+</button>
        <button
          v-if="archivedThreads.length > 0"
          type="button"
          class="pcp-tab-archive-trigger"
          title="Archived threads"
          @click="archiveMenuOpen = !archiveMenuOpen"
        >▾</button>
        <div v-if="archiveMenuOpen" class="pcp-archived-menu">
          <div class="pcp-archived-menu-header">Archived threads</div>
          <button
            v-for="t in archivedThreads"
            :key="t.id"
            type="button"
            class="pcp-archived-menu-item"
            @click="unarchiveThread(t.id)"
          >{{ t.name }}</button>
        </div>
      </span>
    </div>

    <div ref="messagesEl" class="pcp-messages">
      <div v-if="renderedMessages.length === 0" class="pcp-empty">
        No messages yet.
      </div>
      <template v-for="(m, i) in renderedMessages" :key="i">
        <div
          v-if="m.role === 'system'"
          class="pcp-system"
        >{{ m.rawText }}</div>
        <div
          v-else
          class="pcp-bubble"
          :class="{
            ['pcp-bubble--' + m.role]: true,
            'pcp-bubble--reverted': m.reverted,
          }"
        >
          <span class="pcp-av">{{ m.role === 'user' ? 'me' : 'wf' }}</span>
          <div class="pcp-bubble-body">
            <div class="pcp-bubble-meta">
              <span class="pcp-bubble-name">{{ m.role === 'user' ? 'you' : 'plan-agent' }}</span>
              <span v-if="m.timestamp" class="pcp-bubble-time">{{ timeOf(m.timestamp) }}</span>
            </div>
            <div v-if="m.errorText" class="pcp-bubble-error">{{ m.errorText }}</div>
            <div v-if="m.role === 'user'" class="pcp-bubble-content">{{ m.rawText }}</div>
            <div
              v-else-if="m.contentHtml"
              class="pcp-bubble-content prose-content"
              v-html="m.contentHtml"
            />
            <div
              v-else-if="m.isStreaming"
              class="pcp-thinking"
            ><span>.</span><span>.</span><span>.</span></div>
            <details
              v-if="m.hasActivity"
              class="pcp-activity"
              :open="m.isStreaming"
            >
              <summary>Agent activity</summary>
              <div class="pcp-activity-log">
                <div
                  v-for="(row, ri) in m.activity"
                  :key="ri"
                  class="pcp-activity-row"
                  :class="'pcp-activity-row--' + row.kind"
                >
                  <span class="pcp-activity-icon">{{ activityIcon(row.kind) }}</span>
                  <span class="pcp-activity-label">{{ row.label }}</span>
                  <span v-if="row.summary" class="pcp-activity-summary">{{ row.summary }}</span>
                  <details v-if="row.detail" class="pcp-activity-detail" :open="row.defaultOpen">
                    <summary>show</summary>
                    <pre>{{ row.detail }}</pre>
                  </details>
                </div>
              </div>
            </details>
          </div>
          <div
            v-if="m.role === 'assistant' && m.planRound > 0 && !m.reverted"
            class="pcp-bubble-actions"
          >
            <button
              type="button"
              class="pcp-undo"
              :disabled="m.planRound !== latestRound"
              :title="m.planRound === latestRound ? 'Undo round ' + m.planRound : 'Only the most recent round can be undone'"
              :aria-label="'Undo round ' + m.planRound"
              @click="undoRound(m)"
            >↺</button>
          </div>
        </div>
        <div
          v-if="i === interruptedAt"
          class="pcp-interrupted"
        >interrupted</div>
      </template>
    </div>

    <div ref="queueWrapEl" class="pcp-queue">
      <div v-for="q in currentQueue" :key="q.id" class="pcp-queue-chip">
        <span class="pcp-queue-text">{{ q.text }}</span>
        <button
          type="button"
          class="pcp-queue-remove"
          @click="removeFromQueue(q.id)"
        >×</button>
      </div>
    </div>

    <div class="pcp-composer">
      <div class="pcp-composer-input">
        <textarea
          ref="inputEl"
          v-model="inputText"
          class="pcp-textarea"
          placeholder="Message…"
          rows="1"
          @input="onInput"
          @keydown="onKeydown"
        />
        <div v-if="slashOpen" class="pcp-dropdown">
          <button
            v-for="(c, i) in slashFiltered"
            :key="c.name"
            type="button"
            class="pcp-dropdown-item"
            :class="{ 'pcp-dropdown-item--active': i === slashIndex }"
            @mousedown.prevent="applySlash(c)"
          >
            <span class="pcp-dropdown-name">/{{ c.name }}</span>
            <span class="pcp-dropdown-desc">{{ c.description }}</span>
          </button>
        </div>
        <div v-if="mentionOpen" class="pcp-dropdown">
          <button
            v-for="(f, i) in mentionFiltered"
            :key="f"
            type="button"
            class="pcp-dropdown-item"
            :class="{ 'pcp-dropdown-item--active': i === mentionIndex }"
            @mousedown.prevent="applyMention(f)"
          >
            <span class="pcp-dropdown-name">{{ f.split('/').pop() }}</span>
            <span class="pcp-dropdown-desc">{{ f }}</span>
          </button>
        </div>
      </div>
      <div class="pcp-composer-bar">
        <div class="pcp-composer-actions">
          <button
            type="button"
            class="pcp-composer-action"
            title="Slash commands"
            @mousedown.prevent="insertChar('/')"
          >/</button>
          <button
            type="button"
            class="pcp-composer-action"
            title="Mention a file"
            @mousedown.prevent="insertChar('@')"
          >@</button>
        </div>
        <div class="pcp-composer-right">
          <span class="pcp-send-hint">{{ sendHint }}</span>
          <div class="pcp-send-group">
            <button
              v-if="streaming"
              type="button"
              class="pcp-send pcp-interrupt"
              title="Interrupt"
              @click="onInterrupt"
            >■</button>
            <button
              v-else
              type="button"
              class="pcp-send"
              :disabled="!inputText.trim()"
              @click="() => { const t = inputText.trim(); if (t) sendMessage(t); }"
            >➤</button>
            <button
              type="button"
              class="pcp-send-toggle"
              title="Toggle send shortcut"
              @click="toggleSendMode"
            >▾</button>
          </div>
        </div>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.planning-chat-panel {
  width: 360px;
  min-width: 280px;
  max-width: 50vw;
  display: flex;
  flex-direction: column;
  border-left: 1px solid var(--rule);
  background: var(--bg-card);
}

.pcp-header {
  padding: 8px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--rule);
}

.pcp-title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--ink-3);
}

.pcp-header-actions {
  display: flex;
  gap: 4px;
}

.pcp-clear,
.pcp-fold {
  font-size: 11px;
  padding: 2px 8px;
  background: transparent;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink-3);
  cursor: pointer;
}

.pcp-clear:hover,
.pcp-fold:hover {
  background: var(--bg-hover);
  color: var(--ink);
}

.pcp-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--rule);
  align-items: center;
}

.pcp-tab {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  font-size: 11px;
  background: transparent;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink-3);
  cursor: pointer;
  font-family: inherit;
}

.pcp-tab:hover {
  background: var(--bg-hover);
  color: var(--ink);
}

.pcp-tab--active {
  background: var(--bg-active);
  color: var(--ink);
  border-color: var(--accent);
}

.pcp-tab-rename {
  font-size: 11px;
  padding: 1px 4px;
  border: 1px solid var(--accent);
  background: var(--bg);
  color: var(--ink);
  outline: none;
  width: 120px;
}

.pcp-tab-unread {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
}

.pcp-tab-pencil,
.pcp-tab-close {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--ink-4);
  font-size: 11px;
  padding: 0 2px;
}

.pcp-tab-pencil:hover,
.pcp-tab-close:hover {
  color: var(--ink);
}

.pcp-tab-newwrap {
  position: relative;
  display: inline-flex;
  gap: 2px;
}

.pcp-tab-new,
.pcp-tab-archive-trigger {
  font-size: 14px;
  padding: 0 8px;
  background: transparent;
  border: 1px dashed var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink-3);
  cursor: pointer;
  line-height: 1.5;
}

.pcp-tab-new:hover,
.pcp-tab-archive-trigger:hover {
  background: var(--bg-hover);
  color: var(--ink);
}

.pcp-archived-menu {
  position: absolute;
  top: 100%;
  left: 0;
  margin-top: 4px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  z-index: 10;
  min-width: 180px;
}

.pcp-archived-menu-header {
  padding: 6px 10px;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--ink-3);
  border-bottom: 1px solid var(--rule);
}

.pcp-archived-menu-item {
  display: block;
  width: 100%;
  padding: 6px 10px;
  font-size: 12px;
  text-align: left;
  background: transparent;
  border: none;
  color: var(--ink);
  cursor: pointer;
}

.pcp-archived-menu-item:hover {
  background: var(--bg-hover);
}

.pcp-messages {
  flex: 1;
  overflow-y: auto;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.pcp-empty {
  color: var(--ink-4);
  font-size: 12px;
  text-align: center;
  padding: 20px 10px;
}

.pcp-bubble {
  display: flex;
  gap: 8px;
  max-width: 100%;
  position: relative;
}

.pcp-bubble--reverted {
  opacity: 0.5;
  text-decoration: line-through;
  text-decoration-color: var(--ink-4);
}

.pcp-av {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--bg-sunk);
  font-size: 10px;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--ink-3);
  flex-shrink: 0;
}

.pcp-bubble--user .pcp-av {
  background: var(--accent);
  color: #fff;
}

.pcp-bubble-body {
  flex: 1;
  min-width: 0;
}

.pcp-bubble-meta {
  display: flex;
  gap: 6px;
  align-items: baseline;
  font-size: 10px;
  color: var(--ink-3);
  margin-bottom: 2px;
}

.pcp-bubble-name {
  font-weight: 500;
}

.pcp-bubble-time {
  color: var(--ink-4);
}

.pcp-bubble-content {
  font-size: 12px;
  line-height: 1.55;
  word-break: break-word;
  white-space: pre-wrap;
}

.pcp-bubble--assistant .pcp-bubble-content {
  white-space: normal;
}

.pcp-bubble-content :deep(p) { margin: 0.4em 0; }
.pcp-bubble-content :deep(ul),
.pcp-bubble-content :deep(ol) { margin: 0.4em 0; padding-left: 1.2em; }
.pcp-bubble-content :deep(code) {
  font-family: var(--font-mono);
  font-size: 0.9em;
  background: var(--bg-sunk);
  padding: 0 3px;
  border-radius: 2px;
}
.pcp-bubble-content :deep(pre) {
  font-family: var(--font-mono);
  font-size: 11px;
  background: var(--bg-sunk);
  padding: 8px;
  border-radius: var(--r-sm);
  overflow-x: auto;
}
.pcp-bubble-content :deep(pre code) {
  background: transparent;
  padding: 0;
}

.pcp-bubble-error {
  background: #4d1f1f;
  color: #ffb4b4;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  font-size: 11px;
  margin-bottom: 4px;
}

.pcp-thinking {
  font-size: 14px;
  color: var(--ink-4);
  letter-spacing: 4px;
  display: inline-block;
}

.pcp-thinking span {
  animation: pcp-blink 1.4s infinite both;
}

.pcp-thinking span:nth-child(2) { animation-delay: 0.2s; }
.pcp-thinking span:nth-child(3) { animation-delay: 0.4s; }

@keyframes pcp-blink {
  0%, 80%, 100% { opacity: 0.2; }
  40% { opacity: 1; }
}

.pcp-activity {
  margin-top: 6px;
  font-size: 11px;
}

.pcp-activity summary {
  cursor: pointer;
  color: var(--ink-3);
  user-select: none;
  padding: 2px 0;
}

.pcp-activity-log {
  font-family: var(--font-mono);
  font-size: 10px;
  background: var(--bg-sunk);
  padding: 6px 8px;
  border-radius: var(--r-sm);
  max-height: 300px;
  overflow-y: auto;
  margin: 4px 0 0;
  color: var(--ink-2);
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.pcp-activity-row {
  display: grid;
  grid-template-columns: 14px auto 1fr;
  align-items: baseline;
  gap: 6px;
  line-height: 1.4;
  word-break: break-word;
}

.pcp-activity-icon {
  text-align: center;
}

.pcp-activity-label {
  color: var(--ink-3);
  text-transform: lowercase;
}

.pcp-activity-row--tool .pcp-activity-label { color: var(--accent); }
.pcp-activity-row--tool_result .pcp-activity-label { color: var(--ok); }
.pcp-activity-row--thinking .pcp-activity-label { color: var(--warn); font-style: italic; }

.pcp-activity-summary {
  white-space: pre-wrap;
}

.pcp-activity-detail {
  grid-column: 2 / 4;
  margin-top: 2px;
}

.pcp-activity-detail summary {
  cursor: pointer;
  color: var(--ink-4);
  font-size: 9px;
}

.pcp-activity-detail pre {
  margin: 4px 0 0;
  padding: 6px;
  background: var(--bg);
  border-radius: 3px;
  white-space: pre-wrap;
  font-size: 10px;
}

.pcp-system {
  font-size: 11px;
  color: var(--ink-3);
  background: var(--bg-sunk);
  padding: 6px 10px;
  border-radius: var(--r-sm);
  white-space: pre-wrap;
}

.pcp-interrupted {
  font-size: 10px;
  color: var(--ink-4);
  text-align: center;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  padding: 4px 0;
}

.pcp-bubble-actions {
  position: absolute;
  top: 0;
  right: 0;
}

.pcp-undo {
  background: none;
  border: 1px solid var(--rule);
  border-radius: 50%;
  width: 22px;
  height: 22px;
  font-size: 12px;
  cursor: pointer;
  color: var(--ink-3);
}

.pcp-undo:hover:not(:disabled) {
  color: var(--accent);
  border-color: var(--accent);
}

.pcp-undo:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.pcp-queue {
  display: flex;
  gap: 4px;
  padding: 0 12px;
  flex-wrap: wrap;
}

.pcp-queue-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  background: var(--bg-sunk);
  border: 1px dashed var(--rule);
  border-radius: var(--r-sm);
  font-size: 11px;
  max-width: 240px;
}

.pcp-queue-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pcp-queue-remove {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--ink-4);
  font-size: 12px;
  padding: 0 2px;
}

.pcp-composer {
  padding: 8px 12px 10px;
  border-top: 1px solid var(--rule);
}

.pcp-composer-input {
  position: relative;
}

.pcp-textarea {
  width: 100%;
  font-family: var(--font-sans);
  font-size: 12px;
  padding: 8px 10px;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  background: var(--bg);
  color: var(--ink);
  resize: none;
  line-height: 1.4;
  box-sizing: border-box;
  max-height: 200px;
}

.pcp-textarea:focus {
  outline: none;
  border-color: var(--accent);
}

.pcp-dropdown {
  position: absolute;
  bottom: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 240px;
  overflow-y: auto;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  z-index: 5;
}

.pcp-dropdown-item {
  display: flex;
  width: 100%;
  flex-direction: column;
  padding: 6px 10px;
  background: transparent;
  border: none;
  cursor: pointer;
  text-align: left;
  font-family: inherit;
  color: var(--ink);
}

.pcp-dropdown-item:hover,
.pcp-dropdown-item--active {
  background: var(--bg-hover);
}

.pcp-dropdown-name {
  font-size: 12px;
  font-weight: 500;
}

.pcp-dropdown-desc {
  font-size: 10px;
  color: var(--ink-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.pcp-composer-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 6px;
}

.pcp-composer-actions {
  display: flex;
  gap: 4px;
}

.pcp-composer-action {
  font-size: 12px;
  width: 24px;
  height: 24px;
  background: transparent;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink-3);
  cursor: pointer;
}

.pcp-composer-action:hover {
  background: var(--bg-hover);
  color: var(--ink);
}

.pcp-composer-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.pcp-send-hint {
  font-size: 10px;
  color: var(--ink-4);
}

.pcp-send-group {
  display: inline-flex;
}

.pcp-send,
.pcp-send-toggle {
  font-size: 13px;
  padding: 4px 12px;
  background: var(--accent);
  color: #fff;
  border: none;
  cursor: pointer;
}

.pcp-send {
  border-top-left-radius: var(--r-sm);
  border-bottom-left-radius: var(--r-sm);
}

.pcp-send-toggle {
  border-left: 1px solid rgba(255, 255, 255, 0.2);
  padding: 4px 6px;
  border-top-right-radius: var(--r-sm);
  border-bottom-right-radius: var(--r-sm);
}

.pcp-send:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.pcp-interrupt {
  background: var(--warn, #c87b1c);
}
</style>
