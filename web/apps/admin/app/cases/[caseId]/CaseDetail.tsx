"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";

import { CaseOpsNav } from "@/components/CaseOpsNav";
import {
  fetchModerationCase,
  type CaseDetailLoadResult,
  type StaffCaseDetail,
  type StaffCaseHistory,
} from "@/lib/moderation-cases";

function formatTimestamp(value: string): string {
  if (!value) {
    return "—";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toISOString().replace("T", " ").replace(/\.\d+Z$/, " UTC");
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="ops-field">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function historyChange(from: string | undefined, to: string | undefined): string {
  if (!from && !to) {
    return "—";
  }
  return `${from ?? "—"} → ${to ?? "—"}`;
}

function HistoryRow({ caseId, row }: { caseId: string; row: StaffCaseHistory }) {
  return (
    <tr>
      <td>{row.kind}</td>
      <td>{formatTimestamp(row.createdAt)}</td>
      <td>{row.actorStaffId ? <code>{row.actorStaffId}</code> : "—"}</td>
      <td>
        {row.reportId ? (
          <Link href={`/moderation/${encodeURIComponent(row.reportId)}`}>
            <code>{row.reportId}</code>
          </Link>
        ) : (
          "—"
        )}
      </td>
      <td>{historyChange(row.fromStatus, row.toStatus)}</td>
      <td>{historyChange(row.fromPriority, row.toPriority)}</td>
      <td>{historyChange(row.fromAssignedStaffId, row.toAssignedStaffId)}</td>
      <td>{row.note ?? "—"}</td>
      <td>
        {row.evidenceId ? (
          <Link href={`/cases/${encodeURIComponent(caseId)}/evidence`}>
            <code>{row.evidenceId}</code>
          </Link>
        ) : (
          "—"
        )}
      </td>
      <td>
        {row.actionId ? (
          <Link href={`/cases/${encodeURIComponent(caseId)}/actions/${encodeURIComponent(row.actionId)}`}>
            <code>{row.actionId}</code>
          </Link>
        ) : (
          "—"
        )}
      </td>
      <td>
        {row.appealId ? (
          <Link href={`/cases/${encodeURIComponent(caseId)}/appeals/${encodeURIComponent(row.appealId)}`}>
            <code>{row.appealId}</code>
          </Link>
        ) : (
          "—"
        )}
      </td>
    </tr>
  );
}

export function CaseDetail({ caseId }: { caseId: string }) {
  const [result, setResult] = useState<CaseDetailLoadResult | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);

    void fetchModerationCase(caseId, controller.signal)
      .then((next) => {
        if (controller.signal.aborted) {
          return;
        }
        setResult(next);
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
            message: "Vaka isteği başarısız oldu.",
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

  const detail: StaffCaseDetail | null = result?.ok ? result.data : null;

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href="/cases">← Kuyruk</Link>
        </p>
        <h1>Vaka</h1>
        <p>
          <code>{caseId}</code>
        </p>
        <CaseOpsNav caseId={caseId} />
      </header>

      {loading ? <p className="ops-banner">Vaka yükleniyor…</p> : null}

      {!loading && result && !result.ok ? (
        <p
          className={`ops-banner ${result.kind === "error" || result.kind === "bad_request" ? "ops-banner-error" : "ops-banner-auth"}`}
          role="alert"
        >
          {result.message}
        </p>
      ) : null}

      {!loading && detail ? (
        <>
          <dl className="ops-fields">
            <Field label="Vaka">
              <code>{detail.caseId}</code>
            </Field>
            <Field label="Durum">
              <span className={`status-chip status-${detail.status}`}>{detail.status}</span>
            </Field>
            <Field label="Öncelik">
              <span className={`status-chip priority-${detail.priority}`}>{detail.priority}</span>
            </Field>
            <Field label="Hedef türü">{detail.subjectType}</Field>
            <Field label="Hedef">
              <code>{detail.subjectId}</code>
            </Field>
            <Field label="Başlık">{detail.title || "—"}</Field>
            <Field label="Atanan">
              {detail.assignedStaffId ? <code>{detail.assignedStaffId}</code> : "—"}
            </Field>
            <Field label="Oluşturma">{formatTimestamp(detail.createdAt)}</Field>
            <Field label="Güncelleme">{formatTimestamp(detail.updatedAt)}</Field>
          </dl>

          <section className="ops-section">
            <h2>Bağlı raporlar</h2>
            {detail.reportIds.length === 0 ? (
              <p className="ops-banner">Bağlı rapor yok.</p>
            ) : (
              <ul className="ops-id-list">
                {detail.reportIds.map((reportId) => (
                  <li key={reportId}>
                    <Link href={`/moderation/${encodeURIComponent(reportId)}`}>
                      <code>{reportId}</code>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="ops-section">
            <h2>Geçmiş</h2>
            {detail.history.length === 0 ? (
              <p className="ops-banner">Geçmiş kaydı yok.</p>
            ) : (
              <div className="ops-table-wrap">
                <table className="ops-table">
                  <thead>
                    <tr>
                      <th scope="col">Tür</th>
                      <th scope="col">Zaman</th>
                      <th scope="col">Aktör</th>
                      <th scope="col">Rapor</th>
                      <th scope="col">Durum</th>
                      <th scope="col">Öncelik</th>
                      <th scope="col">Atama</th>
                      <th scope="col">Not</th>
                      <th scope="col">Kanıt</th>
                      <th scope="col">Aksiyon</th>
                      <th scope="col">İtiraz</th>
                    </tr>
                  </thead>
                  <tbody>
                    {detail.history.map((row, index) => (
                      <HistoryRow
                        key={`${row.kind}-${row.createdAt}-${index}`}
                        caseId={detail.caseId}
                        row={row}
                      />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </>
      ) : null}
    </main>
  );
}
