// useChatSession — the planning-chat conversation lifecycle, extracted from
// PlanningChatPanel so multiple surfaces (the dedicated Chat view, the spec-mode
// floating popup, the legacy docked panel) drive identical behaviour from one
// implementation. Owns the rendered message list, streaming, the per-thread send
// queue, thread switching/rename/archive, and per-round undo. Reads and writes
// the planning store; introduces no new persistent state.
//
// A host component calls useChatSession() once in setup and passes the returned
// object to <ChatMessageList :session> and wires <ChatComposer @send @interrupt>
// to its sendMessage/onInterrupt. Session-navigator chrome (tabs, sub-sidebar,
// dropdown) calls the same returned thread actions.

import { ref, computed, onMounted, onUnmounted, nextTick, watch, type Ref, type ComputedRef } from 'vue';
import { storeToRefs } from 'pinia';
import { api, authHeaders } from '../api/client';
import { renderMarkdown } from '../lib/markdown';
import { startStreamingFetch, type StreamingFetchHandle } from './useStreamingFetch';
import { hasActivity, parseActivity } from '../lib/prettyNdjson';
import { enhanceMermaid } from '../lib/mermaidRender';
import { usePlanningStore } from '../stores/planning';
import type { PlanningMessage, PlanningThread } from '../stores/planning';
import {
  type RenderedBubble,
  extractAssistantText,
  extractError,
  bubbleFromMessage,
  applyStreamingUpdate,
} from '../lib/planningBubble';

export interface QueueItem { id: number; text: string }

export interface ChatSession {
  // ── Conversation state ──
  renderedMessages: Ref<RenderedBubble[]>;
  streaming: Ref<boolean>;
  interruptedAt: Ref<number>;
  messagesEl: Ref<HTMLElement | null>;
  userScrolledUp: Ref<boolean>;
  latestRound: ComputedRef<number>;

  // ── Actions ──
  loadHistory: () => Promise<void>;
  sendMessage: (text: string, opts?: { threadID?: string }) => Promise<void>;
  onInterrupt: () => Promise<void>;
  clearHistory: () => Promise<void>;
  appendSystem: (text: string) => void;
  onScroll: () => void;
  undoRound: (bubble: RenderedBubble) => Promise<void>;

  // ── Queue ──
  currentQueue: ComputedRef<QueueItem[]>;
  editingQueueId: Ref<number | null>;
  editQueueDraft: Ref<string>;
  removeFromQueue: (id: number) => void;
  startQueueEdit: (q: QueueItem) => void;
  commitQueueEdit: (id: number) => void;
  cancelQueueEdit: () => void;

  // ── Threads (sessions) ──
  createThread: () => Promise<void>;
  switchToThread: (id: string) => Promise<void>;
  archiveThread: (id: string) => Promise<void>;
  unarchiveThread: (id: string) => Promise<void>;
  renamingId: Ref<string>;
  renameDraft: Ref<string>;
  startRename: (id: string) => void;
  commitRename: () => Promise<void>;
  cancelRename: () => void;
  archiveMenuOpen: Ref<boolean>;
}

export function useChatSession(): ChatSession {
  const planning = usePlanningStore();
  const {
    threads, threadOrder, activeThreadId,
    streaming, streamingThreadId, focusedSpecPath,
  } = storeToRefs(planning);

  const messagesEl = ref<HTMLElement | null>(null);
  const userScrolledUp = ref(false);

  const renderedMessages = ref<RenderedBubble[]>([]);
  const interruptedAt = ref<number>(-1);

  let streamHandle: StreamingFetchHandle | null = null;
  // Monotonic counter for stable streaming-bubble ids (see applyStreamingUpdate).
  let streamBubbleSeq = 0;

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

  function startStreaming() {
    streaming.value = true;
    const bubbleId = 'stream-' + String(++streamBubbleSeq);
    const bubble: RenderedBubble = {
      id: bubbleId,
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
            // Locate the bubble by id, not a cached index: if the active thread
            // changed mid-stream, loadHistory replaced renderedMessages and the
            // streaming bubble is gone — drop the update rather than corrupt an
            // unrelated message.
            applyStreamingUpdate(renderedMessages.value, bubbleId, {
              rawText: text,
              contentHtml: text ? renderMarkdown(text) : '',
              rawOutput: rawBuffer,
              activity,
              hasActivity: hasAct,
              errorText: errorText || undefined,
            });
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
          applyStreamingUpdate(renderedMessages.value, bubbleId, {
            rawText: text,
            contentHtml: text ? renderMarkdown(text) : '',
            rawOutput: rawBuffer,
            activity,
            hasActivity: activity.length > 0,
            errorText: errorText || undefined,
            isStreaming: false,
          });
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
          ...authHeaders(),
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
        return;
      }
      if (!res.ok) {
        appendSystem('Error: ' + (await res.text()));
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
          ...authHeaders(),
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
          ...authHeaders(),
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

  // Inline-edit a queued message (double-click the chip). Enter/blur saves a
  // non-empty value; Escape cancels. Mirrors ui/js/planning-chat.js _editQueueItem.
  const editingQueueId = ref<number | null>(null);
  const editQueueDraft = ref('');
  function startQueueEdit(q: QueueItem) {
    editingQueueId.value = q.id;
    editQueueDraft.value = q.text;
    void nextTick(() => document.querySelector<HTMLInputElement>('.pcp-queue-edit')?.focus());
  }
  function commitQueueEdit(id: number) {
    if (editingQueueId.value !== id) return;
    const t = activeThreadId.value ? threads.value[activeThreadId.value] : null;
    const item = t?.queue.find(q => q.id === id);
    const next = editQueueDraft.value.trim();
    if (item && next) item.text = next;
    editingQueueId.value = null;
  }
  function cancelQueueEdit() {
    editingQueueId.value = null;
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

  // ── Threads (sessions) ─────────────────────────────────────────────

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
    api('PATCH', '/api/planning/threads/' + encodeURIComponent(id), { state: 'active' }).catch(() => {});
    await loadHistory();
  }

  function startRename(id: string) {
    const t = threads.value[id];
    if (!t) return;
    renamingId.value = id;
    renameDraft.value = t.name;
    void nextTick(() => {
      const inp = document.querySelector<HTMLInputElement>('.pcp-tab-rename, .chat-session-rename');
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
    if (typeof confirm === 'function' && !confirm('Archive this thread? You can restore it later.')) return;
    try {
      const res = await fetch('/api/planning/threads/' + encodeURIComponent(id), {
        method: 'PATCH',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          ...authHeaders(),
        },
        body: JSON.stringify({ state: 'archived' }),
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
      await api('PATCH', '/api/planning/threads/' + encodeURIComponent(id), { state: 'visible' });
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
          ...authHeaders(),
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

  return {
    renderedMessages, streaming, interruptedAt, messagesEl, userScrolledUp, latestRound,
    loadHistory, sendMessage, onInterrupt, clearHistory, appendSystem, onScroll, undoRound,
    currentQueue, editingQueueId, editQueueDraft, removeFromQueue, startQueueEdit, commitQueueEdit, cancelQueueEdit,
    createThread, switchToThread, archiveThread, unarchiveThread,
    renamingId, renameDraft, startRename, commitRename, cancelRename, archiveMenuOpen,
  };
}
