"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";

import { TargetLink } from "@/components/TargetLink";

import {
  fetchModerationReport,
  isReportStatus,
  transitionModerationReport,
  type ReportLoadResult,
} from "@/lib/moderation-report";
import { REPORT_STATUSES, type StaffReport } from "@/lib/moderation-queue";

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

export function ModerationReportDetail({ reportId }: { reportId: string }) {
  const [result, setResult] = useState<ReportLoadResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [nextStatus, setNextStatus] = useState("");
  const [staffNote, setStaffNote] = useState("");
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setActionError(null);

    void fetchModerationReport(reportId, controller.signal)
      .then((next) => {
        if (controller.signal.aborted) {
          return;
        }
        setResult(next);
        if (next.ok) {
          setNextStatus(next.data.status);
          setStaffNote(next.data.staffNote ?? "");
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
            message: "Rapor isteği başarısız oldu.",
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
  }, [reportId]);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || !result?.ok) {
      return;
    }
    if (!isReportStatus(nextStatus)) {
      setActionError("Geçerli bir durum seçin.");
      return;
    }
    setPending(true);
    setActionError(null);
    const next = await transitionModerationReport(reportId, nextStatus, staffNote);
    setPending(false);
    if (!next.ok) {
      setActionError(next.message);
      return;
    }
    setResult(next);
    setNextStatus(next.data.status);
    setStaffNote(next.data.staffNote ?? "");
  }

  const report: StaffReport | null = result?.ok ? result.data : null;

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href="/moderation">← Kuyruk</Link>
        </p>
        <h1>Rapor</h1>
        <p>
          <code>{reportId}</code>
        </p>
      </header>

      {loading ? <p className="ops-banner">Rapor yükleniyor…</p> : null}

      {!loading && result && !result.ok ? (
        <p
          className={`ops-banner ${result.kind === "error" || result.kind === "bad_request" || result.kind === "conflict" ? "ops-banner-error" : "ops-banner-auth"}`}
          role="alert"
        >
          {result.message}
        </p>
      ) : null}

      {!loading && report ? (
        <>
          <dl className="ops-fields">
            <Field label="Rapor">
              <code>{report.reportId}</code>
            </Field>
            <Field label="Hedef türü">{report.targetType}</Field>
            <Field label="Hedef">
              <TargetLink type={report.targetType} id={report.targetId} />
            </Field>
            <Field label="Gerekçe">{report.reasonCode}</Field>
            <Field label="Açıklama">{report.description ?? "—"}</Field>
            <Field label="Durum">
              <span className={`status-chip status-${report.status}`}>{report.status}</span>
            </Field>
            <Field label="Personel notu">{report.staffNote ?? "—"}</Field>
            <Field label="Durumu değiştiren">
              {report.statusChangedBy ? <code>{report.statusChangedBy}</code> : "—"}
            </Field>
            <Field label="Oluşturma">{formatTimestamp(report.createdAt)}</Field>
            <Field label="Güncelleme">{formatTimestamp(report.updatedAt)}</Field>
          </dl>

          <form className="ops-action" onSubmit={onSubmit}>
            <h2>Durum geçişi</h2>
            <p>Geçiş backend sözleşmesine göre doğrulanır. İzin verilen değerler: submitted, triaged, closed.</p>
            <label>
              Yeni durum
              <select
                value={nextStatus}
                disabled={pending}
                onChange={(event) => setNextStatus(event.target.value)}
              >
                {REPORT_STATUSES.map((status) => (
                  <option key={status} value={status}>
                    {status}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Personel notu (isteğe bağlı)
              <textarea
                value={staffNote}
                disabled={pending}
                rows={3}
                onChange={(event) => setStaffNote(event.target.value)}
              />
            </label>
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
