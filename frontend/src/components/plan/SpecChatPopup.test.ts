// Geometry behaviour for the floating spec-mode chat popup: default bottom-right
// anchor, drag + persist, viewport clamping, and open-state persistence. The
// chat children and network are stubbed — this pins the popup chrome only.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';

vi.mock('./ChatMessageList.vue', () => ({
  default: { name: 'ChatMessageList', template: '<div class="cml-stub"></div>' },
}));
vi.mock('./ChatComposer.vue', () => ({
  default: { name: 'ChatComposer', template: '<div class="cc-stub"></div>' },
}));

const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

import SpecChatPopup from './SpecChatPopup.vue';

const KEY = 'wallfacer-spec-chat-popup';

function setViewport(w: number, h: number) {
  Object.defineProperty(window, 'innerWidth', { value: w, configurable: true });
  Object.defineProperty(window, 'innerHeight', { value: h, configurable: true });
}

async function mount(): Promise<{ app: App; host: HTMLElement; vm: any }> {
  setActivePinia(createPinia());
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(SpecChatPopup);
  const vm = app.mount(host) as unknown as { toggle: () => void; isOpen: boolean };
  await nextTick();
  return { app, host, vm };
}

function pointer(type: string, x: number, y: number): MouseEvent {
  return new MouseEvent(type, { clientX: x, clientY: y, bubbles: true });
}

describe('SpecChatPopup geometry', () => {
  beforeEach(() => {
    memStore.clear();
    setViewport(1280, 800);
    globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({ threads: [] }), { status: 200 })) as never;
  });
  afterEach(() => { document.body.innerHTML = ''; });

  it('starts collapsed with a launcher, no window', async () => {
    const { host } = await mount();
    expect(host.querySelector('.scp-launcher')).not.toBeNull();
    // v-show keeps the window in the DOM but hidden.
    const win = host.querySelector('.scp-window') as HTMLElement;
    expect(win.style.display).toBe('none');
  });

  it('opens bottom-right by default and persists open state', async () => {
    const { host, vm } = await mount();
    vm.toggle();
    await nextTick();
    const win = host.querySelector('.scp-window') as HTMLElement;
    expect(win.style.display).not.toBe('none');
    // Default 380x520 anchored 16px from the bottom-right of 1280x800.
    expect(win.style.left).toBe(`${1280 - 380 - 16}px`);
    expect(win.style.top).toBe(`${800 - 520 - 16}px`);
    const saved = JSON.parse(memStore.get(KEY)!);
    expect(saved.open).toBe(true);
  });

  it('drags the window and persists the new position', async () => {
    const { host, vm } = await mount();
    vm.toggle();
    await nextTick();
    const header = host.querySelector('.scp-header') as HTMLElement;
    header.dispatchEvent(pointer('pointerdown', 900, 250));
    window.dispatchEvent(pointer('pointermove', 700, 230));
    window.dispatchEvent(pointer('pointerup', 700, 230));
    await nextTick();
    const win = host.querySelector('.scp-window') as HTMLElement;
    // Moved by (-200, -20) from the default anchor (884, 264) → (684, 244),
    // both within the viewport so neither axis clamps.
    expect(win.style.left).toBe('684px');
    expect(win.style.top).toBe('244px');
    expect(JSON.parse(memStore.get(KEY)!).x).toBe(684);
    expect(JSON.parse(memStore.get(KEY)!).y).toBe(244);
  });

  it('clamps an out-of-viewport saved position back on screen', async () => {
    memStore.set(KEY, JSON.stringify({ x: 5000, y: 5000, w: 380, h: 520, open: true }));
    const { host } = await mount();
    const win = host.querySelector('.scp-window') as HTMLElement;
    expect(win.style.left).toBe(`${1280 - 380}px`);
    expect(win.style.top).toBe(`${800 - 520}px`);
  });

  it('restores persisted geometry and open state on reopen', async () => {
    memStore.set(KEY, JSON.stringify({ x: 100, y: 120, w: 420, h: 480, open: true }));
    const { host } = await mount();
    const win = host.querySelector('.scp-window') as HTMLElement;
    expect(win.style.display).not.toBe('none');
    expect(win.style.left).toBe('100px');
    expect(win.style.top).toBe('120px');
    expect(win.style.width).toBe('420px');
    expect(win.style.height).toBe('480px');
  });
});
