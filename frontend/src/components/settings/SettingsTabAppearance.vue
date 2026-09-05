<script setup lang="ts">
// Appearance settings: the light/dark/auto mode and the color palette axis
// (Slack-style named presets). Both apply instantly via the prefs store and
// persist to localStorage; see specs/shared/visual-identity/theme-system.md.
import { storeToRefs } from 'pinia';
import { usePrefsStore, PALETTES, type Theme } from '../../stores/prefs';

const prefs = usePrefsStore();
const { theme, palette } = storeToRefs(prefs);

const modes: { key: Theme; label: string; hint: string }[] = [
  { key: 'light', label: 'Light', hint: 'Always light' },
  { key: 'dark', label: 'Dark', hint: 'Always dark' },
  { key: 'auto', label: 'Auto', hint: 'Follow the system' },
];
</script>

<template>
  <div class="card compact" data-settings-tab="appearance">
    <div class="card-head"><span class="eyebrow">Appearance</span></div>
    <div class="rows">
      <div class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Mode</span>
          <span class="set-row__help">Light, dark, or follow the operating system.</span>
        </div>
        <div class="set-row__end">
          <div class="seg ap-modes" role="radiogroup" aria-label="Theme mode">
            <button
              v-for="m in modes"
              :key="m.key"
              type="button"
              role="radio"
              class="seg-btn"
              :class="{ on: theme === m.key }"
              :aria-checked="theme === m.key"
              :title="m.hint"
              :data-mode="m.key"
              @click="prefs.setTheme(m.key)"
            >
              <svg v-if="m.key === 'light'" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="12" cy="12" r="4"></circle><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"></path></svg>
              <svg v-else-if="m.key === 'dark'" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"></path></svg>
              <svg v-else width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><rect x="2" y="4" width="20" height="14" rx="2"></rect><path d="M8 21h8M12 18v3"></path></svg>
              {{ m.label }}
            </button>
          </div>
        </div>
      </div>
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">Color theme</span>
          <span class="set-row__help">The palette applies to the whole workspace in both light and dark mode.</span>
        </div>
        <div class="ap-palettes" role="radiogroup" aria-label="Color theme">
          <button
            v-for="p in PALETTES"
            :key="p.name"
            type="button"
            role="radio"
            class="ap-palette"
            :class="{ 'is-active': palette === p.name }"
            :aria-checked="palette === p.name"
            :data-palette="p.name"
            @click="prefs.setPalette(p.name)"
          >
            <span class="ap-swatch" aria-hidden="true">
              <i :style="{ background: p.swatches[0] }" />
              <i :style="{ background: p.swatches[1] }" />
              <i :style="{ background: p.swatches[2] }" />
              <i :style="{ background: p.swatches[3] }" />
            </span>
            <span class="ap-palette-name">{{ p.label }}</span>
            <span v-if="p.name === 'clay'" class="ap-default-tag">default</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ap-modes .seg-btn {
  gap: 5px;
}
.ap-palettes {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 8px;
}
.ap-palette {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  padding: 0 12px;
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  background: var(--bg-card);
  color: var(--ink);
  cursor: pointer;
  text-align: left;
  font: inherit;
  transition: border-color var(--dur-hover), background var(--dur-hover);
}
.ap-palette:hover {
  border-color: var(--rule-2);
}
.ap-palette.is-active {
  border-color: var(--accent-line);
  background: var(--accent-soft);
}
.ap-swatch {
  display: grid;
  grid-template-columns: 1fr 1fr;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  overflow: hidden;
  border: 1px solid var(--rule);
  flex-shrink: 0;
}
.ap-swatch i {
  display: block;
}
.ap-palette-name {
  font-size: 12.5px;
  font-weight: 600;
}
.ap-default-tag {
  margin-left: auto;
  font: 500 var(--fs-9) / 1 var(--font-mono);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ink-4);
}
</style>
