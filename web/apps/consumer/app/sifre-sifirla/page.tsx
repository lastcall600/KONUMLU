"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";

import {
  AuthClientError,
  completePasswordReset,
  startPasswordReset,
  verifyPasswordReset,
  type IdentifierKind,
} from "@/lib/auth";
import { DEFAULT_LOCALE } from "@/lib/locale";
import { AuthTurnstile, useTurnstileChallenge } from "@/components/TurnstileWidget";

type Step = 1 | 2 | 3 | "done";

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

export default function PasswordResetPage() {
  const [step, setStep] = useState<Step>(1);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [kind, setKind] = useState<IdentifierKind>("email");
  const [identifier, setIdentifier] = useState("");
  const [challengeId, setChallengeId] = useState("");
  const [code, setCode] = useState("");
  const [resetProof, setResetProof] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const startChallenge = useTurnstileChallenge("reset_start");
  const verifyChallenge = useTurnstileChallenge("reset_verify");
  const completeChallenge = useTurnstileChallenge("reset_complete");
  const busy = status.kind === "loading";

  async function onStart(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (startChallenge.blocksSubmit) {
      setStatus({ kind: "error", message: "Güvenlik doğrulaması gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "İstek gönderiliyor…" });
    const challengeToken = startChallenge.consumeToken();
    try {
      const trimmed = identifier.trim();
      const result = await startPasswordReset({
        kind,
        identifier: trimmed,
        locale: DEFAULT_LOCALE,
        challengeToken,
      });
      setIdentifier(trimmed);
      setChallengeId(result.challengeId);
      setCode("");
      setResetProof("");
      setStep(2);
      setStatus({
        kind: "success",
        message:
          "İstek alındı. Bir kod aldıysanız girin. Kod gelmezse daha sonra tekrar deneyin.",
      });
    } catch (error) {
      startChallenge.applyAuthError(error);
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onVerify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (verifyChallenge.blocksSubmit) {
      setStatus({ kind: "error", message: "Güvenlik doğrulaması gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "Kod doğrulanıyor…" });
    const challengeToken = verifyChallenge.consumeToken();
    try {
      const result = await verifyPasswordReset({
        challengeId,
        code: code.trim(),
        challengeToken,
      });
      setResetProof(result.resetProof);
      setCode("");
      setStep(3);
      setStatus({ kind: "success", message: "Doğrulama tamamlandı. Yeni şifrenizi belirleyin." });
    } catch (error) {
      verifyChallenge.applyAuthError(error);
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onComplete(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (password !== passwordConfirm) {
      setStatus({ kind: "error", message: "Şifreler eşleşmiyor. Lütfen tekrar deneyin." });
      return;
    }
    if (completeChallenge.blocksSubmit) {
      setStatus({ kind: "error", message: "Güvenlik doğrulaması gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "Şifre güncelleniyor…" });
    const challengeToken = completeChallenge.consumeToken();
    try {
      await completePasswordReset({
        resetProof,
        newPassword: password,
        challengeToken,
      });
      setResetProof("");
      setChallengeId("");
      setPassword("");
      setPasswordConfirm("");
      setStep("done");
      setStatus({
        kind: "success",
        message: "Şifreniz güncellendi. Tüm oturumlar kapatıldı. Giriş yapmanız gerekir.",
      });
    } catch (error) {
      completeChallenge.applyAuthError(error);
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  function backToIdentifier() {
    setStep(1);
    setCode("");
    setResetProof("");
    setPassword("");
    setPasswordConfirm("");
    setStatus({ kind: "idle" });
  }

  return (
    <main className="auth-page">
      <h1 className="brand">KONUMLU</h1>
      <h2 className="auth-title">Şifre sıfırla</h2>
      <p className="auth-lead">
        Hesabınıza bağlı e-posta veya telefon ile şifrenizi yenileyebilirsiniz. İşlem sonunda otomatik
        giriş yapılmaz.
      </p>

      <div className="auth-status" aria-live="polite">
        {status.kind === "loading" ? <p>{status.message}</p> : null}
        {status.kind === "success" ? <p className="auth-success">{status.message}</p> : null}
        {status.kind === "error" ? <p className="auth-error">{status.message}</p> : null}
      </div>

      {step === "done" ? (
        <section className="auth-card" aria-labelledby="reset-done-heading">
          <h3 id="reset-done-heading">Şifre güncellendi</h3>
          <p>Yeni şifrenizle giriş yapın. Mevcut oturumlar geçersizdir.</p>
          <Link href="/giris">Girişe git</Link>
        </section>
      ) : (
        <>
          <p className="auth-steps" aria-current="step">
            Adım {step} / 3
          </p>

          {step === 1 ? (
            <section className="auth-card" aria-labelledby="reset-identifier-heading">
              <h3 id="reset-identifier-heading">Hesap bilgisi</h3>
              <form onSubmit={(event) => void onStart(event)}>
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

                <label className="auth-field" htmlFor="reset-identifier">
                  {kind === "email" ? "E-posta" : "Telefon"}
                </label>
                <input
                  id="reset-identifier"
                  name="identifier"
                  type={kind === "email" ? "email" : "tel"}
                  autoComplete={kind === "email" ? "email" : "tel"}
                  placeholder={kind === "email" ? "ornek@eposta.com" : "+90 5xx xxx xx xx"}
                  value={identifier}
                  onChange={(event) => setIdentifier(event.target.value)}
                  required
                  disabled={busy}
                />

                <AuthTurnstile challenge={startChallenge} />
                <button type="submit" disabled={busy || startChallenge.blocksSubmit}>
                  Devam
                </button>
              </form>
            </section>
          ) : null}

          {step === 2 ? (
            <section className="auth-card" aria-labelledby="reset-verify-heading">
              <h3 id="reset-verify-heading">Doğrulama</h3>
              <p>
                {kind === "email" ? "E-posta" : "Telefon"}: {identifier}
              </p>
              <form onSubmit={(event) => void onVerify(event)}>
                <label className="auth-field" htmlFor="reset-code">
                  Doğrulama kodu
                </label>
                <input
                  id="reset-code"
                  name="code"
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  placeholder="Doğrulama kodu"
                  value={code}
                  onChange={(event) => setCode(event.target.value)}
                  required
                  disabled={busy}
                />
                <AuthTurnstile challenge={verifyChallenge} />
                <div className="auth-actions">
                  <button type="button" onClick={backToIdentifier} disabled={busy}>
                    Geri
                  </button>
                  <button type="submit" disabled={busy || verifyChallenge.blocksSubmit}>
                    Doğrula
                  </button>
                </div>
              </form>
            </section>
          ) : null}

          {step === 3 ? (
            <section className="auth-card auth-fallback" aria-labelledby="reset-password-heading">
              <h3 id="reset-password-heading">Yeni şifre</h3>
              <form onSubmit={(event) => void onComplete(event)}>
                <label className="auth-field" htmlFor="reset-password">
                  Yeni şifre
                </label>
                <input
                  id="reset-password"
                  name="newPassword"
                  type="password"
                  autoComplete="new-password"
                  placeholder="Yeni şifre"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  required
                  disabled={busy}
                />
                <label className="auth-field" htmlFor="reset-password-confirm">
                  Şifre tekrarı
                </label>
                <input
                  id="reset-password-confirm"
                  name="newPasswordConfirm"
                  type="password"
                  autoComplete="new-password"
                  placeholder="Şifreyi tekrar yazın"
                  value={passwordConfirm}
                  onChange={(event) => setPasswordConfirm(event.target.value)}
                  required
                  disabled={busy}
                />
                <AuthTurnstile challenge={completeChallenge} />
                <button type="submit" disabled={busy || completeChallenge.blocksSubmit}>
                  Şifreyi güncelle
                </button>
              </form>
            </section>
          ) : null}
        </>
      )}
    </main>
  );
}
