import {
  ENDPOINT_ID_STORAGE_KEY,
  REVOKE_PUSH_ON_LOGOUT,
  SYNC_ATTEMPTED_SESSION_KEY,
  buildWebPushRegisterBody,
  bufferToUrlBase64,
  deriveUiStatus,
  parseOpaqueEndpointId,
  registerBodyHasUserId,
  vapidPublicKeyToUint8Array,
  type BrowserPermission,
  type PushEndpointView,
  type WebPushRegisterBody,
  type WebPushUiStatus,
} from "./webPushCore";

export class WebPushClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "WebPushClientError";
    this.code = code;
  }
}

export type KvStore = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
};

export type PushSubscriptionLike = {
  endpoint: string;
  getKey(name: "p256dh" | "auth"): ArrayBuffer | null;
  unsubscribe(): Promise<boolean>;
};

export type WebPushHost = {
  permission: BrowserPermission;
  vapidPublicKey: string;
  appStorage: KvStore;
  sessionStorage: KvStore;
  requestPermission(): Promise<BrowserPermission>;
  registerWorker(): Promise<void>;
  getSubscription(): Promise<PushSubscriptionLike | null>;
  subscribe(applicationServerKey: Uint8Array): Promise<PushSubscriptionLike>;
  postRegister(body: WebPushRegisterBody): Promise<PushEndpointView>;
  deleteEndpoint(id: string): Promise<void>;
};

const GENERIC = "Bildirimler güncellenemedi. Lütfen tekrar deneyin.";
const UNSUPPORTED = "Bu tarayıcı web bildirimlerini desteklemiyor.";
const DENIED = "Tarayıcı bildirim iznini kapattı. Açmak için tarayıcı ayarlarını kullanın.";
const VAPID = "Bildirim yapılandırması eksik veya geçersiz.";
const BACKEND = "Bildirim kaydı tamamlanamadı. Lütfen tekrar deneyin.";
const REVOKE_FAILED = "Bildirim kapatılamadı. Lütfen tekrar deneyin.";

export function createWebPushController(host: WebPushHost) {
  function storedEndpointId(): string | null {
    return parseOpaqueEndpointId(host.appStorage.getItem(ENDPOINT_ID_STORAGE_KEY));
  }

  function persistEndpointId(id: string): void {
    const opaque = parseOpaqueEndpointId(id);
    if (!opaque) {
      throw new WebPushClientError("generic", BACKEND);
    }
    host.appStorage.setItem(ENDPOINT_ID_STORAGE_KEY, opaque);
  }

  function clearEndpointId(): void {
    host.appStorage.removeItem(ENDPOINT_ID_STORAGE_KEY);
  }

  function uiStatus(): WebPushUiStatus {
    return deriveUiStatus({
      permission: host.permission,
      backendEndpointId: storedEndpointId(),
    });
  }

  function enabled(): boolean {
    return uiStatus() === "on";
  }

  async function materialFrom(sub: PushSubscriptionLike): Promise<{ p256dh: string; auth: string }> {
    const p256dh = sub.getKey("p256dh");
    const auth = sub.getKey("auth");
    if (!p256dh || !auth) {
      throw new WebPushClientError("generic", GENERIC);
    }
    return { p256dh: bufferToUrlBase64(p256dh), auth: bufferToUrlBase64(auth) };
  }

  async function registerSubscription(sub: PushSubscriptionLike): Promise<PushEndpointView> {
    const keys = await materialFrom(sub);
    const body = buildWebPushRegisterBody({
      endpoint: sub.endpoint,
      p256dh: keys.p256dh,
      auth: keys.auth,
    });
    if (registerBodyHasUserId(body)) {
      throw new WebPushClientError("generic", GENERIC);
    }
    const view = await host.postRegister(body);
    if (view.revoked || !parseOpaqueEndpointId(view.id)) {
      throw new WebPushClientError("unavailable", BACKEND);
    }
    persistEndpointId(view.id);
    return view;
  }

  async function enableFromUserGesture(): Promise<WebPushUiStatus> {
    if (host.permission === "unsupported") {
      throw new WebPushClientError("unsupported", UNSUPPORTED);
    }
    if (host.permission === "denied") {
      throw new WebPushClientError("denied", DENIED);
    }

    let permission: BrowserPermission = host.permission;
    if (permission === "default") {
      permission = await host.requestPermission();
    }
    if (permission === "denied") {
      host.permission = "denied";
      throw new WebPushClientError("denied", DENIED);
    }
    if (permission !== "granted") {
      throw new WebPushClientError("generic", GENERIC);
    }
    host.permission = "granted";

    let applicationServerKey: Uint8Array;
    try {
      applicationServerKey = vapidPublicKeyToUint8Array(host.vapidPublicKey);
    } catch {
      throw new WebPushClientError("misconfigured", VAPID);
    }

    try {
      await host.registerWorker();
    } catch {
      throw new WebPushClientError("generic", GENERIC);
    }

    let sub: PushSubscriptionLike | null;
    try {
      sub = await host.getSubscription();
      if (!sub) {
        sub = await host.subscribe(applicationServerKey);
      }
    } catch {
      throw new WebPushClientError("generic", GENERIC);
    }

    try {
      await registerSubscription(sub);
    } catch (error) {
      if (error instanceof WebPushClientError) {
        throw error;
      }
      throw new WebPushClientError("unavailable", BACKEND);
    }

    return uiStatus();
  }

  async function syncExistingIfGranted(): Promise<WebPushUiStatus> {
    if (host.permission !== "granted") {
      return uiStatus();
    }
    if (host.sessionStorage.getItem(SYNC_ATTEMPTED_SESSION_KEY) === "1") {
      return uiStatus();
    }
    host.sessionStorage.setItem(SYNC_ATTEMPTED_SESSION_KEY, "1");

    if (!host.vapidPublicKey.trim()) {
      return uiStatus();
    }

    try {
      await host.registerWorker();
      const sub = await host.getSubscription();
      if (!sub) {
        return uiStatus();
      }
      await registerSubscription(sub);
    } catch {
      return uiStatus();
    }
    return uiStatus();
  }

  async function disableCurrentEndpoint(): Promise<WebPushUiStatus> {
    const sub = await host.getSubscription().catch(() => null);
    let id = storedEndpointId();
    if (!id && sub) {
      try {
        const view = await registerSubscription(sub);
        id = parseOpaqueEndpointId(view.id);
      } catch {
        throw new WebPushClientError("unavailable", REVOKE_FAILED);
      }
    }
    if (id) {
      try {
        await host.deleteEndpoint(id);
      } catch {
        throw new WebPushClientError("unavailable", REVOKE_FAILED);
      }
    }
    clearEndpointId();
    if (sub) {
      try {
        await sub.unsubscribe();
      } catch {
        return uiStatus();
      }
    }
    return uiStatus();
  }

  function onLogout(): void {
    if (REVOKE_PUSH_ON_LOGOUT) {
      throw new WebPushClientError("generic", GENERIC);
    }
  }

  return {
    uiStatus,
    enabled,
    storedEndpointId,
    enableFromUserGesture,
    syncExistingIfGranted,
    disableCurrentEndpoint,
    onLogout,
  };
}
