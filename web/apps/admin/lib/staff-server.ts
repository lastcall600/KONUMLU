/**
 * Server-only Staff IAM development credential handling.
 * Never import this module from client components.
 * Never expose STAFF_DEV_IDP_TOKEN via NEXT_PUBLIC_*.
 */

const LOCAL_API_BASE_URL = "http://localhost:8080";

const ALLOWED_QUEUE_PARAMS = [
  "status",
  "order",
  "limit",
  "cursor",
  "targetType",
  "reasonCode",
] as const;

function trimTrailingSlash(url: string): string {
  return url.replace(/\/+$/, "");
}

function envFlag(name: string): boolean {
  const raw = process.env[name]?.trim().toLowerCase() ?? "";
  return raw === "1" || raw === "true" || raw === "yes" || raw === "required";
}

export function staffBackendBaseUrl(): string {
  const fromEnv =
    process.env.API_BASE_URL?.trim() ||
    process.env.API_PROXY_TARGET?.trim() ||
    process.env.NEXT_PUBLIC_API_BASE_URL?.trim();
  const apiBaseUrl = fromEnv
    ? trimTrailingSlash(fromEnv)
    : process.env.NODE_ENV === "production"
      ? ""
      : LOCAL_API_BASE_URL;

  if (!apiBaseUrl) {
    throw new Error("API_BASE_URL must be set for staff BFF requests in production");
  }
  return apiBaseUrl;
}

/** Returns the dev Staff IAM bearer token only when the explicit local fixture is enabled. */
export function staffDevBearerToken(): string | null {
  if (!envFlag("STAFF_DEV_IDP_ENABLED")) {
    return null;
  }
  const token = process.env.STAFF_DEV_IDP_TOKEN?.trim() ?? "";
  return token === "" ? null : token;
}

export function staffDevIdpEnabled(): boolean {
  return envFlag("STAFF_DEV_IDP_ENABLED");
}

export function copyAllowedQueueParams(src: URLSearchParams): URLSearchParams {
  const out = new URLSearchParams();
  for (const key of ALLOWED_QUEUE_PARAMS) {
    const value = src.get(key)?.trim();
    if (value) {
      out.set(key, value);
    }
  }
  return out;
}
