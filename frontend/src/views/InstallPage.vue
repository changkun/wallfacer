<script setup lang="ts">
import { useHead } from '@unhead/vue';
import DefaultLayout from '../layouts/DefaultLayout.vue';
import { useT } from '../i18n';
import { useReveal } from '../composables/useReveal';

const t = useT();
useReveal();
useHead({ title: 'Download — Wallfacer', meta: [{ name: 'description', content: 'Download Wallfacer for macOS, Windows, and Linux.' }] });

function copyInstall() {
  navigator.clipboard.writeText('curl -fsSL https://latere.ai/wallfacer/install.sh | sh');
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
          <p class="hero-version">Latest release: <a href="https://github.com/changkun/wallfacer/releases/latest" target="_blank" rel="noopener">GitHub</a></p>
        </div>
      </section>

      <section class="section">
        <div class="section-container">
          <div class="download-grid">
            <a href="https://github.com/changkun/wallfacer/releases/latest" class="download-card" target="_blank" rel="noopener">
              <div class="download-icon">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.8-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z"/></svg>
              </div>
              <h3>macOS</h3>
              <p class="download-arch">Universal (Apple Silicon &amp; Intel)</p>
              <span class="download-file">.app</span>
            </a>
            <a href="https://github.com/changkun/wallfacer/releases/latest" class="download-card" target="_blank" rel="noopener">
              <div class="download-icon">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M3 12V6.75l8-1.25V12H3zm0 .5h8v6.5l-8-1.25V12.5zM11.5 5.34l9.5-1.59V12H11.5V5.34zM11.5 12.5H21v7.75l-9.5-1.59V12.5z"/></svg>
              </div>
              <h3>Windows</h3>
              <p class="download-arch">x86_64</p>
              <span class="download-file">.exe</span>
            </a>
            <a href="https://github.com/changkun/wallfacer/releases/latest" class="download-card" target="_blank" rel="noopener">
              <div class="download-icon">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M12.504 0c-.155 0-.315.008-.48.021-4.226.333-3.105 4.807-3.17 6.298-.076 1.092-.3 1.953-1.05 3.02-.885 1.051-2.127 2.75-2.716 4.521-.278.832-.41 1.684-.287 2.489a.424.424 0 00-.11.135c-.26.268-.45.6-.663.839-.199.199-.485.267-.797.4-.313.136-.658.269-.864.68-.09.189-.136.394-.132.602 0 .199.027.4.055.536.058.399.116.728.04.97-.249.68-.28 1.145-.106 1.484.174.334.535.47.94.601.81.2 1.91.135 2.774.6.926.466 1.866.67 2.616.47.526-.116.97-.464 1.208-.946.587.26 1.22.396 1.846.396h.11c.626 0 1.263-.136 1.846-.396.238.482.683.83 1.208.946.75.2 1.69-.004 2.616-.47.863-.465 1.964-.4 2.774-.6.405-.131.766-.267.94-.601.174-.34.142-.804-.106-1.484-.076-.242-.018-.571.036-.97.029-.136.055-.337.055-.536.004-.208-.04-.413-.132-.602-.206-.411-.551-.544-.864-.68-.312-.133-.598-.2-.797-.4-.213-.239-.403-.571-.663-.839a.424.424 0 00-.11-.135c.123-.805-.009-1.657-.287-2.489-.589-1.771-1.831-3.47-2.716-4.521-.75-1.067-.974-1.928-1.05-3.02-.065-1.491 1.056-5.965-3.17-6.298A5.11 5.11 0 0012.504 0z"/></svg>
              </div>
              <h3>Linux</h3>
              <p class="download-arch">x86_64</p>
              <span class="download-file">binary</span>
            </a>
          </div>
        </div>
      </section>

      <section class="section">
        <div class="section-container">
          <div class="about-content">
            <div class="about-block">
              <h2 v-html="t('wf.dl.cli.title')"></h2>
              <div class="install-block">
                <code>curl -fsSL https://latere.ai/wallfacer/install.sh | sh</code>
                <button class="copy-btn" @click="copyInstall" title="Copy"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg></button>
              </div>
              <p v-html="t('wf.dl.cli.desc')"></p>
            </div>

            <div class="about-block">
              <h2 v-html="t('wf.dl.prereq.title')"></h2>
              <ul class="setup-list">
                <li v-html="t('wf.dl.prereq.1')"></li>
                <li v-html="t('wf.dl.prereq.2')"></li>
                <li v-html="t('wf.dl.prereq.3')"></li>
              </ul>
            </div>

            <div class="about-block">
              <h2 v-html="t('wf.dl.quick.title')"></h2>
              <div class="setup-steps">
                <div class="setup-step">
                  <h3 v-html="t('wf.dl.step1.title')"></h3>
                  <pre><code>wallfacer doctor</code></pre>
                  <p v-html="t('wf.dl.step1.desc')"></p>
                </div>
                <div class="setup-step">
                  <h3 v-html="t('wf.dl.step2.title')"></h3>
                  <pre><code>wallfacer run</code></pre>
                  <p v-html="t('wf.dl.step2.desc')"></p>
                </div>
                <div class="setup-step">
                  <h3 v-html="t('wf.dl.step3.title')"></h3>
                  <p v-html="t('wf.dl.step3.desc')"></p>
                  <ul>
                    <li v-html="t('wf.dl.step3.opt1')"></li>
                    <li v-html="t('wf.dl.step3.opt2')"></li>
                    <li v-html="t('wf.dl.step3.opt3')"></li>
                  </ul>
                </div>
                <div class="setup-step">
                  <h3 v-html="t('wf.dl.step4.title')"></h3>
                  <p v-html="t('wf.dl.step4.desc')"></p>
                </div>
              </div>
            </div>

            <div class="about-block">
              <h2 v-html="t('wf.dl.cli.ref.title')"></h2>
              <pre><code>wallfacer run [flags]              # Start the server
wallfacer doctor                   # Check prerequisites
wallfacer status                   # Print board state
wallfacer status -watch            # Live-updating board
wallfacer status -json             # JSON output
wallfacer exec &lt;task-id&gt;           # Attach to container
wallfacer exec --sandbox claude    # New sandbox shell</code></pre>
              <p>Common flags for <code>wallfacer run</code>:</p>
              <table>
                <tr><td><code>-addr</code></td><td><code>:8080</code></td><td>Listen address</td></tr>
                <tr><td><code>-no-browser</code></td><td><code>false</code></td><td>Skip auto-opening browser</td></tr>
                <tr><td><code>-container</code></td><td>auto</td><td>Container runtime (<code>podman</code> or <code>docker</code>)</td></tr>
                <tr><td><code>-log-format</code></td><td><code>text</code></td><td>Log format: <code>text</code> or <code>json</code></td></tr>
              </table>
            </div>
          </div>
        </div>
      </section>
    </div>
  </DefaultLayout>
</template>
