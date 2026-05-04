<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick, computed } from 'vue';
import { api, ApiError } from '../api/client';

interface BranchesResponse { current: string; branches: string[] }

const props = defineProps<{
  modelValue: boolean;
  workspacePath: string;
  currentBranch: string;
  anchor: { top: number; left: number } | null;
}>();
const emit = defineEmits<{
  'update:modelValue': [boolean];
  'switched': [];
}>();

const loading = ref(true);
const errorMsg = ref('');
const branches = ref<string[]>([]);
const current = ref(props.currentBranch);
const query = ref('');
const inputEl = ref<HTMLInputElement | null>(null);
const pending = ref('');

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return branches.value;
  return branches.value.filter(b => b.toLowerCase().includes(q));
});
const exactMatch = computed(() =>
  branches.value.some(b => b.toLowerCase() === query.value.trim().toLowerCase()),
);
const showCreate = computed(() => query.value.trim() !== '' && !exactMatch.value);

function close() { emit('update:modelValue', false); }

async function load() {
  loading.value = true;
  errorMsg.value = '';
  try {
    const data = await api<BranchesResponse>(
      'GET',
      `/api/git/branches?workspace=${encodeURIComponent(props.workspacePath)}`,
    );
    current.value = data.current || props.currentBranch;
    branches.value = data.branches || [];
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : 'Failed to load branches';
  } finally {
    loading.value = false;
  }
}

async function selectBranch(branch: string) {
  if (branch === current.value || pending.value) return;
  pending.value = branch;
  try {
    await api('POST', '/api/git/checkout', { workspace: props.workspacePath, branch });
    emit('switched');
    close();
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) {
      window.alert('Branch switch blocked: another git operation is in progress.');
    } else {
      window.alert('Branch switch failed: ' + (e instanceof Error ? e.message : String(e)));
    }
  } finally {
    pending.value = '';
  }
}

async function createBranch() {
  const branch = query.value.trim();
  if (!branch || pending.value) return;
  pending.value = branch;
  try {
    await api('POST', '/api/git/create-branch', { workspace: props.workspacePath, branch });
    emit('switched');
    close();
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) {
      window.alert('Create branch blocked: another git operation is in progress.');
    } else {
      window.alert('Failed to create branch: ' + (e instanceof Error ? e.message : String(e)));
    }
  } finally {
    pending.value = '';
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') { e.preventDefault(); close(); return; }
  if (e.key === 'Enter') {
    e.preventDefault();
    if (showCreate.value) { void createBranch(); return; }
    const first = filtered.value[0];
    if (first) void selectBranch(first);
  }
}

function onOutsideClick(e: MouseEvent) {
  const t = e.target as HTMLElement;
  if (!t.closest('.branch-dropdown') && !t.closest('.status-bar-branch')) close();
}

watch(
  () => props.modelValue,
  async (open) => {
    if (!open) return;
    query.value = '';
    await load();
    await nextTick();
    inputEl.value?.focus();
  },
);

onMounted(() => {
  if (props.modelValue) {
    query.value = '';
    void load();
    nextTick(() => inputEl.value?.focus());
  }
  document.addEventListener('mousedown', onOutsideClick);
});
onUnmounted(() => document.removeEventListener('mousedown', onOutsideClick));
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue && anchor"
      class="branch-dropdown"
      :style="{ position: 'fixed', top: anchor.top + 'px', left: anchor.left + 'px', zIndex: 9999 }"
      @keydown="onKeydown"
    >
      <div v-if="loading" class="branch-dropdown-loading">Loading branches...</div>
      <div v-else-if="errorMsg" class="branch-dropdown-loading" style="color: var(--err)">{{ errorMsg }}</div>
      <template v-else>
        <div class="branch-dropdown-header">Switch branch</div>
        <div class="branch-dropdown-search">
          <input
            ref="inputEl"
            v-model="query"
            type="text"
            class="branch-search-input"
            placeholder="Filter or create branch..."
            autocomplete="off"
            spellcheck="false"
          />
        </div>
        <div class="branch-dropdown-list">
          <button
            v-for="b in filtered"
            :key="b"
            type="button"
            class="branch-dropdown-item"
            :class="{ current: b === current }"
            :disabled="!!pending"
            @click="selectBranch(b)"
          >
            <svg
              v-if="b === current"
              width="12" height="12" viewBox="0 0 20 20" fill="currentColor"
              style="flex-shrink: 0; color: var(--accent);"
            ><path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd"/></svg>
            <span v-else style="width: 12px; display: inline-block;"></span>
            <span class="branch-dropdown-item-name">{{ b }}</span>
            <span v-if="pending === b" style="margin-left: auto; opacity: 0.6;">…</span>
          </button>
          <div
            v-if="filtered.length === 0 && !showCreate"
            class="branch-dropdown-loading"
            style="padding: 12px;"
          >No matches</div>
        </div>
        <div class="branch-dropdown-footer">
          <button
            v-if="showCreate"
            type="button"
            class="branch-dropdown-create"
            :disabled="!!pending"
            @click="createBranch"
          >
            <svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor" style="flex-shrink: 0;"><path fill-rule="evenodd" d="M10 3a1 1 0 011 1v5h5a1 1 0 110 2h-5v5a1 1 0 11-2 0v-5H4a1 1 0 110-2h5V4a1 1 0 011-1z" clip-rule="evenodd"/></svg>
            <span>Create branch "{{ query.trim() }}"</span>
          </button>
        </div>
      </template>
    </div>
  </Teleport>
</template>
