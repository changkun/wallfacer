<script setup lang="ts">
// Interactive autonomy-spectrum demo for the landing page: four stops from
// conversational exploration to fully automated execution. Pure client-side
// state; renders stop 2 statically under SSG so the prerendered page shows a
// complete scene.
import { ref, computed } from 'vue';
import { useT } from '../../i18n';

const t = useT();
const active = ref(2);

const stops = computed(() => [1, 2, 3, 4].map((n) => ({
  n,
  name: t.value(`wf.spectrum.${n}.name`),
  desc: t.value(`wf.spectrum.${n}.desc`),
  human: t.value(`wf.spectrum.${n}.human`),
})));

const fillPct = computed(() => (active.value - 1) / 3 * 100);

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowRight' || e.key === 'ArrowUp') {
    active.value = Math.min(4, active.value + 1);
    e.preventDefault();
  } else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') {
    active.value = Math.max(1, active.value - 1);
    e.preventDefault();
  }
}
</script>

<template>
  <div class="spectrum" role="group" :aria-label="t('wf.spectrum.title')">
    <div
      class="spectrum-track"
      role="slider"
      :aria-valuemin="1"
      :aria-valuemax="4"
      :aria-valuenow="active"
      :aria-valuetext="stops[active - 1].name"
      tabindex="0"
      @keydown="onKeydown"
    >
      <div class="spectrum-rail">
        <div class="spectrum-fill" :style="{ width: fillPct + '%' }" />
      </div>
      <button
        v-for="s in stops"
        :key="s.n"
        type="button"
        class="spectrum-stop"
        :class="{ 'spectrum-stop--active': s.n === active, 'spectrum-stop--passed': s.n < active }"
        :style="{ left: ((s.n - 1) / 3 * 100) + '%' }"
        :aria-pressed="s.n === active"
        @click="active = s.n"
      >
        <span class="spectrum-dot" />
        <span class="spectrum-stop-name">{{ s.name }}</span>
      </button>
    </div>

    <transition name="spectrum-swap" mode="out-in">
      <div :key="active" class="spectrum-detail">
        <p class="spectrum-desc">{{ stops[active - 1].desc }}</p>
        <p class="spectrum-human"><span class="spectrum-human-label">{{ t('wf.spectrum.you') }}</span> {{ stops[active - 1].human }}</p>
      </div>
    </transition>
  </div>
</template>

<style scoped>
.spectrum {
  max-width: 720px;
  margin: 0 auto;
}

.spectrum-track {
  position: relative;
  height: 64px;
  margin: 0 12%;
  outline: none;
}
.spectrum-track:focus-visible .spectrum-rail {
  box-shadow: 0 0 0 3px var(--accent-tint);
}

.spectrum-rail {
  position: absolute;
  top: 14px;
  left: 0;
  right: 0;
  height: 4px;
  border-radius: 2px;
  background: var(--rule);
}
.spectrum-fill {
  height: 100%;
  border-radius: 2px;
  background: var(--accent-gradient);
  transition: width 0.35s cubic-bezier(0.65, 0, 0.35, 1);
}

.spectrum-stop {
  position: absolute;
  top: 0;
  transform: translateX(-50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 8px 6px 0;
  border: none;
  background: none;
  cursor: pointer;
  color: var(--ink-3);
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.04em;
}
.spectrum-dot {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid var(--rule-2);
  background: var(--bg-card);
  transition: border-color 0.2s ease, box-shadow 0.2s ease, transform 0.2s ease;
}
.spectrum-stop--passed .spectrum-dot { border-color: var(--accent); }
.spectrum-stop--active { color: var(--accent); }
.spectrum-stop--active .spectrum-dot {
  border-color: var(--accent);
  box-shadow: 0 0 0 5px var(--accent-tint);
  transform: scale(1.15);
}
.spectrum-stop:hover .spectrum-dot { border-color: var(--accent); }

.spectrum-detail {
  margin-top: 18px;
  text-align: center;
  min-height: 96px;
}
.spectrum-desc {
  margin: 0 auto;
  max-width: 52ch;
  font-size: 15px;
  line-height: 1.6;
  color: var(--text-secondary);
}
.spectrum-human {
  margin: 10px auto 0;
  font-size: 13px;
  color: var(--ink-3);
}
.spectrum-human-label {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--accent);
  margin-right: 6px;
}

.spectrum-swap-enter-active,
.spectrum-swap-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}
.spectrum-swap-enter-from { opacity: 0; transform: translateY(4px); }
.spectrum-swap-leave-to { opacity: 0; transform: translateY(-4px); }

@media (prefers-reduced-motion: reduce) {
  .spectrum-fill { transition: none; }
  .spectrum-swap-enter-active,
  .spectrum-swap-leave-active { transition: none; }
}
@media (max-width: 560px) {
  .spectrum-track { margin: 0 6%; }
  .spectrum-stop-name { font-size: 9px; }
}
</style>
