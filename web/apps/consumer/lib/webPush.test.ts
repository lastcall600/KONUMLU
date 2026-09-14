import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import { test } from "node:test";

import {
  REVOKE_PUSH_ON_LOGOUT,
  bytesToUrlBase64,
  buildWebPushRegisterBody,
  deriveUiStatus,
  notificationCopy,
  parseOpaqueEndpointId,
  parsePushPayload,
  redactForLog,
  registerBodyHasUserId,
  resolvePushNavigation,
  sanitizeInternalPath,
  uiStatusLabel,
  vapidPublicKeyToUint8Array,
  webPushCategoryPreferences,
} from "./webPushCore";
import { createWebPushController, type PushSubscriptionLike, type WebPushHost } from "./webPushController";

const HERE = dirname(fileURLToPath(import.meta.url));

function sampleVapidPublicKey(): string {
  const bytes = new Uint8Array(65);
  bytes[0] = 0x04;
  for (let i = 1; i < 65; i += 1) {
    bytes[i] = i;
  }
  return bytesToUrlBase64(bytes);
}

function memoryStore(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial));
  return {
    getItem(key: string) {
      return data.get(key) ?? null;
    },
    setItem(key: string, value: string) {
      data.set(key, value);
    },
    removeItem(key: string) {
      data.delete(key);
    },
    snapshot() {
      return Object.fromEntries(data);
    },
  };
}

function keyBuffer(seed: number): ArrayBuffer {
  const bytes = new Uint8Array(32);
  bytes.fill(seed);
  return bytes.buffer;
}

function fakeSub(endpoint = "https://push.example.test/subscription/abc"): PushSubscriptionLike {
  return {
    endpoint,
    getKey(name) {
      return name === "p256dh" ? keyBuffer(11) : keyBuffer(17);
    },
    async unsubscribe() {
      return true;
    },
  };
}

function host(overrides: Partial<WebPushHost> & Pick<WebPushHost, "permission">): WebPushHost & {
  posts: unknown[];
  deletes: string[];
  permissionRequests: number;
} {
  const posts: unknown[] = [];
  const deletes: string[] = [];
  const appStorage = memoryStore();
  const sessionStorage = memoryStore();
  const state = {
    posts,
    deletes,
    permissionRequests: 0,
    permission: overrides.permission,
    vapidPublicKey: overrides.vapidPublicKey ?? sampleVapidPublicKey(),
    appStorage: overrides.appStorage ?? appStorage,
    sessionStorage: overrides.sessionStorage ?? sessionStorage,
    async requestPermission() {
      state.permissionRequests += 1;
      if (overrides.requestPermission) {
        return overrides.requestPermission();
      }
      return "granted" as const;
    },
    async registerWorker() {
      if (overrides.registerWorker) {
        return overrides.registerWorker();
      }
    },
    async getSubscription() {
      if (overrides.getSubscription) {
        return overrides.getSubscription();
      }
      return null;
    },
    async subscribe(applicationServerKey: Uint8Array) {
      if (overrides.subscribe) {
        return overrides.subscribe(applicationServerKey);
      }
      return fakeSub();
    },
    async postRegister(body: Parameters<WebPushHost["postRegister"]>[0]) {
      posts.push(body);
      if (overrides.postRegister) {
        return overrides.postRegister(body);
      }
      return {
        id: "11111111-1111-4111-8111-111111111111",
        channel: "web_push",
        platform: "web",
        provider: "webpush",
        revoked: false,
      };
    },
    async deleteEndpoint(id: string) {
      deletes.push(id);
      if (overrides.deleteEndpoint) {
        return overrides.deleteEndpoint(id);
      }
    },
  };
  return state as WebPushHost & { posts: unknown[]; deletes: string[]; permissionRequests: number };
}

test("does not request permission on startup sync", async () => {
  const h = host({ permission: "default" });
  const ctl = createWebPushController(h);
  await ctl.syncExistingIfGranted();
  assert.equal(h.permissionRequests, 0);
  assert.equal(ctl.enabled(), false);
});

test("explicit user action requests permission", async () => {
  const h = host({ permission: "default" });
  const ctl = createWebPushController(h);
  const status = await ctl.enableFromUserGesture();
  assert.equal(h.permissionRequests, 1);
  assert.equal(status, "on");
});

test("unsupported browser is handled without prompting", async () => {
  const h = host({ permission: "unsupported" });
  const ctl = createWebPushController(h);
  await assert.rejects(() => ctl.enableFromUserGesture(), /desteklemiyor/);
  assert.equal(h.permissionRequests, 0);
  assert.equal(ctl.uiStatus(), "unsupported");
  assert.equal(uiStatusLabel("unsupported"), "Tarayıcı desteklemiyor");
});

