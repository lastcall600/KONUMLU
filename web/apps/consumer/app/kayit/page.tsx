"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";

import {
  AuthClientError,
  completeSignup,
  finishSignupVerification,
  getSession,
  logout,
  startSignupVerification,
  type IdentifierKind,
} from "@/lib/auth";
import { DEFAULT_LOCALE } from "@/lib/locale";
import { AuthTurnstile, useTurnstileChallenge } from "@/components/TurnstileWidget";

type Step = 1 | 2 | 3;

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

export default function SignupPage() {
  const router = useRouter();
  const [ready, setReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [step, setStep] = useState<Step>(1);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [kind, setKind] = useState<IdentifierKind>("email");
  const [identifier, setIdentifier] = useState("");
  const [challengeId, setChallengeId] = useState("");
  const [code, setCode] = useState("");
  const [signupProof, setSignupProof] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const startChallenge = useTurnstileChallenge("signup_start");
  const finishChallenge = useTurnstileChallenge("signup_finish");
  const completeChallenge = useTurnstileChallenge("signup_complete");
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

  async function onStart(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (startChallenge.blocksSubmit) {
      setStatus({ kind: "error", message: "Güvenlik doğrulaması gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "Doğrulama başlatılıyor…" });
    const challengeToken = startChallenge.consumeToken();
    try {
      const trimmed = identifier.trim();
      const result = await startSignupVerification({
        kind,
        identifier: trimmed,
        locale: DEFAULT_LOCALE,
        challengeToken,
      });
      setIdentifier(trimmed);
      setChallengeId(result.challengeId);
      setCode("");
      setSignupProof("");
      setStep(2);
      setStatus({
        kind: "success",
        message:
          "Doğrulama adımına geçildi. Bir kod aldıysanız girin. Kod gelmezse daha sonra tekrar deneyin.",
      });
    } catch (error) {
      startChallenge.applyAuthError(error);
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onFinish(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (finishChallenge.blocksSubmit) {
      setStatus({ kind: "error", message: "Güvenlik doğrulaması gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "Kod doğrulanıyor…" });
    const challengeToken = finishChallenge.consumeToken();
    try {
      const result = await finishSignupVerification({
        challengeId,
        code: code.trim(),
        challengeToken,
      });
      setSignupProof(result.signupProof);
      setCode("");
      setStep(3);
      setStatus({ kind: "success", message: "Doğrulama tamamlandı. Hesabı oluşturabilirsiniz." });
    } catch (error) {
      finishChallenge.applyAuthError(error);
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onComplete(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (password !== "" && password !== passwordConfirm) {
      setStatus({ kind: "error", message: "Şifreler eşleşmiyor. Lütfen tekrar deneyin." });
      return;
    }
    if (completeChallenge.blocksSubmit) {
      setStatus({ kind: "error", message: "Güvenlik doğrulaması gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "Hesap oluşturuluyor…" });
    const challengeToken = completeChallenge.consumeToken();
    try {
      await completeSignup({
        signupProof,
        password: password === "" ? undefined : password,
        challengeToken,
      });
      setSignupProof("");
      setPassword("");
      setPasswordConfirm("");
      setStatus({ kind: "success", message: "Hesap oluşturuldu." });
      router.replace("/");
    } catch (error) {
      completeChallenge.applyAuthError(error);
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

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

  function backToIdentifier() {
    setStep(1);
    setCode("");
    setStatus({ kind: "idle" });
  }

  function backToVerification() {
    setStep(2);
    setPassword("");
    setPasswordConfirm("");
    setStatus({ kind: "idle" });
  }

  return (
    <main className="auth-page">
      <h1 className="brand">KONUMLU</h1>
      <h2 className="auth-title">Kayıt</h2>
      <p className="auth-lead">
        Geçiş anahtarı tercih edilen yöntemdir. Şifre isteğe bağlı bir yedektir. Kayıt sırasında geçiş
        anahtarı henüz eklenmez.
      </p>

      <div className="auth-status" aria-live="polite">
        {status.kind === "loading" ? <p>{status.message}</p> : null}
        {status.kind === "success" ? <p className="auth-success">{status.message}</p> : null}
        {status.kind === "error" ? <p className="auth-error">{status.message}</p> : null}
      </div>

      {!ready ? null : authenticated ? (
        <section className="auth-card" aria-labelledby="session-heading">
          <h3 id="session-heading">Oturum açık</h3>
          <p>Giriş yaptınız.</p>
          <button type="button" onClick={() => void onLogout()} disabled={busy}>
            Çıkış yap
          </button>
        </section>
      ) : (
        <>
          <p className="auth-steps" aria-current="step">
            Adım {step} / 3
          </p>

          {step === 1 ? (
            <section className="auth-card" aria-labelledby="identifier-heading">
              <h3 id="identifier-heading">İletişim bilgisi</h3>
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

                <label className="auth-field" htmlFor="signup-identifier">
                  {kind === "email" ? "E-posta" : "Telefon"}
                </label>
                <input
                  id="signup-identifier"
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
            <section className="auth-card" aria-labelledby="verify-heading">
              <h3 id="verify-heading">Doğrulama</h3>
              <p>
                {kind === "email" ? "E-posta" : "Telefon"}: {identifier}
              </p>
              <form onSubmit={(event) => void onFinish(event)}>
                <label className="auth-field" htmlFor="signup-code">
                  Doğrulama kodu
                </label>
                <input
                  id="signup-code"
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
                <AuthTurnstile challenge={finishChallenge} />
                <div className="auth-actions">
                  <button type="button" onClick={backToIdentifier} disabled={busy}>
                    Geri
                  </button>
                  <button type="submit" disabled={busy || finishChallenge.blocksSubmit}>
                    Doğrula
                  </button>
                </div>
              </form>
            </section>
          ) : null}

          {step === 3 ? (
            <section className="auth-card auth-fallback" aria-labelledby="complete-heading">
              <h3 id="complete-heading">Hesabı tamamla</h3>
              <p>Şifre zorunlu değildir. İsterseniz yedek olarak ekleyebilirsiniz.</p>
              <form onSubmit={(event) => void onComplete(event)}>
                <label className="auth-field" htmlFor="signup-password">
                  Şifre (isteğe bağlı)
                </label>
                <input
                  id="signup-password"
                  name="password"
                  type="password"
                  autoComplete="new-password"
                  placeholder="İsteğe bağlı şifre"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  disabled={busy}
                />
                {password !== "" ? (
                  <>
                    <label className="auth-field" htmlFor="signup-password-confirm">
                      Şifre tekrarı
                    </label>
                    <input
                      id="signup-password-confirm"
                      name="passwordConfirm"
                      type="password"
                      autoComplete="new-password"
                      placeholder="Şifreyi tekrar yazın"
                      value={passwordConfirm}
                      onChange={(event) => setPasswordConfirm(event.target.value)}
                      required
                      disabled={busy}
                    />
                  </>
                ) : null}
                <AuthTurnstile challenge={completeChallenge} />
                <div className="auth-actions">
                  <button type="button" onClick={backToVerification} disabled={busy}>
                    Geri
                  </button>
                  <button type="submit" disabled={busy || completeChallenge.blocksSubmit}>
                    Hesabı oluştur
                  </button>
                </div>
              </form>
            </section>
          ) : null}
        </>
      )}
    </main>
  );
}
