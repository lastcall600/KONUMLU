"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import { AuthClientError, getSession } from "@/lib/auth";
import {
  TrustClientError,
  formatProviderServiceAverage,
  getMyTrustPassport,
  isReviewSignalsZeroState,
  isTrustZeroState,
  trustLevelLabel,
  type TrustPassport,
} from "@/lib/trust";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";

type PageState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "unavailable"; message: string }
  | { kind: "error"; message: string }
  | { kind: "ready"; passport: TrustPassport };

function messageFromError(error: unknown, fallback: string): string {
  if (error instanceof TrustClientError || error instanceof AuthClientError) {
    return error.message;
  }
  return fallback;
}

function formatDate(value: string): string {
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    return value;
  }
  return new Intl.DateTimeFormat("tr-TR", { dateStyle: "medium" }).format(new Date(parsed));
}

export default function TrustPassportPage() {
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
      const passport = await getMyTrustPassport();
      setState({ kind: "ready", passport });
    } catch (error) {
      if (
        (error instanceof AuthClientError && error.code === "unauthenticated") ||
        (error instanceof TrustClientError && error.code === "unauthenticated")
      ) {
        setState({ kind: "unauthenticated" });
        router.replace("/giris");
        return;
      }
      if (error instanceof TrustClientError && error.code === "unavailable") {
        setState({ kind: "unavailable", message: messageFromError(error, UNAVAILABLE) });
        return;
      }
      setState({ kind: "error", message: messageFromError(error, GENERIC) });
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
        <Link href="/ara">İlan ara</Link>
      </p>
      <h1 className="auth-title">Konumlu Güven Pasaportu</h1>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unauthenticated" ? (
        <p>
          Güven Pasaportu için <Link href="/giris">giriş yapın</Link>.
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
      {state.kind === "error" ? (
        <p className="auth-error" role="alert">
          {state.message}
        </p>
      ) : null}
      {state.kind === "ready" ? <PassportView passport={state.passport} /> : null}
    </main>
  );
}

function PassportView({ passport }: { passport: TrustPassport }) {
  const zero = isTrustZeroState(passport);

  return (
    <>
      <section className="auth-card" aria-labelledby="trust-passport-heading">
        <h2 id="trust-passport-heading" className="auth-title">
          Güven Pasaportum
        </h2>
        <p>Güven seviyesi: {trustLevelLabel(passport.level)}</p>
        <p>Doğrulanmış etkileşim: {passport.verifiedInteractionCount}</p>
        <p>Talep eden olarak: {passport.requesterVerifiedInteractionCount}</p>
        <p>Sağlayıcı olarak: {passport.providerVerifiedInteractionCount}</p>
        {passport.lastVerifiedInteractionAt ? (
          <p>Son doğrulanmış etkileşim: {formatDate(passport.lastVerifiedInteractionAt)}</p>
        ) : null}
        <p>Mevcut güven seviyesi yalnızca doğrulanmış etkileşim sayısına göre belirlenir.</p>
        <p>Değerlendirme sinyalleri ayrı gösterilir ve şu anda güven seviyesini değiştirmez.</p>
        <p>
          Güven seviyesi şu anda yalnızca Konumlu doğrulanmış etkileşimlere dayanır. Doğrulanmış
          fiziksel görüşmeler katkı sağlar. Değerlendirmeler, işlemler, EİDS ve diğer güven
          sinyalleri henüz dahil değildir. Bu seviye tam bir dolandırıcılık veya güvenlik
          garantisi değildir.
        </p>
        {zero ? (
          <div>
            <p className="search-placeholder">Henüz doğrulanmış etkileşiminiz yok.</p>
            <p>
              <Link href="/randevular">Randevular</Link>
              {" · "}
              <Link href="/ara">İlan ara</Link>
            </p>
          </div>
        ) : null}
      </section>
      <ReviewSignalsView passport={passport} />
    </>
  );
}

function ReviewSignalsView({ passport }: { passport: TrustPassport }) {
  const zero = isReviewSignalsZeroState(passport);
  const averageLabel =
    passport.providerServiceAverage !== null
      ? formatProviderServiceAverage(passport.providerServiceAverage)
      : "";

  return (
    <section className="auth-card" aria-labelledby="verified-review-history-heading">
      <h2 id="verified-review-history-heading" className="auth-title">
        Doğrulanmış Değerlendirme Geçmişi
      </h2>
      {zero ? (
        <p className="search-placeholder">Henüz doğrulanmış değerlendirme geçmişiniz yok.</p>
      ) : (
        <>
          <p>Yazdığınız doğrulanmış değerlendirme sayısı: {passport.verifiedReviewCount}</p>
          <p>
            Aldığınız doğrulanmış hizmet değerlendirmesi sayısı:{" "}
            {passport.providerServiceReviewCount}
          </p>
          {averageLabel !== "" ? (
            <p>Satıcı / Profesyonel Hizmeti ortalaması: {averageLabel} / 5</p>
          ) : null}
          {passport.lastVerifiedReviewAt ? (
            <p>Son doğrulanmış değerlendirme: {formatDate(passport.lastVerifiedReviewAt)}</p>
          ) : null}
        </>
      )}
    </section>
  );
}
