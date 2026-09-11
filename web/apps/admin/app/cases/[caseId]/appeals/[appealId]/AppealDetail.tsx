"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";

import { CaseOpsNav, formatOpsTimestamp } from "@/components/CaseOpsNav";
import {
  appealDecisionNeedsConfirm,
  fetchCaseAppeal,
  isStaffAppealDecision,
  STAFF_APPEAL_DECISIONS,
  transitionCaseAppeal,
  type AppealDetailResult,
  type StaffAppeal,
} from "@/lib/moderation-appeals";

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="ops-field">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

export function AppealDetail({ caseId, appealId }: { caseId: string; appealId: string }) {
  const [result, setResult] = useState<AppealDetailResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [nextStatus, setNextStatus] = useState("under_review");
  const [confirmed, setConfirmed] = useState(false);
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setActionError(null);
    setConfirmed(false);

    void fetchCaseAppeal(caseId, appealId, controller.signal)
      .then((next) => {
        if (controller.signal.aborted) {
          return;
        }
        setResult(next);
        if (next.ok) {
          setNextStatus(next.data.status === "submitted" ? "under_review" : next.data.status);
        }
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === "AbortError") {
          return;
        }
        if (!controller.signal.aborted) {
          setResult({
            ok: false,
            kind: "error",
            status: 0,
            message: "İtiraz isteği başarısız oldu.",
          });
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });

    return () => {
      controller.abort();
    };
  }, [caseId, appealId]);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || !result?.ok) {
      return;
    }
    if (!isStaffAppealDecision(nextStatus)) {
      setActionError("Geçerli bir karar seçin.");
      return;
    }
    if (appealDecisionNeedsConfirm(nextStatus) && !confirmed) {
      setActionError("Bu karar için onay gerekli.");
      return;
    }
    setPending(true);
    setActionError(null);
    const next = await transitionCaseAppeal(caseId, appealId, nextStatus);
    setPending(false);
    if (!next.ok) {
      setActionError(next.message);
      return;
    }
    setResult(next);
    setNextStatus(next.data.status === "submitted" ? "under_review" : next.data.status);
    setConfirmed(false);
  }

  const appeal: StaffAppeal | null = result?.ok ? result.data : null;
  const needsConfirm = appealDecisionNeedsConfirm(nextStatus);
  const terminal = appeal
    ? appeal.status === "accepted" || appeal.status === "rejected" || appeal.status === "withdrawn"
    : false;

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href={`/cases/${encodeURIComponent(caseId)}/appeals`}>← İtirazlar</Link>
        </p>
        <h1>İtiraz</h1>
        <p>
          <code>{appealId}</code>
        </p>
        <CaseOpsNav caseId={caseId} />
      </header>

      {loading ? <p className="ops-banner">İtiraz yükleniyor…</p> : null}

      {!loading && result && !result.ok ? (
        <p
          className={`ops-banner ${result.kind === "unauthenticated" || result.kind === "forbidden" ? "ops-banner-auth" : "ops-banner-error"}`}
          role="alert"
        >
          {result.message}
        </p>
      ) : null}

      {!loading && appeal ? (
        <>
          <dl className="ops-fields">
            <Field label="İtiraz">
              <code>{appeal.appealId}</code>
            </Field>
            <Field label="Vaka">
              <Link href={`/cases/${encodeURIComponent(appeal.caseId)}`}>
                <code>{appeal.caseId}</code>
              </Link>
            </Field>
            <Field label="Aksiyon">
              <Link
                href={`/cases/${encodeURIComponent(appeal.caseId)}/actions/${encodeURIComponent(appeal.actionId)}`}
              >
                <code>{appeal.actionId}</code>
              </Link>
            </Field>
            <Field label="Başvuran">
              <code>{appeal.appellantUserId}</code>
            </Field>
            <Field label="Durum">
              <span className={`status-chip status-${appeal.status}`}>{appeal.status}</span>
            </Field>
            <Field label="Metin">{appeal.statement || "—"}</Field>
            <Field label="Gönderim">{formatOpsTimestamp(appeal.createdAt)}</Field>
            <Field label="Güncelleme">{formatOpsTimestamp(appeal.updatedAt)}</Field>
            <Field label="Karar zamanı">
              {appeal.decidedAt ? formatOpsTimestamp(appeal.decidedAt) : "—"}
            </Field>
            <Field label="Karar veren">
              {appeal.decidedByStaffId ? <code>{appeal.decidedByStaffId}</code> : "—"}
            </Field>
          </dl>

          <p className="ops-banner">
            Kabul, aksiyon kaydını otomatik olarak geri almaz. Aksiyon <code>executed</code> kalır.
            listing/public_profile restrict/remove için backend restoration uygulayabilir; no_action ve
            warning restoration yapmaz.
          </p>

          {terminal ? null : (
            <form className="ops-action" onSubmit={onSubmit}>
              <h2>Personel kararı</h2>
              <p>
                Personel geçişleri: submitted → under_review; under_review → accepted veya rejected. Yetki:{" "}
                <code>moderation.appeal.review</code>.
              </p>
              <label>
                Karar
                <select
                  value={nextStatus}
                  disabled={pending}
                  onChange={(event) => {
                    setNextStatus(event.target.value);
                    setConfirmed(false);
                  }}
                >
                  {STAFF_APPEAL_DECISIONS.map((status) => (
                    <option key={status} value={status}>
                      {status}
                    </option>
                  ))}
                </select>
              </label>
              {needsConfirm ? (
                <label className="ops-confirm">
                  <input
                    type="checkbox"
                    checked={confirmed}
                    disabled={pending}
                    onChange={(event) => setConfirmed(event.target.checked)}
                  />
                  {nextStatus === "accepted" ? "Kabul kararını onaylıyorum." : "Red kararını onaylıyorum."}
                </label>
              ) : null}
              {actionError ? (
                <p className="ops-banner ops-banner-error" role="alert">
                  {actionError}
                </p>
              ) : null}
              <button type="submit" disabled={pending}>
                {pending ? "Gönderiliyor…" : "Kararı gönder"}
              </button>
            </form>
          )}
        </>
      ) : null}
    </main>
  );
}
