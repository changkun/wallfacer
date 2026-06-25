// Single shared 1s ticker. Every consumer reads the same reactive `now` ref
// so N components share one setInterval instead of running N timers (and N
// useless recomputes/sec). The interval starts on the first subscriber and
// stops when the last one unmounts.

import { onBeforeUnmount, readonly, ref, type Ref } from 'vue';

const now = ref(Date.now());
let handle: ReturnType<typeof setInterval> | null = null;
let subscribers = 0;

function ensureTicking() {
  if (handle === null) {
    now.value = Date.now();
    handle = setInterval(() => { now.value = Date.now(); }, 1000);
  }
}

function stopIfIdle() {
  if (subscribers === 0 && handle !== null) {
    clearInterval(handle);
    handle = null;
  }
}

/** Subscribe to the shared 1s clock. Returns a read-only `now` ref. The
 *  subscription is released automatically on component unmount. */
export function useNow(): Readonly<Ref<number>> {
  subscribers++;
  ensureTicking();
  onBeforeUnmount(() => {
    subscribers--;
    stopIfIdle();
  });
  return readonly(now);
}
