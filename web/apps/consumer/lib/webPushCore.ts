export const WEB_PUSH_CHANNEL = "web_push" as const;
export const WEB_PUSH_PLATFORM = "web" as const;
export const WEB_PUSH_PROVIDER = "webpush" as const;

export const ENDPOINT_ID_STORAGE_KEY = "konumlu.webpush.endpoint_id";
export const SYNC_ATTEMPTED_SESSION_KEY = "konumlu.webpush.sync_attempted";

/** Logout must not revoke push endpoints (NOTIFY-C). */
export const REVOKE_PUSH_ON_LOGOUT = false;

const ENDPOINT_ID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const ALLOWED_INTERNAL_PATHS = new Set([
  "/",
  "/bildirimler",
  "/mesajlar",
  "/kayitli-aramalar",
  "/randevular",
  "/favoriler",
]);

const SENSITIVE_LOG_KEYS = [
  "endpoint",
  "p256dh",
  "auth",
  "keys",
  "subscription",
  "pushsubscription",
];

export type BrowserPermission = "unsupported" | "default" | "granted" | "denied";

export type WebPushUiStatus = "unsupported" | "denied" | "off" | "on";

export type PushPublicPayload = {
  category?: string;
  template_key?: string;
  reference_id?: string;
};

export type WebPushRegisterBody = {
  channel: typeof WEB_PUSH_CHANNEL;
  platform: typeof WEB_PUSH_PLATFORM;
  provider: typeof WEB_PUSH_PROVIDER;
  endpoint: string;
  p256dh: string;
  auth: string;
};

export type PushEndpointView = {
  id: string;
  channel: string;
  platform: string;
  provider: string;
  revoked: boolean;
};

export function parseOpaqueEndpointId(raw: unknown): string | null {
  if (typeof raw !== "string") {
    return null;
  }
  const id = raw.trim();
  if (!ENDPOINT_ID_RE.test(id)) {
    return null;
  }
  return id.toLowerCase();
}

export function vapidPublicKeyToUint8Array(input: string): Uint8Array {
  const trimmed = input.trim();
  if (!trimmed || /[\s]/.test(trimmed)) {
    throw new Error("malformed_vapid_public_key");
  }
  const padding = "=".repeat((4 - (trimmed.length % 4)) % 4);
  const base64 = (trimmed + padding).replace(/-/g, "+").replace(/_/g, "/");
  let raw: string;
  try {
    raw = atob(base64);
  } catch {
    throw new Error("malformed_vapid_public_key");
  }
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) {
    out[i] = raw.charCodeAt(i);
  }
  if (out.byteLength !== 65 || out[0] !== 0x04) {
    throw new Error("malformed_vapid_public_key");
  }
  return out;
}

export function bytesToUrlBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.byteLength; i += 1) {
    binary += String.fromCharCode(bytes[i] ?? 0);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function bufferToUrlBase64(buffer: ArrayBuffer): string {
  return bytesToUrlBase64(new Uint8Array(buffer));
}

export function buildWebPushRegisterBody(input: {
  endpoint: string;
  p256dh: string;
  auth: string;
}): WebPushRegisterBody {
  return {
    channel: WEB_PUSH_CHANNEL,
    platform: WEB_PUSH_PLATFORM,
    provider: WEB_PUSH_PROVIDER,
    endpoint: input.endpoint,
    p256dh: input.p256dh,
    auth: input.auth,
  };
}

export function registerBodyHasUserId(body: object): boolean {
  return Object.prototype.hasOwnProperty.call(body, "user_id") || Object.prototype.hasOwnProperty.call(body, "userId");
}

export function deriveUiStatus(input: {
  permission: BrowserPermission;
  backendEndpointId: string | null;
}): WebPushUiStatus {
  if (input.permission === "unsupported") {
    return "unsupported";
  }
  if (input.permission === "denied") {
    return "denied";
  }
  if (input.permission === "granted" && input.backendEndpointId) {
    return "on";
  }
  return "off";
}

export function uiStatusLabel(status: WebPushUiStatus): string {
  switch (status) {
    case "on":
      return "Açık";
    case "unsupported":
      return "Tarayıcı desteklemiyor";
    case "denied":
      return "Tarayıcı tarafından engellendi";
    default:
      return "Kapalı";
  }
}

export function parsePushPayload(raw: unknown): PushPublicPayload {
  if (typeof raw !== "string" || raw.trim() === "") {
    return {};
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw) as unknown;
  } catch {
    return {};
  }
  if (typeof parsed !== "object" || parsed === null) {
    return {};
  }
  const record = parsed as Record<string, unknown>;
  const out: PushPublicPayload = {};
  if (typeof record.category === "string") {
    out.category = record.category.slice(0, 64);
  }
  if (typeof record.template_key === "string") {
    out.template_key = record.template_key.slice(0, 128);
  }
  if (typeof record.reference_id === "string") {
    out.reference_id = record.reference_id.slice(0, 128);
  }
  return out;
}

