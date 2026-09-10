import Link from "next/link";

import { FavoriteAction } from "@/components/FavoriteAction";
import { hasValidCoordinates, type SearchListing } from "@/lib/search";

export type ListingCardProps = {
  listing: SearchListing;
  categoryLabel?: string;
  selected?: boolean;
  onHighlight?: () => void;
  sessionReady?: boolean;
  authenticated?: boolean;
  favorited?: boolean;
  onFavoriteChange?: (favorited: boolean) => void;
};

function formatPrice(listing: SearchListing): string | null {
  if (!listing.priceAmount) {
    return null;
  }
  if (listing.priceCurrency) {
    return `${listing.priceAmount} ${listing.priceCurrency}`;
  }
  return listing.priceAmount;
}

function formatPublishedAt(value: string): string | null {
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    return null;
  }
  return new Intl.DateTimeFormat("tr-TR", { dateStyle: "medium" }).format(new Date(parsed));
}

export function ListingCard({
  listing,
  categoryLabel,
  selected,
  onHighlight,
  sessionReady = false,
  authenticated = false,
  favorited = false,
  onFavoriteChange,
}: ListingCardProps) {
  const price = formatPrice(listing);
  const published = listing.publishedAt ? formatPublishedAt(listing.publishedAt) : null;
  const href = `/ilan/${encodeURIComponent(listing.listingId)}`;
  const mappable = hasValidCoordinates(listing);

  return (
    <article
      id={`search-listing-${listing.listingId}`}
      className={selected ? "listing-card listing-card-selected" : "listing-card"}
      onMouseEnter={mappable ? onHighlight : undefined}
      onFocus={mappable ? onHighlight : undefined}
      onClick={mappable ? onHighlight : undefined}
    >
      <h3 className="listing-card-title">
        <Link href={href}>{listing.title === "" ? "Başlıksız ilan" : listing.title}</Link>
      </h3>
      {price ? <p className="listing-card-price">{price}</p> : null}
      {categoryLabel ? <p className="listing-card-meta">{categoryLabel}</p> : null}
      {mappable ? <p className="listing-card-meta">Konum bilgisi var</p> : null}
      {published ? <p className="listing-card-meta">Yayın: {published}</p> : null}
      <FavoriteAction
        listingId={listing.listingId}
        sessionReady={sessionReady}
        authenticated={authenticated}
        favorited={favorited}
        onChanged={onFavoriteChange}
      />
    </article>
  );
}
