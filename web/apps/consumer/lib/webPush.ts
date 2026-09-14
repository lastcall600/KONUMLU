import { apiFetch } from "@/lib/api";
import { getPublicConfig } from "@/lib/config";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";
import { parseOpaqueEndpointId, type PushEndpointView } from "@/lib/webPushCore";
import {
  WebPushClientError,
  createWebPushController,
  type PushSubscriptionLike,
  type WebPushHost,
} from "@/lib/webPushController";

const SW_URL = "/sw.js";
const GENERIC = "Bildirimler güncellenemedi. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";

type ErrorBody = {
  error?: string;
};

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

function errorFromResponse(status: number, body: unknown, fallback: string): WebPushClientError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new WebPushClientError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 503 || code === "unavailable") {
    return new WebPushClientError("unavailable", UNAVAILABLE);
  }
  return new WebPushClientError("generic", fallback);
}

function parseEndpointView(body: unknown): PushEndpointView {
  if (typeof body !== "object" || body === null) {
    throw new WebPushClientError("generic", GENERIC);
  }
  const row = body as { id?: unknown; channel?: unknown; platform?: unknown; provider?: unknown; revoked?: unknown };
  const id = parseOpaqueEndpointId(row.id);
  if (!id || typeof row.channel !== "string" || typeof row.platform !== "string" || typeof row.provider !== "string") {
    throw new WebPushClientError("generic", GENERIC);
  }
  return {
    id,
    channel: row.channel,
    platform: row.platform,
    provider: row.provider,
    revoked: row.revoked === true,
  };
}

function memoryStore(): WebPushHost["appStorage"] {
  const data = new Map<string, string>();
  return {
    getItem(key) {
      return data.get(key) ?? null;
    },
    setItem(key, value) {
      data.set(key, value);
    },
    removeItem(key) {
      data.delete(key);
    },
  };
}

function browserStore(which: "local" | "session"): WebPushHost["appStorage"] {
  try {
    const storage = which === "local" ? window.localStorage : window.sessionStorage;
    storage.getItem("konumlu.webpush.probe");
    return storage;
  } catch {
    return memoryStore();
  }
}

function permissionFromBrowser(): WebPushHost["permission"] {
  if (typeof window === "undefined" || typeof navigator === "undefined") {
    return "unsupported";
  }
  if (!("serviceWorker" in navigator) || !("PushManager" in window) || typeof Notification === "undefined") {
    return "unsupported";
  }
  const value = Notification.permission;
  if (value === "granted" || value === "denied" || value === "default") {
    return value;
  }
  return "unsupported";
}

function wrapSubscription(sub: PushSubscription): PushSubscriptionLike {
  return {
    endpoint: sub.endpoint,
    getKey(name) {
      return sub.getKey(name);
    },
    unsubscribe() {
      return sub.unsubscribe();
    },
  };
}

async function postRegister(body: Parameters<WebPushHost["postRegister"]>[0]): Promise<PushEndpointView> {
  const response = await apiFetch("/v1/push-endpoints", {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify(body),
  });
  const parsed = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, parsed, GENERIC);
  }
  return parseEndpointView(parsed);
}

async function deleteEndpoint(id: string): Promise<void> {
  const response = await apiFetch(`/v1/push-endpoints/${id}`, {
    method: "DELETE",
    headers: mutateHeaders(),
  });
  const parsed = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, parsed, GENERIC);
  }
}

function createBrowserHost(): WebPushHost {
  return {
    permission: permissionFromBrowser(),
    vapidPublicKey: getPublicConfig().webPushVapidPublicKey,
    appStorage: browserStore("local"),
    sessionStorage: browserStore("session"),
    async requestPermission() {
      const result = await Notification.requestPermission();
      if (result === "granted" || result === "denied" || result === "default") {
        return result;
      }
      return "denied";
    },
    async registerWorker() {
      await navigator.serviceWorker.register(SW_URL, { scope: "/" });
    },
    async getSubscription() {
      const ready = await navigator.serviceWorker.ready;
      const sub = await ready.pushManager.getSubscription();
      return sub ? wrapSubscription(sub) : null;
    },
    async subscribe(applicationServerKey) {
      const ready = await navigator.serviceWorker.ready;
      const keyCopy = new Uint8Array(applicationServerKey.byteLength);
      keyCopy.set(applicationServerKey);
      const sub = await ready.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: keyCopy,
      });
      return wrapSubscription(sub);
    },
    postRegister,
    deleteEndpoint,
  };
}

function controller() {
  return createWebPushController(createBrowserHost());
}

export { WebPushClientError };

export function webPushSupported(): boolean {
  return permissionFromBrowser() !== "unsupported";
}

export function readWebPushPermission(): WebPushHost["permission"] {
  return permissionFromBrowser();
}

export async function registerWebPushWorker(): Promise<void> {
  if (permissionFromBrowser() === "unsupported") {
    return;
  }
  await navigator.serviceWorker.register(SW_URL, { scope: "/" });
}

export function webPushUiStatus() {
  return controller().uiStatus();
}

export function webPushEnabled(): boolean {
  return controller().enabled();
}

export async function enableWebPushFromUserGesture() {
  return controller().enableFromUserGesture();
}

export async function syncWebPushIfGranted() {
  return controller().syncExistingIfGranted();
}

export async function disableCurrentWebPush() {
  return controller().disableCurrentEndpoint();
}

export function onWebPushLogout(): void {
  controller().onLogout();
}