test("denied permission is handled without re-prompting", async () => {
  const h = host({ permission: "denied" });
  const ctl = createWebPushController(h);
  await assert.rejects(() => ctl.enableFromUserGesture(), /tarayıcı ayarlarını/);
  assert.equal(h.permissionRequests, 0);
  assert.equal(uiStatusLabel("denied"), "Tarayıcı tarafından engellendi");
});

test("granted permission subscribes with userVisibleOnly application key", async () => {
  let seen: Uint8Array | null = null;
  const h = host({
    permission: "granted",
    async subscribe(key) {
      seen = key;
      return fakeSub();
    },
  });
  const ctl = createWebPushController(h);
  await ctl.enableFromUserGesture();
  assert.ok(seen);
  assert.equal(seen.byteLength, 65);
  assert.equal(seen[0], 0x04);
  assert.equal(h.permissionRequests, 0);
});

test("VAPID public key converts to uncompressed P-256 bytes", () => {
  const key = sampleVapidPublicKey();
  const bytes = vapidPublicKeyToUint8Array(key);
  assert.equal(bytes.byteLength, 65);
  assert.equal(bytes[0], 0x04);
  assert.throws(() => vapidPublicKeyToUint8Array("not-a-key"), /malformed/);
  assert.throws(() => vapidPublicKeyToUint8Array("abcd efgh"), /malformed/);
});

test("POST payload shape is web_push/web/webpush with material only", async () => {
  const h = host({ permission: "granted" });
  const ctl = createWebPushController(h);
  await ctl.enableFromUserGesture();
  assert.equal(h.posts.length, 1);
  const body = h.posts[0] as Record<string, unknown>;
  assert.deepEqual(Object.keys(body).sort(), ["auth", "channel", "endpoint", "p256dh", "platform", "provider"]);
  assert.equal(body.channel, "web_push");
  assert.equal(body.platform, "web");
  assert.equal(body.provider, "webpush");
  assert.equal(typeof body.endpoint, "string");
  assert.equal(typeof body.p256dh, "string");
  assert.equal(typeof body.auth, "string");
});

test("user_id is never sent", () => {
  const body = buildWebPushRegisterBody({
    endpoint: "https://push.example.test/subscription/abc",
    p256dh: "p256",
    auth: "authsecret",
  });
  assert.equal(registerBodyHasUserId(body), false);
});

test("raw push material is not logged", () => {
  const redacted = JSON.stringify(
    redactForLog({
      endpoint: "https://fcm.googleapis.com/x",
      p256dh: "secret-p256",
      auth: "secret-auth",
    }),
  );
  assert.equal(redacted.includes("secret-p256"), false);
  assert.equal(redacted.includes("secret-auth"), false);
  assert.equal(redacted.includes("fcm.googleapis.com"), false);
  assert.ok(redacted.includes("[redacted]"));
});

test("successful browser + backend registration enables UI", async () => {
  const h = host({ permission: "granted" });
  const ctl = createWebPushController(h);
  const status = await ctl.enableFromUserGesture();
  assert.equal(status, "on");
  assert.equal(ctl.enabled(), true);
  assert.equal(uiStatusLabel(status), "Açık");
  assert.equal(ctl.storedEndpointId(), "11111111-1111-4111-8111-111111111111");
});

test("backend registration failure is not falsely enabled", async () => {
  const h = host({
    permission: "granted",
    async postRegister() {
      throw new Error("backend down");
    },
  });
  const ctl = createWebPushController(h);
  await assert.rejects(() => ctl.enableFromUserGesture());
  assert.equal(ctl.enabled(), false);
  assert.equal(ctl.storedEndpointId(), null);
  assert.equal(deriveUiStatus({ permission: "granted", backendEndpointId: null }), "off");
});

test("duplicate registration remains safe and posts the same current endpoint", async () => {
  let calls = 0;
  const h = host({
    permission: "granted",
    async getSubscription() {
      return fakeSub();
    },
    async postRegister(body) {
      calls += 1;
      assert.equal((body as { endpoint: string }).endpoint, "https://push.example.test/subscription/abc");
      return {
        id: "11111111-1111-4111-8111-111111111111",
        channel: "web_push",
        platform: "web",
        provider: "webpush",
        revoked: false,
      };
    },
  });
  const ctl = createWebPushController(h);
  await ctl.enableFromUserGesture();
  await ctl.enableFromUserGesture();
  assert.equal(calls, 2);
  assert.equal(ctl.storedEndpointId(), "11111111-1111-4111-8111-111111111111");
});

