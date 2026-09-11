"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { CaseOpsNav, formatOpsTimestamp } from "@/components/CaseOpsNav";
import { fetchCaseAppeals, type AppealLoadResult, type StaffAppeal } from "@/lib/moderation-appeals";

export function CaseAppeals({ caseId }: { caseId: string }) {
  const [result, setResult] = useState<AppealLoadResult | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);

    void fetchCaseAppeals(caseId, controller.signal)
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
  }, [caseId]);

  const rows: StaffAppeal[] = result?.ok ? result.data.appeals : [];

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href={`/cases/${encodeURIComponent(caseId)}`}>← Vaka</Link>
        </p>
        <h1>İtirazlar</h1>
        <p>
          <code>{caseId}</code>
        </p>
        <CaseOpsNav caseId={caseId} />
      </header>

      {loading ? <p className="ops-banner">İtirazlar yükleniyor…</p> : null}

      {!loading && result && !result.ok ? (
        <p
          className={`ops-banner ${result.kind === "unauthenticated" || result.kind === "forbidden" ? "ops-banner-auth" : "ops-banner-error"}`}
          role="alert"
        >
          {result.message}
        </p>
      ) : null}

      {!loading && result?.ok && rows.length === 0 ? (
        <p className="ops-banner">Bu vakada itiraz yok.</p>
      ) : null}

      {!loading && result?.ok && rows.length > 0 ? (
        <div className="ops-table-wrap">
          <table className="ops-table">
            <thead>
              <tr>
                <th scope="col">İtiraz</th>
                <th scope="col">Aksiyon</th>
                <th scope="col">Durum</th>
                <th scope="col">Gönderim</th>
                <th scope="col">Karar</th>
                <th scope="col">Aç</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.appealId}>
                  <td>
                    <Link
                      href={`/cases/${encodeURIComponent(caseId)}/appeals/${encodeURIComponent(row.appealId)}`}
                    >
                      <code>{row.appealId}</code>
                    </Link>
                  </td>
                  <td>
                    <Link
                      href={`/cases/${encodeURIComponent(caseId)}/actions/${encodeURIComponent(row.actionId)}`}
                    >
                      <code>{row.actionId}</code>
                    </Link>
                  </td>
                  <td>
                    <span className={`status-chip status-${row.status}`}>{row.status}</span>
                  </td>
                  <td>{formatOpsTimestamp(row.createdAt)}</td>
                  <td>{row.decidedAt ? formatOpsTimestamp(row.decidedAt) : "—"}</td>
                  <td>
                    <Link
                      href={`/cases/${encodeURIComponent(caseId)}/appeals/${encodeURIComponent(row.appealId)}`}
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
    </main>
  );
}
