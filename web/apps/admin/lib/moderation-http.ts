export type StaffOpKind =
  | "unauthenticated"
  | "forbidden"
  | "not_found"
  | "bad_request"
  | "conflict"
  | "error";

const SAFE_ERROR_CODES = new Set([
  "unauthenticated",
  "forbidden",
  "not_found",
  "bad_request",
  "conflict",
  "unavailable",
]);

export function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

export async function readJson(res: Response): Promise<unknown> {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

export function readErrorCode(value: unknown): string | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const code = (value as { error?: unknown }).error;
  return typeof code === "string" && SAFE_ERROR_CODES.has(code) ? code : null;
}

export function kindFromStatus(status: number): StaffOpKind {
  if (status === 401) return "unauthenticated";
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 400 || status === 422) return "bad_request";
  if (status === 409) return "conflict";
  return "error";
}

export function isAbortError(err: unknown): boolean {
  return err instanceof DOMException && err.name === "AbortError";
}
