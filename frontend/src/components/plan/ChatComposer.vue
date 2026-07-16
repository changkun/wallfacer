<script setup lang="ts">
// ChatComposer — the message input. Self-contained: owns its draft text,
// send-mode preference, and slash/mention autocomplete. Emits `send(text)` and
// `interrupt()` so it stays decoupled from where it's mounted. The `variant`
// prop sizes it for the entry-screen hero, the docked conversation, the legacy
// panel, or the compact spec popup.
import { ref, computed } from 'vue';
import { useAgentAutocomplete } from '../../composables/useAgentAutocomplete';

withDefaults(defineProps<{
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
  // Clear the draft after sending OR queuing. A message queued mid-stream is
  // already committed (it emitted above and shows as a queued chip), so leaving
  // its text in the box reads as "not sent" and invites a duplicate send.
  inputText.value = '';
  autoGrow();
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
        <!-- The send affordance is hidden on an empty draft and springs in once
             there is something to send (Slack-style). Interrupt is exempt: while
             streaming it must always be reachable regardless of draft text. -->
        <Transition name="pcp-send-pop">
          <div v-if="streaming || inputText.trim()" class="pcp-send-wrap">
            <span class="pcp-send-hint">{{ sendHint }}</span>
            <div class="pcp-send-group">
              <button
                v-if="streaming"
                type="button"
                class="pcp-send pcp-interrupt"
                title="Interrupt"
                @click="emit('interrupt')"
              >
                <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="6" y="6" width="12" height="12" rx="2"></rect></svg>
              </button>
              <button
                v-else
                type="button"
                class="pcp-send"
                title="Send"
                @click="doSend"
              >
                <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="12" y1="19" x2="12" y2="5"></line><polyline points="6 11 12 5 18 11"></polyline></svg>
              </button>
              <button
                type="button"
                class="pcp-send-toggle"
                title="Toggle send shortcut"
                @click="toggleSendMode"
              >
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="6 9 12 15 18 9"></polyline></svg>
              </button>
            </div>
          </div>
        </Transition>
      </div>
    </div>
  </div>
</template>

<style scoped src="./ChatComposer.css"></style>
