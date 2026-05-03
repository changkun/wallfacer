import { ref } from 'vue';
import { api } from '../api/client';
import type { EnvConfig, EnvUpdatePayload } from '../api/types';

const env = ref<EnvConfig | null>(null);
const loading = ref(false);
const error = ref('');

export function useEnvConfig() {
  async function fetchEnv() {
    loading.value = true;
    error.value = '';
    try {
      env.value = await api<EnvConfig>('GET', '/api/env');
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
  }

  async function updateEnv(patch: EnvUpdatePayload): Promise<void> {
    await api('PUT', '/api/env', patch);
    await fetchEnv();
  }

  return { env, loading, error, fetchEnv, updateEnv };
}
