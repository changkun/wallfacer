<script setup lang="ts">
import { nextTick, ref } from 'vue';
import { useTaskStore } from '../stores/tasks';

const store = useTaskStore();
const prompt = ref('');
const submitting = ref(false);
const expanded = ref(false);
const textareaRef = ref<HTMLTextAreaElement | null>(null);
const modKey = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform) ? '⌘' : 'Ctrl';

async function expand() {
  expanded.value = true;
  await nextTick();
  textareaRef.value?.focus();
}

function collapse() {
  expanded.value = false;
  prompt.value = '';
}

async function submit() {
  const text = prompt.value.trim();
  if (!text || submitting.value) return;
  submitting.value = true;
  try {
    await store.createTask(text);
    prompt.value = '';
    expanded.value = false;
  } catch (e) {
    console.error('create task:', e);
  } finally {
    submitting.value = false;
  }
}

function clear() {
  prompt.value = '';
}

function onKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
    e.preventDefault();
    submit();
    return;
  }
  if (e.key === 'Escape') {
    e.preventDefault();
    collapse();
  }
}
</script>

<template>
  <button
    v-if="!expanded"
    type="button"
    class="composer-add"
    @click="expand"
  >
    + New Task
  </button>
  <form v-else class="composer" @submit.prevent="submit">
    <textarea
      ref="textareaRef"
      v-model="prompt"
      class="composer__prompt"
      :placeholder="`Describe the task… (Markdown supported, ${modKey}↵ to save)`"
      rows="4"
      @keydown="onKeydown"
    />
    <div class="composer__actions">
      <button
        type="button"
        class="composer__btn composer__btn--ghost"
        @click="collapse"
      >
        Cancel
      </button>
      <button
        type="button"
        class="composer__btn composer__btn--ghost"
        :disabled="!prompt.trim() || submitting"
        @click="clear"
      >
        Clear
      </button>
      <button
        type="submit"
        class="composer__btn composer__btn--primary"
        :disabled="!prompt.trim() || submitting"
      >
        {{ submitting ? 'Saving…' : 'Save' }}
      </button>
    </div>
  </form>
</template>
