"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import { WebPushSettings } from "@/components/WebPushSettings";
import { AuthClientError, getSession } from "@/lib/auth";

export default function NotificationsSettingsPage() {
  const router = useRouter();
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const session = await getSession();
        if (cancelled) {
          return;
        }
        if (session === null) {
          router.replace("/giris");
          return;
        }
        setReady(true);
      } catch (error) {
        if (cancelled) {
          return;
        }
        if (error instanceof AuthClientError && error.code === "unauthenticated") {
          router.replace("/giris");
          return;
        }
        setReady(true);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [router]);

  if (!ready) {
    return (
      <main>
        <p>Yükleniyor…</p>
      </main>
    );
  }

  return (
    <main>
      <p>
        <Link href="/">Ana sayfa</Link>
      </p>
      <h1 className="brand">Bildirimler</h1>
      <WebPushSettings />
    </main>
  );
}
