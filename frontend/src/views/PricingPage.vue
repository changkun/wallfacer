<script setup lang="ts">
import { useHead } from '@unhead/vue';
import DefaultLayout from '../layouts/DefaultLayout.vue';
import { useT } from '../i18n';
import { useReveal } from '../composables/useReveal';

const t = useT();
useReveal();
useHead({ title: 'Pricing — Wallfacer', meta: [{ name: 'description', content: 'Wallfacer pricing. Start free. Scale when you\'re ready.' }] });
</script>

<template>
  <DefaultLayout>
    <div class="wallfacer-page">
      <section class="hero hero-compact">
        <div class="hero-container">
          <h1 class="hero-title" v-html="t('wf.pricing.title')"></h1>
          <p class="hero-sub" v-html="t('wf.pricing.sub')"></p>
        </div>
      </section>

      <section class="section">
        <div class="section-container">
          <div class="pricing-grid">
            <div class="pricing-card pricing-card-featured">
              <div class="pricing-header">
                <h3 v-html="t('wf.pricing.local')"></h3>
                <span class="pricing-badge" v-html="t('wf.pricing.local.badge')"></span>
              </div>
              <div class="pricing-amount">
                <span class="pricing-value">$0</span>
                <span class="pricing-period" v-html="t('wf.pricing.local.period')"></span>
              </div>
              <p class="pricing-desc" v-html="t('wf.pricing.local.desc')"></p>
              <ul class="pricing-features">
                <li v-html="t('wf.pricing.local.f1')"></li>
                <li v-html="t('wf.pricing.local.f2')"></li>
                <li v-html="t('wf.pricing.local.f3')"></li>
                <li v-html="t('wf.pricing.local.f4')"></li>
                <li v-html="t('wf.pricing.local.f5')"></li>
                <li v-html="t('wf.pricing.local.f6')"></li>
                <li v-html="t('wf.pricing.local.f7')"></li>
              </ul>
              <router-link to="/install" class="btn btn-accent" v-html="t('wf.pricing.local.btn')"></router-link>
            </div>
            <div class="pricing-card pricing-card-muted">
              <div class="pricing-header">
                <h3 v-html="t('wf.pricing.cloud')"></h3>
                <span class="pricing-badge pricing-badge-muted" v-html="t('wf.pricing.cloud.badge')"></span>
              </div>
              <div class="pricing-amount">
                <span class="pricing-value">&mdash;</span>
              </div>
              <p class="pricing-desc" v-html="t('wf.pricing.cloud.desc')"></p>
              <ul class="pricing-features">
                <li v-html="t('wf.pricing.cloud.f1')"></li>
                <li v-html="t('wf.pricing.cloud.f2')"></li>
                <li v-html="t('wf.pricing.cloud.f3')"></li>
                <li v-html="t('wf.pricing.cloud.f4')"></li>
                <li v-html="t('wf.pricing.cloud.f5')"></li>
              </ul>
              <a href="mailto:contact@latere.ai" class="btn btn-ghost" v-html="t('wf.pricing.cloud.btn')"></a>
            </div>
            <div class="pricing-card pricing-card-muted">
              <div class="pricing-header">
                <h3 v-html="t('wf.pricing.enterprise')"></h3>
                <span class="pricing-badge pricing-badge-muted" v-html="t('wf.pricing.enterprise.badge')"></span>
              </div>
              <div class="pricing-amount">
                <span class="pricing-value">&mdash;</span>
              </div>
              <p class="pricing-desc" v-html="t('wf.pricing.enterprise.desc')"></p>
              <ul class="pricing-features">
                <li v-html="t('wf.pricing.enterprise.f1')"></li>
                <li v-html="t('wf.pricing.enterprise.f2')"></li>
                <li v-html="t('wf.pricing.enterprise.f3')"></li>
                <li v-html="t('wf.pricing.enterprise.f4')"></li>
                <li v-html="t('wf.pricing.enterprise.f5')"></li>
              </ul>
              <a href="mailto:contact@latere.ai" class="btn btn-ghost" v-html="t('wf.pricing.enterprise.btn')"></a>
            </div>
          </div>
        </div>
      </section>
    </div>
  </DefaultLayout>
</template>
