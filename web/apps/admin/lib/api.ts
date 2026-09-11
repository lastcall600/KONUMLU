import { getPublicConfig } from "@/lib/config";

export function apiUrl(path: string): string {
  const prefix = path.startsWith("/") ? path : `/${path}`;
  return `${getPublicConfig().apiBaseUrl}${prefix}`;
}

/**
 * Cookie-session fetch for a future staff auth context (HttpOnly cookies from the API).
 * Do not read or write tokens in localStorage or sessionStorage.
 * Do not send or assume consumer Identity session cookies.
 * Do not attach Staff IAM bearer tokens in the browser.
 */
export function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  return fetch(apiUrl(path), {
    ...init,
    credentials: "include",
    headers,
  });
}

/**
 * Same-origin Management Center BFF. The Next.js server attaches Staff IAM
 * credentials (dev Bearer token) so the browser never holds STAFF_DEV_IDP_TOKEN.
 */
export function staffBffUrl(path: string): string {
  const prefix = path.startsWith("/") ? path : `/${path}`;
  return `/api/staff${prefix}`;
}

export function staffApiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  return fetch(staffBffUrl(path), {
    ...init,
    credentials: "same-origin",
    cache: "no-store",
    headers,
  });
}
