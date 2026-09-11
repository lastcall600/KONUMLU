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

function jsonError(status: number, code: string): Response {
  return Response.json({ error: code }, { status });
}

export type StaffProxyInit = {
  method?: string;
  search?: URLSearchParams;
  jsonBody?: unknown;
};

/** Server-only proxy to staff HTTP. Attaches the dev Bearer token when enabled. */
export async function proxyStaffRequest(backendPath: string, init: StaffProxyInit = {}): Promise<Response> {
  if (staffDevIdpEnabled() && staffDevBearerToken() === null) {
    return jsonError(401, "unauthenticated");
  }

  let backendBase: string;
  try {
    backendBase = staffBackendBaseUrl();
  } catch {
    return jsonError(503, "unavailable");
  }

  const target = new URL(backendPath, `${backendBase}/`);
  if (init.search) {
    target.search = init.search.toString();
  }

  const headers = new Headers({ Accept: "application/json" });
  const token = staffDevBearerToken();
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const fetchInit: RequestInit = {
    method: init.method ?? "GET",
    headers,
    cache: "no-store",
  };
  if (init.jsonBody !== undefined) {
    headers.set("Content-Type", "application/json");
    fetchInit.body = JSON.stringify(init.jsonBody);
  }

  let upstream: Response;
  try {
    upstream = await fetch(target, fetchInit);
  } catch {
    return jsonError(503, "unavailable");
  }

  const body = await upstream.text();
  const contentType = upstream.headers.get("content-type") ?? "application/json";
  return new Response(body, {
    status: upstream.status,
    headers: { "Content-Type": contentType },
  });
}
