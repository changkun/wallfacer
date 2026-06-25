<script setup lang="ts">
// VS Code-style tab strip in the board's top-bar spacer. The first tab is the
// pinned Board (which surfaces in-progress / waiting task status); the rest are
// open files from the editorTabs store. Single-click focuses; double-click pins
// a preview tab; the active file tab closes via its ×, middle-click, or
// Cmd/Ctrl+W.
import { onMounted, onUnmounted } from 'vue';
import { useEditorTabsStore, BOARD_TAB_ID, type FileTab } from '../../stores/editorTabs';
import { useTaskStore } from '../../stores/tasks';
import { fileIcon } from '../../lib/fileIcon';

const tabs = useEditorTabsStore();
const store = useTaskStore();

function iconFor(tab: FileTab) {
  return fileIcon(tab.name, false);
}

function closeTab(e: Event, path: string) {
  e.stopPropagation();
  void tabs.close(path);
}

function onAuxClick(e: MouseEvent, path: string) {
  if (e.button === 1) { // middle-click closes
    e.preventDefault();
    void tabs.close(path);
  }
}

// Cmd/Ctrl+W closes the active file tab (the board tab is pinned).
function onKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'w' && tabs.activeId !== BOARD_TAB_ID) {
    e.preventDefault();
    void tabs.close(tabs.activeId);
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown));
onUnmounted(() => window.removeEventListener('keydown', onKeydown));
</script>

<template>
  <div class="editor-tabs" role="tablist" aria-label="Open editors">
    <button
      type="button"
      class="editor-tab editor-tab--board"
      :class="{ 'editor-tab--active': tabs.activeId === BOARD_TAB_ID }"
      role="tab"
      :aria-selected="tabs.activeId === BOARD_TAB_ID"
      title="Board"
      @click="tabs.focus(BOARD_TAB_ID)"
    >
      <svg
        class="editor-tab__icon"
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <rect x="3" y="3" width="18" height="18" rx="2"></rect>
        <line x1="9" y1="3" x2="9" y2="21"></line>
        <line x1="15" y1="3" x2="15" y2="21"></line>
      </svg>
      <span class="editor-tab__label">Board</span>
      <span
        v-if="store.inProgress.length"
        class="editor-tab__spinner"
        :title="store.inProgress.length + ' task(s) in progress'"
        aria-label="tasks in progress"
      ></span>
      <span
        v-if="store.waiting.length"
        class="editor-tab__wait-dot"
        :title="store.waiting.length + ' task(s) waiting for feedback'"
        aria-label="tasks waiting for feedback"
      ></span>
    </button>

    <button
      v-for="tab in tabs.tabs"
      :key="tab.path"
      type="button"
      class="editor-tab editor-tab--file"
      :class="{
        'editor-tab--active': tabs.activeId === tab.path,
        'editor-tab--dirty': tabs.isDirty(tab.path),
        'editor-tab--preview': tab.preview,
      }"
      role="tab"
      :aria-selected="tabs.activeId === tab.path"
      :title="tab.path"
      @click="tabs.focus(tab.path)"
      @dblclick="tabs.promote(tab.path)"
      @auxclick="onAuxClick($event, tab.path)"
    >
      <svg
        class="editor-tab__icon"
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        :stroke="iconFor(tab).color"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
        v-html="iconFor(tab).paths"
      ></svg>
      <span class="editor-tab__label">{{ tabs.labelFor(tab) }}</span>
      <span
        class="editor-tab__close"
        role="button"
        aria-label="Close tab"
        title="Close (Cmd/Ctrl+W)"
        @click="closeTab($event, tab.path)"
      >
        <span class="editor-tab__dot" aria-hidden="true"></span>
        <span class="editor-tab__x" aria-hidden="true">×</span>
      </span>
    </button>
  </div>
</template>

<style scoped>
.editor-tabs {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
  align-items: stretch;
  /* Fill the header band and bleed past its vertical padding so tabs read as a
     VS Code tab bar rather than floating pills. */
  height: calc(100% + 12px);
  margin: -6px 0;
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: none;
}
.editor-tabs::-webkit-scrollbar {
  display: none;
}

.editor-tab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex: 0 0 auto;
  max-width: 180px;
  height: 100%;
  padding: 0 8px 0 11px;
  border: none;
  border-right: 1px solid var(--border);
  background: transparent;
  color: var(--text-muted, var(--ink-3));
  font-size: 12.5px;
  line-height: 1;
  cursor: pointer;
  white-space: nowrap;
}
.editor-tab:first-child {
  border-left: 1px solid var(--border);
}
.editor-tab:hover {
  background: color-mix(in oklab, var(--bg) 55%, var(--bg-card));
  color: var(--ink-2, var(--text-secondary));
}

/* Active tab matches the editor surface and grows a top accent rule — the
   signature VS Code "this tab owns the pane below" cue. */
.editor-tab--active {
  background: var(--bg);
  color: var(--ink, var(--text));
}
.editor-tab--active::before {
  content: "";
  position: absolute;
  left: 0;
  right: 0;
  top: 0;
  height: 2px;
  background: var(--accent);
}

/* Preview (temporary) tabs read in italics until promoted. */
.editor-tab--preview .editor-tab__label {
  font-style: italic;
}

.editor-tab__icon {
  flex: 0 0 auto;
}

.editor-tab__label {
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Board task-status cues. */
.editor-tab__spinner {
  flex: 0 0 auto;
  width: 11px;
  height: 11px;
  border: 2px solid color-mix(in oklab, var(--accent) 28%, transparent);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: editor-tab-spin 0.7s linear infinite;
}
.editor-tab__wait-dot {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--col-waiting, #e0a52e);
  box-shadow: 0 0 0 2px color-mix(in oklab, var(--col-waiting, #e0a52e) 22%, transparent);
}
@keyframes editor-tab-spin {
  to { transform: rotate(360deg); }
}
@media (prefers-reduced-motion: reduce) {
  .editor-tab__spinner { animation: none; }
}

.editor-tab__close {
  position: relative;
  flex: 0 0 auto;
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  color: var(--ink-3, var(--text-muted));
}
.editor-tab__close:hover {
  background: color-mix(in oklab, var(--ink, #000) 12%, transparent);
  color: var(--ink, var(--text));
}

.editor-tab__x {
  font-size: 15px;
  line-height: 1;
}
.editor-tab__dot {
  position: absolute;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: currentColor;
  display: none;
}
.editor-tab--dirty .editor-tab__dot {
  display: block;
}
.editor-tab--dirty .editor-tab__x {
  display: none;
}
.editor-tab--file:not(.editor-tab--dirty) .editor-tab__x {
  opacity: 0;
}
.editor-tab--file:hover .editor-tab__x,
.editor-tab--active .editor-tab__x {
  display: inline;
  opacity: 1;
}
.editor-tab--file:hover.editor-tab--dirty .editor-tab__dot {
  display: none;
}
</style>
