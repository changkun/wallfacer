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
      class="composer-input"
      placeholder="Describe a task..."
      rows="3"
      @keydown="onKeydown"
    />
    <div class="composer-footer">
      <span class="composer-hint">{{ modKey }}+Enter to submit</span>
      <button type="submit" class="composer-btn" :disabled="!prompt.trim() || submitting">
        {{ submitting ? 'Creating...' : 'Create Task' }}
      </button>
    </div>
  </form>
</template>

<style scoped>
.composer {
  padding: 8px;
  background: var(--bg-elevated);
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  margin: 6px;
}
.composer-input {
  width: 100%;
  border: none;
  background: transparent;
  color: var(--ink);
  font-family: var(--font-sans);
  font-size: 12px;
  resize: vertical;
  outline: none;
  line-height: 1.5;
}
.composer-input::placeholder {
  color: var(--ink-4);
}
.composer-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 6px;
}
.composer-hint {
  font-size: 10px;
  color: var(--ink-4);
}
.composer-btn {
  padding: 4px 12px;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: var(--r-sm);
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
}
.composer-btn:hover { background: var(--accent-2); }
.composer-btn:disabled { opacity: 0.4; cursor: default; }
</style>
