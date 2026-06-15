<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useHead } from '@unhead/vue';
import DefaultLayout from '../layouts/DefaultLayout.vue';
import { useT } from '../i18n';
import { useReveal } from '../composables/useReveal';
import {
  PLATFORMS,
  type Arch,
  archFromUAData,
  archLabel,
  assetName,
  clampArch,
  detectPlatform,
  downloadURL,
  osLabel,
} from '../lib/platform';

const t = useT();
useReveal();
useHead({
  title: 'Download Wallfacer',
  meta: [{ name: 'description', content: 'Download Wallfacer for macOS, Windows, and Linux.' }],
});

const INSTALL_CMD = 'curl -fsSL https://latere.ai/wallfacer/install.sh | sh';
const RELEASES_URL = 'https://github.com/changkun/wallfacer/releases/latest';

// Single-path SVG glyphs for each OS, keyed by the platform id.
const OS_ICON: Record<string, string> = {
  darwin:
    'M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.8-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z',
  windows:
    'M3 12V6.75l8-1.25V12H3zm0 .5h8v6.5l-8-1.25V12.5zM11.5 5.34l9.5-1.59V12H11.5V5.34zM11.5 12.5H21v7.75l-9.5-1.59V12.5z',
  linux:
    'M12.504 0c-.155 0-.315.008-.48.021-4.226.333-3.105 4.807-3.17 6.298-.076 1.092-.3 1.953-1.05 3.02-.885 1.051-2.127 2.75-2.716 4.521-.278.832-.41 1.684-.287 2.489a.424.424 0 00-.11.135c-.26.268-.45.6-.663.839-.199.199-.485.267-.797.4-.313.136-.658.269-.864.68-.09.189-.136.394-.132.602 0 .199.027.4.055.536.058.399.116.728.04.97-.249.68-.28 1.145-.106 1.484.174.334.535.47.94.601.81.2 1.91.135 2.774.6.926.466 1.866.67 2.616.47.526-.116.97-.464 1.208-.946.587.26 1.22.396 1.846.396h.11c.626 0 1.263-.136 1.846-.396.238.482.683.83 1.208.946.75.2 1.69-.004 2.616-.47.863-.465 1.964-.4 2.774-.6.405-.131.766-.267.94-.601.174-.34.142-.804-.106-1.484-.076-.242-.018-.571.036-.97.029-.136.055-.337.055-.536.004-.208-.04-.413-.132-.602-.206-.411-.551-.544-.864-.68-.312-.133-.598-.2-.797-.4-.213-.239-.403-.571-.663-.839a.424.424 0 00-.11-.135c.123-.805-.009-1.657-.287-2.489-.589-1.771-1.831-3.47-2.716-4.521-.75-1.067-.974-1.928-1.05-3.02-.065-1.491 1.056-5.965-3.17-6.298A5.11 5.11 0 0012.504 0z',
};

// OS is known synchronously from the UA; arch is refined in onMounted.
const detected = ref(detectPlatform(typeof navigator !== 'undefined' ? navigator.userAgent : ''));
const os = computed(() => detected.value.os);
const arch = computed<Arch>(() => clampArch(os.value, detected.value.arch));
const known = computed(() => os.value !== 'unknown');

// The complementary Mac arch, so an Intel visitor on an Apple-Silicon default
// (or vice versa) can grab the other build without scrolling.
const macAltArch = computed<Arch | null>(() =>
  os.value === 'darwin' ? (arch.value === 'arm64' ? 'amd64' : 'arm64') : null,
);

const recLabel = computed(() => `${osLabel(os.value)} (${archLabel(os.value, arch.value)})`);

onMounted(async () => {
  const uaData = (navigator as unknown as { userAgentData?: { getHighEntropyValues?: (h: string[]) => Promise<{ architecture?: string }> } }).userAgentData;
  if (!uaData?.getHighEntropyValues) return;
  try {
    const hv = await uaData.getHighEntropyValues(['architecture']);
    detected.value = { os: os.value, arch: clampArch(os.value, archFromUAData(hv.architecture, os.value)) };
  } catch {
    // High-entropy hints are best-effort; keep the synchronous default.
  }
});

