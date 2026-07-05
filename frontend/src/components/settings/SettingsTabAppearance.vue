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
  <div class="appearance-tab">
    <section class="ap-section">
      <h3 class="ap-heading">Mode</h3>
      <p class="ap-sub">Light, dark, or follow the operating system.</p>
      <div class="ap-modes" role="radiogroup" aria-label="Theme mode">
        <button
          v-for="m in modes"
          :key="m.key"
          type="button"
          role="radio"
          class="ap-mode"
          :class="{ 'is-active': theme === m.key }"
          :aria-checked="theme === m.key"
          :title="m.hint"
          @click="prefs.setTheme(m.key)"
        >{{ m.label }}</button>
      </div>
    </section>

    <section class="ap-section">
      <h3 class="ap-heading">Color theme</h3>
      <p class="ap-sub">The palette applies to the whole workspace in both light and dark mode.</p>
      <div class="ap-palettes" role="radiogroup" aria-label="Color theme">
        <button
          v-for="p in PALETTES"
          :key="p.name"
          type="button"
          role="radio"
          class="ap-palette"
          :class="{ 'is-active': palette === p.name }"
          :aria-checked="palette === p.name"
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
    </section>
  </div>
</template>

<style scoped>
.ap-section { margin-bottom: 28px; }
.ap-heading {
  margin: 0 0 2px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ink-2);
}
.ap-sub { margin: 0 0 12px; font-size: 12px; color: var(--ink-3); }

.ap-modes { display: inline-flex; gap: 0; border: 1px solid var(--rule); border-radius: 8px; overflow: hidden; }
.ap-mode {
  padding: 6px 16px;
  border: none;
  background: var(--bg-card);
  color: var(--ink-2);
  font-size: 12px;
  cursor: pointer;
}
.ap-mode + .ap-mode { border-left: 1px solid var(--rule); }
.ap-mode.is-active { background: var(--accent-tint); color: var(--accent); font-weight: 600; }
.ap-mode:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }

.ap-palettes { display: grid; grid-template-columns: repeat(auto-fill, minmax(170px, 1fr)); gap: 10px; max-width: 620px; }
.ap-palette {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--rule);
  border-radius: 10px;
  background: var(--bg-card);
  cursor: pointer;
  text-align: left;
}
.ap-palette:hover { border-color: var(--rule-2); }
.ap-palette.is-active {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-tint);
}
.ap-palette:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.ap-swatch {
  display: grid;
  grid-template-columns: 1fr 1fr;
  width: 26px;
  height: 26px;
  border-radius: 50%;
  overflow: hidden;
  border: 1px solid var(--rule);
  flex-shrink: 0;
}
.ap-swatch i { display: block; }
.ap-palette-name { font-size: 12.5px; font-weight: 600; color: var(--ink); }
.ap-default-tag {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 9px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ink-4);
}
</style>
