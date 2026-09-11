"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import {
  fetchOwnedListingsByOwner,
  type OpsLoadResult,
  type StaffListingSummary,
} from "@/lib/listings-360";

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

export function ListingsLookup() {
  const router = useRouter();
  const [listingId, setListingId] = useState("");
  const [ownerId, setOwnerId] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [ownerResult, setOwnerResult] = useState<OpsLoadResult<StaffListingSummary[]> | null>(null);
  const [pending, setPending] = useState(false);

  function onLookupListing(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const id = listingId.trim();
    if (!id) {
      setError("İlan kimliği girin.");
      return;
    }
    setError(null);
    router.push(`/listings/${encodeURIComponent(id)}`);
  }

  async function onLookupOwner(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const id = ownerId.trim();
    if (!id) {
      setError("Sahip public profile kimliği girin.");
      return;
    }
    setError(null);
    setPending(true);
    const next = await fetchOwnedListingsByOwner(id);
    setPending(false);
    setOwnerResult(next);
  }

  return (
    <main className="ops-page">
      <header className="ops-header">
        <h1>Listings</h1>
        <p>Staff ilan araması yok. İlan kimliği veya sahip public profile kimliği ile bakın.</p>
      </header>

      <form className="ops-action" onSubmit={onLookupListing}>
        <h2>İlan bakışı</h2>
        <label>
          Listing ID
          <input
            value={listingId}
            onChange={(event) => setListingId(event.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        <button type="submit">Aç</button>
      </form>

      <form className="ops-action" onSubmit={onLookupOwner}>
        <h2>Sahibe göre liste</h2>
        <label>
          Owner public profile ID
          <input
            value={ownerId}
            onChange={(event) => setOwnerId(event.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        <button type="submit" disabled={pending}>
          {pending ? "Yükleniyor…" : "Listele"}
        </button>
      </form>

      {error ? (
        <p className="ops-banner ops-banner-error" role="alert">
          {error}
        </p>
      ) : null}

      {ownerResult && !ownerResult.ok ? (
        <p
          className={`ops-banner ${ownerResult.kind === "error" || ownerResult.kind === "bad_request" ? "ops-banner-error" : "ops-banner-auth"}`}
          role="alert"
        >
          {ownerResult.message}
        </p>
      ) : null}

      {ownerResult?.ok && ownerResult.data.length === 0 ? (
        <p className="ops-banner">Bu kullanıcıya ait ilan yok.</p>
      ) : null}

      {ownerResult?.ok && ownerResult.data.length > 0 ? (
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
              {ownerResult.data.map((row) => (
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
    </main>
  );
}
