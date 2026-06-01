<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue';
import { filterTemplates } from '../lib/templateFilter';
import type { PromptTemplate } from '../api/types';

// Searchable anchored template picker: a trigger button opens a dropdown with
// a live name/body filter, one-line body previews, auto-focused search, and
// Esc / outside-click to close. Mirrors ui/js/templates.js openTemplatesPicker.
const props = defineProps<{ templates: PromptTemplate[] }>();
const emit = defineEmits<{ select: [body: string] }>();

const open = ref(false);
const query = ref('');
const root = ref<HTMLElement | null>(null);
const searchInput = ref<HTMLInputElement | null>(null);

const filtered = computed(() => filterTemplates(props.templates, query.value));

async function toggle() {
  open.value = !open.value;
  if (open.value) {
    query.value = '';
    await nextTick();
    searchInput.value?.focus();
  }
}
function choose(t: PromptTemplate) {
  emit('select', t.body);
  open.value = false;
}
function onDocClick(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false;
}
function onEsc(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value) { open.value = false; }
}
onMounted(() => { document.addEventListener('mousedown', onDocClick, true); });
onUnmounted(() => { document.removeEventListener('mousedown', onDocClick, true); });

function preview(body: string): string {
  return body.replace(/\n/g, ' ');
}
</script>

<template>
  <div ref="root" class="tmpl-picker" :class="{ open }">
    <button type="button" class="composer__more" title="Insert a saved template" @click="toggle">Insert…</button>
    <div v-show="open" class="tmpl-picker__dropdown" @keydown="onEsc">
      <input
        ref="searchInput"
        v-model="query"
        type="text"
        class="tmpl-picker__search"
        placeholder="Search templates…"
      />
      <div class="tmpl-picker__list">
        <div v-if="filtered.length === 0" class="tmpl-picker__empty">
          {{ templates.length === 0 ? 'No templates saved yet.' : 'No matches.' }}
        </div>
        <button
          v-for="t in filtered"
          :key="t.id"
          type="button"
          class="tmpl-picker__row"
          @click="choose(t)"
        >
          <span class="tmpl-picker__name">{{ t.name }}</span>
          <span class="tmpl-picker__preview">{{ preview(t.body) }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tmpl-picker { position: relative; display: inline-flex; }
.tmpl-picker__dropdown {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  z-index: 200;
  min-width: 280px;
  max-width: 480px;
  max-height: 320px;
  display: flex;
  flex-direction: column;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
}
.tmpl-picker__search {
  margin: 8px;
  padding: 5px 8px;
  font-size: 12px;
  box-sizing: border-box;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text);
}
.tmpl-picker__list { overflow-y: auto; flex: 1; min-height: 0; padding-bottom: 4px; }
.tmpl-picker__empty { padding: 10px 12px; font-size: 12px; color: var(--text-muted); }
.tmpl-picker__row {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 100%;
  text-align: left;
  padding: 7px 12px;
  cursor: pointer;
  border: none;
  border-bottom: 1px solid var(--border);
  background: none;
}
.tmpl-picker__row:hover { background: var(--bg-hover, var(--bg-raised)); }
.tmpl-picker__name { font-size: 13px; font-weight: 500; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tmpl-picker__preview { font-size: 11px; color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