export function resolvePushNavigation(payload: PushPublicPayload): string {
  switch (payload.template_key) {
    case "messaging.message_received":
      return "/mesajlar";
    case "saved_search.match":
      return "/kayitli-aramalar";
    case "offer.received":
    case "offer.accepted":
    case "offer.rejected":
      return "/";
    case "transaction.created":
    case "transaction.completed":
    case "delivery.status_changed":
      return "/";
    case "security.login_new":
      return "/bildirimler";
    default:
      break;
  }
  switch (payload.category) {
    case "messages":
      return "/mesajlar";
    case "saved_searches":
      return "/kayitli-aramalar";
    default:
      return "/bildirimler";
  }
}

export function sanitizeInternalPath(raw: unknown): string {
  if (typeof raw !== "string") {
    return "/bildirimler";
  }
  const value = raw.trim();
  if (!value.startsWith("/") || value.startsWith("//") || value.includes("\\") || value.includes("://")) {
    return "/bildirimler";
  }
  try {
    const url = new URL(value, "https://konumlu.invalid");
    if (url.origin !== "https://konumlu.invalid" || url.username || url.password) {
      return "/bildirimler";
    }
    if (ALLOWED_INTERNAL_PATHS.has(url.pathname)) {
      return url.pathname;
    }
  } catch {
    return "/bildirimler";
  }
  return "/bildirimler";
}

export function notificationCopy(payload: PushPublicPayload): { title: string; body: string } {
  switch (payload.category) {
    case "messages":
      return { title: "KONUMLU", body: "Yeni bir mesajınız var." };
    case "offers":
      return { title: "KONUMLU", body: "Tekliflerinizde bir güncelleme var." };
    case "transactions":
      return { title: "KONUMLU", body: "Bir işleminizde güncelleme var." };
    case "saved_searches":
      return { title: "KONUMLU", body: "Kayıtlı aramanızla ilgili bir güncelleme var." };
    case "security":
      return { title: "KONUMLU", body: "Hesabınızla ilgili bir güvenlik bildirimi var." };
    default:
      return { title: "KONUMLU", body: "Yeni bir bildiriminiz var." };
  }
}

export function containsSensitivePushMaterial(value: string): boolean {
  const lower = value.toLowerCase();
  return (
    lower.includes("p256dh") ||
    lower.includes("\"auth\"") ||
    /https:\/\/.+push/i.test(value) ||
    lower.includes("pushsubscription")
  );
}

export function redactForLog(value: unknown): unknown {
  if (typeof value === "string") {
    if (containsSensitivePushMaterial(value)) {
      return "[redacted]";
    }
    return value;
  }
  if (Array.isArray(value)) {
    return value.map(redactForLog);
  }
  if (typeof value === "object" && value !== null) {
    const out: Record<string, unknown> = {};
    for (const [key, nested] of Object.entries(value as Record<string, unknown>)) {
      if (SENSITIVE_LOG_KEYS.includes(key.toLowerCase())) {
        out[key] = "[redacted]";
      } else {
        out[key] = redactForLog(nested);
      }
    }
    return out;
  }
  return value;
}

export function webPushCategoryPreferences<T extends { channel: string; scopeType: string; scopeKey: string }>(
  rows: T[],
): T[] {
  return rows.filter(
    (row) =>
      row.channel === WEB_PUSH_CHANNEL &&
      (row.scopeType === "category" || (row.scopeType === "channel" && row.scopeKey === "*")),
  );
}

export function categoryPreferenceLabel(scopeKey: string): string {
  switch (scopeKey) {
    case "messages":
      return "Mesajlar";
    case "offers":
      return "Teklifler";
    case "transactions":
      return "İşlemler";
    case "saved_searches":
      return "Kayıtlı aramalar";
    case "security":
      return "Güvenlik";
    case "marketing":
      return "Pazarlama";
    case "community":
      return "Topluluk";
    case "*":
      return "Web bildirim kanalı";
    default:
      return scopeKey;
  }
}
