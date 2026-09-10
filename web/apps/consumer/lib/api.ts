/**
 * Same-origin API path. Next.js rewrites `/v1/*` to the backend so `__Host-`
 * session/CSRF cookies are bound to the consumer host and credentials work.
 * Auth lives in HttpOnly cookies. Do not read or write tokens in storage.
 */
export function apiUrl(path: string): string {
  const prefix = path.startsWith("/") ? path : `/${path}`;
  return prefix;
}

/**
 * Cookie-session fetch. Auth lives in HttpOnly cookies set by the API.
 * Do not read or write tokens in localStorage or sessionStorage.
 */
export function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  return fetch(apiUrl(path), {
    ...init,
    credentials: "include",
    headers,
  });
}
