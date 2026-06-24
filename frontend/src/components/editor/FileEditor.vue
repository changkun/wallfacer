<script setup lang="ts">
// One open file tab's editor pane. The buffer lives in the editorTabs store
// (the source of truth that survives BoardPage unmount), so this component is a
// thin view: it reads `tab.content` and writes edits back via `setContent`.
// The text-editing internals (currently a <textarea>) are deliberately isolated
// here so they can be upgraded (e.g. CodeMirror) without touching the tab shell.
import { computed } from 'vue';
import { useEditorTabsStore } from '../../stores/editorTabs';

const props = defineProps<{ path: string }>();
const tabs = useEditorTabsStore();

const tab = computed(() => tabs.find(props.path));
const dirty = computed(() => tabs.isDirty(props.path));

function onInput(e: Event) {
  tabs.setContent(props.path, (e.target as HTMLTextAreaElement).value);
}

function onSave() {
  void tabs.save(props.path);
}

// Cmd/Ctrl+S saves; Tab inserts two spaces rather than leaving the field.
function onKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
    e.preventDefault();
    onSave();
    return;
  }
  if (e.key === 'Tab') {
    e.preventDefault();
    const ta = e.target as HTMLTextAreaElement;
    const start = ta.selectionStart ?? 0;
    const end = ta.selectionEnd ?? 0;
    const next = (tab.value?.content ?? '').slice(0, start) + '  ' + (tab.value?.content ?? '').slice(end);
    tabs.setContent(props.path, next);
    requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = start + 2; });
  }
}
</script>

<template>
  <section v-if="tab" class="file-editor">
    <div class="file-editor__toolbar">
      <span class="file-editor__path" :title="tab.path">{{ tab.path }}</span>
      <span class="file-editor__spacer" />
      <span v-if="tab.saveError" class="file-editor__error" :title="tab.saveError">save failed</span>
      <button
        type="button"
        class="file-editor__save"
        :disabled="tab.saving || !dirty"
        :title="dirty ? 'Save (Cmd/Ctrl+S)' : 'No unsaved changes'"
        @click="onSave"
      >{{ tab.saving ? 'Saving…' : 'Save' }}</button>
    </div>
    <div class="file-editor__body">
      <div v-if="tab.loading" class="file-editor__placeholder">Loading…</div>
      <div
        v-else-if="tab.loadError"
        class="file-editor__placeholder file-editor__placeholder--error"
      >{{ tab.loadError }}</div>
      <textarea
        v-else
        class="file-editor__textarea"
        spellcheck="false"
        autocomplete="off"
        autocapitalize="off"
        :value="tab.content"
        @input="onInput"
        @keydown="onKeydown"
      ></textarea>
    </div>
  </section>
</template>

<style scoped>
.file-editor {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg);
}

.file-editor__toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 12px;
  border-bottom: 1px solid var(--rule);
  background: color-mix(in oklab, var(--bg) 92%, var(--bg-card));
  font-size: 12px;
}

.file-editor__path {
  font-family: var(--font-mono);
  color: var(--ink-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.file-editor__spacer {
  flex: 1;
}

.file-editor__error {
  color: var(--danger, #e5534b);
  font-size: 11px;
}

.file-editor__save {
  font-size: 12px;
  font-weight: 500;
  padding: 3px 12px;
  border: 1px solid var(--rule);
  border-radius: var(--r-md, 6px);
  background: var(--accent);
  color: #fff;
  cursor: pointer;
}
.file-editor__save:disabled {
  opacity: 0.5;
  cursor: default;
  background: var(--bg-card);
  color: var(--ink-3);
}

.file-editor__body {
  flex: 1;
  min-height: 0;
  display: flex;
}

.file-editor__placeholder {
  margin: auto;
  color: var(--ink-4);
  font-size: 13px;
}
.file-editor__placeholder--error {
  color: var(--danger, #e5534b);
}

.file-editor__textarea {
  flex: 1;
  min-height: 0;
  width: 100%;
  resize: none;
  border: none;
  outline: none;
  padding: 12px 16px;
  background: var(--bg);
  color: var(--ink);
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 1.6;
  tab-size: 2;
  white-space: pre;
  overflow: auto;
}
</style>
