<script setup lang="ts">
import SiteNav from '../components/SiteNav.vue';
import SiteFooter from '../components/SiteFooter.vue';
</script>

<template>
  <div class="page-shell">
    <SiteNav />
    <main class="page-content"><slot /></main>
    <SiteFooter />
  </div>
</template>
