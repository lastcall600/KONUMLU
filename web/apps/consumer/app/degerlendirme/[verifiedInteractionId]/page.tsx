"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";

import { AuthClientError, getSession } from "@/lib/auth";
import {
  ReviewsClientError,
  bodyWithinLimit,
  classifyEligibility,
  createReview,
  getReviewEligibility,
  utf8ByteLength,
  type ReviewEligibilityKind,
} from "@/lib/reviews";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const EXPLAIN =
  "Bu değerlendirme, doğrulanmış fiziksel etkileşim sonrasında oluşturulmuştur.";
const RATINGS = [1, 2, 3, 4, 5] as const;

type PageState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "unavailable"; message: string }
  | { kind: "not_found" }
  | { kind: "eligibility"; status: ReviewEligibilityKind }
  | { kind: "submitted" };

function messageFromError(error: unknown): string {
  if (error instanceof ReviewsClientError || error instanceof AuthClientError) {
    return error.message;
  }
  return UNAVAILABLE;
}

export default function ReviewPage() {
  const router = useRouter();
  const params = useParams<{ verifiedInteractionId: string }>();
  const verifiedInteractionId =
    typeof params.verifiedInteractionId === "string" ? params.verifiedInteractionId : "";
  const [state, setState] = useState<PageState>({ kind: "loading" });
  const [listingAccuracy, setListingAccuracy] = useState<number | null>(null);
  const [providerService, setProviderService] = useState<number | null>(null);
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    setSubmitError(null);
    if (verifiedInteractionId === "") {
      setState({ kind: "not_found" });
      return;
    }
    try {
      const session = await getSession();
      if (session === null) {
        setState({ kind: "unauthenticated" });
        router.replace("/giris");
        return;
      }
      const eligibility = await getReviewEligibility(verifiedInteractionId);
      setState({ kind: "eligibility", status: classifyEligibility(eligibility) });
    } catch (error) {
      if (
        (error instanceof AuthClientError && error.code === "unauthenticated") ||
        (error instanceof ReviewsClientError && error.code === "unauthenticated")
      ) {
        setState({ kind: "unauthenticated" });
        router.replace("/giris");
        return;
      }
      if (error instanceof ReviewsClientError && error.code === "not_found") {
        setState({ kind: "not_found" });
        return;
      }
      if (error instanceof ReviewsClientError && error.code === "unavailable") {
        setState({ kind: "unavailable", message: messageFromError(error) });
        return;
      }
      setState({ kind: "unavailable", message: messageFromError(error) });
    }
  }, [router, verifiedInteractionId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (listingAccuracy === null || providerService === null) {
      setSubmitError(GENERIC);
      return;
    }
    if (!bodyWithinLimit(comment)) {
      setSubmitError(GENERIC);
      return;
    }
    setBusy(true);
    setSubmitError(null);
    try {
      const body = comment.trim();
      await createReview({
        verifiedInteractionId,
        listingAccuracy,
        providerService,
        ...(body === "" ? {} : { body }),
      });
      setState({ kind: "submitted" });
    } catch (error) {
      if (error instanceof ReviewsClientError && error.code === "unauthenticated") {
        setState({ kind: "unauthenticated" });
        router.replace("/giris");
        return;
      }
      if (error instanceof ReviewsClientError && error.code === "conflict") {
        setState({ kind: "eligibility", status: "already_reviewed" });
        setSubmitError(error.message);
        return;
      }
      if (error instanceof ReviewsClientError && error.code === "not_found") {
        setState({ kind: "not_found" });
        return;
      }
      if (error instanceof ReviewsClientError && error.code === "unavailable") {
        setSubmitError(error.message);
        return;
      }
      setSubmitError(messageFromError(error));
    } finally {
      setBusy(false);
    }
  }

  const commentBytes = utf8ByteLength(comment);

  return (
    <main>
      <p className="search-nav">
        <Link href="/">Ana sayfa</Link>
        {" · "}
        <Link href="/randevular">Randevular</Link>
        {" · "}
        <Link href="/degerlendirmelerim">Değerlendirmelerim</Link>
      </p>
      <h1 className="auth-title">Değerlendirme</h1>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unauthenticated" ? (
        <p>
          Değerlendirme için <Link href="/giris">giriş yapın</Link>.
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
      {state.kind === "not_found" ? (
        <p className="auth-error" role="alert">
          Kayıt bulunamadı veya bu işlem için yetkiniz yok.
        </p>
      ) : null}
      {state.kind === "eligibility" && state.status === "already_reviewed" ? (
        <section className="auth-card">
          <p>Değerlendirildi</p>
          <p>{EXPLAIN}</p>
          <p>
            <Link href="/randevular">Randevulara dön</Link>
            {" · "}
            <Link href="/degerlendirmelerim">Değerlendirmelerim</Link>
          </p>
        </section>
      ) : null}
      {state.kind === "eligibility" && state.status === "expired" ? (
        <section className="auth-card">
          <p>Değerlendirme süresi doldu</p>
          <p>
            <Link href="/randevular">Randevulara dön</Link>
          </p>
        </section>
      ) : null}
      {state.kind === "submitted" ? (
        <section className="auth-card">
          <p className="auth-success" role="status">
            Değerlendirmeniz kaydedildi.
          </p>
          <p>{EXPLAIN}</p>
          <p>
            <Link href="/randevular">Randevulara dön</Link>
            {" · "}
            <Link href="/degerlendirmelerim">Değerlendirmelerim</Link>
          </p>
        </section>
      ) : null}
      {state.kind === "eligibility" && state.status === "eligible" ? (
        <section className="auth-card">
          <p>{EXPLAIN}</p>
          <form onSubmit={(event) => void onSubmit(event)}>
            <fieldset className="auth-fieldset" disabled={busy}>
              <legend>İlan Doğruluğu</legend>
              {RATINGS.map((value) => (
                <label key={`listing-${value}`}>
                  <input
                    type="radio"
                    name="listingAccuracy"
                    value={value}
                    checked={listingAccuracy === value}
                    onChange={() => setListingAccuracy(value)}
                    required
                  />
                  {value}
                </label>
              ))}
            </fieldset>
            <fieldset className="auth-fieldset" disabled={busy}>
              <legend>Satıcı / Profesyonel Hizmeti</legend>
              {RATINGS.map((value) => (
                <label key={`service-${value}`}>
                  <input
                    type="radio"
                    name="providerService"
                    value={value}
                    checked={providerService === value}
                    onChange={() => setProviderService(value)}
                    required
                  />
                  {value}
                </label>
              ))}
            </fieldset>
            <label className="auth-field" htmlFor="review-body">
              Yorum
            </label>
            <textarea
              id="review-body"
              name="body"
              rows={6}
              value={comment}
              onChange={(event) => setComment(event.target.value)}
              disabled={busy}
            />
            <span className="field-help">{commentBytes} / 4000 bayt</span>
            {submitError ? (
              <p className="auth-error" role="alert">
                {submitError}
              </p>
            ) : null}
            <button
              type="submit"
              className="auth-primary-action"
              disabled={busy || !bodyWithinLimit(comment)}
            >
              Gönder
            </button>
          </form>
        </section>
      ) : null}
    </main>
  );
}
