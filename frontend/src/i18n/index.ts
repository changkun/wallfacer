import { computed } from 'vue';
import { usePrefsStore } from '../stores/prefs';
import { en } from './en';
import { zh } from './zh';

type Dict = Record<string, string>;
const dicts: Record<string, Dict> = { en, zh };

export function useT() {
  const prefs = usePrefsStore();
  const t = computed(() => {
    const active = dicts[prefs.locale] || en;
    return (key: string, vars?: Record<string, string | number>): string => {
      let s = active[key] ?? en[key] ?? key;
      if (vars) {
        for (const k in vars) s = s.replace(new RegExp(`\\{${k}\\}`, 'g'), String(vars[k]));
      }
      return s;
    };
  });
  return t;
}
