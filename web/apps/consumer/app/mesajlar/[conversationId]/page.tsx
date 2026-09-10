"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";

import { FlowVerificationAction } from "@/components/FlowVerificationAction";
import { AuthClientError, getSession, type AuthSession } from "@/lib/auth";
import { getOwnedListing, ListingsClientError } from "@/lib/listings";
import {
  MessagingClientError,
  getConversation,
  listMessages,
  markConversationRead,
  sendMessage,
  type Conversation,
  type Message,
} from "@/lib/messaging";

const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";

type PageState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "not_found" }
  | { kind: "unavailable"; message: string }
  | {
      kind: "ready";
      conversation: Conversation;
      messages: Message[];
      session: AuthSession;
      isListingOwner: boolean;
    };

function messageFromError(error: unknown): string {
  if (error instanceof MessagingClientError || error instanceof AuthClientError) {
    return error.message;
  }
  return UNAVAILABLE;
}

export default function ConversationPage() {
  const params = useParams<{ conversationId: string }>();
  const conversationId = typeof params.conversationId === "string" ? params.conversationId : "";
  const [state, setState] = useState<PageState>({ kind: "loading" });
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      const session = await getSession();
      if (session === null) {
        setState({ kind: "unauthenticated" });
        return;
      }
      const [conversation, messages] = await Promise.all([
        getConversation(conversationId),
        listMessages(conversationId),
      ]);
      let isListingOwner = false;
      try {
        await getOwnedListing(conversation.listingId);
        isListingOwner = true;
      } catch (error) {
        if (error instanceof ListingsClientError && error.code === "unauthenticated") {
          setState({ kind: "unauthenticated" });
          return;
        }
        isListingOwner = false;
      }
      try {
        await markConversationRead(conversationId);
      } catch {
        // History still loads if mark-read fails.
      }
      setState({ kind: "ready", conversation, messages, session, isListingOwner });
    } catch (error) {
      if (error instanceof AuthClientError && error.code === "unauthenticated") {
        setState({ kind: "unauthenticated" });
        return;
      }
      if (error instanceof MessagingClientError && error.code === "unauthenticated") {
        setState({ kind: "unauthenticated" });
        return;
      }
      if (error instanceof MessagingClientError && error.code === "not_found") {
        setState({ kind: "not_found" });
        return;
      }
      setState({ kind: "unavailable", message: messageFromError(error) });
    }
  }, [conversationId]);

  useEffect(() => {
    if (conversationId === "") {
      setState({ kind: "not_found" });
      return;
    }
    void load();
  }, [conversationId, load]);

  async function onSend(event: FormEvent) {
    event.preventDefault();
    if (state.kind !== "ready") {
      return;
    }
    setBusy(true);
    setSendError(null);
    try {
      await sendMessage(conversationId, draft);
      setDraft("");
      const messages = await listMessages(conversationId);
      setState({ ...state, messages });
    } catch (error) {
      setSendError(messageFromError(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main>
      <p className="search-nav">
        <Link href="/mesajlar">Mesajlara dön</Link>
      </p>
      <h1 className="auth-title">Sohbet</h1>
      {state.kind === "loading" ? <p>Yükleniyor…</p> : null}
      {state.kind === "unauthenticated" ? (
        <p>
          Sohbeti görmek için <Link href="/giris">giriş yapın</Link>.
        </p>
      ) : null}
      {state.kind === "not_found" ? (
        <p className="auth-error" role="alert">
          Sohbet bulunamadı.
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
      {state.kind === "ready" ? (
        <>
          <p className="listing-card-meta">
            <Link href={`/ilan/${state.conversation.listingId}`}>İlana git</Link>
          </p>
          <FlowVerificationAction
            listingId={state.conversation.listingId}
            requesterUserId={state.conversation.counterpartUserId}
            isListingOwner={state.isListingOwner}
          />
          <ol className="message-thread">
            {state.messages.length === 0 ? (
              <li className="search-placeholder">Henüz mesaj yok.</li>
            ) : (
              state.messages.map((msg) => {
                const mine = msg.senderUserId === state.session.userId;
                return (
                  <li key={msg.messageId} className={mine ? "message-mine" : "message-theirs"}>
                    <p className="message-body">{msg.body}</p>
                  </li>
                );
              })
            )}
          </ol>
          <form onSubmit={(event) => void onSend(event)} className="message-compose">
            <label htmlFor="message-body">Mesaj</label>
            <textarea
              id="message-body"
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              maxLength={4000}
              rows={3}
              required
            />
            <button type="submit" disabled={busy}>
              Gönder
            </button>
            {sendError ? (
              <p className="auth-error" role="alert">
                {sendError}
              </p>
            ) : null}
          </form>
        </>
      ) : null}
    </main>
  );
}
