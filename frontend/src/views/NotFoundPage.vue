<script setup lang="ts">
import { useHead } from '@unhead/vue';
import DefaultLayout from '../layouts/DefaultLayout.vue';
import { useReveal } from '../composables/useReveal';

useReveal();
useHead({ title: '404 — Wallfacer' });
</script>

<template>
  <DefaultLayout>
    <div class="wallfacer-page">
      <div class="section" style="text-align:center;padding-top:120px;">
        <h1>404</h1>
        <p style="margin-top:16px;color:var(--text-secondary)">Page not found.</p>
        <p style="margin-top:24px"><router-link to="/" style="text-decoration:underline">Back to home</router-link></p>
      </div>
    </div>
  </DefaultLayout>
</template>
