import { defineStore } from 'pinia';
import { ref } from 'vue';
import { api, ApiError } from '../api/client';
import type { Me } from '../api/types';

export const useAuthStore = defineStore('auth', () => {
  const me = ref<Me | null>(null);
  const loaded = ref(false);
  const error = ref<string | null>(null);

  async function fetchMe() {
    try {
      me.value = await api<Me>('GET', '/api/me');
      error.value = null;
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        me.value = null;
      } else {
        error.value = (e as Error).message;
      }
    } finally {
      loaded.value = true;
    }
  }

  return { me, loaded, error, fetchMe };
});
