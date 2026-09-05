<script setup lang="ts">
// One open file tab's editor pane, backed by CodeMirror 6. The buffer lives in
// the editorTabs store (the source of truth that survives BoardPage unmount),
// so this component is a thin view: the store seeds the initial doc, edits flow
// back via setContent, and async loads / external changes are pushed into the
// view. CodeMirror is constructed in onMounted only, so vite-ssg's prerender
// (which has no DOM) never touches it.
import { onMounted, onBeforeUnmount, ref, shallowRef, computed, watch } from 'vue';
import { EditorView, keymap } from '@codemirror/view';
import { EditorState, Compartment } from '@codemirror/state';
import { basicSetup } from 'codemirror';
import { indentWithTab } from '@codemirror/commands';
import { LanguageDescription } from '@codemirror/language';
import { languages } from '@codemirror/language-data';
import { consoleTheme } from '../../lib/editorTheme';
import { useEditorTabsStore } from '../../stores/editorTabs';

const props = defineProps<{ path: string }>();
const tabs = useEditorTabsStore();

const tab = computed(() => tabs.find(props.path));
const dirty = computed(() => tabs.isDirty(props.path));

const host = ref<HTMLElement | null>(null);
const view = shallowRef<EditorView | null>(null);
const languageConf = new Compartment();
const themeConf = new Compartment();
// True while we push store→view, so the update listener doesn't echo back into
// the store (which would clobber the dirty baseline).
let applyingExternal = false;
let themeObserver: MutationObserver | null = null;

// The app writes the resolved theme to <html data-theme>; the console theme
// reads tokens, so only the dark flag (CodeMirror's own light/dark class)
// needs to follow it.
function activeTheme() {
  return consoleTheme(document.documentElement.getAttribute('data-theme') === 'dark');
}

function onSave() {
  void tabs.save(props.path);
}

// Lazily load the language for this filename (keeps grammars out of the main
// bundle). Plain text when the extension is unknown.
async function loadLanguage(path: string) {
  const desc = LanguageDescription.matchFilename(languages, path.split('/').pop() || path);
  if (!desc) return;
  try {
    const support = await desc.load();
    view.value?.dispatch({ effects: languageConf.reconfigure(support) });
  } catch {
    /* unknown / failed grammar: leave as plain text */
  }
}

onMounted(() => {
  if (!host.value) return;
  const state = EditorState.create({
    doc: tab.value?.content ?? '',
    extensions: [
      basicSetup,
      keymap.of([
        { key: 'Mod-s', preventDefault: true, run: () => { onSave(); return true; } },
        indentWithTab,
      ]),
      languageConf.of([]),
      themeConf.of(activeTheme()),
      EditorView.updateListener.of((u) => {
        if (u.docChanged && !applyingExternal) {
          tabs.setContent(props.path, u.state.doc.toString());
        }
      }),
    ],
  });
  view.value = new EditorView({ state, parent: host.value });
  void loadLanguage(props.path);

  themeObserver = new MutationObserver(() => {
    view.value?.dispatch({ effects: themeConf.reconfigure(activeTheme()) });
  });
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme', 'data-palette'],
  });
});

// Push external content changes (async load completing, or a future reload)
// into the view without echoing back through the update listener.
watch(
  () => tab.value?.content,
  (next) => {
    const v = view.value;
    if (!v || next == null || next === v.state.doc.toString()) return;
    applyingExternal = true;
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: next } });
    applyingExternal = false;
  },
);

onBeforeUnmount(() => {
  themeObserver?.disconnect();
  view.value?.destroy();
});
</script>

<template>
  <section v-if="tab" class="file-editor">
    <div class="file-editor__toolbar">
      <span class="file-editor__path" :title="tab.path">{{ tab.path }}</span>
      <span class="file-editor__spacer" />
      <span v-if="tab.saveError" class="file-editor__error" :title="tab.saveError">save failed</span>
      <button
        type="button"
        class="btn sm file-editor__save"
        :disabled="tab.saving || !dirty"
        :title="dirty ? 'Save (Cmd/Ctrl+S)' : 'No unsaved changes'"
        @click="onSave"
      >{{ tab.saving ? 'Saving…' : 'Save' }}</button>
    </div>
    <div class="file-editor__body">
      <div ref="host" class="file-editor__cm"></div>
      <div v-if="tab.loading" class="file-editor__overlay">Loading…</div>
      <div
        v-else-if="tab.loadError"
        class="file-editor__overlay file-editor__overlay--error"
      >{{ tab.loadError }}</div>
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
  height: 36px;
  padding: 0 12px;
  border-bottom: 1px solid var(--rule);
  background: var(--bg-sunk);
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
  color: var(--err);
  font-size: var(--fs-10);
}
.file-editor__body {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
}
.file-editor__cm {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.file-editor__cm :deep(.cm-editor) {
  height: 100%;
}
.file-editor__cm :deep(.cm-editor.cm-focused) {
  outline: none;
}
.file-editor__overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg);
  color: var(--ink-4);
  font-size: var(--fs-base);
}
.file-editor__overlay--error {
  color: var(--err);
}
</style>
