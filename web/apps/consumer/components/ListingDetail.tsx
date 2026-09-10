"use client";

import { Fragment, useCallback, useEffect, useState } from "react";
import Link from "next/link";

import { AppointmentAction } from "@/components/AppointmentAction";
import { FavoriteAction } from "@/components/FavoriteAction";
import { MessageAction } from "@/components/MessageAction";
import { getSession } from "@/lib/auth";
import { FavoritesClientError, getFavoriteState } from "@/lib/favorites";
import { getOwnedListing, ListingsClientError } from "@/lib/listings";
import { DEFAULT_LOCALE } from "@/lib/locale";
import {
  getCategoryFormAtVersion,
  MasterDataClientError,
  type CategoryForm,
  type FormField,
} from "@/lib/masterdata";
import {
  getPublicListing,
  PublicListingClientError,
  type PublicListing,
} from "@/lib/publicListing";
import {
  formatAverageDisplay,
  getPublicListingReviewSummary,
  REVIEW_SUMMARY_EXPLAINABILITY,
  type ListingReviewSummary,
} from "@/lib/reviewSummary";
import {
  getPublicListingReviews,
  PUBLIC_REVIEWS_EXPLAINABILITY,
  type PublicListingReview,
} from "@/lib/publicReviews";

type DetailState =
  | { kind: "loading" }
  | { kind: "not_found" }
  | { kind: "unavailable"; message: string }
  | { kind: "error"; message: string }
  | { kind: "ready"; listing: PublicListing; form: CategoryForm | null };

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";

function messageFromError(error: unknown): string {
  if (error instanceof PublicListingClientError || error instanceof MasterDataClientError) {
    return error.message;
  }
  return GENERIC;
}

function formatPrice(listing: PublicListing): string | null {
  if (!listing.priceAmount) {
    return null;
  }
  if (listing.priceCurrency) {
    return `${listing.priceAmount} ${listing.priceCurrency}`;
  }
  return listing.priceAmount;
}

function formatDate(value: string): string | null {
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    return null;
  }
  return new Intl.DateTimeFormat("tr-TR", { dateStyle: "medium" }).format(new Date(parsed));
}

function attributeDisplayValue(field: FormField, raw: unknown): string | null {
  if (raw === undefined || raw === null) {
    return null;
  }
  switch (field.valueType) {
    case "boolean":
      if (typeof raw !== "boolean") {
        return null;
      }
      return raw ? "Evet" : "Hayır";
    case "enum": {
      if (typeof raw !== "string" || raw === "") {
        return null;
      }
      const option = field.options.find((item) => item.code === raw);
      if (!option || option.label === "") {
        return null;
      }
      return option.label;
    }
    case "integer":
    case "decimal":
      if (typeof raw === "number" && Number.isFinite(raw)) {
        return String(raw);
      }
      if (typeof raw === "string" && raw !== "") {
        return raw;
      }
      return null;
    case "text":
      if (typeof raw !== "string" || raw === "") {
        return null;
      }
      return raw;
    default:
      return null;
  }
}

function visibleAttributes(
  form: CategoryForm | null,
  attributes: Record<string, unknown>,
): Array<{ code: string; label: string; value: string }> {
  if (!form) {
    return [];
  }
  const rows: Array<{ code: string; label: string; value: string }> = [];
  for (const field of form.fields) {
    const label = field.label.trim();
    if (label === "") {
      continue;
    }
    const value = attributeDisplayValue(field, attributes[field.code]);
    if (value === null) {
      continue;
    }
    rows.push({ code: field.code, label, value });
  }
  return rows;
}

export function ListingDetail({ listingId }: { listingId: string }) {
  const [state, setState] = useState<DetailState>({ kind: "loading" });

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      const listing = await getPublicListing(listingId);
      let form: CategoryForm | null = null;
      if (listing.categorySchemaVersion >= 1) {
        try {
          form = await getCategoryFormAtVersion(
            listing.categoryId,
            listing.categorySchemaVersion,
            DEFAULT_LOCALE,
          );
        } catch (error) {
          if (error instanceof MasterDataClientError && error.code === "unavailable") {
            setState({ kind: "unavailable", message: error.message });
            return;
          }
          form = null;
        }
      }
      setState({ kind: "ready", listing, form });
    } catch (error) {
      if (error instanceof PublicListingClientError && error.code === "not_found") {
        setState({ kind: "not_found" });
        return;
      }
      if (error instanceof PublicListingClientError && error.code === "unavailable") {
        setState({ kind: "unavailable", message: error.message });
        return;
      }
      setState({ kind: "error", message: messageFromError(error) });
    }
  }, [listingId]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <main className="search-main">
      <p className="search-nav">
        <Link href="/ara">Aramaya dön</Link>
        {" · "}
        <Link href="/favoriler">Favoriler</Link>
        {" · "}
        <Link href="/mesajlar">Mesajlar</Link>
        {" · "}
        <Link href="/randevular">Randevular</Link>
      </p>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "not_found" ? (
        <p className="auth-error" role="alert">
          İlan bulunamadı veya artık yayında değil.
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
      {state.kind === "error" ? (
        <p className="auth-error" role="alert">
          {state.message}
        </p>
      ) : null}
      {state.kind === "ready" ? <ReadyListing listing={state.listing} form={state.form} /> : null}
    </main>
  );
}

