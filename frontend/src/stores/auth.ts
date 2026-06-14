// The session store is the shared latere-ui factory, the same one every other
// latere SPA uses: it holds the resolved principal and the login / logout /
// switch-org actions, and resolves the session from GET /api/me. wallfacer
// keeps its own fetch wrapper (which injects the local-mode server API key), so
// we adapt it to latere-ui's ApiClient shape rather than swapping the client.
//
// expiredSessionMode is 'graceful': a local `wallfacer run` is reachable
// anonymously, so an absent/expired session must never bounce the user to
// /login. Org switch follows the server's {redirect} round-trip.
import { createSessionStore } from 'latere-ui';

import { api as rawApi } from '../api/client';

const sessionClient = {
  api: <T = unknown>(method: string, path: string, body?: unknown): Promise<T> =>
    rawApi<T>(method, path, body),
  apiUpload: <T = unknown>(): Promise<T> =>
    Promise.reject(new Error('apiUpload is not supported in wallfacer')),
  csrfToken: () => '',
};

export const useAuthStore = createSessionStore({
  client: sessionClient,
  storeId: 'auth',
  meEndpoint: '/api/me',
  switchOrgEndpoint: '/api/me/switch-org',
  defaultReturnTo: '/',
  expiredSessionMode: 'graceful',
  switchOrgMode: 'follow-redirect',
});
