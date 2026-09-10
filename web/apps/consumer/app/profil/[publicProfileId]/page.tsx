"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";

import {
  PublicProfileClientError,
  getPublicProfile,
  type PublicIdentityProfile,
} from "@/lib/publicProfile";
import {
  TrustClientError,
  formatProviderServiceAverage,
  getPublicTrustPassport,
  isPublicTrustZeroState,
  trustLevelLabel,
  type PublicTrustPassport,
} from "@/lib/trust";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const NOT_FOUND = "Profil bulunamadı.";
const NEUTRAL_NAME = "Konumlu üyesi";

type PageState =
  | { kind: "loading" }
  | { kind: "not_found" }
  | { kind: "unavailable"; message: string }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      profile: PublicIdentityProfile;
      trust: PublicTrustPassport | null;
      trustUnavailable: boolean;
    };

function messageFromError(error: unknown, fallback: string): string {
  if (error instanceof PublicProfileClientError || error instanceof TrustClientError) {
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

export default function PublicProfilePage() {
  const params = useParams<{ publicProfileId: string }>();
  const publicProfileId = typeof params.publicProfileId === "string" ? params.publicProfileId : "";
  const [state, setState] = useState<PageState>({ kind: "loading" });

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    if (publicProfileId === "") {
      setState({ kind: "not_found" });
      return;
    }
    try {
      const profile = await getPublicProfile(publicProfileId);
      try {
        const trust = await getPublicTrustPassport(publicProfileId);
        setState({ kind: "ready", profile, trust, trustUnavailable: false });
      } catch (error) {
        if (error instanceof TrustClientError && error.code === "unavailable") {
          setState({ kind: "ready", profile, trust: null, trustUnavailable: true });
          return;
        }
        setState({ kind: "ready", profile, trust: null, trustUnavailable: true });
      }
    } catch (error) {
      if (error instanceof PublicProfileClientError && error.code === "not_found") {
        setState({ kind: "not_found" });
        return;
      }
      if (error instanceof PublicProfileClientError && error.code === "unavailable") {
        setState({ kind: "unavailable", message: messageFromError(error, UNAVAILABLE) });
        return;
      }
      setState({ kind: "error", message: messageFromError(error, GENERIC) });
    }
  }, [publicProfileId]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <main>
      <p className="search-nav">
        <Link href="/">Ana sayfa</Link>
        {" · "}
        <Link href="/ara">İlan ara</Link>
      </p>
      <h1 className="auth-title">Profil</h1>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "not_found" ? (
        <p className="auth-error" role="alert">
          {NOT_FOUND}
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
      {state.kind === "ready" ? (
        <PublicProfileView
          profile={state.profile}
          trust={state.trust}
          trustUnavailable={state.trustUnavailable}
        />
      ) : null}
    </main>
  );
}

function PublicProfileView({
  profile,
  trust,
  trustUnavailable,
}: {
  profile: PublicIdentityProfile;
  trust: PublicTrustPassport | null;
  trustUnavailable: boolean;
}) {
  const name = profile.displayName ?? NEUTRAL_NAME;

  return (
    <>
      <section className="auth-card" aria-labelledby="public-profile-heading">
        <h2 id="public-profile-heading" className="auth-title">
          {name}
        </h2>
        <p>Üyelik: {formatDate(profile.memberSince)}</p>
      </section>
      {trustUnavailable || trust === null ? (
        <section className="auth-card" aria-labelledby="public-trust-heading">
          <h2 id="public-trust-heading" className="auth-title">
            Konumlu Güven Pasaportu
          </h2>
          <p className="search-placeholder">Güven Pasaportu şu anda görüntülenemiyor.</p>
        </section>
      ) : (
        <PublicTrustView passport={trust} />
      )}
    </>
  );
}

function PublicTrustView({ passport }: { passport: PublicTrustPassport }) {
  const zero = isPublicTrustZeroState(passport);
  const averageLabel =
    passport.providerServiceAverage !== null
      ? formatProviderServiceAverage(passport.providerServiceAverage)
      : "";

  return (
    <section className="auth-card" aria-labelledby="public-trust-heading">
      <h2 id="public-trust-heading" className="auth-title">
        Konumlu Güven Pasaportu
      </h2>
      <p>Güven seviyesi: {trustLevelLabel(passport.level)}</p>
      <p>Doğrulanmış etkileşim: {passport.verifiedInteractionCount}</p>
      <p>Sağlayıcı olarak: {passport.providerVerifiedInteractionCount}</p>
      {passport.providerServiceReviewCount > 0 ? (
        <p>
          Satıcı / Profesyonel Hizmeti: {passport.providerServiceReviewCount} değerlendirme
          {averageLabel !== "" ? ` · ortalama ${averageLabel} / 5` : ""}
        </p>
      ) : null}
      {passport.lastVerifiedInteractionAt ? (
        <p>Son doğrulanmış etkileşim: {formatDate(passport.lastVerifiedInteractionAt)}</p>
      ) : null}
      <p>Bu güven seviyesi, Konumlu üzerinde doğrulanmış etkileşim sayısına göre oluşturulur.</p>
      <p>Değerlendirme ortalamaları ayrı gösterilir ve güven seviyesini değiştirmez.</p>
      <p>Bu bilgiler tek başına dolandırıcılık veya güvenlik garantisi değildir.</p>
      {zero ? <p className="search-placeholder">Henüz doğrulanmış etkileşim yok.</p> : null}
    </section>
  );
}
