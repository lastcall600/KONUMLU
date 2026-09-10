import type { Metadata } from "next";

import { ListingDetail } from "@/components/ListingDetail";
import { getPublicListing } from "@/lib/publicListing";

export const dynamic = "force-dynamic";

type PageProps = {
  params: Promise<{ listingId: string }>;
};

function truncateDescription(value: string, maxLength: number): string {
  const normalized = value.trim().replace(/\s+/g, " ");
  if (normalized.length <= maxLength) {
    return normalized;
  }
  return `${normalized.slice(0, maxLength).trimEnd()}…`;
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { listingId } = await params;
  try {
    const listing = await getPublicListing(listingId);
    const title = listing.title.trim() === "" ? "İlan" : listing.title.trim();
    const description = listing.description.trim()
      ? truncateDescription(listing.description, 160)
      : undefined;
    return description ? { title, description } : { title };
  } catch {
    return { title: "İlan" };
  }
}

export default async function PublicListingPage({ params }: PageProps) {
  const { listingId } = await params;
  return <ListingDetail listingId={listingId} />;
}
