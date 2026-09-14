"use client";

import { FormEvent, useEffect, useState } from "react";
import Link from "next/link";

import {
  AuthClientError,
  getSession,
  loginWithPasskey,
  loginWithPassword,
  logout,
  type AuthSession,
  type IdentifierKind,
} from "@/lib/auth";
import { webAuthnLoginSupported } from "@/lib/webauthn";
import { AuthTurnstile, useTurnstileChallenge } from "@/components/TurnstileWidget";

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

export default function LoginPage() {
  const [session, setSession] = useState<AuthSession | null>(null);
  const [ready, setReady] = useState(false);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [kind, setKind] = useState<IdentifierKind>("email");
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [passkeySupported, setPasskeySupported] = useState(true);
  const passwordChallenge = useTurnstileChallenge("password_login");
  const passkeyBeginChallenge = useTurnstileChallenge("passkey_login_begin");
  const passkeyFinishChallenge = useTurnstileChallenge("passkey_login_finish");
  const busy = status.kind === "loading";

  useEffect(() => {
    setPasskeySupported(webAuthnLoginSupported());
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function loadSession() {
      setStatus({ kind: "loading", message: "Oturum kontrol ediliyor…" });
      try {
        const current = await getSession();
        if (cancelled) {
          return;
        }
        setSession(current);
        setStatus({ kind: "idle" });
      } catch (error) {
        if (cancelled) {
          return;
        }
        setSession(null);
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

  async function onPasskey() {
    if (passkeyBeginChallenge.blocksSubmit || passkeyFinishChallenge.blocksSubmit) {
      setStatus({ kind: "error", message: "Güvenlik doğrulaması gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "Geçiş anahtarı bekleniyor…" });
    const beginChallengeToken = passkeyBeginChallenge.consumeToken();
    const finishChallengeToken = passkeyFinishChallenge.consumeToken();
    try {
      const next = await loginWithPasskey({ beginChallengeToken, finishChallengeToken });
      setSession(next);
      setPassword("");
      setStatus({ kind: "success", message: "Giriş başarılı." });
    } catch (error) {
      passkeyBeginChallenge.applyAuthError(error);
      passkeyFinishChallenge.applyAuthError(error);
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (passwordChallenge.blocksSubmit) {
      setStatus({ kind: "error", message: "Güvenlik doğrulaması gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "Giriş yapılıyor…" });
    const challengeToken = passwordChallenge.consumeToken();
    try {
      const next = await loginWithPassword({
        kind,
        identifier: identifier.trim(),
        password,
        challengeToken,
      });
      setSession(next);
      setPassword("");
      setStatus({ kind: "success", message: "Giriş başarılı." });
    } catch (error) {
      passwordChallenge.applyAuthError(error);
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onLogout() {
    setStatus({ kind: "loading", message: "Çıkış yapılıyor…" });
    try {
      await logout();
      setSession(null);
      setStatus({ kind: "success", message: "Çıkış yapıldı." });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  return (
    <main className="auth-page">
      <h1 className="brand">KONUMLU</h1>
      <h2 className="auth-title">Giriş</h2>

      <div className="auth-status" aria-live="polite">
        {status.kind === "loading" ? <p>{status.message}</p> : null}
        {status.kind === "success" ? <p className="auth-success">{status.message}</p> : null}
        {status.kind === "error" ? <p className="auth-error">{status.message}</p> : null}
      </div>

      {!ready ? null : session ? (
        <section className="auth-card" aria-labelledby="session-heading">
          <h3 id="session-heading">Oturum açık</h3>
          <p>Giriş yaptınız.</p>
          <button type="button" onClick={() => void onLogout()} disabled={busy}>
            Çıkış yap
          </button>
        </section>
      ) : (
        <>
          <section className="auth-card auth-primary" aria-labelledby="passkey-heading">
            <h3 id="passkey-heading">Geçiş anahtarı</h3>
            <p>Tercih edilen giriş yöntemi.</p>
            {passkeySupported ? (
              <>
                <button
                  type="button"
                  className="auth-primary-action"
                  onClick={() => void onPasskey()}
                  disabled={busy || passkeyBeginChallenge.blocksSubmit || passkeyFinishChallenge.blocksSubmit}
                >
                  Geçiş anahtarı ile giriş
                </button>
                <AuthTurnstile challenge={passkeyBeginChallenge} />
                <AuthTurnstile challenge={passkeyFinishChallenge} />
              </>
            ) : (
              <p className="auth-error">Bu tarayıcı geçiş anahtarlarını desteklemiyor.</p>
            )}
          </section>

          <section className="auth-card auth-fallback" aria-labelledby="password-heading">
            <h3 id="password-heading">Şifre ile giriş</h3>
            <p>Geçiş anahtarı kullanamıyorsanız.</p>
            <form onSubmit={(event) => void onPassword(event)}>
              <fieldset className="auth-fieldset" disabled={busy}>
                <legend>Kimlik türü</legend>
                <label>
                  <input
                    type="radio"
                    name="identifier-kind"
                    value="email"
                    checked={kind === "email"}
                    onChange={() => setKind("email")}
                  />
                  E-posta
                </label>
                <label>
                  <input
                    type="radio"
                    name="identifier-kind"
                    value="phone"
                    checked={kind === "phone"}
                    onChange={() => setKind("phone")}
                  />
                  Telefon
                </label>
              </fieldset>

              <label className="auth-field" htmlFor="identifier">
                {kind === "email" ? "E-posta" : "Telefon"}
              </label>
              <input
                id="identifier"
                name="identifier"
                type={kind === "email" ? "email" : "tel"}
                autoComplete={kind === "email" ? "username" : "tel"}
                placeholder={kind === "email" ? "ornek@eposta.com" : "+90 5xx xxx xx xx"}
                value={identifier}
                onChange={(event) => setIdentifier(event.target.value)}
                required
                disabled={busy}
              />

              <label className="auth-field" htmlFor="password">
                Şifre
              </label>
              <input
                id="password"
                name="password"
                type="password"
                autoComplete="current-password"
                placeholder="Şifreniz"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                required
                disabled={busy}
              />

              <AuthTurnstile challenge={passwordChallenge} />
              <button type="submit" disabled={busy || passwordChallenge.blocksSubmit}>
                Şifre ile giriş
              </button>
            </form>
            <p className="auth-lead">
              <Link href="/sifre-sifirla">Şifremi unuttum</Link>
            </p>
          </section>
        </>
      )}
    </main>
  );
}
