// --- Transport layer: auth helpers and fetch wrappers ---
//
// This module owns all HTTP authentication logic and the low-level fetch
// wrappers used by every other module. No domain knowledge lives here.

function getWallfacerToken() {
  if (!document || typeof document.querySelector !== "function") return "";
  var el = document.querySelector('meta[name="wallfacer-token"]');
  return el && el.content ? el.content : "";
}

function withAuthHeaders(headers, method) {
  var merged = Object.assign({}, headers || {});
  var token = getWallfacerToken();
  if (!token) return merged;
  if (String(method || "GET").toUpperCase() === "GET") return merged;
  merged.Authorization = "Bearer " + token;
  return merged;
}

function withBearerHeaders(headers) {
  var token = getWallfacerToken();
  var merged = Object.assign({}, headers || {});
  if (token) merged.Authorization = "Bearer " + token;
  return merged;
}

function withAuthToken(url) {
  var token = getWallfacerToken();
  if (!token) return url;
  var sep = url.indexOf("?") === -1 ? "?" : "&";
  return url + sep + "token=" + encodeURIComponent(token);
}

async function apiGet(path, opts = {}) {
  const res = await fetch(path, {
    headers: withBearerHeaders(opts.headers || {}),
    signal: opts.signal,
    ...opts,
  });
  if (!res.ok && res.status !== 204) {
    const text = await res.text();
    throw new Error(text);
  }
  if (res.status === 204) return null;
  return res.json();
}

async function api(path, opts = {}) {
  const method = opts.method || "GET";
  const headers = withAuthHeaders(
    { "Content-Type": "application/json", ...(opts.headers || {}) },
    method,
  );
  const res = await fetch(path, {
    headers,
    signal: opts.signal,
    ...opts,
  });
  if (!res.ok && res.status !== 204) {
    const text = await res.text();
    throw new Error(text);
  }
  if (res.status === 204) return null;
  return res.json();
}
