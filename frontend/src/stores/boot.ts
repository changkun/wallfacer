import { defineStore } from 'pinia';
import { computed } from 'vue';

export interface WallfacerConfig {
  mode: 'local' | 'cloud';
  serverApiKey: string;
  version: string;
}

declare global {
  interface Window {
    __WALLFACER__?: WallfacerConfig;
  }
}

const defaults: WallfacerConfig = {
  mode: 'cloud',
  serverApiKey: '',
  version: '',
};

function read(): WallfacerConfig {
  if (typeof window !== 'undefined' && window.__WALLFACER__) {
    return { ...defaults, ...window.__WALLFACER__ };
  }
  return defaults;
}

export const useBootStore = defineStore('boot', () => {
  const cfg = read();

  const mode = computed(() => cfg.mode);
  const isLocal = computed(() => cfg.mode === 'local');
  const isCloud = computed(() => cfg.mode === 'cloud');
  const serverApiKey = computed(() => cfg.serverApiKey);
  const version = computed(() => cfg.version);

  return { mode, isLocal, isCloud, serverApiKey, version };
});
