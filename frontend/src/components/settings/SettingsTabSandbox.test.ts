import { afterEach, describe, expect, it, vi } from 'vitest';
import { createApp } from 'vue';
import { createPinia } from 'pinia';
import SettingsTabSandbox from './SettingsTabSandbox.vue';

const mock = vi.hoisted(() => ({ api: vi.fn() }));
vi.mock('../../api/client', () => ({ api: mock.api }));

let cleanup = () => {};
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

async function mount() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(SettingsTabSandbox);
  app.use(createPinia());
  app.mount(host);
  cleanup = () => { app.unmount(); host.remove(); };
  await settle();
  return host;
}

describe('provider credential storage', () => {
  it('saves an explicit choice and displays storage errors', async () => {
    mock.api.mockImplementation(async (method: string) => {
      if (method === 'PUT') throw new Error('Unlock the system keyring');
      return { secret_store: 'file', default_sandbox: 'claude' };
    });
    const host = await mount();
    const select = host.querySelector<HTMLSelectElement>('#credential-storage')!;
    expect(select.value).toBe('file');
    select.value = 'keyring';
    select.dispatchEvent(new Event('change'));
    const save = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Save harness configuration')!;
    save.click();
    await settle();
    expect(mock.api).toHaveBeenCalledWith('PUT', '/api/env', expect.objectContaining({ secret_store: 'keyring' }));
    expect(host.querySelector('#env-config-status')?.textContent).toContain('Unlock the system keyring');
    expect(select.value).toBe('keyring');
  });

  it('shows the saved review model and saves an edited one', async () => {
    mock.api.mockImplementation(async (method: string) => {
      if (method === 'PUT') return null;
      return { secret_store: 'file', default_sandbox: 'claude', review_model: 'gpt-5-codex' };
    });
    const host = await mount();
    const input = host.querySelector<HTMLInputElement>('#env-review-model')!;
    expect(input.value).toBe('gpt-5-codex');
    input.value = ' claude-opus-4-6 ';
    input.dispatchEvent(new Event('input'));
    const save = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Save harness configuration')!;
    save.click();
    await settle();
    expect(mock.api).toHaveBeenCalledWith('PUT', '/api/env', expect.objectContaining({ review_model: 'claude-opus-4-6' }));
  });

  it('shows a failed initial read and prevents overwriting an unreadable configuration', async () => {
    mock.api.mockRejectedValue(new Error('Credential bundle unavailable'));
    const host = await mount();
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Credential bundle unavailable');
    const save = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Save harness configuration')!;
    expect(save.disabled).toBe(true);
  });
});
