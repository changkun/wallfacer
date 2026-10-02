// The GitHub tab's "Sign in" starts sign-in the way the account menu does: by
// device code, with the browser redirect to /login only as the fallback where
// device sign-in is not wired, and the reason in the modal where the server
// reports that redirect off. Once the sign-in completes the tab loads the
// GitHub connection of the account that signed in.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createApp, h, nextTick, reactive, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';

const { apiMock, loginMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  loginMock: vi.fn(),
}));

// Keep the real ApiError so the composable's `instanceof ApiError` 503 check works.
vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>();
  return { ...actual, api: apiMock };
});

// A reactive stand-in for the session store: fetchMe resolves the principal,
// as it does once the device flow has set the session cookie.
const session = reactive<{ me: { sub: string } | null }>({ me: null });
vi.mock('../../stores/auth', () => ({
  useAuthStore: () => ({
    get me() {
      return session.me;
    },
    login: loginMock,
    fetchMe: async () => {
      session.me = { sub: 'u-1' };
    },
  }),
}));

import { ApiError } from '../../api/client';
import type { ServerConfig } from '../../api/types';
import { en } from '../../i18n/en';
import { useTaskStore } from '../../stores/tasks';
import SettingsTabGithub from './SettingsTabGithub.vue';

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
  app = createApp({ render: () => h(SettingsTabGithub) });
  app.use(pinia);
  app.mount(host);
}

async function settle() {
  for (let i = 0; i < 5; i++) await Promise.resolve();
  await nextTick();
}

function clickSignIn() {
  (host.querySelector('[data-settings-tab="github"] button') as HTMLElement).click();
}

beforeEach(() => {
  pinia = createPinia();
  setActivePinia(pinia);
  apiMock.mockReset();
  loginMock.mockReset();
  session.me = null;
  app = null;
});

afterEach(() => {
  vi.useRealTimers();
  app?.unmount();
  host?.remove();
  document.querySelectorAll('.modal-overlay').forEach((n) => n.remove());
});

describe('SettingsTabGithub sign-in', () => {
  it('signs in by device code and does not send the browser to /login', async () => {
    apiMock.mockResolvedValueOnce(startBody);
    mount({ auth_redirect_enabled: true });

    clickSignIn();
    await settle();

    expect(apiMock).toHaveBeenCalledWith('POST', '/api/auth/device/start');
    expect(loginMock).not.toHaveBeenCalled();
    expect(document.body.querySelector('.device-code')?.textContent).toContain('ABCD-1234');
    // Unmounting does not stop the poll timer; cancel the flow to clear it.
    apiMock.mockResolvedValueOnce(null);
    (document.body.querySelector('.device-btn--ghost') as HTMLElement).click();
    await settle();
  });

  it('falls back to the /login redirect when device sign-in is unavailable', async () => {
    apiMock.mockRejectedValueOnce(new ApiError(503, null, 'device-code auth not configured'));
    mount({ auth_redirect_enabled: true });

    clickSignIn();
    await settle();

    expect(loginMock).toHaveBeenCalledTimes(1);
  });

  it('shows the reason instead of the /login redirect when redirect sign-in is off', async () => {
    apiMock.mockRejectedValueOnce(new ApiError(503, null, 'device-code auth not configured'));
    mount({ auth_redirect_enabled: false });

    clickSignIn();
    await settle();

    expect(loginMock).not.toHaveBeenCalled();
    expect(document.body.querySelector('.device-error')?.textContent).toContain(en['auth.redirect_unavailable']);
  });

  it('loads the GitHub connection once the sign-in completes', async () => {
    vi.useFakeTimers();
    apiMock.mockImplementation((method: string, path: string) => {
      if (path === '/api/auth/device/start') return Promise.resolve(startBody);
      if (path === '/api/auth/device/poll') return Promise.resolve({ status: 'done' });
      if (path === '/api/github/auth/status') {
        return Promise.resolve({ available: true, connected: true, login: 'octocat', can_connect: true });
      }
      return Promise.reject(new Error(`unexpected ${method} ${path}`));
    });
    mount({ auth_redirect_enabled: true });

    clickSignIn();
    await settle();
    // One poll interval: the server reports the flow done.
    await vi.advanceTimersByTimeAsync(2000);
    await settle();

    expect(apiMock).toHaveBeenCalledWith('GET', '/api/github/auth/status');
    expect(host.textContent).toContain('@octocat');
  });
});
