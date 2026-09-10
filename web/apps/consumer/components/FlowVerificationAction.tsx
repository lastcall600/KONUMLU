"use client";

import { FormEvent, useEffect, useState } from "react";

import { IssuedChallengePanel } from "@/components/IssuedChallengePanel";
import {
  FLOW_INTERACTION_DELIVERY,
  FLOW_INTERACTION_TRANSACTION,
  VERIFIED_OTP_DIGITS,
  VerifiedClientError,
  digitsOnly,
  finishFlowVerification,
  flowInteractionTypeLabel,
  getVerifiedInteraction,
  listVerificationFlows,
  startDeliveryVerification,
  startTransactionVerification,
  type FlowInteractionType,
  type IssuedChallenge,
  type VerificationFlow,
  type VerificationMethod,
  type VerifiedInteraction,
} from "@/lib/verified";

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const REQUESTER_TOKEN_HINT =
  "Sağlayıcı QR kodunu taradıysanız veya jetonu size uygulama dışında ilettiyse doğrulamayı tamamlamak için ham jetonu girin. Jeton otomatik gelmez.";
const REQUESTER_OTP_HINT =
  "Sağlayıcı size 6 haneli kodu uygulama dışında ilettiyse doğrulamayı tamamlamak için kodu girin. Kod otomatik gelmez.";
const FLOW_ID_HINT =
  "Akış kimliği sır değildir. Karşı tarafın doğrulamayı tamamlaması için kod veya jetonla birlikte uygulama dışında iletin.";

function pickActiveFlow(flows: VerificationFlow[]): VerificationFlow | null {
  return flows.find((flow) => flow.status === "open") ?? null;
}

function pickCompletedFlow(flows: VerificationFlow[]): VerificationFlow | null {
  return flows.find((flow) => flow.status === "completed") ?? null;
}

