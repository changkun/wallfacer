// AccountControl decides what a sign-in and an org switch do when the server
// reports that the browser redirect behind /login cannot complete on this
// instance (auth_redirect_enabled false in /api/config): the browser is not
// sent to /login, and the device modal shows the reason. The shared account
// menu is replaced by two buttons that emit what it emits.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createApp, h, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';

const { apiMock, loginMock, switchOrgMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  loginMock: vi.fn(),
  switchOrgMock: vi.fn(),
}));

// Keep the real ApiError so the composable's `instanceof ApiError` 503 check works.
vi.mock('../api/client', async (orig) => {
  const actual = await orig<typeof import('../api/client')>();
  return { ...actual, api: apiMock };
});
vi.mock('../stores/auth', () => ({
  useAuthStore: () => ({
    me: null,
    login: loginMock,
    logout: vi.fn(),
    switchOrg: switchOrgMock,
    fetchMe: vi.fn(),
  }),
}));
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock('latere-ui', async () => {
  const { defineComponent, h: render } = await import('vue');
  return {
    AccountMenu: defineComponent({
      emits: ['login', 'switch-org'],
      setup(_, { emit }) {
        return () =>
          render('div', [
            render('button', { class: 'stub-login', onClick: () => emit('login') }),
            render('button', { class: 'stub-switch', onClick: () => emit('switch-org', 'org-b') }),
          ]);
      },
    }),
    AccountPrefs: defineComponent({ render: () => null }),
  };
});

import { ApiError } from '../api/client';
import type { ServerConfig } from '../api/types';
import { en } from '../i18n/en';
import { useTaskStore } from '../stores/tasks';
import AccountControl from './AccountControl.vue';

const startBody = {
  user_code: 'ABCD-1234',
  verification_uri: 'https://auth.latere.ai/device',
  verification_uri_complete: 'https://auth.latere.ai/device?user_code=ABCD-1234',
  expires_in: 600,
};

let app: App | null = null;
let host: HTMLElement;
let pinia: Pinia;

function mount(config: Partial<ServerConfig>) {
  useTaskStore().config = { auth_enabled: true, ...config } as ServerConfig;
  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({ render: () => h(AccountControl) });
  app.use(pinia);
  app.mount(host);
}

async function settle() {
  for (let i = 0; i < 5; i++) await Promise.resolve();
  await nextTick();
}

function click(selector: string) {
  (host.querySelector(selector) as HTMLElement).click();
}

function modalError(): string {
  return document.body.querySelector('.device-error')?.textContent ?? '';
}

beforeEach(() => {
  pinia = createPinia();
  setActivePinia(pinia);
  apiMock.mockReset();
  loginMock.mockReset();
  switchOrgMock.mockReset();
  app = null;
});

afterEach(() => {
  app?.unmount();
  host?.remove();
  document.querySelectorAll('.modal-overlay').forEach((n) => n.remove());
});

describe('AccountControl', () => {
  it('falls back to the /login redirect when device sign-in is unavailable', async () => {
    apiMock.mockRejectedValueOnce(new ApiError(503, null, 'device-code auth not configured'));
    mount({ auth_redirect_enabled: true });

    click('.stub-login');
    await settle();

    expect(loginMock).toHaveBeenCalledTimes(1);
  });

  it('shows the reason instead of the /login redirect when redirect sign-in is off', async () => {
    apiMock.mockRejectedValueOnce(new ApiError(503, null, 'device-code auth not configured'));
    mount({ auth_redirect_enabled: false });

    click('.stub-login');
    await settle();

    expect(loginMock).not.toHaveBeenCalled();
    expect(modalError()).toContain(en['auth.redirect_unavailable']);
  });

  it('still signs in by device code when redirect sign-in is off', async () => {
    apiMock.mockResolvedValueOnce(startBody);
    mount({ auth_redirect_enabled: false });

    click('.stub-login');
    await settle();

    expect(loginMock).not.toHaveBeenCalled();
    expect(document.body.querySelector('.device-code')?.textContent).toContain('ABCD-1234');
    // Unmounting does not stop the poll timer; cancel the flow to clear it.
    apiMock.mockResolvedValueOnce(null);
    (document.body.querySelector('.device-btn--ghost') as HTMLElement).click();
    await settle();
  });

  it('switches org through the session store when redirect sign-in is on', async () => {
    mount({ auth_redirect_enabled: true });

    click('.stub-switch');
    await settle();

    expect(switchOrgMock).toHaveBeenCalledWith('org-b');
  });

  it('shows the reason instead of switching org when redirect sign-in is off', async () => {
    mount({ auth_redirect_enabled: false });

    click('.stub-switch');
    await settle();

    expect(switchOrgMock).not.toHaveBeenCalled();
    expect(modalError()).toContain(en['auth.redirect_unavailable']);
  });
});
