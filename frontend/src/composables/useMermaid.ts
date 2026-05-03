import { nextTick, onMounted, onUnmounted, watch, type Ref } from 'vue';

export function useMermaid(containerSelector: string, trigger?: Ref<unknown>) {
  let cleanup: (() => void) | null = null;

  async function init() {
    if (import.meta.env.SSR) return;

    await nextTick();
    const container = document.querySelector(containerSelector);
    if (!container) return;

    const blocks = container.querySelectorAll('pre code.language-mermaid');
    if (blocks.length === 0) return;

    const mermaid = await import('mermaid');
    mermaid.default.initialize({ startOnLoad: false, theme: 'default' });

    blocks.forEach((block, i) => {
      const pre = block.parentElement;
      if (!pre) return;
      const div = document.createElement('div');
      div.className = 'mermaid';
      div.id = `mermaid-${i}`;
      div.textContent = block.textContent;
      pre.replaceWith(div);
    });

    await mermaid.default.run();

    if (cleanup) cleanup();
    const handlers: Array<[Element, EventListener]> = [];
    container.querySelectorAll('.mermaid').forEach(div => {
      const handler = () => openOverlay(div as HTMLElement);
      div.addEventListener('click', handler);
      handlers.push([div, handler]);
    });
    cleanup = () => handlers.forEach(([el, h]) => el.removeEventListener('click', h));
  }

  function openOverlay(source: HTMLElement) {
    const svg = source.querySelector('svg');
    if (!svg) return;

    const overlay = document.createElement('div');
    overlay.className = 'mermaid-overlay';

    const inner = document.createElement('div');
    inner.className = 'mermaid-overlay-inner';
    inner.innerHTML = svg.outerHTML;
    overlay.appendChild(inner);

    overlay.addEventListener('click', (e) => {
      if (e.target === overlay) overlay.remove();
    });
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { overlay.remove(); document.removeEventListener('keydown', onEsc); }
    };
    document.addEventListener('keydown', onEsc);
    document.body.appendChild(overlay);
  }

  onMounted(init);
  if (trigger) watch(trigger, init);
  onUnmounted(() => { if (cleanup) cleanup(); });
}
