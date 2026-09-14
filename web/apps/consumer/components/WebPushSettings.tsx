"use client";

import { useCallback, useEffect, useState } from "react";

import {
  NotificationPreferenceError,
  getNotificationPreferences,
  patchNotificationPreference,
  type NotificationPreference,
} from "@/lib/notificationPreferences";
import {
  categoryPreferenceLabel,
  uiStatusLabel,
  webPushCategoryPreferences,
  type WebPushUiStatus,
} from "@/lib/webPushCore";
import {
  WebPushClientError,
  disableCurrentWebPush,
  enableWebPushFromUserGesture,
  readWebPushPermission,
  webPushUiStatus,
} from "@/lib/webPush";

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";

type Status = { kind: "idle" } | { kind: "busy" } | { kind: "error"; message: string };

function messageFromError(error: unknown): string {
  if (error instanceof WebPushClientError || error instanceof NotificationPreferenceError) {
    return error.message;
  }
  return GENERIC;
}

export function WebPushSettings() {
  const [browserStatus, setBrowserStatus] = useState<WebPushUiStatus>("off");
  const [preferences, setPreferences] = useState<NotificationPreference[]>([]);
  const [status, setStatus] = useState<Status>({ kind: "idle" });

  const refreshBrowser = useCallback(() => {
    setBrowserStatus(webPushUiStatus());
  }, []);

  const load = useCallback(async () => {
    setStatus({ kind: "busy" });
    try {
      refreshBrowser();
      const rows = await getNotificationPreferences();
      setPreferences(webPushCategoryPreferences(rows));
      setStatus({ kind: "idle" });
    } catch (error) {
      refreshBrowser();
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }, [refreshBrowser]);

  useEffect(() => {
    void load();
  }, [load]);

  async function onEnable() {
    setStatus({ kind: "busy" });
    try {
      const next = await enableWebPushFromUserGesture();
      setBrowserStatus(next);
      setStatus({ kind: "idle" });
    } catch (error) {
      refreshBrowser();
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onDisable() {
    setStatus({ kind: "busy" });
    try {
      const next = await disableCurrentWebPush();
      setBrowserStatus(next);
      setStatus({ kind: "idle" });
    } catch (error) {
      refreshBrowser();
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onTogglePreference(row: NotificationPreference, enabled: boolean) {
    setStatus({ kind: "busy" });
    try {
      const rows = await patchNotificationPreference({
        channel: row.channel,
        scopeType: row.scopeType,
        scopeKey: row.scopeKey,
        enabled,
      });
      setPreferences(webPushCategoryPreferences(rows));
      setStatus({ kind: "idle" });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  const permission = readWebPushPermission();
  const busy = status.kind === "busy";
  const canEnable = permission === "default" || (permission === "granted" && browserStatus !== "on");
  const canDisable = browserStatus === "on";

  return (
    <section className="auth-card" aria-labelledby="web-push-heading">
      <h2 id="web-push-heading" className="auth-title">
        Tarayıcı bildirimleri
      </h2>
      <p className="auth-lead">
        Tarayıcı izni ile KONUMLU bildirim tercihleri ayrıdır. Bildirim gelmesi için ikisinin de açık olması gerekir.
      </p>
      <p>
        Durum: <strong>{uiStatusLabel(browserStatus)}</strong>
      </p>
      {permission === "denied" ? (
        <p className="auth-lead">
          İzin tarayıcı tarafından kapatıldı. KONUMLU tekrar sormaz. Açmak için tarayıcı ayarlarını kullanın.
        </p>
      ) : null}
      {permission === "unsupported" ? (
        <p className="auth-lead">Bu tarayıcı Web Push desteklemiyor.</p>
      ) : null}
      <div className="auth-actions">
        {canEnable ? (
          <button type="button" className="auth-primary-action" onClick={() => void onEnable()} disabled={busy}>
            Bildirimleri Aç
          </button>
        ) : null}
        {canDisable ? (
          <button type="button" onClick={() => void onDisable()} disabled={busy}>
            Bildirimleri Kapat
          </button>
        ) : null}
      </div>
      {status.kind === "error" ? <p className="auth-error">{status.message}</p> : null}

      <h3 className="auth-title">Web bildirim tercihleri</h3>
      <p className="auth-lead">Bu tercihler tarayıcı izninden bağımsızdır.</p>
      <ul className="pref-list">
        {preferences.map((row) => (
          <li key={`${row.channel}:${row.scopeType}:${row.scopeKey}`}>
            <label>
              <input
                type="checkbox"
                checked={row.enabled}
                disabled={busy || row.required}
                onChange={(event) => void onTogglePreference(row, event.target.checked)}
              />{" "}
              {categoryPreferenceLabel(row.scopeKey)}
            </label>
          </li>
        ))}
      </ul>
    </section>
  );
}