const copied = ref(false);
function copyInstall() {
  navigator.clipboard?.writeText(INSTALL_CMD);
  copied.value = true;
  setTimeout(() => (copied.value = false), 1500);
}
</script>

<template>
  <DefaultLayout>
    <div class="wallfacer-page">
      <section class="hero hero-compact">
        <div class="hero-container">
          <svg class="wallfacer-hero-icon" width="48" height="48" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" style="image-rendering:pixelated;"><rect x="0" y="0" width="6" height="3" fill="#d97757"/><rect x="7" y="0" width="9" height="3" fill="#c4623f"/><rect x="0" y="4" width="4" height="3" fill="#a84e2e"/><rect x="5" y="4" width="6" height="3" fill="#d97757"/><rect x="12" y="4" width="4" height="3" fill="#c4623f"/><rect x="0" y="8" width="7" height="3" fill="#c4623f"/><rect x="8" y="8" width="8" height="3" fill="#a84e2e"/><rect x="0" y="12" width="3" height="4" fill="#d97757"/><rect x="4" y="12" width="6" height="4" fill="#a84e2e"/><rect x="11" y="12" width="5" height="4" fill="#d97757"/></svg>
          <h1 class="hero-title" v-html="t('wf.dl.title')"></h1>
          <p class="hero-sub" v-html="t('wf.dl.sub')"></p>
        </div>
      </section>

      <section class="section">
        <div class="section-container install-top">
          <div class="install-rec">
            <div class="install-rec__head">
              <span class="install-rec__glyph" aria-hidden="true">
                <svg v-if="known" width="22" height="22" viewBox="0 0 24 24" fill="currentColor"><path :d="OS_ICON[os]" /></svg>
                <svg v-else width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              </span>
              <span class="install-rec__eyebrow">
                {{ known ? t('wf.dl.rec.eyebrow', { os: recLabel }) : t('wf.dl.rec.generic') }}
              </span>
            </div>

            <div class="install-cmd">
              <code>{{ INSTALL_CMD }}</code>
              <button class="install-cmd__copy" :class="{ 'is-copied': copied }" @click="copyInstall" :title="t('wf.dl.copy')">
                <svg v-if="!copied" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                <svg v-else width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span>{{ copied ? t('wf.dl.copied') : t('wf.dl.copy') }}</span>
              </button>
            </div>
            <p class="install-rec__caption" v-html="t('wf.dl.cli.desc')"></p>

            <div class="install-rec__actions" v-if="known">
              <a class="btn btn-accent btn-lg" :href="downloadURL(os, arch)" target="_blank" rel="noopener">
                {{ t('wf.dl.download', { label: recLabel }) }}
              </a>
              <a v-if="macAltArch" class="install-rec__alt" :href="downloadURL('darwin', macAltArch)" target="_blank" rel="noopener">
                {{ t('wf.dl.alt.mac', { label: archLabel('darwin', macAltArch) }) }} &rarr;
              </a>
            </div>

            <p class="install-rec__note">
              {{ t('wf.dl.latest') }} <a :href="RELEASES_URL" target="_blank" rel="noopener">GitHub</a>.
            </p>
          </div>
        </div>
      </section>

      <section class="section">
        <div class="section-container">
          <span class="section-label">{{ t('wf.dl.all.title') }}</span>
          <p class="install-all__sub">{{ t('wf.dl.all.sub') }}</p>
          <div class="platform-grid">
            <div v-for="p in PLATFORMS" :key="p.id" class="platform-col" :class="{ 'is-detected': p.id === os }">
              <div class="platform-col__head">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><path :d="OS_ICON[p.id]" /></svg>
                <h3>{{ p.label }}</h3>
              </div>
              <a v-for="a in p.archs" :key="a" class="platform-dl" :href="downloadURL(p.id, a)" target="_blank" rel="noopener">
                <span class="platform-dl__arch">{{ archLabel(p.id, a) }}</span>
                <span class="platform-dl__file">{{ assetName(p.id, a) }}</span>
              </a>
            </div>
          </div>
          <p class="install-all__note" v-html="t('wf.dl.all.note')"></p>
        </div>
      </section>

      <section class="section">
        <div class="section-container">
          <span class="section-label">{{ t('wf.dl.prereq.title') }}</span>
          <ul class="setup-list install-prose">
            <li v-html="t('wf.dl.prereq.1')"></li>
            <li v-html="t('wf.dl.prereq.2')"></li>
            <li v-html="t('wf.dl.prereq.3')"></li>
          </ul>
        </div>
      </section>

      <section class="section">
        <div class="section-container">
          <span class="section-label">{{ t('wf.dl.quick.title') }}</span>
          <div class="install-flow">
            <div class="install-flow__step">
              <div class="install-flow__marker">1</div>
              <div class="install-flow__body">
                <h3 v-html="t('wf.dl.step1.title')"></h3>
                <pre><code>wallfacer doctor</code></pre>
                <p v-html="t('wf.dl.step1.desc')"></p>
              </div>
            </div>
            <div class="install-flow__step">
              <div class="install-flow__marker">2</div>
              <div class="install-flow__body">
                <h3 v-html="t('wf.dl.step2.title')"></h3>
                <pre><code>wallfacer run</code></pre>
                <p v-html="t('wf.dl.step2.desc')"></p>
              </div>
            </div>
            <div class="install-flow__step">
              <div class="install-flow__marker">3</div>
              <div class="install-flow__body">
                <h3 v-html="t('wf.dl.step3.title')"></h3>
                <p v-html="t('wf.dl.step3.desc')"></p>
                <ul>
                  <li v-html="t('wf.dl.step3.opt1')"></li>
                  <li v-html="t('wf.dl.step3.opt2')"></li>
                  <li v-html="t('wf.dl.step3.opt3')"></li>
                </ul>
              </div>
            </div>
            <div class="install-flow__step">
              <div class="install-flow__marker">4</div>
              <div class="install-flow__body">
                <h3 v-html="t('wf.dl.step4.title')"></h3>
                <p v-html="t('wf.dl.step4.desc')"></p>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section class="section">
        <div class="section-container">
          <span class="section-label">{{ t('wf.dl.cli.ref.title') }}</span>
          <div class="install-prose install-cliref">
            <pre><code>wallfacer run [flags]              # Start the server
