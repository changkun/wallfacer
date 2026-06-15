export class ApiError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, body: unknown, message: string) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

// getServerApiKey reads the local-mode server API key injected into the page.
// It is the single source for the key so callers don't each reach into the
// window global; if auth ever moves off window.__WALLFACER__ only this changes.
export function getServerApiKey(): string {
  if (typeof window !== 'undefined' && window.__WALLFACER__) {
    return window.__WALLFACER__.serverApiKey || '';
  }
  return '';
}

// authHeaders returns the Authorization header for fetch when a server API key
// is configured, or an empty object otherwise.
export function authHeaders(): Record<string, string> {
  const key = getServerApiKey();
  return key ? { Authorization: `Bearer ${key}` } : {};
}

// withAuthToken appends the server API key as a ?token= query parameter, used
// by EventSource/WebSocket endpoints that cannot set an Authorization header.
// It is a no-op when no key is configured.
export function withAuthToken(url: string): string {
  const key = getServerApiKey();
  if (!key) return url;
  return url + (url.includes('?') ? '&' : '?') + 'token=' + encodeURIComponent(key);
}

export async function api<T = unknown>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = { 'Accept': 'application/json' };
  const key = getServerApiKey();
  if (key) {
    headers['Authorization'] = `Bearer ${key}`;
  }
  let payload: BodyInit | undefined;
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    payload = JSON.stringify(body);
  }
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: payload,
  });
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try { data = JSON.parse(text); } catch { data = text; }
  }
  if (!res.ok) {
    let msg = res.statusText;
    // Prefer a server-provided message. Handlers report errors two ways: a JSON
    // body with a `message`/`error` field, or a plain-text body via http.Error.
    if (data && typeof data === 'object') {
      const obj = data as Record<string, unknown>;
      const m = obj.message ?? obj.error;
      if (typeof m === 'string' && m.trim()) msg = m.trim();
    } else if (typeof data === 'string' && data.trim()) {
      msg = data.trim();
    }
    throw new ApiError(res.status, data, msg);
  }
  return data as T;
}
