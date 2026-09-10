"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";

import { AuthClientError, getSession } from "@/lib/auth";
import {
  MessagingClientError,
  listConversations,
  type ConversationSummary,
} from "@/lib/messaging";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";

type PageState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; conversations: ConversationSummary[] };

function messageFromError(error: unknown): string {
  if (error instanceof MessagingClientError || error instanceof AuthClientError) {
    return error.message;
  }
  return UNAVAILABLE;
}

export default function MessagesPage() {
  const [state, setState] = useState<PageState>({ kind: "loading" });

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      const session = await getSession();
      if (session === null) {
        setState({ kind: "unauthenticated" });
        return;
      }
      const conversations = await listConversations();
      setState({ kind: "ready", conversations });
    } catch (error) {
      if (error instanceof AuthClientError && error.code === "unauthenticated") {
        setState({ kind: "unauthenticated" });
        return;
      }
      if (error instanceof MessagingClientError && error.code === "unauthenticated") {
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
    <main>
      <p className="search-nav">
        <Link href="/">Ana sayfa</Link>
      </p>
      <h1 className="auth-title">Mesajlar</h1>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unauthenticated" ? (
        <p>
          Mesajları görmek için <Link href="/giris">giriş yapın</Link>.
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
      {state.kind === "ready" && state.conversations.length === 0 ? (
        <p className="search-placeholder">Henüz sohbet yok.</p>
      ) : null}
      {state.kind === "ready" && state.conversations.length > 0 ? (
        <ul className="message-list">
          {state.conversations.map((row) => (
            <li key={row.conversationId}>
              <Link href={`/mesajlar/${row.conversationId}`} className="message-list-item">
                <span>İlan {row.listingId}</span>
                {row.unreadCount > 0 ? (
                  <span className="message-unread">{row.unreadCount} okunmamış</span>
                ) : null}
                {row.lastMessagePreview ? (
                  <span className="message-preview">{row.lastMessagePreview}</span>
                ) : (
                  <span className="message-preview">Henüz mesaj yok</span>
                )}
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
    </main>
  );
}
