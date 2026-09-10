"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import { AuthClientError, getSession } from "@/lib/auth";
import { ReviewsClientError, listMyReviews, type Review } from "@/lib/reviews";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const EXPLAIN =
  "Bu değerlendirme, doğrulanmış fiziksel etkileşim sonrasında oluşturulmuştur.";

type PageState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; reviews: Review[] };

function messageFromError(error: unknown): string {
  if (error instanceof ReviewsClientError || error instanceof AuthClientError) {
    return error.message;
  }
  return UNAVAILABLE;
}

function formatDate(value: string): string {
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    return value;
  }
  return new Intl.DateTimeFormat("tr-TR", { dateStyle: "medium" }).format(new Date(parsed));
}

export default function MyReviewsPage() {
  const router = useRouter();
  const [state, setState] = useState<PageState>({ kind: "loading" });

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      const session = await getSession();
      if (session === null) {
        setState({ kind: "unauthenticated" });
        router.replace("/giris");
        return;
      }
      const reviews = await listMyReviews();
      setState({ kind: "ready", reviews });
    } catch (error) {
      if (
        (error instanceof AuthClientError && error.code === "unauthenticated") ||
        (error instanceof ReviewsClientError && error.code === "unauthenticated")
      ) {
        setState({ kind: "unauthenticated" });
        router.replace("/giris");
        return;
      }
      setState({ kind: "unavailable", message: messageFromError(error) });
    }
  }, [router]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <main>
      <p className="search-nav">
        <Link href="/">Ana sayfa</Link>
        {" · "}
        <Link href="/randevular">Randevular</Link>
        {" · "}
        <Link href="/guven-pasaportum">Güven Pasaportum</Link>
      </p>
      <h1 className="auth-title">Değerlendirmelerim</h1>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unauthenticated" ? (
        <p>
          Değerlendirmeleriniz için <Link href="/giris">giriş yapın</Link>.
        </p>
      ) : null}
      {state.kind === "unavailable" ? (
        <div className="page-status">
          <p className="auth-error" role="alert">
            {state.message}
          </p>
          <button type="button" onClick={() => void load()}>
            Tekrar dene
          </button>
        </div>
      ) : null}
      {state.kind === "ready" && state.reviews.length === 0 ? (
        <p className="search-placeholder">Henüz değerlendirmeniz yok.</p>
      ) : null}
      {state.kind === "ready" && state.reviews.length > 0 ? (
        <ul className="message-list">
          {state.reviews.map((row) => (
            <li key={row.reviewId}>
              <article className="message-list-item">
                <p>Tarih: {formatDate(row.createdAt)}</p>
                {row.listingId ? (
                  <p>
                    İlan: <Link href={`/ilan/${row.listingId}`}>İlan {row.listingId}</Link>
                  </p>
                ) : null}
                <p>İlan doğruluğu: {row.listingAccuracy}</p>
                <p>Hizmet: {row.providerService}</p>
                {row.body ? <p>{row.body}</p> : null}
                <p className="field-help">{EXPLAIN}</p>
              </article>
            </li>
          ))}
        </ul>
      ) : null}
    </main>
  );
}
