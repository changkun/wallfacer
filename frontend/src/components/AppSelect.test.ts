// AppSelect is the app-native replacement for the browser <select>. These pin
// the contract callers rely on: the trigger shows the selected option's label
// (or the placeholder when nothing matches), opening reveals a listbox, picking
// an option emits the option's value with its original type (number stays a
// number, not a stringified one), and disabled options can't be selected.

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { nextTick, ref, h, createApp, type App } from 'vue';
import AppSelect from './AppSelect.vue';

let app: App | null = null;
let host: HTMLElement;

function mount<T extends string | number>(props: {
  modelValue: T;
  options: { value: T; label: string; disabled?: boolean }[];
  placeholder?: string;
}) {
  host = document.createElement('div');
  document.body.appendChild(host);
  const model = ref<T>(props.modelValue);
  const emitted: T[] = [];
  app = createApp({
    render: () =>
      h(AppSelect, {
        modelValue: model.value,
        options: props.options,
        placeholder: props.placeholder,
        // AppSelect is generic, so via h() the emit handler is typed to the
        // constraint (string | number), not the narrower T; cast back to T.
        'onUpdate:modelValue': (v: string | number) => {
          emitted.push(v as T);
          model.value = v as T;
        },
      }),
  });
  app.mount(host);
  return { model, emitted };
}

beforeEach(() => {
  app = null;
});

afterEach(() => {
  app?.unmount();
  host?.remove();
});

describe('AppSelect', () => {
  it('shows the selected option label on the trigger', async () => {
    mount({ modelValue: 'b', options: [
      { value: 'a', label: 'Apple' },
      { value: 'b', label: 'Banana' },
    ] });
    await nextTick();
    const trigger = host.querySelector('.app-select__trigger') as HTMLElement;
    expect(trigger.textContent).toContain('Banana');
  });

  it('shows the placeholder when nothing matches', async () => {
    mount({ modelValue: '', placeholder: '(pick one)', options: [
      { value: 'a', label: 'Apple' },
    ] });
    await nextTick();
    const trigger = host.querySelector('.app-select__trigger') as HTMLElement;
    expect(trigger.textContent).toContain('(pick one)');
  });

  it('opens the listbox on trigger click and closes after a pick', async () => {
    mount({ modelValue: 'a', options: [
      { value: 'a', label: 'Apple' },
      { value: 'b', label: 'Banana' },
    ] });
    await nextTick();
    expect(host.querySelector('[role="listbox"]')).toBeNull();

    (host.querySelector('.app-select__trigger') as HTMLElement).click();
    await nextTick();
    expect(host.querySelector('[role="listbox"]')).not.toBeNull();

    const opts = host.querySelectorAll('.app-select__opt');
    (opts[1] as HTMLElement).click();
    await nextTick();
    expect(host.querySelector('[role="listbox"]')).toBeNull();
  });

  it('emits the picked value preserving its number type', async () => {
    const { emitted } = mount<number>({ modelValue: 5, options: [
      { value: 5, label: '5 min' },
      { value: 60, label: '60 min' },
    ] });
    await nextTick();
    (host.querySelector('.app-select__trigger') as HTMLElement).click();
    await nextTick();
    const opts = host.querySelectorAll('.app-select__opt');
    (opts[1] as HTMLElement).click();
    await nextTick();
    expect(emitted).toEqual([60]);
    expect(typeof emitted[0]).toBe('number');
  });

  it('does not emit when a disabled option is clicked', async () => {
    const { emitted } = mount({ modelValue: 'a', options: [
      { value: 'a', label: 'Apple' },
      { value: 'b', label: 'Banana', disabled: true },
    ] });
    await nextTick();
    (host.querySelector('.app-select__trigger') as HTMLElement).click();
    await nextTick();
    const opts = host.querySelectorAll('.app-select__opt');
    (opts[1] as HTMLElement).click();
    await nextTick();
    expect(emitted).toEqual([]);
  });
});
