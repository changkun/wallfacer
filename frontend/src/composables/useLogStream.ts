import { ref, watch, onUnmounted } from 'vue';
import type { Ref } from 'vue';

export function useLogStream(taskId: Ref<string | null>) {
  const lines = ref<string[]>([]);
  const streaming = ref(false);
  let es: EventSource | null = null;

  function addAuthParam(url: string): string {
    if (typeof window !== 'undefined' && window.__WALLFACER__?.serverApiKey) {
      const sep = url.includes('?') ? '&' : '?';
      return url + sep + 'token=' + encodeURIComponent(window.__WALLFACER__.serverApiKey);
    }
    return url;
  }

  function connect(id: string) {
    stop();
    lines.value = [];
    streaming.value = true;
    es = new EventSource(addAuthParam(`/api/tasks/${id}/logs`));
    es.onmessage = (ev) => {
      try {
        const parsed = JSON.parse(ev.data);
        if (parsed.line) {
          lines.value.push(parsed.line);
          if (lines.value.length > 500) lines.value.splice(0, lines.value.length - 500);
        }
      } catch {
        lines.value.push(ev.data);
      }
    };
    es.onerror = () => {
      streaming.value = false;
      es?.close();
      es = null;
    };
  }

  function stop() {
    es?.close();
    es = null;
    streaming.value = false;
  }

  watch(taskId, (id) => {
    if (id) connect(id);
    else stop();
  }, { immediate: true });

  onUnmounted(stop);

  return { lines, streaming };
}
