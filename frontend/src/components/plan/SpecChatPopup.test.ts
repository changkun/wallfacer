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

  it('anchors the launcher bottom-right by default', async () => {
    const { host } = await mount();
    const fab = host.querySelector('.scp-launcher') as HTMLElement;
    // 48px button, 20px from the bottom-right of 1280x800.
    expect(fab.style.left).toBe(`${1280 - 48 - 20}px`);
    expect(fab.style.top).toBe(`${800 - 48 - 20}px`);
  });

  it('a plain launcher click (no drag) opens the chat', async () => {
    const { host } = await mount();
    const fab = host.querySelector('.scp-launcher') as HTMLElement;
    fab.dispatchEvent(pointer('pointerdown', 1212, 732));
    window.dispatchEvent(pointer('pointerup', 1212, 732));
    await nextTick();
    const win = host.querySelector('.scp-window') as HTMLElement;
    expect(win.style.display).not.toBe('none');
    expect(JSON.parse(memStore.get(KEY)!).open).toBe(true);
  });

  it('drags the launcher anywhere and persists it without opening', async () => {
    const { host } = await mount();
    const fab = host.querySelector('.scp-launcher') as HTMLElement;
    fab.dispatchEvent(pointer('pointerdown', 1212, 732));
    window.dispatchEvent(pointer('pointermove', 200, 150)); // far past the threshold
    window.dispatchEvent(pointer('pointerup', 200, 150));
    await nextTick();
    // Moved by (-1012, -582) from (1212, 732) → (200, 150), both on-screen.
    expect(fab.style.left).toBe('200px');
    expect(fab.style.top).toBe('150px');
    const saved = JSON.parse(memStore.get(KEY)!);
    expect(saved.lx).toBe(200);
    expect(saved.ly).toBe(150);
    expect(saved.open).toBe(false); // a drag must not open the chat
    expect(host.querySelector('.scp-window')?.matches('[style*="display: none"]')).toBe(true);
  });

  it('clamps a launcher drag so it stays fully on-screen', async () => {
    const { host } = await mount();
    const fab = host.querySelector('.scp-launcher') as HTMLElement;
    fab.dispatchEvent(pointer('pointerdown', 1212, 732));
    window.dispatchEvent(pointer('pointermove', 5000, 5000)); // far off the bottom-right
    window.dispatchEvent(pointer('pointerup', 5000, 5000));
    await nextTick();
    // Bottoms out at viewport minus the 48px button.
    expect(fab.style.left).toBe(`${1280 - 48}px`);
    expect(fab.style.top).toBe(`${800 - 48}px`);
  });

  it('restores a persisted launcher position', async () => {
    memStore.set(KEY, JSON.stringify({ lx: 40, ly: 300, open: false }));
    const { host } = await mount();
    const fab = host.querySelector('.scp-launcher') as HTMLElement;
    expect(fab.style.left).toBe('40px');
    expect(fab.style.top).toBe('300px');
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

  it('resizes from the east edge, keeping the top-left pinned', async () => {
    memStore.set(KEY, JSON.stringify({ x: 200, y: 150, w: 400, h: 400, open: true }));
    const { host } = await mount();
    const e = host.querySelector('.scp-rz-e') as HTMLElement;
    e.dispatchEvent(pointer('pointerdown', 600, 350));
    window.dispatchEvent(pointer('pointermove', 660, 350)); // +60 wider
    window.dispatchEvent(pointer('pointerup', 660, 350));
    await nextTick();
    const win = host.querySelector('.scp-window') as HTMLElement;
    expect(win.style.left).toBe('200px'); // origin unchanged
    expect(win.style.width).toBe('460px');
    expect(JSON.parse(memStore.get(KEY)!).w).toBe(460);
  });

  it('resizes from the west edge, shifting the origin while pinning the right edge', async () => {
    memStore.set(KEY, JSON.stringify({ x: 200, y: 150, w: 400, h: 400, open: true }));
    const { host } = await mount();
    const w = host.querySelector('.scp-rz-w') as HTMLElement;
    w.dispatchEvent(pointer('pointerdown', 200, 350));
    window.dispatchEvent(pointer('pointermove', 150, 350)); // drag left edge 50px left
    window.dispatchEvent(pointer('pointerup', 150, 350));
    await nextTick();
    const win = host.querySelector('.scp-window') as HTMLElement;
    // Right edge pinned at 600; left moves to 150, width grows to 450.
    expect(win.style.left).toBe('150px');
    expect(win.style.width).toBe('450px');
  });

  it('resizes from the north edge, shifting y while pinning the bottom edge', async () => {
    memStore.set(KEY, JSON.stringify({ x: 200, y: 150, w: 400, h: 400, open: true }));
    const { host } = await mount();
    const n = host.querySelector('.scp-rz-n') as HTMLElement;
    n.dispatchEvent(pointer('pointerdown', 400, 150));
    window.dispatchEvent(pointer('pointermove', 400, 110)); // drag top edge 40px up
    window.dispatchEvent(pointer('pointerup', 400, 110));
    await nextTick();
    const win = host.querySelector('.scp-window') as HTMLElement;
    // Bottom edge pinned at 550; top moves to 110, height grows to 440.
    expect(win.style.top).toBe('110px');
    expect(win.style.height).toBe('440px');
  });

  it('clamps a wildly out-of-viewport saved position to a grabbable strip', async () => {
    memStore.set(KEY, JSON.stringify({ x: 5000, y: 5000, w: 380, h: 520, open: true }));
    const { host } = await mount();
    const win = host.querySelector('.scp-window') as HTMLElement;
    // 48px of the popup stays on-screen on each axis so the header is reachable.
    expect(win.style.left).toBe(`${1280 - 48}px`);
    expect(win.style.top).toBe(`${800 - 48}px`);
  });

  it('lets the window be dragged partly off the left edge, keeping a grabbable strip', async () => {
    memStore.set(KEY, JSON.stringify({ x: 200, y: 150, w: 380, h: 520, open: true }));
    const { host } = await mount();
    const header = host.querySelector('.scp-header') as HTMLElement;
    header.dispatchEvent(pointer('pointerdown', 300, 250));
    window.dispatchEvent(pointer('pointermove', -1000, 250)); // far past the left edge
    window.dispatchEvent(pointer('pointerup', -1000, 250));
    await nextTick();
    const win = host.querySelector('.scp-window') as HTMLElement;
    // x bottoms out at KEEP_VISIBLE - w = 48 - 380, leaving 48px on screen.
    expect(win.style.left).toBe(`${48 - 380}px`);
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