function ReadyListing({ listing, form }: { listing: PublicListing; form: CategoryForm | null }) {
  const title = listing.title.trim() === "" ? "Başlıksız ilan" : listing.title;
  const price = formatPrice(listing);
  const published = listing.publishedAt ? formatDate(listing.publishedAt) : null;
  const updated = formatDate(listing.updatedAt);
  const showUpdated =
    updated !== null && (published === null || listing.updatedAt !== listing.publishedAt);
  const categoryLabel = form?.label.trim() ? form.label : null;
  const attributes = visibleAttributes(form, listing.attributes);
  const hasLocation = listing.location !== undefined;
  const [sessionReady, setSessionReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [favorited, setFavorited] = useState(false);
  const [isOwner, setIsOwner] = useState(false);

  useEffect(() => {
    let cancelled = false;
    async function loadFavorite() {
      try {
        const session = await getSession();
        if (cancelled) {
          return;
        }
        if (session === null) {
          setAuthenticated(false);
          setFavorited(false);
          setIsOwner(false);
          return;
        }
        setAuthenticated(true);
        try {
          await getOwnedListing(listing.listingId);
          if (!cancelled) {
            setIsOwner(true);
          }
        } catch (error) {
          if (!cancelled) {
            setIsOwner(false);
            if (error instanceof ListingsClientError && error.code === "unauthenticated") {
              setAuthenticated(false);
            }
          }
        }
        try {
          const current = await getFavoriteState(listing.listingId);
          if (!cancelled) {
            setFavorited(current);
          }
        } catch (error) {
          if (!cancelled && error instanceof FavoritesClientError && error.code === "unauthenticated") {
            setAuthenticated(false);
          }
        }
      } finally {
        if (!cancelled) {
          setSessionReady(true);
        }
      }
    }
    void loadFavorite();
    return () => {
      cancelled = true;
    };
  }, [listing.listingId]);

  return (
    <article>
      <h1 className="auth-title">{title}</h1>
      <FavoriteAction
        listingId={listing.listingId}
        sessionReady={sessionReady}
        authenticated={authenticated}
        favorited={favorited}
        onChanged={setFavorited}
      />
      <MessageAction
        listingId={listing.listingId}
        sessionReady={sessionReady}
        authenticated={authenticated}
        isOwner={isOwner}
      />
      <AppointmentAction
        listingId={listing.listingId}
        sessionReady={sessionReady}
        authenticated={authenticated}
        isOwner={isOwner}
      />
      {price ? <p className="listing-card-price">{price}</p> : null}
      {categoryLabel ? <p className="listing-card-meta">{categoryLabel}</p> : null}
      {published ? <p className="listing-card-meta">Yayın: {published}</p> : null}
      {showUpdated ? <p className="listing-card-meta">Güncelleme: {updated}</p> : null}
      {hasLocation ? <p className="listing-card-meta">Konum bilgisi mevcut</p> : null}

      <SellerSection seller={listing.seller} />

      <MediaGallery title={title} media={listing.media} />

      {listing.description.trim() !== "" ? <p className="auth-lead">{listing.description}</p> : null}

      {attributes.length > 0 ? (
        <section aria-labelledby="listing-attributes-heading">
          <h2 id="listing-attributes-heading" className="auth-title">
            Özellikler
          </h2>
          <dl className="listing-summary">
            {attributes.map((row) => (
              <Fragment key={row.code}>
                <dt>{row.label}</dt>
                <dd>{row.value}</dd>
              </Fragment>
            ))}
          </dl>
        </section>
      ) : null}

      <ListingAccuracySummary listingId={listing.listingId} />
    </article>
  );
}

const SELLER_FALLBACK_NAME = "Konumlu üyesi";

function SellerSection({ seller }: { seller: PublicListing["seller"] }) {
  const name =
    seller?.displayName && seller.displayName.trim() !== ""
      ? seller.displayName.trim()
      : SELLER_FALLBACK_NAME;
  return (
    <section aria-labelledby="listing-seller-heading">
      <h2 id="listing-seller-heading" className="auth-title">
        Satıcı
      </h2>
      {seller?.publicProfileId ? (
        <p>
          <Link href={`/profil/${encodeURIComponent(seller.publicProfileId)}`}>{name}</Link>
        </p>
      ) : (
        <p className="search-placeholder">Satıcı profili şu anda görüntülenemiyor.</p>
      )}
    </section>
  );
}

type ListingAccuracyState =
  | { kind: "loading" }
  | { kind: "unavailable" }
  | { kind: "ready"; summary: ListingReviewSummary };

type PublicReviewsState =
  | { kind: "loading" }
  | { kind: "unavailable" }
  | { kind: "ready"; reviews: PublicListingReview[]; nextCursor?: string };

function ListingAccuracySummary({ listingId }: { listingId: string }) {
  const [state, setState] = useState<ListingAccuracyState>({ kind: "loading" });
  const [reviewsState, setReviewsState] = useState<PublicReviewsState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    async function loadSummary() {
      try {
        const summary = await getPublicListingReviewSummary(listingId);
        if (!cancelled) {
          setState({ kind: "ready", summary });
        }
      } catch {
        if (!cancelled) {
          setState({ kind: "unavailable" });
        }
      }
    }
    async function loadReviews() {
      try {
        const page = await getPublicListingReviews(listingId);
        if (!cancelled) {
          setReviewsState({
            kind: "ready",
            reviews: page.reviews,
            nextCursor: page.nextCursor,
          });
        }
      } catch {
        if (!cancelled) {
          setReviewsState({ kind: "unavailable" });
        }
      }
    }
    void loadSummary();
    void loadReviews();
    return () => {
      cancelled = true;
    };
  }, [listingId]);

  const rating = state.kind === "ready" ? state.summary.listingAccuracy : null;
  const averageLabel =
    rating !== null && rating.average !== null ? formatAverageDisplay(rating.average) : "";

  return (
    <section aria-labelledby="listing-review-summary-heading">
      <h2 id="listing-review-summary-heading" className="auth-title">
        Doğrulanmış Değerlendirmeler
      </h2>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unavailable" ? (
        <p className="search-placeholder">Doğrulanmış değerlendirme özeti şu anda görüntülenemiyor.</p>
      ) : null}
      {state.kind === "ready" && rating !== null && rating.reviewCount > 0 && averageLabel !== "" ? (
        <div>
          <p>
            İlan Doğruluğu: {averageLabel} / 5
          </p>
          <p>
            {rating.reviewCount} doğrulanmış değerlendirme
          </p>
        </div>
      ) : null}
      {state.kind === "ready" && rating !== null && rating.reviewCount > 0 && averageLabel === "" ? (
        <p className="search-placeholder">Doğrulanmış değerlendirme özeti şu anda görüntülenemiyor.</p>
      ) : null}
      <p className="auth-lead">{REVIEW_SUMMARY_EXPLAINABILITY}</p>
      <PublicVerifiedReviews listingId={listingId} state={reviewsState} onStateChange={setReviewsState} />
    </section>
  );
}

