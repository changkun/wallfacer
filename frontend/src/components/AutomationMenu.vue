<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue';
import { RouterLink } from 'vue-router';
import { useAutomationToggles } from '../composables/useAutomationToggles';

// Board automation popover, anchored under the lightning button. Restores the
// legacy automation menu (toggles inline on the board) so the controls are one
// click away instead of buried in Settings. The numeric knobs (parallelism,
// oversight interval, auto-push threshold) stay on the Execution settings page,
// reachable via the footer link.
defineProps<{
  modelValue: boolean;
  anchor: { top: number; right: number } | null;
}>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();

const { AUTOMATION_KEYS, automationLabels, automationHints, isOn, isBusy, toggle } =
  useAutomationToggles();

function close() {
  emit('update:modelValue', false);
}

function onOutsideClick(e: MouseEvent) {
  const t = e.target as HTMLElement;
  if (!t.closest('.automation-menu') && !t.closest('.automation-btn')) close();
}
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault();
    close();
  }
}

onMounted(() => {
  document.addEventListener('mousedown', onOutsideClick);
  document.addEventListener('keydown', onKeydown);
});
onUnmounted(() => {
  document.removeEventListener('mousedown', onOutsideClick);
  document.removeEventListener('keydown', onKeydown);
});
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue && anchor"
      class="automation-menu"
      :style="{ position: 'fixed', top: anchor.top + 'px', right: anchor.right + 'px', zIndex: 9999 }"
    >
      <div class="automation-menu__header">Automation</div>
      <ul class="automation-menu__list">
        <li v-for="k in AUTOMATION_KEYS" :key="k" class="automation-menu__row">
          <button
            type="button"
            role="switch"
            :aria-checked="isOn(k)"
            class="automation-switch"
            :class="{ 'automation-switch--on': isOn(k) }"
            :disabled="isBusy(k)"
            @click="toggle(k)"
          >
            <span class="automation-switch__track"><span class="automation-switch__thumb"></span></span>
          </button>
          <div class="automation-menu__text">
            <span class="automation-menu__label">{{ automationLabels[k] }}</span>
            <span class="automation-menu__hint">{{ automationHints[k] }}</span>
          </div>
        </li>
      </ul>
      <RouterLink
        to="/settings?tab=execution"
        class="automation-menu__more"
        @click="close()"
      >
        All execution settings →
      </RouterLink>
    </div>
  </Teleport>
</template>

<style scoped>
.automation-menu {
  width: 280px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-pop);
  padding: var(--sp-2);
  font-size: var(--fs-base);
}
.automation-menu__header {
  font-size: var(--fs-9);
  text-transform: uppercase;
  letter-spacing: var(--tracking-label);
  color: var(--ink-3);
  padding: var(--sp-2) var(--sp-3) var(--sp-1);
}
.automation-menu__list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.automation-menu__row {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-3);
  padding: var(--sp-2) var(--sp-3);
  border-radius: var(--r-md);
}
.automation-menu__row:hover {
  background: var(--bg-hover);
}
.automation-menu__text {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}
.automation-menu__label {
  font-weight: 600;
  color: var(--ink);
}
.automation-menu__hint {
  font-size: var(--fs-9);
  color: var(--ink-3);
  line-height: 1.35;
}
.automation-menu__more {
  display: block;
  margin-top: var(--sp-1);
  padding: var(--sp-2) var(--sp-3);
  border-top: 1px solid var(--rule);
  color: var(--accent);
  font-size: var(--fs-10);
  text-decoration: none;
}
.automation-menu__more:hover {
  text-decoration: underline;
}

/* Switch — flat track + thumb, clay accent when on. */
.automation-switch {
  flex-shrink: 0;
  margin-top: 1px;
  padding: 0;
  background: none;
  border: none;
  cursor: pointer;
}
.automation-switch:disabled {
  cursor: progress;
  opacity: 0.6;
}
.automation-switch__track {
  display: inline-block;
  width: 30px;
  height: 18px;
  border-radius: 999px;
  background: var(--bg-sunk);
  border: 1px solid var(--rule-2);
  transition: background 0.14s, border-color 0.14s;
}
.automation-switch__thumb {
  display: block;
  width: 12px;
  height: 12px;
  margin: 2px;
  border-radius: 50%;
  background: var(--ink-3);
  transition: transform 0.14s, background 0.14s;
}
.automation-switch--on .automation-switch__track {
  background: var(--accent);
  border-color: var(--accent);
}
.automation-switch--on .automation-switch__thumb {
  background: #fff;
  transform: translateX(12px);
}
</style>
