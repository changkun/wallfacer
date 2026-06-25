// useNow shares one 1s ticker across all consumers. These tests pin that a
// single setInterval backs N subscribers and that it stops once the last
// subscriber unmounts (no leaked timer).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createApp, defineComponent, h, type App } from 'vue';
import { useNow } from './useNow';

const apps: App[] = [];
const hosts: HTMLElement[] = [];

function mountConsumer(): App {
  const C = defineComponent({
    setup() {
      const now = useNow();
      return () => h('span', String(now.value));
    },
  });
  // Each app gets its own host; mounting several apps into one element makes
  // Vue replace (and unmount) the prior one, which would skew the counts.
  const host = document.createElement('div');
  document.body.appendChild(host);
  hosts.push(host);
  const app = createApp(C);
  app.mount(host);
  apps.push(app);
  return app;
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  while (apps.length) apps.pop()!.unmount();
  while (hosts.length) hosts.pop()!.remove();
  vi.useRealTimers();
});

describe('useNow', () => {
  it('runs a single interval for multiple subscribers', () => {
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval');
    mountConsumer();
    mountConsumer();
    mountConsumer();
    expect(setIntervalSpy).toHaveBeenCalledTimes(1);
  });

  it('clears the interval once the last subscriber unmounts', () => {
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval');
    const a = mountConsumer();
    const b = mountConsumer();

    a.unmount();
    apps.splice(apps.indexOf(a), 1);
    expect(clearIntervalSpy).not.toHaveBeenCalled();

    b.unmount();
    apps.splice(apps.indexOf(b), 1);
    expect(clearIntervalSpy).toHaveBeenCalledTimes(1);
  });
});
