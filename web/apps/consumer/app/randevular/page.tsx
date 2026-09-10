"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import Link from "next/link";

import { AuthClientError, getSession } from "@/lib/auth";
import {
  ReviewsClientError,
  classifyEligibility,
  getReviewEligibility,
  type ReviewEligibilityKind,
} from "@/lib/reviews";
import {
  VerifiedClientError,
  acceptAppointment,
  appointmentStatusLabel,
  cancelAppointment,
  finishVerification,
  listAppointments,
  rejectAppointment,
  startVerification,
  type Appointment,
  type IssuedChallenge,
  type VerificationMethod,
  VERIFIED_OTP_DIGITS,
  digitsOnly,
} from "@/lib/verified";
import { IssuedChallengePanel } from "@/components/IssuedChallengePanel";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const REQUESTER_TOKEN_HINT =
  "Sağlayıcı QR kodunu taradıysanız veya jetonu size uygulama dışında ilettiyse doğrulamayı tamamlamak için ham jetonu girin. Jeton otomatik gelmez.";
const REQUESTER_OTP_HINT =
  "Sağlayıcı size 6 haneli kodu uygulama dışında ilettiyse doğrulamayı tamamlamak için kodu girin. Kod otomatik gelmez.";

type PageState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; userId: string; appointments: Appointment[] };

type IssuedByAppointment = Record<string, IssuedChallenge>;
type CompletedByAppointment = Record<string, true>;

function messageFromError(error: unknown): string {
  if (
    error instanceof VerifiedClientError ||
    error instanceof AuthClientError ||
    error instanceof ReviewsClientError
  ) {
    return error.message;
  }
  return UNAVAILABLE;
}

function formatDateTime(value: string | undefined): string {
  if (!value) {
    return "Planlanmadı";
  }
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    return "Planlanmadı";
  }
  return new Intl.DateTimeFormat("tr-TR", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(parsed));
}

export default function AppointmentsPage() {
  const [state, setState] = useState<PageState>({ kind: "loading" });
  const [issued, setIssued] = useState<IssuedByAppointment>({});
  const [completed, setCompleted] = useState<CompletedByAppointment>({});

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      const session = await getSession();
      if (session === null) {
        setState({ kind: "unauthenticated" });
        return;
      }
      const appointments = await listAppointments();
      const done: CompletedByAppointment = {};
      for (const row of appointments) {
        if (row.status === "completed") {
          done[row.appointmentId] = true;
        }
      }
      setCompleted(done);
      setState({ kind: "ready", userId: session.userId, appointments });
    } catch (error) {
      if (error instanceof AuthClientError && error.code === "unauthenticated") {
        setState({ kind: "unauthenticated" });
        return;
      }
      if (error instanceof VerifiedClientError && error.code === "unauthenticated") {
        setState({ kind: "unauthenticated" });
        return;
      }
      setState({ kind: "unavailable", message: messageFromError(error) });
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  function replaceAppointment(updated: Appointment) {
    setState((current) => {
      if (current.kind !== "ready") {
        return current;
      }
      return {
        ...current,
        appointments: current.appointments.map((row) =>
          row.appointmentId === updated.appointmentId ? updated : row,
        ),
      };
    });
    if (updated.status === "completed") {
      setCompleted((current) => ({ ...current, [updated.appointmentId]: true }));
    }
  }

  return (
    <main>
      <p className="search-nav">
        <Link href="/">Ana sayfa</Link>
        {" · "}
        <Link href="/mesajlar">Mesajlar</Link>
        {" · "}
        <Link href="/guven-pasaportum">Güven Pasaportum</Link>
        {" · "}
        <Link href="/degerlendirmelerim">Değerlendirmelerim</Link>
      </p>
      <h1 className="auth-title">Randevular</h1>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unauthenticated" ? (
        <p>
          Randevuları görmek için <Link href="/giris">giriş yapın</Link>.
        </p>
      ) : null}
      {state.kind === "unavailable" ? (
        <div className="page-status">
          <p className="auth-error" role="alert">
            {state.message}
          </p>
          <button type="button" onClick={() => void load()}>
            Tekrar dene
          </button>
        </div>
      ) : null}
      {state.kind === "ready" && state.appointments.length === 0 ? (
        <p className="search-placeholder">Henüz randevu yok.</p>
      ) : null}
      {state.kind === "ready" && state.appointments.length > 0 ? (
        <ul className="message-list">
          {state.appointments.map((row) => (
            <li key={row.appointmentId}>
              <AppointmentRow
                appointment={row}
                userId={state.userId}
                issued={issued[row.appointmentId]}
                verified={completed[row.appointmentId] === true}
                onUpdated={replaceAppointment}
                onIssued={(challenge) => {
                  const appointmentId = challenge.appointmentId;
                  if (!appointmentId) {
                    return;
                  }
                  setIssued((current) => ({
                    ...current,
                    [appointmentId]: challenge,
                  }));
                }}
                onVerified={(appointmentId) => {
                  setCompleted((current) => ({ ...current, [appointmentId]: true }));
                }}
              />
            </li>
          ))}
        </ul>
      ) : null}
    </main>
  );
}