function PublicVerifiedReviews({
  listingId,
  state,
  onStateChange,
}: {
  listingId: string;
  state: PublicReviewsState;
  onStateChange: (state: PublicReviewsState) => void;
}) {
  const [loadingMore, setLoadingMore] = useState(false);

  async function onLoadMore() {
    if (state.kind !== "ready" || !state.nextCursor || loadingMore) {
      return;
    }
    setLoadingMore(true);
    try {
      const page = await getPublicListingReviews(listingId, { cursor: state.nextCursor });
      onStateChange({
        kind: "ready",
        reviews: [...state.reviews, ...page.reviews],
        nextCursor: page.nextCursor,
      });
    } catch {
      onStateChange({ kind: "unavailable" });
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <div>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unavailable" ? (
        <p className="search-placeholder">Doğrulanmış değerlendirmeler şu anda görüntülenemiyor.</p>
      ) : null}
      {state.kind === "ready" && state.reviews.length === 0 ? (
        <p className="search-placeholder">Henüz yayımlanmış doğrulanmış değerlendirme yok.</p>
      ) : null}
      {state.kind === "ready" && state.reviews.length > 0 ? (
        <>
          <ul className="listing-summary">
            {state.reviews.map((review) => (
              <li key={review.reviewId}>
                <p>✓ Konumlu Verified</p>
                {review.body ? <p>{review.body}</p> : null}
                <p>İlan Doğruluğu: {review.listingAccuracy} / 5</p>
                <p>Satıcı / Profesyonel Hizmeti: {review.providerService} / 5</p>
                {formatDate(review.createdAt) ? (
                  <p className="listing-card-meta">{formatDate(review.createdAt)}</p>
                ) : null}
              </li>
            ))}
          </ul>
          {state.nextCursor ? (
            <div className="auth-actions">
              <button type="button" onClick={() => void onLoadMore()} disabled={loadingMore}>
                {loadingMore ? "Yükleniyor…" : "Daha fazla yükle"}
              </button>
            </div>
          ) : null}
        </>
      ) : null}
      <p className="auth-lead">{PUBLIC_REVIEWS_EXPLAINABILITY}</p>
    </div>
  );
}

function MediaGallery({
  title,
  media,
}: {
  title: string;
  media: PublicListing["media"];
}) {
  if (media.length === 0) {
    return <p className="search-placeholder">Görsel yok</p>;
  }
  return (
    <div className="search-results listing-gallery">
      {media.map((item, index) => {
        const alt = media.length === 1 ? title : `${title} ${index + 1}`;
        return (
          <figure key={`${item.url}-${index}`} className="listing-card">
            <img
              src={item.url}
              alt={alt}
              width={item.width}
              height={item.height}
            />
          </figure>
        );
      })}
    </div>
  );
}