export function FlowVerificationAction({
  listingId,
  requesterUserId,
  isListingOwner,
}: {
  listingId: string;
  requesterUserId: string;
  isListingOwner: boolean;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [copiedFlow, setCopiedFlow] = useState(false);
  const [kind, setKind] = useState<FlowInteractionType>(FLOW_INTERACTION_TRANSACTION);
  const [method, setMethod] = useState<VerificationMethod>("qr");
  const [issued, setIssued] = useState<IssuedChallenge | null>(null);
  const [finishFlowId, setFinishFlowId] = useState("");
  const [finishToken, setFinishToken] = useState("");
  const [finishMethod, setFinishMethod] = useState<VerificationMethod>("otp");
  const [completed, setCompleted] = useState<VerifiedInteraction | null>(null);
  const [completedFromFlow, setCompletedFromFlow] = useState<VerificationFlow | null>(null);
  const [discoveredFlowId, setDiscoveredFlowId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function loadFlows() {
      try {
        const flows = await listVerificationFlows(listingId);
        if (cancelled) {
          return;
        }
        const active = pickActiveFlow(flows);
        const done = pickCompletedFlow(flows);
        if (active) {
          setDiscoveredFlowId(active.flowId);
          setFinishFlowId(active.flowId);
        } else {
          setDiscoveredFlowId(null);
        }
        if (done) {
          setCompletedFromFlow(done);
          const interactionId = done.completedInteractionId;
          if (interactionId) {
            try {
              const row = await getVerifiedInteraction(interactionId);
              if (!cancelled) {
                setCompleted(row);
              }
            } catch {
              if (!cancelled) {
                setCompleted(null);
              }
            }
          }
        }
      } catch {
        if (!cancelled) {
          setDiscoveredFlowId(null);
        }
      }
    }
    void loadFlows();
    return () => {
      cancelled = true;
    };
  }, [listingId]);

  async function onStart() {
    setBusy(true);
    setError(null);
    setCopied(false);
    setCopiedFlow(false);
    try {
      const challenge =
        kind === FLOW_INTERACTION_DELIVERY
          ? await startDeliveryVerification(listingId, requesterUserId, method)
          : await startTransactionVerification(listingId, requesterUserId, method);
      setIssued(challenge);
      if (challenge.flowId) {
        setFinishFlowId(challenge.flowId);
      }
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

  async function onCopySecret() {
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

  async function onCopyFlowId() {
    const flowId = issued?.flowId;
    if (!flowId) {
      return;
    }
    try {
      await navigator.clipboard.writeText(flowId);
      setCopiedFlow(true);
    } catch {
      setCopiedFlow(false);
      setError("Akış kimliği kopyalanamadı. Metni elle seçin.");
    }
  }

  async function onFinish(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const secret = finishMethod === "otp" ? digitsOnly(finishToken) : finishToken.trim();
      const flowId = (discoveredFlowId ?? finishFlowId).trim();
      const row = await finishFlowVerification(flowId, secret);
      setFinishToken("");
      setCompleted(row);
      setCompletedFromFlow(null);
      setDiscoveredFlowId(null);
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

  const startTypeLabel = flowInteractionTypeLabel(kind);
  const issuedTypeLabel = issued?.interactionType
    ? flowInteractionTypeLabel(issued.interactionType)
    : startTypeLabel;
  const completedLabel = completed
    ? flowInteractionTypeLabel(completed.interactionType)
    : completedFromFlow
      ? flowInteractionTypeLabel(completedFromFlow.interactionType)
      : "";
  const completedId = completed?.interactionId ?? completedFromFlow?.completedInteractionId ?? "";
  const knownFinishFlowId = discoveredFlowId ?? issued?.flowId ?? "";
  const finishUsesDiscoveredId = knownFinishFlowId !== "";

  return (
    <section className="listing-card-meta" aria-labelledby="flow-verification-heading">
      <h2 id="flow-verification-heading" className="auth-title">
        Konumlu doğrulama
      </h2>
      {completed || completedFromFlow ? (
        <p className="auth-success">
          ✓ {completedLabel} tamamlandı.
          {completedId ? (
            <>
              {" "}
              Etkileşim kimliği: <span className="verified-qr-token">{completedId}</span>
            </>
          ) : null}
        </p>
      ) : null}

      {isListingOwner ? (
        <div>
          <p>
            <label>
              Doğrulama türü{" "}
              <select
                value={kind}
                disabled={busy}
                onChange={(event) => {
                  const next = event.target.value;
                  if (next === FLOW_INTERACTION_TRANSACTION || next === FLOW_INTERACTION_DELIVERY) {
                    setKind(next);
                  }
                }}
              >
                <option value={FLOW_INTERACTION_TRANSACTION}>İşlem Doğrulaması</option>
                <option value={FLOW_INTERACTION_DELIVERY}>Teslimat Doğrulaması</option>
              </select>
            </label>
          </p>
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
              {startTypeLabel} başlat
            </button>
          </p>
          {!issued && discoveredFlowId ? (
            <p>
              Bu ilan için açık akış: <span className="verified-qr-token">{discoveredFlowId}</span>
              . Kod veya jeton yalnızca başlatma anında gösterilir.
            </p>
          ) : null}
          {issued ? (
            <div>
              <p>
                Tür: <strong>{issuedTypeLabel}</strong>
              </p>
              {issued.flowId ? (
                <p>
                  Akış kimliği: <span className="verified-qr-token">{issued.flowId}</span>{" "}
                  <button type="button" onClick={() => void onCopyFlowId()}>
                    Akış kimliğini kopyala
                  </button>
                </p>
              ) : null}
              <p>{FLOW_ID_HINT}</p>
              {copiedFlow ? <p>Akış kimliği kopyalandı.</p> : null}
              <IssuedChallengePanel issued={issued} copied={copied} onCopy={() => void onCopySecret()} />
            </div>
          ) : null}
        </div>
      ) : (
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
          {finishUsesDiscoveredId ? (
            <p>
              Aktif akış: <span className="verified-qr-token">{knownFinishFlowId}</span>
            </p>
          ) : (
            <label>
              Akış kimliği
              <input
                type="text"
                name="flowId"
                autoComplete="off"
                value={finishFlowId}
                onChange={(event) => setFinishFlowId(event.target.value)}
                disabled={busy}
                required
              />
            </label>
          )}
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
      )}

      {error ? (
        <p className="auth-error" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  );
}
