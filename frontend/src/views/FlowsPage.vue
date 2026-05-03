<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { api } from '../api/client';

interface FlowStep {
  agent: string;
  label?: string;
}

interface Flow {
  slug: string;
  name: string;
  description: string;
  builtin: boolean;
  steps: FlowStep[];
}

const flows = ref<Flow[]>([]);
const loading = ref(true);
const selectedFlow = ref<Flow | null>(null);
const search = ref('');

onMounted(async () => {
  try {
    flows.value = await api<Flow[]>('GET', '/api/flows');
  } catch (e) {
    console.error('flows:', e);
  }
  loading.value = false;
});

async function selectFlow(f: Flow) {
  selectedFlow.value = f;
  try {
    const detail = await api<Flow>('GET', `/api/flows/${f.slug}`);
    selectedFlow.value = detail;
  } catch (e) {
    console.error('flow detail:', e);
  }
}

const filteredFlows = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return flows.value;
  return flows.value.filter((f) => {
    return (
      f.slug.toLowerCase().includes(q) ||
      (f.name || '').toLowerCase().includes(q) ||
      (f.description || '').toLowerCase().includes(q)
    );
  });
});

const builtinFlows = computed(() => filteredFlows.value.filter((f) => f.builtin));
const userFlows = computed(() => filteredFlows.value.filter((f) => !f.builtin));
</script>

<template>
  <div class="flows-mode-container">
    <div class="flows-mode__inner">
      <header class="flows-mode__header">
        <div class="flows-mode__header-row">
          <div>
            <h2 class="flows-mode__title">Flows</h2>
            <p class="flows-mode__subtitle">
              A flow is an ordered chain of sub-agents a task runs against. Pick a built-in
              to inspect its steps, or browse user-authored flows.
            </p>
          </div>
        </div>
      </header>

      <div class="flows-mode__split">
        <aside class="flows-mode__rail">
          <div class="flows-mode__search">
            <input
              v-model="search"
              type="search"
              placeholder="Search flows..."
              aria-label="Search flows"
              autocomplete="off"
            />
          </div>
          <div class="flows-mode__rail-list" :aria-busy="loading">
            <p v-if="loading" class="flows-mode__empty">Loading flows...</p>
            <template v-else>
              <p v-if="filteredFlows.length === 0" class="flows-mode__empty">
                No flows match.
              </p>

              <template v-if="builtinFlows.length">
                <div class="flows-rail__group">Built-in</div>
                <button
                  v-for="f in builtinFlows"
                  :key="f.slug"
                  type="button"
                  class="flows-rail__item"
                  :class="{ 'flows-rail__item--active': selectedFlow?.slug === f.slug }"
                  @click="selectFlow(f)"
                >
                  <span class="flows-rail__name">{{ f.name || f.slug }}</span>
                  <span class="flows-rail__meta">{{ f.steps?.length ?? 0 }}</span>
                </button>
              </template>

              <template v-if="userFlows.length">
                <div class="flows-rail__group">User</div>
                <button
                  v-for="f in userFlows"
                  :key="f.slug"
                  type="button"
                  class="flows-rail__item flows-rail__item--user"
                  :class="{ 'flows-rail__item--active': selectedFlow?.slug === f.slug }"
                  @click="selectFlow(f)"
                >
                  <span class="flows-rail__name">{{ f.name || f.slug }}</span>
                  <span class="flows-rail__meta">{{ f.steps?.length ?? 0 }}</span>
                </button>
              </template>
            </template>
          </div>
        </aside>

        <section class="flows-mode__detail">
          <div v-if="!selectedFlow" class="flows-mode__empty-detail">
            <p>Pick a flow on the left to inspect its steps.</p>
          </div>
          <div v-else>
            <div class="flows-detail__head">
              <div>
                <h3 class="flows-detail__title">
                  {{ selectedFlow.name || selectedFlow.slug }}
                </h3>
                <div class="flows-detail__subtitle">
                  <code>{{ selectedFlow.slug }}</code>
                  <span
                    class="flows-detail__badge"
                    :class="{ 'flows-detail__badge--user': !selectedFlow.builtin }"
                  >
                    {{ selectedFlow.builtin ? 'Built-in' : 'User' }}
                  </span>
                </div>
              </div>
            </div>

            <div class="flows-detail__body">
              <p v-if="selectedFlow.description" class="flows-detail__desc">
                {{ selectedFlow.description }}
              </p>

              <div v-if="selectedFlow.steps?.length" class="flows-detail__chain">
                <template v-for="(step, i) in selectedFlow.steps" :key="i">
                  <span v-if="i > 0" class="flows-chain__sep">&rarr;</span>
                  <span class="flows-chip">
                    {{ step.label || step.agent }}
                  </span>
                </template>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>
