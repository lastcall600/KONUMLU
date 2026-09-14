"use client";

import { useEffect } from "react";

import { getSession } from "@/lib/auth";
import { registerWebPushWorker, syncWebPushIfGranted } from "@/lib/webPush";

/**
 * Registers the Web Push service worker. Does not request notification permission.
 * If permission is already granted and the user is authenticated, syncs the
 * existing PushManager subscription with the backend at most once per tab session.
 */
export function WebPushLifecycle() {
  useEffect(() => {
    let cancelled = false;

    async function boot() {
      try {
        await registerWebPushWorker();
      } catch {
        return;
      }
      if (cancelled) {
        return;
      }
      try {
        const session = await getSession();
        if (cancelled || session === null) {
          return;
        }
        await syncWebPushIfGranted();
      } catch {
        return;
      }
    }

    void boot();
    return () => {
      cancelled = true;
    };
  }, []);

  return null;
}
