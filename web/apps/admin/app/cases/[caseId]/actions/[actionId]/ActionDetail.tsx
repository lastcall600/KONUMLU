"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";

import { CaseOpsNav, formatOpsTimestamp } from "@/components/CaseOpsNav";
import {
  ACTION_STATUSES,
  actionTransitionNeedsConfirm,
  fetchCaseAction,
  isActionStatus,
  transitionCaseAction,
  type ActionDetailResult,
  type StaffAction,
} from "@/lib/moderation-actions";
import { fetchCaseAppeals, type StaffAppeal } from "@/lib/moderation-appeals";
import { TargetLink } from "@/components/TargetLink";

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="ops-field">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

export function ActionDetail({ caseId, actionId }: { caseId: string; actionId: string }) {
  const [result, setResult] = useState<ActionDetailResult | null>(null);
  const [appeals, setAppeals] = useState<StaffAppeal[] | null>(null);
  const [appealsError, setAppealsError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [nextStatus, setNextStatus] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setActionError(null);
    setConfirmed(false);
    setAppeals(null);
    setAppealsError(null);

    void fetchCaseAction(caseId, actionId, controller.signal)
      .then((next) => {
        if (controller.signal.aborted) {
          return;
        }
        setResult(next);
        if (next.ok) {
          setNextStatus(next.data.status);
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
            message: "Aksiyon isteği başarısız oldu.",
          });
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });

    void fetchCaseAppeals(caseId, controller.signal)
      .then((next) => {
        if (controller.signal.aborted) {
          return;
        }
        if (next.ok) {
          setAppeals(next.data.appeals.filter((row) => row.actionId === actionId));
          setAppealsError(null);
        } else {
          setAppeals(null);
          setAppealsError(next.message);
        }
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === "AbortError") {
          return;
        }
        if (!controller.signal.aborted) {
          setAppealsError("İtiraz listesi yüklenemedi.");
        }
      });

    return () => {
      controller.abort();
    };
  }, [caseId, actionId]);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || !result?.ok) {
      return;
    }
    if (!isActionStatus(nextStatus)) {
      setActionError("Geçerli bir durum seçin.");
      return;
    }
    if (actionTransitionNeedsConfirm(nextStatus) && !confirmed) {
      setActionError("Bu geçiş için onay gerekli.");
      return;
    }
    setPending(true);
    setActionError(null);
    const next = await transitionCaseAction(caseId, actionId, nextStatus);
    setPending(false);
    if (!next.ok) {
      setActionError(next.message);
      return;
    }
    setResult(next);
    setNextStatus(next.data.status);
    setConfirmed(false);
  }

  const action: StaffAction | null = result?.ok ? result.data : null;
  const needsConfirm = actionTransitionNeedsConfirm(nextStatus);

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href={`/cases/${encodeURIComponent(caseId)}/actions`}>← Aksiyonlar</Link>
        </p>
        <h1>Aksiyon</h1>
        <p>
          <code>{actionId}</code>
        </p>
        <CaseOpsNav caseId={caseId} />
      </header>

      {loading ? <p className="ops-banner">Aksiyon yükleniyor…</p> : null}

      {!loading && result && !result.ok ? (
        <p
          className={`ops-banner ${result.kind === "unauthenticated" || result.kind === "forbidden" ? "ops-banner-auth" : "ops-banner-error"}`}
          role="alert"
        >
          {result.message}
        </p>
      ) : null}

      {!loading && action ? (
        <>
          <dl className="ops-fields">
            <Field label="Aksiyon">
              <code>{action.actionId}</code>
            </Field>
            <Field label="Vaka">
              <Link href={`/cases/${encodeURIComponent(action.caseId)}`}>
                <code>{action.caseId}</code>
              </Link>
            </Field>
            <Field label="Hedef türü">{action.targetType}</Field>
            <Field label="Hedef">
              <TargetLink type={action.targetType} id={action.targetId} />
            </Field>
            <Field label="Tür">{action.actionType}</Field>
            <Field label="Durum">
              <span className={`status-chip status-${action.status}`}>{action.status}</span>
            </Field>
            <Field label="Gerekçe">{action.reasonCode}</Field>
            <Field label="Not">{action.rationale ?? "—"}</Field>
            <Field label="Aktör">
              {action.actorStaffId ? <code>{action.actorStaffId}</code> : "—"}
            </Field>
            <Field label="Oluşturma">{formatOpsTimestamp(action.createdAt)}</Field>
            <Field label="Güncelleme">{formatOpsTimestamp(action.updatedAt)}</Field>
          </dl>

          <section className="ops-section">
            <h2>Bağlı itirazlar</h2>
            {appealsError ? (
              <p className="ops-banner ops-banner-error" role="alert">
                {appealsError}
              </p>
            ) : null}
            {appeals && appeals.length === 0 ? (
              <p className="ops-banner">Bu aksiyona bağlı itiraz yok.</p>
            ) : null}
            {appeals && appeals.length > 0 ? (
              <ul className="ops-id-list">
                {appeals.map((row) => (
                  <li key={row.appealId}>
                    <Link
                      href={`/cases/${encodeURIComponent(caseId)}/appeals/${encodeURIComponent(row.appealId)}`}
                    >
                      <code>{row.appealId}</code>
                    </Link>{" "}
                    <span className={`status-chip status-${row.status}`}>{row.status}</span>
                  </li>
                ))}
              </ul>
            ) : null}
          </section>

          <form className="ops-action" onSubmit={onSubmit}>
            <h2>Durum geçişi</h2>
            <p>
              İzin verilen geçişler backend tarafından doğrulanır. Onay için{" "}
              <code>moderation.action.approve</code> gerekir. Yürütme semantiği sunucudadır.
            </p>
            <label>
              Yeni durum
              <select
                value={nextStatus}
                disabled={pending}
                onChange={(event) => {
                  setNextStatus(event.target.value);
                  setConfirmed(false);
                }}
              >
                {ACTION_STATUSES.map((status) => (
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
                {nextStatus === "executed" ? "Yürütmeyi onaylıyorum." : "İptali onaylıyorum."}
              </label>
            ) : null}
            {actionError ? (
              <p className="ops-banner ops-banner-error" role="alert">
                {actionError}
              </p>
            ) : null}
            <button type="submit" disabled={pending}>
              {pending ? "Gönderiliyor…" : "Durumu güncelle"}
            </button>
          </form>
        </>
      ) : null}
    </main>
  );
}
