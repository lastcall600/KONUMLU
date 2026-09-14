/* KONUMLU consumer Web Push service worker.
 * Keep navigation allowlist in sync with lib/webPushCore.ts.
 * Payload must stay privacy-minimal: category, template_key, reference_id.
 */
const ALLOWED_INTERNAL_PATHS = {
  "/": true,
  "/bildirimler": true,
  "/mesajlar": true,
  "/kayitli-aramalar": true,
  "/randevular": true,
  "/favoriler": true,
};

function parsePushPayload(raw) {
  if (typeof raw !== "string" || raw.trim() === "") {
    return {};
  }
  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) {
      return {};
    }
    const out = {};
    if (typeof parsed.category === "string") {
      out.category = parsed.category.slice(0, 64);
    }
    if (typeof parsed.template_key === "string") {
      out.template_key = parsed.template_key.slice(0, 128);
    }
    if (typeof parsed.reference_id === "string") {
      out.reference_id = parsed.reference_id.slice(0, 128);
    }
    return out;
  } catch {
    return {};
  }
}

function resolvePushNavigation(payload) {
  switch (payload.template_key) {
    case "messaging.message_received":
      return "/mesajlar";
    case "saved_search.match":
      return "/kayitli-aramalar";
    case "offer.received":
    case "offer.accepted":
    case "offer.rejected":
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

function sanitizeInternalPath(raw) {
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
    if (ALLOWED_INTERNAL_PATHS[url.pathname]) {
      return url.pathname;
    }
  } catch {
    return "/bildirimler";
  }
  return "/bildirimler";
}

function notificationCopy(payload) {
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

self.addEventListener("push", (event) => {
  event.waitUntil(
    (async () => {
      const text = event.data ? event.data.text() : "";
      const payload = parsePushPayload(text);
      const copy = notificationCopy(payload);
      const path = sanitizeInternalPath(resolvePushNavigation(payload));
      await self.registration.showNotification(copy.title, {
        body: copy.body,
        data: { path },
      });
    })(),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const path = sanitizeInternalPath(event.notification.data && event.notification.data.path);
  event.waitUntil(
    (async () => {
      const clientsList = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      for (const client of clientsList) {
        if ("navigate" in client && "focus" in client) {
          await client.navigate(path);
          await client.focus();
          return;
        }
      }
      if (self.clients.openWindow) {
        await self.clients.openWindow(path);
      }
    })(),
  );
});
