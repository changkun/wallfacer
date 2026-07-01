// DeviceSignInModal is presentational: it renders the user code + verification
// link while pending, a localized reason on error, and emits cancel/retry. It
// Teleports to <body>, so assertions query document.body.
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { nextTick, h, createApp, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import DeviceSignInModal from './DeviceSignInModal.vue';
import type { DeviceSignInStatus } from '../composables/useDeviceSignIn';

let app: App | null = null;
let host: HTMLElement;

function mount(props: {
  status: DeviceSignInStatus;
  userCode?: string;
  verificationUri?: string;
  verificationUriComplete?: string;
  error?: string;
}) {
  host = document.createElement('div');
  document.body.appendChild(host);
  const events: Record<string, number> = { cancel: 0, retry: 0 };
  app = createApp({
    render: () =>
      h(DeviceSignInModal, {
        status: props.status,
        userCode: props.userCode ?? '',
        verificationUri: props.verificationUri ?? '',
        verificationUriComplete: props.verificationUriComplete ?? '',
        error: props.error ?? '',
        onCancel: () => { events.cancel++; },
        onRetry: () => { events.retry++; },
      }),
  });
  app.mount(host);
  return { events };
}

beforeEach(() => {
  setActivePinia(createPinia());
  app = null;
});

afterEach(() => {
  app?.unmount();
  host?.remove();
  document.querySelectorAll('.modal-overlay').forEach((n) => n.remove());
});

describe('DeviceSignInModal', () => {
  it('renders the user code and verification link while pending', async () => {
    mount({
      status: 'pending',
      userCode: 'ABCD-1234',
      verificationUri: 'https://auth.latere.ai/device',
      verificationUriComplete: 'https://auth.latere.ai/device?user_code=ABCD-1234',
    });
    await nextTick();
    const code = document.body.querySelector('.device-code') as HTMLElement;
    expect(code.textContent).toContain('ABCD-1234');
    const link = document.body.querySelector('.device-open') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('https://auth.latere.ai/device?user_code=ABCD-1234');
  });

  it('shows a localized reason and a retry action on error', async () => {
    const { events } = mount({ status: 'error', error: 'expired' });
    await nextTick();
    const err = document.body.querySelector('.device-error') as HTMLElement;
    expect(err.textContent).toContain('expired');

    const buttons = document.body.querySelectorAll('.device-btn');
    // Last button is "Try again".
    (buttons[buttons.length - 1] as HTMLElement).click();
    await nextTick();
    expect(events.retry).toBe(1);
  });

  it('emits cancel from the pending cancel button', async () => {
    const { events } = mount({ status: 'pending', userCode: 'X-1' });
    await nextTick();
    (document.body.querySelector('.device-btn--ghost') as HTMLElement).click();
    await nextTick();
    expect(events.cancel).toBe(1);
  });
});
