"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";

import { AuthClientError, getSession } from "@/lib/auth";
import {
  deleteSavedSearch,
  listSavedSearches,
  SavedSearchClientError,
  savedSearchToAraHref,
  summarizeSavedSearch,
  type SavedSearch,
} from "@/lib/savedSearch";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";

type PageState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; searches: SavedSearch[] };

function messageFromError(error: unknown): string {
  if (error instanceof SavedSearchClientError || error instanceof AuthClientError) {
    return error.message;
  }
  return UNAVAILABLE;
}

export default function SavedSearchesPage() {
  const [state, setState] = useState<PageState>({ kind: "loading" });
  const [busyId, setBusyId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    setActionError(null);
    try {
      const session = await getSession();
      if (session === null) {
        setState({ kind: "unauthenticated" });
        return;
      }
      const searches = await listSavedSearches();
      setState({ kind: "ready", searches });
    } catch (error) {
      if (error instanceof SavedSearchClientError && error.code === "unauthenticated") {
        setState({ kind: "unauthenticated" });
        return;
      }
      setState({ kind: "unavailable", message: messageFromError(error) });
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function onDelete(id: string) {
    setBusyId(id);
    setActionError(null);
    try {
      await deleteSavedSearch(id);
      setState((current) =>
        current.kind === "ready"
          ? { kind: "ready", searches: current.searches.filter((row) => row.id !== id) }
          : current,
      );
    } catch (error) {
      setActionError(messageFromError(error));
    } finally {
      setBusyId(null);
    }
  }

  return (
    <main className="search-main">
      <p className="search-nav">
        <Link href="/ara">Aramaya dön</Link>
        {" · "}
        <Link href="/favoriler">Favoriler</Link>
      </p>
      <h1 className="brand">KONUMLU</h1>
      <h2 className="auth-title">Kayıtlı aramalar</h2>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unauthenticated" ? (
        <p>
          Kayıtlı aramaları görmek için <Link href="/giris">giriş</Link> yapın.
        </p>
      ) : null}
      {state.kind === "unavailable" ? (
        <div className="page-status">
          <p className="auth-error" role="alert">
            {state.message || UNAVAILABLE}
          </p>
          <button type="button" onClick={() => void load()}>
            Tekrar dene
          </button>
        </div>
      ) : null}
      {actionError ? (
        <p className="auth-error" role="alert">
          {actionError}
        </p>
      ) : null}
      {state.kind === "ready" && state.searches.length === 0 ? (
        <p>Henüz kayıtlı aramanız yok.</p>
      ) : null}
      {state.kind === "ready" && state.searches.length > 0 ? (
        <ul className="search-results">
          {state.searches.map((row) => (
            <li key={row.id} className="auth-card">
              <h3 className="auth-title">{row.name}</h3>
              <p className="listing-card-meta">{summarizeSavedSearch(row)}</p>
              <div className="auth-actions">
                <Link href={savedSearchToAraHref(row)}>Aramayı Aç</Link>
                <button type="button" onClick={() => void onDelete(row.id)} disabled={busyId === row.id}>
                  {busyId === row.id ? "Siliniyor…" : "Sil"}
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : null}
    </main>
  );
}
