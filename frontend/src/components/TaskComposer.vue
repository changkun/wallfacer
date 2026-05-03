<script setup lang="ts">
import { ref } from 'vue';
import { useTaskStore } from '../stores/tasks';

const store = useTaskStore();
const prompt = ref('');
const submitting = ref(false);
const modKey = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform) ? '⌘' : 'Ctrl';

async function submit() {
  const text = prompt.value.trim();
  if (!text || submitting.value) return;
  submitting.value = true;
  try {
    await store.createTask(text);
    prompt.value = '';
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
  }
}
</script>

<template>
  <form class="composer" @submit.prevent="submit">
    <textarea
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
