"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";

import { fetchCasesForSubject, type StaffCaseSummary } from "@/lib/moderation-cases";
import { fetchReportsForTarget, type StaffReport } from "@/lib/moderation-queue";
import {
  fetchOwnedListings,
  fetchStaffProfile,
  fetchStaffTrust,
  type OpsLoadResult,
  type StaffOwnedListing,
  type StaffProfile,
  type StaffTrust,
} from "@/lib/users-360";

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

function accountState(profile: StaffProfile): string {
  if (profile.deleted) {
    return "deleted";
  }
  if (profile.disabled) {
    return "disabled";
  }
  if (profile.accountEligible) {
    return "eligible";
  }
  return "ineligible";
}

export function User360({ userId }: { userId: string }) {
  const [profile, setProfile] = useState<OpsLoadResult<StaffProfile> | null>(null);
  const [trust, setTrust] = useState<OpsLoadResult<StaffTrust> | null>(null);
  const [listings, setListings] = useState<OpsLoadResult<StaffOwnedListing[]> | null>(null);
  const [reports, setReports] = useState<OpsLoadResult<{ reports: StaffReport[] }> | null>(null);
  const [cases, setCases] = useState<OpsLoadResult<{ cases: StaffCaseSummary[] }> | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setProfile(null);
    setTrust(null);
    setListings(null);
    setReports(null);
    setCases(null);

    void (async () => {
      const nextProfile = await fetchStaffProfile(userId, controller.signal);
      if (controller.signal.aborted) {
        return;
      }
      setProfile(nextProfile);
      setLoading(false);
      if (!nextProfile.ok) {
        return;
      }
      const publicProfileId = nextProfile.data.publicProfileId;
      const [nextTrust, nextListings, nextReports, nextCases] = await Promise.all([
        fetchStaffTrust(publicProfileId, controller.signal),
        fetchOwnedListings(publicProfileId, controller.signal),
        fetchReportsForTarget("public_profile", publicProfileId, controller.signal),
        fetchCasesForSubject("public_profile", publicProfileId, controller.signal),
      ]);
      if (controller.signal.aborted) {
        return;
      }
      setTrust(nextTrust);
      setListings(nextListings);
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
        setProfile({
          ok: false,
          kind: "error",
          status: 0,
          message: "Kullanıcı isteği başarısız oldu.",
        });
      }
    });

    return () => {
      controller.abort();
    };
  }, [userId]);

  const profileData = profile?.ok ? profile.data : null;

  return (
    <main className="ops-page">
      <header className="ops-header">
        <p className="ops-back">
          <Link href="/users">← Users</Link>
        </p>
        <h1>User 360</h1>
        <p>
          <code>{userId}</code>
        </p>
      </header>

      {loading ? <p className="ops-banner">Kullanıcı yükleniyor…</p> : null}

      {!loading && profile && !profile.ok ? (
        <p className={bannerClass(profile.kind)} role="alert">
          {profile.message}
        </p>
      ) : null}

      {!loading && profileData ? (
        <>
          <section className="ops-section">
            <h2>Identity / Profile</h2>
            <dl className="ops-fields">
              <Field label="Public profile">
                <code>{profileData.publicProfileId}</code>
              </Field>
              <Field label="Görünen ad">{profileData.displayName ?? "—"}</Field>
              <Field label="Hesap durumu">
                <span className={`status-chip status-${accountState(profileData)}`}>
                  {accountState(profileData)}
                </span>
              </Field>
              <Field label="Profil denetimi">
                <span className={`status-chip status-${profileData.moderationState}`}>
                  {profileData.moderationState}
                </span>
              </Field>
              <Field label="Üyelik">{formatTimestamp(profileData.memberSince)}</Field>
              <Field label="Oluşturma">{formatTimestamp(profileData.createdAt)}</Field>
              <Field label="Güncelleme">{formatTimestamp(profileData.updatedAt)}</Field>
            </dl>
          </section>

          <section className="ops-section">
            <h2>Trust / Güven Pasaportu</h2>
            {!trust ? <p className="ops-banner">Güven Pasaportu yükleniyor…</p> : null}
            {trust && !trust.ok ? (
              <p className={bannerClass(trust.kind)} role="alert">
                {trust.message}
              </p>
            ) : null}
            {trust?.ok ? (
              <>
                <dl className="ops-fields">
                  <Field label="Seviye">
                    <span className={`status-chip status-${trust.data.level}`}>{trust.data.level}</span>
                  </Field>
                  <Field label="Doğrulanmış etkileşim">{trust.data.verifiedInteractionCount}</Field>
                  <Field label="Talep eden">{trust.data.requesterVerifiedInteractionCount}</Field>
                  <Field label="Sağlayıcı">{trust.data.providerVerifiedInteractionCount}</Field>
                  <Field label="Son etkileşim">
                    {formatTimestamp(trust.data.lastVerifiedInteractionAt)}
                  </Field>
                  <Field label="Doğrulanmış değerlendirme">{trust.data.verifiedReviewCount}</Field>
                  <Field label="Sağlayıcı hizmet değerlendirmesi">
                    {trust.data.providerServiceReviewCount}
                  </Field>
                  <Field label="Sağlayıcı hizmet ortalaması">
                    {trust.data.providerServiceAverage ?? "—"}
                  </Field>
                  <Field label="Son değerlendirme">
                    {formatTimestamp(trust.data.lastVerifiedReviewAt)}
                  </Field>
                </dl>
                <p className="ops-header">
                  Seviye yalnızca listing_inspection sayımından türetilir. Gizli skor yoktur.
                </p>
                {trust.data.history.length === 0 ? (
                  <p className="ops-banner">Doğrulama geçmişi yok.</p>
                ) : (
                  <div className="ops-table-wrap">
                    <table className="ops-table">
                      <thead>
                        <tr>
                          <th>Tür</th>
                          <th>Rol</th>
                          <th>Yöntem</th>
                          <th>İlan</th>
                          <th>Zaman</th>
                        </tr>
                      </thead>
                      <tbody>
                        {trust.data.history.map((row) => (
                          <tr key={row.interactionId}>
                            <td>{row.interactionType}</td>
                            <td>{row.role}</td>
                            <td>{row.verificationMethod}</td>
                            <td>
                              <Link href={`/listings/${encodeURIComponent(row.listingId)}`}>
                                <code>{row.listingId}</code>
                              </Link>
                            </td>
                            <td>{formatTimestamp(row.verifiedAt)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </>
            ) : null}
          </section>

          <section className="ops-section">
            <h2>Activity / Listings</h2>
            {!listings ? <p className="ops-banner">İlanlar yükleniyor…</p> : null}
            {listings && !listings.ok ? (
              <p className={bannerClass(listings.kind)} role="alert">
                {listings.message}
              </p>
            ) : null}
            {listings?.ok && listings.data.length === 0 ? (
              <p className="ops-banner">Bu kullanıcıya ait ilan yok.</p>
            ) : null}
            {listings?.ok && listings.data.length > 0 ? (
              <div className="ops-table-wrap">
                <table className="ops-table">
                  <thead>
                    <tr>
                      <th>İlan</th>
                      <th>Başlık</th>
                      <th>Durum</th>
                      <th>Denetim</th>
                      <th>Güncelleme</th>
                    </tr>
                  </thead>
                  <tbody>
                    {listings.data.map((row) => (
                      <tr key={row.listingId}>
                        <td>
                          <Link href={`/listings/${encodeURIComponent(row.listingId)}`}>
                            <code>{row.listingId}</code>
                          </Link>
                        </td>
                        <td>{row.title}</td>
                        <td>
                          <span className={`status-chip status-${row.status}`}>{row.status}</span>
                        </td>
                        <td>
                          <span className={`status-chip status-${row.moderationState}`}>
                            {row.moderationState}
                          </span>
                        </td>
                        <td>{formatTimestamp(row.updatedAt)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
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
              <p className="ops-banner">Bu profile bağlı rapor yok.</p>
            ) : null}
            {reports?.ok && reports.data.reports.length > 0 ? (
              <div className="ops-table-wrap">
                <table className="ops-table">
                  <thead>
                    <tr>
                      <th>Rapor</th>
                      <th>Gerekçe</th>
                      <th>Durum</th>
                      <th>Oluşturma</th>
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
                        <td>{formatTimestamp(row.createdAt)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
            {cases?.ok && cases.data.cases.length === 0 ? (
              <p className="ops-banner">Bu profile bağlı vaka yok.</p>
            ) : null}
            {cases?.ok && cases.data.cases.length > 0 ? (
              <div className="ops-table-wrap">
                <table className="ops-table">
                  <thead>
                    <tr>
                      <th>Vaka</th>
                      <th>Başlık</th>
                      <th>Durum</th>
                      <th>Öncelik</th>
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
                        <td>
                          <span className={`status-chip priority-${row.priority}`}>{row.priority}</span>
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
