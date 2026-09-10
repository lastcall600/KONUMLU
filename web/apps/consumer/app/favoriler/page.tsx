"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";

import { FavoriteAction } from "@/components/FavoriteAction";
import { ListingCard } from "@/components/ListingCard";
import { AuthClientError, getSession } from "@/lib/auth";
import { FavoritesClientError, listFavoriteListings } from "@/lib/favorites";
import {
  getPublicListing,
  PublicListingClientError,
  type PublicListing,
} from "@/lib/publicListing";
import type { SearchListing } from "@/lib/search";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";

type PageState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; listings: PublicListing[] };

function toSearchListing(listing: PublicListing): SearchListing {
  const item: SearchListing = {
    listingId: listing.listingId,
    categoryId: listing.categoryId,
    title: listing.title,
  };
  if (listing.priceAmount) {
    item.priceAmount = listing.priceAmount;
  }
  if (listing.priceCurrency) {
    item.priceCurrency = listing.priceCurrency;
  }
  if (listing.location) {
    item.latitude = listing.location.latitude;
    item.longitude = listing.location.longitude;
  }
  if (listing.publishedAt) {
    item.publishedAt = listing.publishedAt;
  }
  return item;
}

function messageFromError(error: unknown): string {
  if (
    error instanceof FavoritesClientError ||
    error instanceof PublicListingClientError ||
    error instanceof AuthClientError
  ) {
    return error.message;
  }
  return UNAVAILABLE;
}

export default function FavoritesPage() {
  const [state, setState] = useState<PageState>({ kind: "loading" });

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      const session = await getSession();
      if (session === null) {
        setState({ kind: "unauthenticated" });
        return;
      }
      const records = await listFavoriteListings();
      const listings: PublicListing[] = [];
      const results = await Promise.all(
        records.map(async (record) => {
          try {
            return await getPublicListing(record.listingId);
          } catch (error) {
            if (error instanceof PublicListingClientError && error.code === "not_found") {
              return null;
            }
            throw error;
          }
        }),
      );
      for (const listing of results) {
        if (listing) {
          listings.push(listing);
        }
      }
      setState({ kind: "ready", listings });
    } catch (error) {
      if (error instanceof FavoritesClientError && error.code === "unauthenticated") {
        setState({ kind: "unauthenticated" });
        return;
      }
      setState({ kind: "unavailable", message: messageFromError(error) });
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <main className="search-main">
      <p className="search-nav">
        <Link href="/ara">Aramaya dön</Link>
      </p>
      <h1 className="brand">KONUMLU</h1>
      <h2 className="auth-title">Favoriler</h2>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unauthenticated" ? (
        <p>
          Favorileri görmek için <Link href="/giris">giriş</Link> yapın.
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
      {state.kind === "ready" && state.listings.length === 0 ? (
        <p>Henüz favori ilanınız yok</p>
      ) : null}
      {state.kind === "ready" && state.listings.length > 0 ? (
        <div className="search-results">
          {state.listings.map((listing) => (
            <ListingCard
              key={listing.listingId}
              listing={toSearchListing(listing)}
              sessionReady
              authenticated
              favorited
              onFavoriteChange={(next) => {
                if (!next) {
                  setState((current) =>
                    current.kind === "ready"
                      ? {
                          kind: "ready",
                          listings: current.listings.filter((item) => item.listingId !== listing.listingId),
                        }
                      : current,
                  );
                }
              }}
            />
          ))}
        </div>
      ) : null}
    </main>
  );
}
