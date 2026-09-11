"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";

import { CaseOpsNav, formatOpsTimestamp } from "@/components/CaseOpsNav";
import {
  ACTION_REASON_CODES,
  ACTION_TYPES,
  createCaseAction,
  fetchCaseActions,
  isActionReasonCode,
  isActionType,
  type ActionLoadResult,
  type StaffAction,
} from "@/lib/moderation-actions";
import { fetchModerationCase, type CaseDetailLoadResult } from "@/lib/moderation-cases";

const EMPTY_FORM = {
  actionType: "warning",
  reasonCode: "policy_violation",
  rationale: "",
};

export function CaseActions({ caseId }: { caseId: string }) {
  const [caseResult, setCaseResult] = useState<CaseDetailLoadResult | null>(null);
  const [result, setResult] = useState<ActionLoadResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [form, setForm] = useState(EMPTY_FORM);
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setActionError(null);

    void Promise.all([fetchModerationCase(caseId, controller.signal), fetchCaseActions(caseId, controller.signal)])
      .then(([nextCase, nextActions]) => {
        if (controller.signal.aborted) {
          return;
        }
        setCaseResult(nextCase);
        setResult(nextActions);
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === "AbortError") {
          return;
        }
        if (!controller.signal.aborted) {
          const failed = {
            ok: false as const,
            kind: "error" as const,
            status: 0,
            message: "Aksiyon isteği başarısız oldu.",
          };
          setResult(failed);
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
  }, [caseId]);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || !caseResult?.ok) {
      return;
    }
    if (!isActionType(form.actionType) || !isActionReasonCode(form.reasonCode)) {
      setActionError("Geçerli aksiyon alanları seçin.");
      return;
    }
    setPending(true);
    setActionError(null);
    const created = await createCaseAction(caseId, {
      targetType: caseResult.data.subjectType,
      targetId: caseResult.data.subjectId,
      actionType: form.actionType,
      reasonCode: form.reasonCode,
      rationale: form.rationale,
    });
    if (!created.ok) {
      setPending(false);
      setActionError(created.message);
      return;
    }
    const next = await fetchCaseActions(caseId);
    setPending(false);
    setResult(next);
    if (!next.ok) {
      setActionError(next.message);
      return;
    }
    setForm(EMPTY_FORM);
  }

  const rows: StaffAction[] = result?.ok ? result.data.actions : [];
  const loadError = !loading && result && !result.ok ? result : null;

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href={`/cases/${encodeURIComponent(caseId)}`}>← Vaka</Link>
        </p>
        <h1>Aksiyonlar</h1>
        <p>
          <code>{caseId}</code>
        </p>
        <CaseOpsNav caseId={caseId} />
      </header>

      {loading ? <p className="ops-banner">Aksiyonlar yükleniyor…</p> : null}

      {loadError ? (
        <p
          className={`ops-banner ${loadError.kind === "unauthenticated" || loadError.kind === "forbidden" ? "ops-banner-auth" : "ops-banner-error"}`}
          role="alert"
        >
          {loadError.message}
        </p>
      ) : null}

      {!loading && result?.ok && rows.length === 0 ? (
        <p className="ops-banner">Bu vakada aksiyon yok.</p>
      ) : null}

      {!loading && result?.ok && rows.length > 0 ? (
        <div className="ops-table-wrap">
          <table className="ops-table">
            <thead>
              <tr>
                <th scope="col">Aksiyon</th>
                <th scope="col">Tür</th>
                <th scope="col">Durum</th>
                <th scope="col">Gerekçe</th>
                <th scope="col">Aktör</th>
                <th scope="col">Güncelleme</th>
                <th scope="col">Aç</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.actionId}>
                  <td>
                    <Link
                      href={`/cases/${encodeURIComponent(caseId)}/actions/${encodeURIComponent(row.actionId)}`}
                    >
                      <code>{row.actionId}</code>
                    </Link>
                  </td>
                  <td>{row.actionType}</td>
                  <td>
                    <span className={`status-chip status-${row.status}`}>{row.status}</span>
                  </td>
                  <td>{row.reasonCode}</td>
                  <td>{row.actorStaffId ? <code>{row.actorStaffId}</code> : "—"}</td>
                  <td>{formatOpsTimestamp(row.updatedAt)}</td>
                  <td>
                    <Link
                      href={`/cases/${encodeURIComponent(caseId)}/actions/${encodeURIComponent(row.actionId)}`}
                    >
                      Aç
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {!loading && caseResult && !caseResult.ok ? (
        <p
          className={`ops-banner ${caseResult.kind === "unauthenticated" || caseResult.kind === "forbidden" ? "ops-banner-auth" : "ops-banner-error"}`}
          role="alert"
        >
          {caseResult.message}
        </p>
      ) : null}

      {result?.ok && caseResult?.ok ? (
        <form className="ops-action" onSubmit={onSubmit}>
          <h2>Aksiyon öner</h2>
          <p>
            Yeni aksiyon <code>proposed</code> olarak oluşur. Hedef, vaka konusu ile aynı olmalıdır. Yaşam
            döngüsü backend tarafından doğrulanır.
          </p>
          <p>
            Hedef: {caseResult.data.subjectType} / <code>{caseResult.data.subjectId}</code>
          </p>
          <label>
            Tür
            <select
              value={form.actionType}
              disabled={pending}
              onChange={(event) => setForm((prev) => ({ ...prev, actionType: event.target.value }))}
            >
              {ACTION_TYPES.map((type) => (
                <option key={type} value={type}>
                  {type}
                </option>
              ))}
            </select>
          </label>
          <label>
            Gerekçe
            <select
              value={form.reasonCode}
              disabled={pending}
              onChange={(event) => setForm((prev) => ({ ...prev, reasonCode: event.target.value }))}
            >
              {ACTION_REASON_CODES.map((code) => (
                <option key={code} value={code}>
                  {code}
                </option>
              ))}
            </select>
          </label>
          <label>
            Gerekçe notu (isteğe bağlı)
            <textarea
              value={form.rationale}
              disabled={pending}
              rows={3}
              onChange={(event) => setForm((prev) => ({ ...prev, rationale: event.target.value }))}
            />
          </label>
          {actionError ? (
            <p className="ops-banner ops-banner-error" role="alert">
              {actionError}
            </p>
          ) : null}
          <button type="submit" disabled={pending}>
            {pending ? "Oluşturuluyor…" : "Aksiyon öner"}
          </button>
        </form>
      ) : null}
    </main>
  );
}
