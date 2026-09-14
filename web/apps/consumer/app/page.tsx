"use client";

import { useEffect, useState } from "react";
import Link from "next/link";

import { AuthClientError, getSession, logout } from "@/lib/auth";

type Status =
  | { kind: "idle" }
  | { kind: "loading"; message: string }
  | { kind: "success"; message: string }
  | { kind: "error"; message: string };

function messageFromError(error: unknown): string {
  if (error instanceof AuthClientError) {
    return error.message;
  }
  return "Bir sorun oluştu. Lütfen tekrar deneyin.";
}

export default function HomePage() {
  const [ready, setReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const busy = status.kind === "loading";

  useEffect(() => {
    let cancelled = false;

    async function loadSession() {
      setStatus({ kind: "loading", message: "Oturum kontrol ediliyor…" });
      try {
        const current = await getSession();
        if (cancelled) {
          return;
        }
        setAuthenticated(current !== null);
        setStatus({ kind: "idle" });
      } catch (error) {
        if (cancelled) {
          return;
        }
        setAuthenticated(false);
        setStatus({ kind: "error", message: messageFromError(error) });
      } finally {
        if (!cancelled) {
          setReady(true);
        }
      }
    }

    void loadSession();
    return () => {
      cancelled = true;
    };
  }, []);

  async function onLogout() {
    setStatus({ kind: "loading", message: "Çıkış yapılıyor…" });
    try {
      await logout();
      setAuthenticated(false);
      setStatus({ kind: "success", message: "Çıkış yapıldı." });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  return (
    <main>
      <h1 className="brand">KONUMLU</h1>
      <div className="auth-status" aria-live="polite">
        {status.kind === "loading" ? <p>{status.message}</p> : null}
        {status.kind === "success" ? <p className="auth-success">{status.message}</p> : null}
        {status.kind === "error" ? <p className="auth-error">{status.message}</p> : null}
      </div>
      {!ready ? null : authenticated ? (
        <section className="auth-card" aria-labelledby="home-session-heading">
          <h2 id="home-session-heading" className="auth-title">
            Oturum açık
          </h2>
          <p>Giriş yaptınız.</p>
          <p>
            <Link href="/ara">İlan ara</Link>
          </p>
          <p>
            <Link href="/favoriler">Favoriler</Link>
          </p>
          <p>
            <Link href="/kayitli-aramalar">Kayıtlı aramalar</Link>
          </p>
          <p>
            <Link href="/mesajlar">Mesajlar</Link>
          </p>
          <p>
            <Link href="/randevular">Randevular</Link>
          </p>
          <p>
            <Link href="/guven-pasaportum">Güven Pasaportum</Link>
          </p>
          <p>
            <Link href="/bildirimler">Bildirimler</Link>
          </p>
          <p>
            <Link href="/ilan-ver">İlan ver</Link>
          </p>
          <button type="button" onClick={() => void onLogout()} disabled={busy}>
            Çıkış yap
          </button>
        </section>
      ) : (
        <p>
          <Link href="/ara" className="search-placeholder">
            İlan ara
          </Link>
        </p>
      )}
    </main>
  );
}
