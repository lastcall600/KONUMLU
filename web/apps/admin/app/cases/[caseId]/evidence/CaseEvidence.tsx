"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";

import { CaseOpsNav, formatOpsTimestamp } from "@/components/CaseOpsNav";
import {
  appendCaseEvidence,
  EVIDENCE_TYPES,
  evidenceNeedsReference,
  fetchCaseEvidence,
  type EvidenceLoadResult,
  type StaffEvidence,
} from "@/lib/moderation-evidence";

const EMPTY_FORM = {
  evidenceType: "staff_note",
  title: "",
  description: "",
  referenceValue: "",
};

export function CaseEvidence({ caseId }: { caseId: string }) {
  const [result, setResult] = useState<EvidenceLoadResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [form, setForm] = useState(EMPTY_FORM);
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setActionError(null);

    void fetchCaseEvidence(caseId, controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) {
          setResult(next);
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
            message: "Kanıt isteği başarısız oldu.",
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
  }, [caseId]);

  async function refetch() {
    const next = await fetchCaseEvidence(caseId);
    setResult(next);
    return next;
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) {
      return;
    }
    setPending(true);
    setActionError(null);
    const created = await appendCaseEvidence(caseId, form);
    if (!created.ok) {
      setPending(false);
      setActionError(created.message);
      return;
    }
    const next = await refetch();
    setPending(false);
    if (!next.ok) {
      setActionError(next.message);
      return;
    }
    setForm({ ...EMPTY_FORM, evidenceType: form.evidenceType });
  }

  const rows: StaffEvidence[] = result?.ok ? result.data.evidence : [];
  const needsReference = evidenceNeedsReference(form.evidenceType);

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href={`/cases/${encodeURIComponent(caseId)}`}>← Vaka</Link>
        </p>
        <h1>Kanıt</h1>
        <p>
          <code>{caseId}</code>
        </p>
        <CaseOpsNav caseId={caseId} />
      </header>

      {loading ? <p className="ops-banner">Kanıt yükleniyor…</p> : null}

      {!loading && result && !result.ok ? (
        <p
          className={`ops-banner ${result.kind === "unauthenticated" || result.kind === "forbidden" ? "ops-banner-auth" : "ops-banner-error"}`}
          role="alert"
        >
          {result.message}
        </p>
      ) : null}

      {!loading && result?.ok && rows.length === 0 ? (
        <p className="ops-banner">Bu vakada kanıt yok.</p>
      ) : null}

      {!loading && result?.ok && rows.length > 0 ? (
        <div className="ops-table-wrap">
          <table className="ops-table">
            <thead>
              <tr>
                <th scope="col">Kanıt</th>
                <th scope="col">Tür</th>
                <th scope="col">Başlık</th>
                <th scope="col">Açıklama</th>
                <th scope="col">Referans</th>
                <th scope="col">Aktör</th>
                <th scope="col">Oluşturma</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.evidenceId}>
                  <td>
                    <code>{row.evidenceId}</code>
                  </td>
                  <td>
                    <span className={`status-chip evidence-${row.evidenceType}`}>{row.evidenceType}</span>
                  </td>
                  <td>{row.title || "—"}</td>
                  <td>{row.description ?? "—"}</td>
                  <td>{row.referenceValue ?? "—"}</td>
                  <td>{row.actorStaffId ? <code>{row.actorStaffId}</code> : "—"}</td>
                  <td>{formatOpsTimestamp(row.createdAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {result?.ok ? (
        <form className="ops-action" onSubmit={onSubmit}>
          <h2>Kanıt ekle</h2>
          <p>Kanıt yalnızca eklenir. Düzenleme veya silme yok. Alanlar backend sözleşmesine göredir.</p>
          <label>
            Tür
            <select
              value={form.evidenceType}
              disabled={pending}
              onChange={(event) =>
                setForm((prev) => ({
                  ...prev,
                  evidenceType: event.target.value,
                  referenceValue: evidenceNeedsReference(event.target.value) ? prev.referenceValue : "",
                }))
              }
            >
              {EVIDENCE_TYPES.map((type) => (
                <option key={type} value={type}>
                  {type}
                </option>
              ))}
            </select>
          </label>
          <label>
            Başlık
            <input
              value={form.title}
              disabled={pending}
              onChange={(event) => setForm((prev) => ({ ...prev, title: event.target.value }))}
            />
          </label>
          <label>
            Açıklama (isteğe bağlı)
            <textarea
              value={form.description}
              disabled={pending}
              rows={3}
              onChange={(event) => setForm((prev) => ({ ...prev, description: event.target.value }))}
            />
          </label>
          {needsReference ? (
            <label>
              Referans değeri
              <input
                value={form.referenceValue}
                disabled={pending}
                onChange={(event) => setForm((prev) => ({ ...prev, referenceValue: event.target.value }))}
              />
            </label>
          ) : null}
          {actionError ? (
            <p className="ops-banner ops-banner-error" role="alert">
              {actionError}
            </p>
          ) : null}
          <button type="submit" disabled={pending}>
            {pending ? "Ekleniyor…" : "Kanıt ekle"}
          </button>
        </form>
      ) : null}
    </main>
  );
}
