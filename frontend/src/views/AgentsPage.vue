<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api } from '../api/client';

interface Agent {
  slug: string;
  name: string;
  description: string;
  builtin: boolean;
  sandbox: string;
  model: string;
}

const agents = ref<Agent[]>([]);
const loading = ref(true);
const selectedAgent = ref<Agent | null>(null);
const agentDetail = ref<{ prompt_template?: string } | null>(null);
const search = ref('');

const builtinAgents = computed(() =>
  agents.value.filter((a) => a.builtin && matchesSearch(a)),
);
const userAgents = computed(() =>
  agents.value.filter((a) => !a.builtin && matchesSearch(a)),
);

function matchesSearch(a: Agent): boolean {
  const q = search.value.trim().toLowerCase();
  if (!q) return true;
  return (
    a.slug.toLowerCase().includes(q) ||
    (a.name || '').toLowerCase().includes(q) ||
    (a.description || '').toLowerCase().includes(q)
  );
}

onMounted(async () => {
  try {
    agents.value = await api<Agent[]>('GET', '/api/agents');
  } catch (e) {
    console.error('agents:', e);
  }
  loading.value = false;
});

async function selectAgent(a: Agent) {
  selectedAgent.value = a;
  agentDetail.value = null;
  try {
    agentDetail.value = await api('GET', `/api/agents/${a.slug}`);
  } catch (e) {
    console.error('agent detail:', e);
  }
}
</script>

<template>
  <div class="agents-mode-container">
    <div class="agents-mode__inner">
      <header class="agents-mode__header">
        <div class="agents-mode__header-row">
          <div>
            <h2 class="agents-mode__title">Agents</h2>
            <p class="agents-mode__subtitle">
              Sub-agent roles each flow step invokes. Clone a built-in or start
              from scratch to pin a harness, tune capabilities, or override the
              system prompt.
            </p>
          </div>
        </div>
      </header>

      <div class="agents-mode__split">
        <aside class="agents-mode__rail">
          <div class="agents-mode__search">
            <input
              v-model="search"
              type="search"
              placeholder="Search agents..."
              aria-label="Search agents"
              autocomplete="off"
            />
          </div>
          <div class="agents-mode__rail-list">
            <p v-if="loading" class="agents-mode__empty">Loading agents...</p>
            <template v-else>
              <template v-if="builtinAgents.length">
                <div class="agents-rail__group">Built-in</div>
                <button
                  v-for="a in builtinAgents"
                  :key="a.slug"
                  type="button"
                  class="agents-rail__item"
                  :class="{
                    'agents-rail__item--active':
                      selectedAgent?.slug === a.slug,
                  }"
                  @click="selectAgent(a)"
                >
                  <span class="agents-rail__name">{{ a.name || a.slug }}</span>
                  <span v-if="a.sandbox" class="agents-rail__meta">{{
                    a.sandbox
                  }}</span>
                </button>
              </template>

              <template v-if="userAgents.length">
                <div class="agents-rail__group">User</div>
                <button
                  v-for="a in userAgents"
                  :key="a.slug"
                  type="button"
                  class="agents-rail__item agents-rail__item--user"
                  :class="{
                    'agents-rail__item--active':
                      selectedAgent?.slug === a.slug,
                  }"
                  @click="selectAgent(a)"
                >
                  <span class="agents-rail__name">{{ a.name || a.slug }}</span>
                  <span v-if="a.sandbox" class="agents-rail__meta">{{
                    a.sandbox
                  }}</span>
                </button>
              </template>

              <p
                v-if="!builtinAgents.length && !userAgents.length"
                class="agents-mode__empty"
              >
                No agents found.
              </p>
            </template>
          </div>
        </aside>

        <section class="agents-mode__detail">
          <div v-if="!selectedAgent" class="agents-mode__empty-detail">
            <p>Pick an agent on the left.</p>
          </div>
          <template v-else-if="agentDetail">
            <div class="agents-detail__head">
              <div>
                <h3 class="agents-detail__title">
                  {{ selectedAgent.name || selectedAgent.slug }}
                </h3>
                <div class="agents-detail__subtitle">
                  <code>{{ selectedAgent.slug }}</code>
                  <span
                    class="agents-detail__badge"
                    :class="{
                      'agents-detail__badge--user': !selectedAgent.builtin,
                    }"
                  >
                    {{ selectedAgent.builtin ? 'Built-in' : 'User' }}
                  </span>
                </div>
              </div>
            </div>

            <div class="agents-detail__body">
              <div v-if="selectedAgent.description" class="agents-detail__kv">
                <div class="agents-detail__kv-key">Description</div>
                <div class="agents-detail__kv-value">
                  {{ selectedAgent.description }}
                </div>
              </div>
              <div class="agents-detail__kv">
                <div class="agents-detail__kv-key">Sandbox</div>
                <div class="agents-detail__kv-value">
                  {{ selectedAgent.sandbox || 'default' }}
                </div>
              </div>
              <div class="agents-detail__kv">
                <div class="agents-detail__kv-key">Model</div>
                <div class="agents-detail__kv-value">
                  {{ selectedAgent.model || 'default' }}
                </div>
              </div>

              <div class="agents-detail__section">
                <div class="agents-detail__section-label">Prompt Template</div>
                <pre
                  v-if="agentDetail.prompt_template"
                  class="agents-detail__tmpl"
                  >{{ agentDetail.prompt_template }}</pre
                >
                <p
                  v-else
                  class="agents-detail__tmpl agents-detail__tmpl--empty"
                >
                  No prompt template.
                </p>
              </div>
            </div>
          </template>
          <div v-else class="agents-mode__empty-detail">
            <p>Loading...</p>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>
