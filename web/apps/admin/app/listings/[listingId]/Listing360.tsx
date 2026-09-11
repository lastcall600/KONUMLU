"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";

import {
  fetchListingAccuracy,
  fetchStaffListing,
  type OpsLoadResult,
  type StaffListing,
  type StaffListingAccuracy,
} from "@/lib/listings-360";
import { fetchCasesForSubject, type StaffCaseSummary } from "@/lib/moderation-cases";
import { fetchReportsForTarget, type StaffReport } from "@/lib/moderation-queue";

function formatTimestamp(value: string | undefined): string {
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

function bannerClass(kind: string): string {
  return kind === "error" || kind === "bad_request" || kind === "conflict"
    ? "ops-banner ops-banner-error"
    : "ops-banner ops-banner-auth";
}

export function Listing360({ listingId }: { listingId: string }) {
  const [listing, setListing] = useState<OpsLoadResult<StaffListing> | null>(null);
  const [reviews, setReviews] = useState<OpsLoadResult<StaffListingAccuracy> | null>(null);
  const [reports, setReports] = useState<OpsLoadResult<{ reports: StaffReport[] }> | null>(null);
  const [cases, setCases] = useState<OpsLoadResult<{ cases: StaffCaseSummary[] }> | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setListing(null);
    setReviews(null);
    setReports(null);
    setCases(null);

    void (async () => {
      const nextListing = await fetchStaffListing(listingId, controller.signal);
      if (controller.signal.aborted) {
        return;
      }
      setListing(nextListing);
      setLoading(false);
      if (!nextListing.ok) {
        return;
      }
      const id = nextListing.data.listingId;
      const [nextReviews, nextReports, nextCases] = await Promise.all([
        fetchListingAccuracy(id, controller.signal),
        fetchReportsForTarget("listing", id, controller.signal),
        fetchCasesForSubject("listing", id, controller.signal),
      ]);
      if (controller.signal.aborted) {
        return;
      }
      setReviews(nextReviews);
      if (nextReports.ok) {
        setReports({ ok: true, data: { reports: nextReports.data.reports } });
      } else {
        setReports({
          ok: false,
          kind: nextReports.kind,
          status: nextReports.status,
          message: nextReports.message,
        });
      }
      if (nextCases.ok) {
        setCases({ ok: true, data: { cases: nextCases.data.cases } });
      } else {
        setCases({
          ok: false,
          kind: nextCases.kind,
          status: nextCases.status,
          message: nextCases.message,
        });
      }
    })().catch((err: unknown) => {
      if (err instanceof DOMException && err.name === "AbortError") {
        return;
      }
      if (!controller.signal.aborted) {
        setLoading(false);
        setListing({
          ok: false,
          kind: "error",
          status: 0,
          message: "İlan isteği başarısız oldu.",
        });
      }
    });

    return () => {
      controller.abort();
    };
  }, [listingId]);

  const data = listing?.ok ? listing.data : null;

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href="/listings">← Listings</Link>
        </p>
        <h1>Listing 360</h1>
        <p>
          <code>{listingId}</code>
        </p>
      </header>

      {loading ? <p className="ops-banner">İlan yükleniyor…</p> : null}

      {!loading && listing && !listing.ok ? (
        <p className={bannerClass(listing.kind)} role="alert">
          {listing.message}
        </p>
      ) : null}

      {!loading && data ? (
        <>
          <section className="ops-section">
            <h2>Listing</h2>
            <dl className="ops-fields">
              <Field label="İlan">
                <code>{data.listingId}</code>
              </Field>
              <Field label="Başlık">{data.title || "—"}</Field>
              <Field label="Kategori">
                <code>{data.categoryId}</code> / v{data.categorySchemaVersion}
              </Field>
              <Field label="Durum">
                <span className={`status-chip status-${data.status}`}>{data.status}</span>
              </Field>
              <Field label="Denetim">
                <span className={`status-chip status-${data.moderationState}`}>{data.moderationState}</span>
              </Field>
              <Field label="Oluşturma">{formatTimestamp(data.createdAt)}</Field>
              <Field label="Güncelleme">{formatTimestamp(data.updatedAt)}</Field>
              <Field label="Yayın">{formatTimestamp(data.publishedAt)}</Field>
              <Field label="Sahip">
                {data.owner ? (
                  <Link href={`/users/${encodeURIComponent(data.owner.publicProfileId)}`}>
                    <code>{data.owner.publicProfileId}</code>
                    {data.owner.displayName ? ` · ${data.owner.displayName}` : ""}
                  </Link>
                ) : (
                  "—"
                )}
              </Field>
            </dl>
          </section>

          <section className="ops-section">
            <h2>Location</h2>
            {data.location ? (
              <dl className="ops-fields">
                <Field label="Enlem">{data.location.latitude}</Field>
                <Field label="Boylam">{data.location.longitude}</Field>
                <Field label="Katalog">
                  {data.location.catalogLocationId ? (
                    <code>{data.location.catalogLocationId}</code>
                  ) : (
                    "—"
                  )}
                </Field>
              </dl>
            ) : (
              <p className="ops-banner">Konum kaydı yok.</p>
            )}
          </section>

          <section className="ops-section">
            <h2>Media</h2>
            {data.media.length === 0 ? (
              <p className="ops-banner">Görünen medya metadata yok.</p>
            ) : (
              <div className="ops-table-wrap">
                <table className="ops-table">
                  <thead>
                    <tr>
                      <th>Asset</th>
                      <th>Sıra</th>
                      <th>Boyut</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.media.map((item) => (
                      <tr key={item.assetId}>
                        <td>
                          <code>{item.assetId}</code>
                        </td>
                        <td>{item.order}</td>
                        <td>
                          {item.width && item.height ? `${item.width}×${item.height}` : "—"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section className="ops-section">
            <h2>Reviews / Trust</h2>
            {!reviews ? <p className="ops-banner">Değerlendirme özeti yükleniyor…</p> : null}
            {reviews && !reviews.ok ? (
              <p className={bannerClass(reviews.kind)} role="alert">
                {reviews.message}
              </p>
            ) : null}
            {reviews?.ok ? (
              <dl className="ops-fields">
                <Field label="İlan doğruluğu sayısı">{reviews.data.reviewCount}</Field>
                <Field label="İlan doğruluğu ortalaması">{reviews.data.average ?? "—"}</Field>
              </dl>
            ) : null}
          </section>

          <section className="ops-section">
            <h2>Moderation</h2>
            {!reports || !cases ? <p className="ops-banner">Moderasyon yükleniyor…</p> : null}
            {reports && !reports.ok ? (
              <p className={bannerClass(reports.kind)} role="alert">
                {reports.message}
              </p>
            ) : null}
            {cases && !cases.ok ? (
              <p className={bannerClass(cases.kind)} role="alert">
                {cases.message}
              </p>
            ) : null}
            {reports?.ok && reports.data.reports.length === 0 ? (
              <p className="ops-banner">Bu ilana bağlı rapor yok.</p>
            ) : null}
            {reports?.ok && reports.data.reports.length > 0 ? (
              <div className="ops-table-wrap">
                <table className="ops-table">
                  <thead>
                    <tr>
                      <th>Rapor</th>
                      <th>Gerekçe</th>
                      <th>Durum</th>
                    </tr>
                  </thead>
                  <tbody>
                    {reports.data.reports.map((row) => (
                      <tr key={row.reportId}>
                        <td>
                          <Link href={`/moderation/${encodeURIComponent(row.reportId)}`}>
                            <code>{row.reportId}</code>
                          </Link>
                        </td>
                        <td>{row.reasonCode}</td>
                        <td>
                          <span className={`status-chip status-${row.status}`}>{row.status}</span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
            {cases?.ok && cases.data.cases.length === 0 ? (
              <p className="ops-banner">Bu ilana bağlı vaka yok.</p>
            ) : null}
            {cases?.ok && cases.data.cases.length > 0 ? (
              <div className="ops-table-wrap">
                <table className="ops-table">
                  <thead>
                    <tr>
                      <th>Vaka</th>
                      <th>Başlık</th>
                      <th>Durum</th>
                    </tr>
                  </thead>
                  <tbody>
                    {cases.data.cases.map((row) => (
                      <tr key={row.caseId}>
                        <td>
                          <Link href={`/cases/${encodeURIComponent(row.caseId)}`}>
                            <code>{row.caseId}</code>
                          </Link>
                        </td>
                        <td>{row.title || "—"}</td>
                        <td>
                          <span className={`status-chip status-${row.status}`}>{row.status}</span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
          </section>
        </>
      ) : null}
    </main>
  );
}
