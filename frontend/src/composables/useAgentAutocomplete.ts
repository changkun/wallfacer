// Slash command + @-file autocomplete for the agent-session chat composer.
// Extracted from AgentChatPanel.vue so the bulky state + keyboard
// matrix lives in one place; the SFC just wires events.
//
// Returned `handleKeydown` returns `true` when the autocomplete consumed
// the event (and called preventDefault). Callers should bail before
// running their own Enter / send-mode logic in that case.
import { ref, type Ref } from 'vue';
import { api } from '../api/client';
import { mentionQueryAt, filterMentionFiles } from '../lib/mentions';

interface SlashCommand { name: string; description?: string }

export interface AgentAutocomplete {
  slashOpen: Ref<boolean>;
  slashFiltered: Ref<SlashCommand[]>;
  slashIndex: Ref<number>;
  mentionOpen: Ref<boolean>;
  mentionFiltered: Ref<string[]>;
  mentionIndex: Ref<number>;
  onInput(): Promise<void>;
  handleKeydown(ev: KeyboardEvent): boolean;
  applySlash(cmd: SlashCommand): void;
  applyMention(file: string): void;
  insertChar(ch: '/' | '@'): void;
  autoGrow(): void;
}

export function useAgentAutocomplete(opts: {
  inputEl: Ref<HTMLTextAreaElement | null>;
  inputText: Ref<string>;
}): AgentAutocomplete {
  const { inputEl, inputText } = opts;

  const slashOpen = ref(false);
  const slashItems = ref<SlashCommand[]>([]);
  const slashFiltered = ref<SlashCommand[]>([]);
  const slashIndex = ref(0);
  const slashStart = ref(-1);

  const mentionOpen = ref(false);
  const mentionItems = ref<string[]>([]);
  const mentionFiltered = ref<string[]>([]);
  const mentionIndex = ref(0);
  const mentionStart = ref(-1);

  let commandsCache: SlashCommand[] | null = null;
  async function fetchCommands(): Promise<SlashCommand[]> {
    if (commandsCache) return commandsCache;
    try {
      commandsCache = await api<SlashCommand[]>('GET', '/api/agent/commands');
      return commandsCache ?? [];
    } catch {
      return [];
    }
  }

  let filesCache: string[] | null = null;
  async function fetchFiles(): Promise<string[]> {
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

    // Mention detection + ranking via the shared lib/mentions helpers, so the
    // agent composer ranks identically to the other @-mention surface
    // (useMentions) instead of an unranked substring match.
    const mq = mentionQueryAt(value, pos);
    if (mq) {
      mentionStart.value = mq.atIdx;
      if (mentionItems.value.length === 0) mentionItems.value = await fetchFiles();
      mentionFiltered.value = filterMentionFiles(mentionItems.value, mq.query);
      mentionIndex.value = 0;
      mentionOpen.value = mentionFiltered.value.length > 0;
      return;
    }
    mentionOpen.value = false;
  }

  function applySlash(cmd: SlashCommand) {
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

  function handleKeydown(ev: KeyboardEvent): boolean {
    if (slashOpen.value) {
      if (ev.key === 'ArrowDown') {
        ev.preventDefault();
        slashIndex.value = (slashIndex.value + 1) % slashFiltered.value.length;
        return true;
      }
      if (ev.key === 'ArrowUp') {
        ev.preventDefault();
        slashIndex.value = (slashIndex.value - 1 + slashFiltered.value.length) % slashFiltered.value.length;
        return true;
      }
      if (ev.key === 'Enter' || ev.key === 'Tab') {
        ev.preventDefault();
        const c = slashFiltered.value[slashIndex.value];
        if (c) applySlash(c);
        return true;
      }
      if (ev.key === 'Escape') {
        ev.preventDefault();
        slashOpen.value = false;
        return true;
      }
    }
    if (mentionOpen.value) {
      if (ev.key === 'ArrowDown') {
        ev.preventDefault();
        mentionIndex.value = (mentionIndex.value + 1) % mentionFiltered.value.length;
        return true;
      }
      if (ev.key === 'ArrowUp') {
        ev.preventDefault();
        mentionIndex.value = (mentionIndex.value - 1 + mentionFiltered.value.length) % mentionFiltered.value.length;
        return true;
      }
      if (ev.key === 'Enter' || ev.key === 'Tab') {
        ev.preventDefault();
        const f = mentionFiltered.value[mentionIndex.value];
        if (f) applyMention(f);
        return true;
      }
      if (ev.key === 'Escape') {
        ev.preventDefault();
        mentionOpen.value = false;
        return true;
      }
    }
    return false;
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

  return {
    slashOpen,
    slashFiltered,
    slashIndex,
    mentionOpen,
    mentionFiltered,
    mentionIndex,
    onInput,
    handleKeydown,
    applySlash,
    applyMention,
    insertChar,
    autoGrow,
  };
}
