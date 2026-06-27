<script setup lang="ts">
// ChatComposer — the message input. Self-contained: owns its draft text,
// send-mode preference, and slash/mention autocomplete. Emits `send(text)` and
// `interrupt()` so it stays decoupled from where it's mounted. The `variant`
// prop sizes it for the entry-screen hero, the docked conversation, the legacy
// panel, or the compact spec popup.
import { ref, computed } from 'vue';
import { useAgentAutocomplete } from '../../composables/useAgentAutocomplete';

const props = withDefaults(defineProps<{
  streaming: boolean;
  variant?: 'panel' | 'hero' | 'docked' | 'compact';
  placeholder?: string;
}>(), {
  variant: 'panel',
  placeholder: 'Message…',
});

const emit = defineEmits<{ send: [text: string]; interrupt: [] }>();

const inputEl = ref<HTMLTextAreaElement | null>(null);
const inputText = ref<string>('');

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

const autocomplete = useAgentAutocomplete({ inputEl, inputText });
const {
  slashOpen, slashFiltered, slashIndex,
  mentionOpen, mentionFiltered, mentionIndex,
  onInput, applySlash, applyMention, insertChar, autoGrow,
} = autocomplete;

function doSend() {
  const text = inputText.value.trim();
  if (!text) return;
  emit('send', text);
  // Mirror the legacy panel: when a message is queued mid-stream, leave the
  // draft in place; otherwise clear and collapse the textarea.
  if (!props.streaming) {
    inputText.value = '';
    autoGrow();
  }
}

function onKeydown(ev: KeyboardEvent) {
  if (autocomplete.handleKeydown(ev)) return;

  if (ev.key === 'Enter') {
    let shouldSend = false;
    if (sendMode.value === 'cmd-enter') {
      shouldSend = ev.metaKey || ev.ctrlKey;
    } else {
      shouldSend = !ev.shiftKey || ev.metaKey || ev.ctrlKey;
    }
    if (shouldSend) {
      ev.preventDefault();
      doSend();
    }
  }
}

defineExpose({
  setText(t: string) {
    inputText.value = t;
    void autoGrow();
    inputEl.value?.focus();
  },
  focus() {
    inputEl.value?.focus();
  },
});
</script>

<template>
  <div class="pcp-composer" :class="'pcp-composer--' + variant">
    <div class="pcp-composer-input">
      <textarea
        ref="inputEl"
        v-model="inputText"
        class="pcp-textarea"
        :placeholder="placeholder"
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
            @click="emit('interrupt')"
          >■</button>
          <button
            v-else
            type="button"
            class="pcp-send"
            :disabled="!inputText.trim()"
            @click="doSend"
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
</template>

<style scoped src="./ChatComposer.css"></style>
