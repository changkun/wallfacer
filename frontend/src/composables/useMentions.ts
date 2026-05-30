// Reactive @-mention file autocomplete for a <textarea>. Wraps the pure
// helpers in lib/mentions.ts with dropdown state + keyboard handling. The
// caller owns the textarea value (v-model) and passes a setValue callback.
import { ref, nextTick } from 'vue';
import { api } from '../api/client';
import { mentionQueryAt, filterMentionFiles, applyMention } from '../lib/mentions';

export function useMentions(opts: { setValue: (v: string) => void; priorityPrefix?: string }) {
  const open = ref(false);
  const items = ref<string[]>([]);
  const activeIndex = ref(0);

  let allFiles: string[] = [];
  let loaded = false;
  let atIdx = -1;
  let caretPos = -1;

  async function ensureFiles() {
    if (loaded) return;
    loaded = true;
    try {
      const res = await api<{ files: string[] }>('GET', '/api/files');
      allFiles = res?.files ?? [];
    } catch {
      allFiles = [];
    }
  }

  function close() {
    open.value = false;
    activeIndex.value = 0;
  }

  async function onInput(el: HTMLTextAreaElement) {
    const caret = el.selectionStart ?? el.value.length;
    const m = mentionQueryAt(el.value, caret);
    if (!m) { close(); return; }
    await ensureFiles();
    items.value = filterMentionFiles(allFiles, m.query, opts.priorityPrefix);
    atIdx = m.atIdx;
    caretPos = caret;
    activeIndex.value = 0;
    open.value = items.value.length > 0;
  }

  function choose(el: HTMLTextAreaElement, file: string) {
    const { text, caret } = applyMention(el.value, atIdx, caretPos, file);
    opts.setValue(text);
    close();
    nextTick(() => {
      el.focus();
      el.setSelectionRange(caret, caret);
    });
  }

  // Returns true when the keystroke was consumed by the dropdown.
  function onKeydown(e: KeyboardEvent, el: HTMLTextAreaElement): boolean {
    if (!open.value || items.value.length === 0) return false;
    switch (e.key) {
      case 'ArrowDown':
        activeIndex.value = (activeIndex.value + 1) % items.value.length;
        e.preventDefault();
        return true;
      case 'ArrowUp':
        activeIndex.value = (activeIndex.value - 1 + items.value.length) % items.value.length;
        e.preventDefault();
        return true;
      case 'Enter':
      case 'Tab':
        choose(el, items.value[activeIndex.value]);
        e.preventDefault();
        return true;
      case 'Escape':
        close();
        e.preventDefault();
        return true;
      default:
        return false;
    }
  }

  return { open, items, activeIndex, onInput, onKeydown, choose, close };
}
