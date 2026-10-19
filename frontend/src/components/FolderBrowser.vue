<script setup lang="ts">
// The server-side folder browser shared by the workspace picker wizard and the
// workspace editor. The parent owns the browse state (a useFolderBrowser
// instance) and the chosen folder; this component renders the path field, the
// breadcrumb, the toolbar and the entry rows, and emits the folder chosen. A
// workspace is one folder, so choosing replaces the previous choice.
import { computed } from 'vue';
import type { useFolderBrowser, BrowseEntry } from '../composables/useFolderBrowser';

const props = defineProps<{
  browser: ReturnType<typeof useFolderBrowser>;
  /** The chosen folder, or '' for none; its row shows "chosen" instead of Choose. */
  selected: string;
  /** Blocks Choose while the parent persists a change. */
  busy?: boolean;
  /** Shows the rename control on each entry (the wizard only). */
  renamable?: boolean;
}>();
const emit = defineEmits<{
  choose: [path: string];
  rename: [entry: BrowseEntry];
}>();

const b = props.browser;

const entries = computed<BrowseEntry[]>(() => {
  let list = b.browseEntries.value;
  if (!b.showHidden.value) list = list.filter((e) => !e.name.startsWith('.'));
  const f = b.filter.value.trim().toLowerCase();
  if (f) list = list.filter((e) => e.name.toLowerCase().includes(f));
  return list;
});
const currentChosen = computed(() => props.selected !== '' && props.selected === b.browsePath.value);
</script>

<template>
  <div class="fb">
    <div class="fb__path">
      <input
        v-model="b.pathInput.value"
        class="field mono fb__path-input ws-picker__path-input"
        type="text"
        placeholder="/absolute/path"
        autocomplete="off"
        @keydown="b.onPathKeydown"
      />
      <button type="button" class="btn sm ghost ws-picker__go-btn" @click="b.goToPath">Go</button>
    </div>

    <nav class="fb__crumb ws-picker__breadcrumb" aria-label="Current folder">
      <template v-for="(seg, i) in b.breadcrumbSegments()" :key="seg.path">
        <!-- The first segment is the root "/" itself, so the separator starts after it. -->
        <span v-if="i > 1" class="fb__crumb-sep" aria-hidden="true">/</span>
        <button
          type="button"
          class="fb__crumb-seg"
          :class="{ 'fb__crumb-seg--leaf': i === b.breadcrumbSegments().length - 1 }"
          @click="b.browse(seg.path)"
        >{{ seg.label }}</button>
      </template>
    </nav>

    <div class="fb__toolbar ws-picker__browser-toolbar">
      <label class="fb__toggle ws-picker__toggle">
        <input v-model="b.showHidden.value" type="checkbox" />
        Show hidden
      </label>
      <span class="fb__status ws-picker__status">
        <span v-if="b.browseLoading.value">Loading...</span>
        <span v-else-if="b.browseError.value" class="fb__error">{{ b.browseError.value }}</span>
      </span>
      <button
        type="button"
        class="btn sm ws-picker__choose-current-btn"
        :disabled="currentChosen || busy"
        @click="emit('choose', b.browsePath.value)"
      >Use this folder</button>
    </div>

    <div class="fb__list ws-picker__list">
      <div class="fb__filter ws-picker__filter-wrap">
        <input
          v-model="b.filter.value"
          class="field fb__filter-input ws-picker__filter"
          type="search"
          placeholder="Filter..."
          autocomplete="off"
        />
      </div>
      <button
        v-if="b.browsePath.value !== '/'"
        type="button"
        class="fb__row fb__row--parent ws-entry--parent"
        @click="b.navigateUp"
      >
        <span class="fb__chev fb__chev--up" aria-hidden="true"></span>
        <span>..</span>
      </button>
      <div
        v-for="entry in entries"
        :key="entry.path"
        class="fb__row ws-entry"
        :class="{ 'fb__row--chosen': selected === entry.path }"
      >
        <button
          type="button"
          class="fb__name ws-entry__name"
          :title="entry.path"
          @click="b.navigateInto(entry)"
        >
          <span class="fb__chev" aria-hidden="true"></span>
          <span class="fb__label">{{ entry.name }}</span>
          <span v-if="entry.is_git_repo" class="pill pill-neutral fb__git ws-entry__badge">git</span>
        </button>
        <button
          v-if="renamable"
          type="button"
          class="icon-btn sm fb__rename ws-entry__rename"
          title="Rename folder"
          aria-label="Rename folder"
          @click="emit('rename', entry)"
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 20h9"></path><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"></path></svg>
        </button>
        <button
          v-if="selected !== entry.path"
          type="button"
          class="btn sm ghost fb__choose ws-entry__choose"
          :disabled="busy"
          @click="emit('choose', entry.path)"
        >Choose</button>
        <span v-else class="fb__chosen ws-entry__chosen">chosen</span>
      </div>
      <div
        v-if="!b.browseLoading.value && entries.length === 0 && b.browsePath.value !== '/'"
        class="fb__empty"
      >{{ b.filter.value.trim() ? 'No matches.' : 'Empty.' }}</div>
    </div>
  </div>
</template>
