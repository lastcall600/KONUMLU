const CSRF_COOKIE_NAME = "__Host-konumlu_csrf";

export const CSRF_HEADER_NAME = "X-CSRF-Token";

/** Readable CSRF cookie only. Session cookie is HttpOnly and must not be read. */
export function readCsrfToken(): string | null {
  if (typeof document === "undefined") {
    return null;
  }

  const cookies = document.cookie.split(";");
  for (const part of cookies) {
    const trimmed = part.trim();
    const eq = trimmed.indexOf("=");
    if (eq === -1) {
      continue;
    }
    const name = trimmed.slice(0, eq);
    if (name !== CSRF_COOKIE_NAME) {
      continue;
    }
    const value = trimmed.slice(eq + 1);
    return value ? decodeURIComponent(value) : null;
  }
  return null;
}
