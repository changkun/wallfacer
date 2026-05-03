import { onMounted, onUnmounted, ref } from 'vue';

export interface TocEntry {
  id: string;
  text: string;
}

export function useToc(containerSelector: string) {
  const entries = ref<TocEntry[]>([]);
  const activeId = ref('');
  let observer: IntersectionObserver | null = null;

  onMounted(() => {
    if (import.meta.env.SSR) return;

    const container = document.querySelector(containerSelector);
    if (!container) return;

    const headings = container.querySelectorAll('h2[id]');
    entries.value = Array.from(headings).map(h => ({
      id: h.id,
      text: h.textContent || '',
    }));

    if (entries.value.length === 0) return;

    observer = new IntersectionObserver((items) => {
      items.forEach(item => {
        if (item.isIntersecting) {
          activeId.value = item.target.id;
        }
      });
    }, { rootMargin: '-96px 0px -60% 0px', threshold: 0 });

    headings.forEach(h => observer!.observe(h));
  });

  onUnmounted(() => {
    observer?.disconnect();
  });

  return { entries, activeId };
}