wallfacer doctor                   # Check prerequisites
wallfacer status                   # Print board state
wallfacer status -watch            # Live-updating board
wallfacer status -json             # JSON output
wallfacer auth login               # Local-mode sign-in
wallfacer auth whoami              # Print saved principal</code></pre>
            <p v-html="t('wf.dl.cli.ref.flags')"></p>
            <table>
              <tr><td><code>-addr</code></td><td><code>:8080</code></td><td>Listen address</td></tr>
              <tr><td><code>-no-browser</code></td><td><code>false</code></td><td>Skip auto-opening browser</td></tr>
              <tr><td><code>-data</code></td><td><code>~/.wallfacer/data</code></td><td>Data directory</td></tr>
              <tr><td><code>-log-format</code></td><td><code>text</code></td><td>Log format: <code>text</code> or <code>json</code></td></tr>
            </table>
          </div>
        </div>
      </section>
    </div>
  </DefaultLayout>
</template>

<style scoped>
.install-top {
  max-width: 720px;
}

/* ===== Recommended panel ===== */
.install-rec {
  border: 1px solid var(--border-strong);
  border-radius: 12px;
  background: var(--bg-surface);
  padding: 24px;
  text-align: center;
}
.install-rec__head {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
  color: var(--accent);
}
.install-rec__glyph {
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.install-rec__eyebrow {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
}

.install-cmd {
  display: flex;
  align-items: stretch;
  justify-content: space-between;
  gap: 8px;
  background: var(--bg-raised);
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  padding: 10px 10px 10px 14px;
  text-align: left;
}
.install-cmd code {
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 13px;
  color: var(--text);
  align-self: center;
  overflow-x: auto;
  white-space: nowrap;
}
.install-cmd__copy {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  flex-shrink: 0;
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  padding: 4px 10px;
  border-radius: 6px;
  border: 1px solid var(--border-strong);
  background: var(--bg-surface);
  color: var(--text-secondary);
  cursor: pointer;
  transition: color 0.15s, border-color 0.15s;
}
.install-cmd__copy:hover { color: var(--text); border-color: var(--accent); }
.install-cmd__copy.is-copied { color: var(--accent); border-color: var(--accent); }

.install-rec__caption {
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.6;
  margin: 12px auto 0;
  max-width: 56ch;
}
.install-rec__caption :deep(code) {
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 11px;
  background: var(--bg-raised);
  padding: 1px 5px;
  border-radius: 4px;
}

.install-rec__actions {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  margin-top: 20px;
  padding-top: 20px;
  border-top: 1px solid var(--border);
}
.install-rec__alt {
  font-size: 12px;
  color: var(--text-muted);
}
.install-rec__alt:hover { color: var(--text); }
.install-rec__note {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 16px;
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
}
.install-rec__note a { color: var(--text-muted); }
.install-rec__note a:hover { color: var(--text); }

/* ===== All platforms ===== */
.install-all__sub {
  text-align: center;
  font-size: 14px;
  color: var(--text-secondary);
  margin-bottom: 24px;
}
.platform-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  max-width: 760px;
  margin: 0 auto;
}
.platform-col {
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 18px;
  background: var(--bg-surface);
}
.platform-col.is-detected { border-color: var(--accent); }
.platform-col__head {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text);
  margin-bottom: 12px;
}
.platform-col__head h3 { font-size: 14px; }
.platform-dl {
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding: 8px 10px;
  border-radius: 6px;
  color: var(--text);
  transition: background 0.15s;
}
.platform-dl + .platform-dl { margin-top: 4px; }
.platform-dl:hover { background: var(--bg-raised); color: var(--text); }
.platform-dl__arch { font-size: 13px; font-weight: 500; }
.platform-dl__file {
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 11px;
  color: var(--text-muted);
}
.install-all__note {
  text-align: center;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.6;
  margin: 24px auto 0;
  max-width: 60ch;
}
.install-all__note :deep(code) {
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 11px;
  background: var(--bg-raised);
  padding: 1px 5px;
  border-radius: 4px;
}

