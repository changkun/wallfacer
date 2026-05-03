<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useTaskStore } from '../stores/tasks';

const store = useTaskStore();

onMounted(async () => {
  if (!store.tasks.length) await store.fetchTasks();
});

const agents = computed(() =>
  store.inProgress.map((task, i) => {
    const hash = hashCode(task.id);
    return {
      id: task.id,
      shortId: task.id.slice(0, 8),
      title: task.title || task.prompt.slice(0, 24),
      color: agentColors[Math.abs(hash) % agentColors.length],
      x: 10 + (Math.abs(hash * 7) % 70),
      y: 10 + (Math.abs(hash * 13) % 60),
      delay: (i * 0.3).toFixed(1),
      sandbox: task.sandbox,
    };
  }),
);

const agentColors = [
  '#e06c75', '#61afef', '#98c379', '#e5c07b',
  '#c678dd', '#56b6c2', '#d19a66', '#be5046',
];

function hashCode(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0;
  }
  return h;
}
</script>

<template>
  <div class="office-page">
    <header class="page-header"><h1>Office</h1></header>

    <div class="office-floor">
      <!-- quiet state -->
      <div v-if="agents.length === 0" class="office-quiet">
        <div class="quiet-icon">
          <div class="quiet-desk" />
          <div class="quiet-lamp" />
        </div>
        <p class="quiet-text">Office is quiet</p>
        <p class="quiet-sub">No agents are working right now</p>
      </div>

      <!-- agents at work -->
      <div
        v-for="agent in agents"
        :key="agent.id"
        class="agent"
        :style="{
          left: agent.x + '%',
          top: agent.y + '%',
          animationDelay: agent.delay + 's',
        }"
      >
        <div class="agent-body" :style="{ background: agent.color }">
          <div class="agent-eyes">
            <span class="eye" /><span class="eye" />
          </div>
        </div>
        <div class="agent-shadow" />
        <div class="agent-label">{{ agent.shortId }}</div>
        <div class="agent-title">{{ agent.title }}</div>
        <div v-if="agent.sandbox" class="agent-sandbox">{{ agent.sandbox }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.office-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg);
  color: var(--ink);
  font-family: var(--font-sans);
}

.page-header {
  padding: 12px 20px;
  border-bottom: 1px solid var(--rule);
  flex-shrink: 0;
}
.page-header h1 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}

/* --- office floor with grid --- */
.office-floor {
  flex: 1;
  position: relative;
  overflow: hidden;
  background:
    linear-gradient(var(--rule) 1px, transparent 1px),
    linear-gradient(90deg, var(--rule) 1px, transparent 1px);
  background-size: 40px 40px;
  background-color: var(--bg-sunk);
}

/* --- quiet state --- */
.office-quiet {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.quiet-icon {
  position: relative;
  width: 64px;
  height: 48px;
  margin-bottom: 8px;
}

.quiet-desk {
  position: absolute;
  bottom: 0;
  left: 50%;
  transform: translateX(-50%);
  width: 48px;
  height: 20px;
  background: var(--bg-card);
  border: 2px solid var(--rule);
  border-radius: var(--r-sm);
}

.quiet-lamp {
  position: absolute;
  top: 0;
  left: 50%;
  transform: translateX(-50%);
  width: 4px;
  height: 28px;
  background: var(--ink-4);
  border-radius: 2px;
}
.quiet-lamp::before {
  content: '';
  position: absolute;
  top: -6px;
  left: 50%;
  transform: translateX(-50%);
  width: 14px;
  height: 8px;
  background: var(--ink-4);
  border-radius: 4px 4px 0 0;
}

.quiet-text {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--ink-2);
}

.quiet-sub {
  margin: 0;
  font-size: 12px;
  color: var(--ink-4);
}

/* --- agent character --- */
.agent {
  position: absolute;
  display: flex;
  flex-direction: column;
  align-items: center;
  animation: bob 1.6s ease-in-out infinite;
  cursor: default;
  user-select: none;
}

.agent-body {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  image-rendering: pixelated;
  box-shadow: 0 2px 0 rgba(0, 0, 0, 0.15);
}

.agent-eyes {
  display: flex;
  gap: 6px;
}

.eye {
  display: block;
  width: 6px;
  height: 6px;
  background: #fff;
  border-radius: 50%;
  position: relative;
}
.eye::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 3px;
  height: 3px;
  background: #222;
  border-radius: 50%;
  animation: look 3s ease-in-out infinite;
}

.agent-shadow {
  width: 24px;
  height: 6px;
  background: rgba(0, 0, 0, 0.08);
  border-radius: 50%;
  margin-top: 2px;
  animation: shadow-pulse 1.6s ease-in-out infinite;
}

.agent-label {
  margin-top: 4px;
  font-size: 9px;
  font-family: var(--font-mono);
  color: var(--ink-3);
  background: var(--bg-card);
  padding: 1px 4px;
  border-radius: 3px;
  border: 1px solid var(--rule);
  white-space: nowrap;
}

.agent-title {
  margin-top: 2px;
  font-size: 10px;
  color: var(--ink-3);
  max-width: 100px;
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-sandbox {
  margin-top: 1px;
  font-size: 8px;
  font-family: var(--font-mono);
  color: var(--ink-4);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

/* --- animations --- */
@keyframes bob {
  0%, 100% { transform: translateY(0); }
  50% { transform: translateY(-6px); }
}

@keyframes shadow-pulse {
  0%, 100% { transform: scaleX(1); opacity: 1; }
  50% { transform: scaleX(0.7); opacity: 0.5; }
}

@keyframes look {
  0%, 40% { transform: translate(0, 0); }
  50% { transform: translate(1px, 0); }
  60%, 100% { transform: translate(0, 0); }
}
</style>
