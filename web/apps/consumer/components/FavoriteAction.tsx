"use client";

import { MouseEvent, useState } from "react";
import Link from "next/link";

import {
  addFavorite,
  FavoritesClientError,
  removeFavorite,
} from "@/lib/favorites";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";

export function FavoriteAction({
  listingId,
  sessionReady,
  authenticated,
  favorited,
  onChanged,
}: {
  listingId: string;
  sessionReady: boolean;
  authenticated: boolean;
  favorited: boolean;
  onChanged?: (favorited: boolean) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!sessionReady) {
    return null;
  }

  if (!authenticated) {
    return (
      <p className="listing-card-meta">
        <Link href="/giris" onClick={stopCardClick}>
          Favorilere ekle
        </Link>
      </p>
    );
  }

  async function onToggle(event: MouseEvent<HTMLButtonElement>) {
    stopCardClick(event);
    setBusy(true);
    setError(null);
    try {
      if (favorited) {
        await removeFavorite(listingId);
        onChanged?.(false);
      } else {
        await addFavorite(listingId);
        onChanged?.(true);
      }
    } catch (err) {
      if (err instanceof FavoritesClientError) {
        setError(err.message);
      } else {
        setError(UNAVAILABLE);
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="listing-card-meta">
      <button type="button" onClick={(event) => void onToggle(event)} disabled={busy}>
        {favorited ? "Favorilerden çıkar" : "Favorilere ekle"}
      </button>
      {error ? (
        <p className="auth-error" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}

function stopCardClick(event: MouseEvent<HTMLElement>) {
  event.stopPropagation();
}
