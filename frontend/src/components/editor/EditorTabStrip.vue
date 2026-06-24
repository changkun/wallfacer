<script setup lang="ts">
// VS Code-style tab strip that lives in the board's top-bar spacer. The first
// tab is the pinned Board; the rest are open files from the editorTabs store.
// Clicking a tab focuses it (swapping the board's center pane); the active file
// tab can be closed via its ×, middle-click, or Cmd/Ctrl+W.
import { onMounted, onUnmounted } from 'vue';
import { useEditorTabsStore, BOARD_TAB_ID } from '../../stores/editorTabs';

const tabs = useEditorTabsStore();

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

// Cmd/Ctrl+W closes the active file tab (the board tab is pinned). Capture phase
// so it wins over the browser's own close-tab where the embedder allows it.
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
        width="13"
        height="13"
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
    </button>

    <button
      v-for="tab in tabs.tabs"
      :key="tab.path"
      type="button"
      class="editor-tab"
      :class="{
        'editor-tab--active': tabs.activeId === tab.path,
        'editor-tab--dirty': tabs.isDirty(tab.path),
      }"
      role="tab"
      :aria-selected="tabs.activeId === tab.path"
      :title="tab.path"
      @click="tabs.focus(tab.path)"
      @auxclick="onAuxClick($event, tab.path)"
    >
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
  gap: 2px;
  overflow-x: auto;
  scrollbar-width: none;
  height: 100%;
}
.editor-tabs::-webkit-scrollbar {
  display: none;
}

.editor-tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
  max-width: 200px;
  padding: 4px 8px 4px 10px;
  border: 1px solid transparent;
  border-bottom: none;
  border-radius: 6px 6px 0 0;
  background: transparent;
  color: var(--ink-3);
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
  align-self: flex-end;
}
.editor-tab:hover {
  background: var(--bg-hover);
  color: var(--ink-2);
}
.editor-tab--active {
  background: var(--bg);
  color: var(--ink);
  border-color: var(--rule);
}

.editor-tab__icon {
  flex: 0 0 auto;
}

.editor-tab__label {
  overflow: hidden;
  text-overflow: ellipsis;
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
  color: var(--ink-3);
}
.editor-tab__close:hover {
  background: var(--bg-sunk, rgba(127, 127, 127, 0.18));
  color: var(--ink);
}

/* A dirty tab shows a dot at rest and swaps to the × on hover, matching VS Code. */
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
.editor-tab:not(.editor-tab--dirty) .editor-tab__x {
  opacity: 0;
}
.editor-tab:hover .editor-tab__x,
.editor-tab--active .editor-tab__x {
  display: inline;
  opacity: 1;
}
.editor-tab:hover.editor-tab--dirty .editor-tab__dot {
  display: none;
}
</style>
