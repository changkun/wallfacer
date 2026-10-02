// The one way the SPA starts a latere.ai sign-in, shared by every "Sign in"
// control (the account menu, the GitHub settings tab) so they cannot diverge.
// It composes the device-code flow (useDeviceSignIn) with the session store
// and the server's redirect flag:
//
//   - start() begins the RFC 8628 device-code flow. Where device sign-in is
//     not wired (cloud deployments answer 503) it falls back to the session
//     store's browser redirect to /login, unless the server reports that
//     redirect off, in which case the modal shows the reason.
//   - A completed flow refreshes the principal, so every view bound to the
//     session store re-renders as signed in, then clears the modal.
//
// Each caller owns one flow and renders DeviceSignInModal from modalOpen and
// modalProps. The server keeps at most one device flow, so starting a second
// one cancels the first.
import { computed, watch } from 'vue';

import { useAuthStore } from '../stores/auth';
import { useTaskStore } from '../stores/tasks';
import { useDeviceSignIn } from './useDeviceSignIn';

// doneDisplayMs is how long the modal shows the success state before it closes.
const doneDisplayMs = 1200;

export function useSignIn() {
  const auth = useAuthStore();
  const tasks = useTaskStore();
  const device = useDeviceSignIn();

  // The server reports in /api/config whether the browser redirect behind
  // /login can complete on this instance; it cannot when the server is bound
  // to a port its redirect URL does not name (a second instance on one
  // machine). An absent flag reads as available.
  const redirectEnabled = computed(() => tasks.config?.auth_redirect_enabled !== false);

  function start() {
    void device.loginOrFallback(redirectEnabled.value ? auth.login : device.failRedirectUnavailable);
  }

  watch(device.status, async (s) => {
    if (s === 'done') {
      await auth.fetchMe();
      window.setTimeout(() => device.reset(), doneDisplayMs);
    }
  });

  // The modal is up from the moment there is a code or a terminal state to
  // show; while /start is in flight there is nothing to render yet.
  const modalOpen = computed(() => device.status.value !== 'idle' && device.status.value !== 'starting');
  const modalProps = computed(() => ({
    status: device.status.value,
    userCode: device.userCode.value,
    verificationUri: device.verificationUri.value,
    verificationUriComplete: device.verificationUriComplete.value,
    error: device.error.value,
  }));

  return {
    start,
    cancel: device.cancel,
    redirectEnabled,
    failRedirectUnavailable: device.failRedirectUnavailable,
    modalOpen,
    modalProps,
  };
}
