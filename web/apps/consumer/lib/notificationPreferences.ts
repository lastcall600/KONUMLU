import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";

export type NotificationPreference = {
  channel: string;
  scopeType: string;
  scopeKey: string;
  stored: boolean | null;
  enabled: boolean;
  required: boolean;
};

export class NotificationPreferenceError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "NotificationPreferenceError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Tercihler güncellenemedi. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";

function mutateHeaders(): Headers {
  const headers = new Headers({ "Content-Type": "application/json" });
  const csrf = readCsrfToken();
  if (csrf) {
    headers.set(CSRF_HEADER_NAME, csrf);
  }
  return headers;
}

async function readJson(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

function readErrorCode(body: unknown): string | undefined {
  if (typeof body !== "object" || body === null || !("error" in body)) {
    return undefined;
  }
  const error = (body as ErrorBody).error;
  return typeof error === "string" ? error : undefined;
}

function errorFromResponse(status: number, body: unknown): NotificationPreferenceError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new NotificationPreferenceError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 503 || code === "unavailable") {
    return new NotificationPreferenceError("unavailable", UNAVAILABLE);
  }
  return new NotificationPreferenceError("generic", GENERIC);
}

function parseSettings(body: unknown): NotificationPreference[] {
  if (typeof body !== "object" || body === null || !("settings" in body)) {
    throw new NotificationPreferenceError("generic", GENERIC);
  }
  const settings = (body as { settings?: unknown }).settings;
  if (!Array.isArray(settings)) {
    throw new NotificationPreferenceError("generic", GENERIC);
  }
  const out: NotificationPreference[] = [];
  for (const row of settings) {
    if (typeof row !== "object" || row === null) {
      continue;
    }
    const item = row as Record<string, unknown>;
    if (
      typeof item.channel !== "string" ||
      typeof item.scopeType !== "string" ||
      typeof item.scopeKey !== "string" ||
      typeof item.enabled !== "boolean" ||
      typeof item.required !== "boolean"
    ) {
      continue;
    }
    out.push({
      channel: item.channel,
      scopeType: item.scopeType,
      scopeKey: item.scopeKey,
      stored: typeof item.stored === "boolean" ? item.stored : null,
      enabled: item.enabled,
      required: item.required,
    });
  }
  return out;
}

export async function getNotificationPreferences(): Promise<NotificationPreference[]> {
  const response = await apiFetch("/v1/notification-preferences", { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  return parseSettings(body);
}

export async function patchNotificationPreference(input: {
  channel: string;
  scopeType: string;
  scopeKey: string;
  enabled: boolean;
}): Promise<NotificationPreference[]> {
  const response = await apiFetch("/v1/notification-preferences", {
    method: "PATCH",
    headers: mutateHeaders(),
    body: JSON.stringify({
      overrides: [
        {
          channel: input.channel,
          scopeType: input.scopeType,
          scopeKey: input.scopeKey,
          enabled: input.enabled,
        },
      ],
    }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  return parseSettings(body);
}