/* ===== Quick-start flow ===== */
.install-flow {
  max-width: 640px;
  margin: 0 auto;
  position: relative;
  padding-left: 36px;
}
.install-flow::before {
  content: '';
  position: absolute;
  left: 13px;
  top: 12px;
  bottom: 12px;
  width: 1px;
  background: var(--border-strong);
}
.install-flow__step {
  position: relative;
  padding: 8px 0 20px;
}
.install-flow__step:last-child { padding-bottom: 0; }
.install-flow__marker {
  position: absolute;
  left: -36px;
  top: 6px;
  width: 26px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 12px;
  font-weight: 700;
  color: var(--accent);
  background: var(--bg);
  border: 1px solid var(--border-strong);
  border-radius: 50%;
}
.install-flow__body h3 { font-size: 14px; margin-bottom: 8px; }
.install-flow__body p {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
  margin-bottom: 4px;
}
.install-flow__body pre {
  background: var(--bg-raised);
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  padding: 10px 14px;
  margin: 8px 0;
  overflow-x: auto;
}
.install-flow__body pre code {
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 13px;
  color: var(--text);
}
.install-flow__body ul { padding-left: 1.4em; margin: 8px 0 0; }
.install-flow__body li {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
  margin-bottom: 4px;
}
.install-flow__body :deep(code) {
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 12px;
  background: var(--bg-raised);
  padding: 1px 5px;
  border-radius: 4px;
}

/* ===== Prose blocks (prerequisites, CLI reference) ===== */
.install-prose {
  max-width: 720px;
  margin: 0 auto;
}
.install-cliref pre {
  background: var(--bg-raised);
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  padding: 14px 16px;
  overflow-x: auto;
  margin: 0 0 16px;
}
.install-cliref pre code {
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 13px;
  color: var(--text);
  line-height: 1.7;
}
.install-cliref > p {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 8px;
}
.install-cliref :deep(p code),
.install-cliref table :deep(code) {
  font-family: ui-monospace, 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 12px;
  background: var(--bg-raised);
  padding: 1px 5px;
  border-radius: 4px;
}
.install-cliref table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.install-cliref td {
  text-align: left;
  padding: 7px 10px;
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
  vertical-align: top;
}
.install-cliref tr:last-child td { border-bottom: none; }
.install-cliref td:first-child { white-space: nowrap; width: 1%; }

@media (max-width: 640px) {
  .platform-grid { grid-template-columns: 1fr; }
}
</style>