function AppointmentRow({
  appointment,
  userId,
  issued,
  verified,
  onUpdated,
  onIssued,
  onVerified,
}: {
  appointment: Appointment;
  userId: string;
  issued: IssuedChallenge | undefined;
  verified: boolean;
  onUpdated: (row: Appointment) => void;
  onIssued: (challenge: IssuedChallenge) => void;
  onVerified: (appointmentId: string) => void;
}) {
  const isRequester = appointment.requesterUserId === userId;
  const isProvider = appointment.providerUserId === userId;
  const roleLabel = isRequester ? "Talep eden" : isProvider ? "Sağlayıcı" : "Katılımcı";
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [finishToken, setFinishToken] = useState("");
  const [finishSuccess, setFinishSuccess] = useState(false);
  const [method, setMethod] = useState<VerificationMethod>("qr");
  const [finishMethod, setFinishMethod] = useState<VerificationMethod>("otp");

  async function run(action: () => Promise<Appointment>) {
    setBusy(true);
    setError(null);
    try {
      const updated = await action();
      onUpdated(updated);
    } catch (err) {
      if (err instanceof VerifiedClientError) {
        setError(err.message);
      } else {
        setError(GENERIC);
      }
    } finally {
      setBusy(false);
    }
  }

  async function onStart() {
    setBusy(true);
    setError(null);
    setCopied(false);
    try {
      const challenge = await startVerification(appointment.appointmentId, method);
      onIssued(challenge);
    } catch (err) {
      if (err instanceof VerifiedClientError) {
        setError(err.message);
      } else {
        setError(GENERIC);
      }
    } finally {
      setBusy(false);
    }
  }

  async function onCopy() {
    if (!issued) {
      return;
    }
    try {
      await navigator.clipboard.writeText(issued.token);
      setCopied(true);
    } catch {
      setCopied(false);
      setError(issued.method === "otp" ? "Kod kopyalanamadı. Metni elle seçin." : "Jeton kopyalanamadı. Metni elle seçin.");
    }
  }

  async function onFinish(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const secret =
        finishMethod === "otp" ? digitsOnly(finishToken) : finishToken.trim();
      const interaction = await finishVerification(appointment.appointmentId, secret);
      setFinishToken("");
      setFinishSuccess(true);
      onVerified(appointment.appointmentId);
      onUpdated({
        ...appointment,
        status: "completed",
        verifiedInteractionId: interaction.interactionId,
      });
    } catch (err) {
      if (err instanceof VerifiedClientError) {
        setError(err.message);
      } else {
        setError(GENERIC);
      }
    } finally {
      setBusy(false);
    }
  }

  const canCancel =
    (isRequester || isProvider) &&
    (appointment.status === "requested" || appointment.status === "accepted");
  const showProviderRequested = isProvider && appointment.status === "requested";
  const showProviderAccepted = isProvider && appointment.status === "accepted";
  const showRequesterFinish = isRequester && appointment.status === "accepted";
  const showVerified = verified || appointment.status === "completed" || finishSuccess;

  return (
    <article className="message-list-item">
      <p>
        <Link href={`/ilan/${appointment.listingId}`}>İlan {appointment.listingId}</Link>
      </p>
      <p>Planlanan: {formatDateTime(appointment.scheduledAt)}</p>
      <p>Durum: {appointmentStatusLabel(appointment.status)}</p>
      <p>Rol: {roleLabel}</p>
      {showVerified ? (
        <p className="auth-success">
          {finishSuccess ? "Doğrulanmış etkileşim tamamlandı. " : null}✓ Doğrulanmış İnceleme
        </p>
      ) : null}
      {isRequester && showVerified && appointment.verifiedInteractionId ? (
        <ReviewEntry interactionId={appointment.verifiedInteractionId} />
      ) : null}

      {showProviderRequested ? (
        <p>
          <button type="button" disabled={busy} onClick={() => void run(() => acceptAppointment(appointment.appointmentId))}>
            Kabul Et
          </button>{" "}
          <button type="button" disabled={busy} onClick={() => void run(() => rejectAppointment(appointment.appointmentId))}>
            Reddet
          </button>
        </p>
      ) : null}

      {showProviderAccepted ? (
        <div>
          {canCancel ? (
            <p>
              <button
                type="button"
                disabled={busy}
                onClick={() => void run(() => cancelAppointment(appointment.appointmentId))}
              >
                İptal Et
              </button>
            </p>
          ) : null}
          <p>
            <label>
              Yöntem{" "}
              <select
                value={method}
                disabled={busy}
                onChange={(event) => {
                  const next = event.target.value;
                  if (next === "qr" || next === "otp") {
                    setMethod(next);
                  }
                }}
              >
                <option value="qr">qr</option>
                <option value="otp">otp</option>
              </select>
            </label>{" "}
            <button type="button" disabled={busy} onClick={() => void onStart()}>
              Doğrulamayı Başlat
            </button>
          </p>
          {issued ? (
            <IssuedChallengePanel issued={issued} copied={copied} onCopy={() => void onCopy()} />
          ) : null}
        </div>
      ) : null}

      {isRequester && canCancel && !showProviderAccepted ? (
        <p>
          <button
            type="button"
            disabled={busy}
            onClick={() => void run(() => cancelAppointment(appointment.appointmentId))}
          >
            İptal Et
          </button>
        </p>
      ) : null}

      {showRequesterFinish ? (
        <form onSubmit={(event) => void onFinish(event)}>
          <p>
            <label>
              Doğrulama türü{" "}
              <select
                value={finishMethod}
                disabled={busy}
                onChange={(event) => {
                  const next = event.target.value;
                  if (next === "qr" || next === "otp") {
                    setFinishMethod(next);
                    setFinishToken("");
                  }
                }}
              >
                <option value="otp">6 haneli kod</option>
                <option value="qr">QR jetonu</option>
              </select>
            </label>
          </p>
          <p>{finishMethod === "otp" ? REQUESTER_OTP_HINT : REQUESTER_TOKEN_HINT}</p>
          {finishMethod === "otp" ? (
            <label>
              Kod
              <input
                type="text"
                name="otp"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern={`\\d{${VERIFIED_OTP_DIGITS}}`}
                maxLength={VERIFIED_OTP_DIGITS}
                autoCorrect="off"
                autoCapitalize="off"
                spellCheck={false}
                value={finishToken}
                onChange={(event) => setFinishToken(digitsOnly(event.target.value).slice(0, VERIFIED_OTP_DIGITS))}
                disabled={busy}
                required
              />
            </label>
          ) : (
            <label>
              Jeton
              <input
                type="text"
                name="token"
                autoComplete="off"
                value={finishToken}
                onChange={(event) => setFinishToken(event.target.value)}
                disabled={busy}
                required
              />
            </label>
          )}
          <button type="submit" disabled={busy}>
            Doğrulamayı Tamamla
          </button>
        </form>
      ) : null}

      {error ? (
        <p className="auth-error" role="alert">
          {error}
        </p>
      ) : null}
    </article>
  );
}

function ReviewEntry({ interactionId }: { interactionId: string }) {
  const [kind, setKind] = useState<ReviewEligibilityKind | "loading" | "hidden">("loading");

  useEffect(() => {
    let cancelled = false;
    async function loadEligibility() {
      try {
        const eligibility = await getReviewEligibility(interactionId);
        if (!cancelled) {
          setKind(classifyEligibility(eligibility));
        }
      } catch (error) {
        if (!cancelled) {
          if (error instanceof ReviewsClientError && error.code === "unauthenticated") {
            setKind("hidden");
            return;
          }
          setKind("hidden");
        }
      }
    }
    void loadEligibility();
    return () => {
      cancelled = true;
    };
  }, [interactionId]);

  if (kind === "loading" || kind === "hidden") {
    return null;
  }
  if (kind === "already_reviewed") {
    return <p>Değerlendirildi</p>;
  }
  if (kind === "expired") {
    return <p>Değerlendirme süresi doldu</p>;
  }
  return (
    <p>
      <Link href={`/degerlendirme/${encodeURIComponent(interactionId)}`}>Değerlendir</Link>
    </p>
  );
}