test("disable revokes only the current stored endpoint", async () => {
  const h = host({
    permission: "granted",
    async getSubscription() {
      return fakeSub();
    },
  });
  const ctl = createWebPushController(h);
  await ctl.enableFromUserGesture();
  const status = await ctl.disableCurrentEndpoint();
  assert.deepEqual(h.deletes, ["11111111-1111-4111-8111-111111111111"]);
  assert.equal(status, "off");
  assert.equal(ctl.storedEndpointId(), null);
});

test("browser unsubscribe is invoked on disable", async () => {
  let unsubscribed = false;
  const sub = fakeSub();
  sub.unsubscribe = async () => {
    unsubscribed = true;
    return true;
  };
  const h = host({
    permission: "granted",
    async getSubscription() {
      return sub;
    },
  });
  const ctl = createWebPushController(h);
  await ctl.enableFromUserGesture();
  await ctl.disableCurrentEndpoint();
  assert.equal(unsubscribed, true);
});

test("logout does not auto-revoke endpoint", async () => {
  const h = host({ permission: "granted" });
  const ctl = createWebPushController(h);
  await ctl.enableFromUserGesture();
  ctl.onLogout();
  assert.equal(REVOKE_PUSH_ON_LOGOUT, false);
  assert.equal(h.deletes.length, 0);
  assert.equal(ctl.enabled(), true);
});

test("category preferences remain separate from browser permission", () => {
  const rows = [
    { channel: "web_push", scopeType: "category", scopeKey: "messages", stored: null, enabled: false, required: false },
    { channel: "email", scopeType: "category", scopeKey: "messages", stored: null, enabled: true, required: false },
  ];
  const filtered = webPushCategoryPreferences(rows);
  assert.equal(filtered.length, 1);
  assert.equal(filtered[0]?.enabled, false);
  assert.equal(deriveUiStatus({ permission: "granted", backendEndpointId: "11111111-1111-4111-8111-111111111111" }), "on");
});

test("service worker push payload displays generic notification copy", () => {
  const copy = notificationCopy({ category: "messages", template_key: "messaging.message_received" });
  assert.equal(copy.title, "KONUMLU");
  assert.equal(copy.body.includes("mesaj"), true);
  assert.equal(copy.body.includes("otp"), false);
});

test("notification click uses controlled internal navigation", () => {
  const path = sanitizeInternalPath(resolvePushNavigation({ category: "messages", template_key: "messaging.message_received" }));
  assert.equal(path, "/mesajlar");
});

test("arbitrary external navigation is rejected", () => {
  assert.equal(sanitizeInternalPath("https://evil.example/phish"), "/bildirimler");
  assert.equal(sanitizeInternalPath("//evil.example"), "/bildirimler");
  assert.equal(sanitizeInternalPath("/\\evil"), "/bildirimler");
  assert.equal(sanitizeInternalPath("/admin"), "/bildirimler");
  const parsed = parsePushPayload(
    JSON.stringify({
      category: "messages",
      url: "https://evil.example",
      title: "secret otp 123456",
      body: "card 4111",
    }),
  );
  assert.equal("url" in parsed, false);
  assert.equal(resolvePushNavigation(parsed), "/mesajlar");
});

test("opaque endpoint id storage rejects raw subscription material", () => {
  assert.equal(parseOpaqueEndpointId("https://push.example.test/subscription/abc"), null);
  assert.equal(parseOpaqueEndpointId("not-a-uuid"), null);
  assert.ok(parseOpaqueEndpointId("11111111-1111-4111-8111-111111111111"));
});

test("startup sync posts at most once per session", async () => {
  const h = host({
    permission: "granted",
    async getSubscription() {
      return fakeSub();
    },
  });
  const ctl = createWebPushController(h);
  await ctl.syncExistingIfGranted();
  await ctl.syncExistingIfGranted();
  assert.equal(h.posts.length, 1);
  assert.equal(h.permissionRequests, 0);
});

test("service worker source rejects payload-driven external URLs", () => {
  const sw = readFileSync(join(HERE, "..", "public", "sw.js"), "utf8");
  assert.match(sw, /addEventListener\("push"/);
  assert.match(sw, /showNotification/);
  assert.match(sw, /notificationclick/);
  assert.equal(sw.includes("event.data.url"), false);
  assert.equal(sw.includes("location.href = payload"), false);
});

test("auth logout source does not revoke push endpoints", () => {
  const auth = readFileSync(join(HERE, "auth.ts"), "utf8");
  assert.equal(auth.includes("push-endpoints"), false);
  assert.match(auth, /\/v1\/auth\/logout/);
});

test("mobile push client files were not added", () => {
  const webPush = readFileSync(join(HERE, "webPush.ts"), "utf8");
  assert.equal(webPush.includes("fcm"), false);
  assert.equal(webPush.toLowerCase().includes("apns"), false);
});
