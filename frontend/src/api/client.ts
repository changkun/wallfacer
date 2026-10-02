// ApiError is a failed API call: the HTTP status, the parsed body, the sentence
// to show (message), and the server's stable error code. code is set when the
// body is the error envelope and is '' otherwise; callers branch on it instead
// of matching the message. The envelope's developer detail stays on body.
export class ApiError extends Error {
  status: number;
  body: unknown;
  code: string;
  constructor(status: number, body: unknown, message: string, code = '') {
    super(message);
    this.status = status;
    this.body = body;
    this.code = code;
  }
}

// text returns v trimmed when it is a string, and '' for anything else.
function text(v: unknown): string {
  return typeof v === 'string' ? v.trim() : '';
}

// errorFields reads the message and the code out of a failed response's body.
// Handlers report errors three ways: the envelope
// {"error": {"code", "message", "details"}} (httpjson.WriteError), a flat JSON
// body with a `message` or a string `error` field, and a plain-text body via
// http.Error. Either field is '' when the body does not carry it.
function errorFields(data: unknown): { message: string; code: string } {
  if (typeof data === 'string') return { message: text(data), code: '' };
  if (!data || typeof data !== 'object') return { message: '', code: '' };
  const obj = data as Record<string, unknown>;
  if (obj.error && typeof obj.error === 'object') {
    const envelope = obj.error as Record<string, unknown>;
    return { message: text(envelope.message), code: text(envelope.code) };
  }
  return { message: text(obj.message) || text(obj.error), code: '' };
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
    // Prefer a server-provided message over the status text.
    const { message, code } = errorFields(data);
    throw new ApiError(res.status, data, message || res.statusText, code);
  }
  return data as T;
}
