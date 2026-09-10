import { getPublicConfig } from "@/lib/config";

export function apiUrl(path: string): string {
  const prefix = path.startsWith("/") ? path : `/${path}`;
  return `${getPublicConfig().apiBaseUrl}${prefix}`;
}

/**
 * Cookie-session fetch for a future staff auth context (HttpOnly cookies from the API).
 * Do not read or write tokens in localStorage or sessionStorage.
 * Do not send or assume consumer Identity session cookies.
 * Staff IAM is not implemented in this scaffold; callers must not treat this as login.
 */
export function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  return fetch(apiUrl(path), {
    ...init,
    credentials: "include",
    headers,
  });
}
