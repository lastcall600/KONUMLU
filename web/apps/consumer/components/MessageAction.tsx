"use client";

import { MouseEvent, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";

import { MessagingClientError, createConversation } from "@/lib/messaging";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";

export function MessageAction({
  listingId,
  sessionReady,
  authenticated,
  isOwner,
}: {
  listingId: string;
  sessionReady: boolean;
  authenticated: boolean;
  isOwner: boolean;
}) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!sessionReady || isOwner) {
    return null;
  }

  if (!authenticated) {
    return (
      <p className="listing-card-meta">
        <Link href="/giris" onClick={stopCardClick}>
          Mesaj Gönder
        </Link>
      </p>
    );
  }

  async function onStart(event: MouseEvent<HTMLButtonElement>) {
    stopCardClick(event);
    setBusy(true);
    setError(null);
    try {
      const conv = await createConversation(listingId);
      router.push(`/mesajlar/${conv.conversationId}`);
    } catch (err) {
      if (err instanceof MessagingClientError) {
        setError(err.message);
      } else {
        setError(UNAVAILABLE);
      }
      setBusy(false);
    }
  }

  return (
    <div className="listing-card-meta">
      <button type="button" onClick={(event) => void onStart(event)} disabled={busy}>
        Mesaj Gönder
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
