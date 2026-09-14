# Consumer Web Push

Browser Web Push for `web/apps/consumer` only. Mobile FCM/APNs clients are out of scope.

## User gesture

The app never calls `Notification.requestPermission` on page load. Permission is requested only after an explicit action (`Bildirimleri Aç` on `/bildirimler`).

## Service worker

`public/sw.js` handles `push` and `notificationclick`. It shows a generic KONUMLU notification from privacy-minimal fields (`category`, `template_key`, `reference_id`). It does not render OTP, tokens, payment data, contact data, or message bodies. Click navigation is an allowlisted internal path only. Payload `url` values are ignored.

## VAPID

Frontend config is the **public** key only:

`NEXT_PUBLIC_WEBPUSH_VAPID_PUBLIC_KEY`

Never put `WEBPUSH_VAPID_PRIVATE_KEY` or server push credentials in the consumer app.

## Registration and revoke

After permission is `granted`, the client registers the service worker, calls `PushManager.subscribe({ userVisibleOnly: true })`, and `POST /v1/push-endpoints` with `channel=web_push`, `platform=web`, `provider=webpush` and `endpoint` / `p256dh` / `auth`. Session identity comes from cookies. `user_id` is not sent.

The registration response `id` is stored as an opaque backend endpoint id (`localStorage` key `konumlu.webpush.endpoint_id`). Raw endpoint URL, `p256dh`, and `auth` stay in the browser PushManager and are not written to application storage or logs.

Disable (`Bildirimleri Kapat`) revokes **that** endpoint (`DELETE /v1/push-endpoints/{id}`) and unsubscribes the current browser subscription. It does not revoke other devices.

If permission is already granted at authenticated startup, the client syncs the existing subscription at most once per tab session. It does not prompt.

## Logout

Logout does **not** revoke Web Push endpoints (NOTIFY-C).

## Browser permission vs KONUMLU preference

Browser permission and `/v1/notification-preferences` (web_push category/channel) are independent. Delivery requires both.

## States

- Açık — permission granted and backend registration succeeded
- Kapalı — default permission, or granted without a successful backend id
- Tarayıcı desteklemiyor
- Tarayıcı tarafından engellendi — no repeated prompts; recover via browser settings
