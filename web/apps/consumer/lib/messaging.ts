import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";

export type Conversation = {
  conversationId: string;
  listingId: string;
  counterpartUserId: string;
  createdAt: string;
  updatedAt: string;
};

export type ConversationSummary = {
  conversationId: string;
  listingId: string;
  counterpartUserId: string;
  lastMessagePreview: string;
  lastMessageAt?: string;
  unreadCount: number;
  updatedAt: string;
};

export type Message = {
  messageId: string;
  conversationId: string;
  senderUserId: string;
  body: string;
  createdAt: string;
};

export class MessagingClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "MessagingClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const NOT_FOUND = "Sohbet bulunamadı.";
const BAD_REQUEST = "Mesaj gönderilemedi. Lütfen metni kontrol edin.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";

function mutateHeaders(): Headers {
  const headers = new Headers();
  headers.set("Content-Type", "application/json");
  const csrf = readCsrfToken();
  if (csrf) {
    headers.set(CSRF_HEADER_NAME, csrf);
  }
  return headers;
}

function readErrorCode(body: unknown): string | undefined {
  if (typeof body !== "object" || body === null || !("error" in body)) {
    return undefined;
  }
  const error = (body as ErrorBody).error;
  return typeof error === "string" ? error : undefined;
}

async function readJson(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

function errorFromResponse(status: number, body: unknown): MessagingClientError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new MessagingClientError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 404 || code === "not_found") {
    return new MessagingClientError("not_found", NOT_FOUND);
  }
  if (status === 400 || code === "bad_request") {
    return new MessagingClientError("bad_request", BAD_REQUEST);
  }
  if (status === 503 || code === "unavailable") {
    return new MessagingClientError("unavailable", UNAVAILABLE);
  }
  return new MessagingClientError("generic", GENERIC);
}

function parseConversation(body: unknown): Conversation | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.conversationId !== "string" || raw.conversationId === "") {
    return null;
  }
  if (typeof raw.listingId !== "string" || raw.listingId === "") {
    return null;
  }
  if (typeof raw.counterpartUserId !== "string" || raw.counterpartUserId === "") {
    return null;
  }
  if (typeof raw.createdAt !== "string" || typeof raw.updatedAt !== "string") {
    return null;
  }
  return {
    conversationId: raw.conversationId,
    listingId: raw.listingId,
    counterpartUserId: raw.counterpartUserId,
    createdAt: raw.createdAt,
    updatedAt: raw.updatedAt,
  };
}

function parseSummary(item: unknown): ConversationSummary | null {
  if (typeof item !== "object" || item === null) {
    return null;
  }
  const raw = item as Record<string, unknown>;
  if (typeof raw.conversationId !== "string" || raw.conversationId === "") {
    return null;
  }
  if (typeof raw.listingId !== "string" || raw.listingId === "") {
    return null;
  }
  if (typeof raw.counterpartUserId !== "string") {
    return null;
  }
  if (typeof raw.unreadCount !== "number" || !Number.isInteger(raw.unreadCount) || raw.unreadCount < 0) {
    return null;
  }
  if (typeof raw.updatedAt !== "string" || raw.updatedAt === "") {
    return null;
  }
  const summary: ConversationSummary = {
    conversationId: raw.conversationId,
    listingId: raw.listingId,
    counterpartUserId: typeof raw.counterpartUserId === "string" ? raw.counterpartUserId : "",
    lastMessagePreview: typeof raw.lastMessagePreview === "string" ? raw.lastMessagePreview : "",
    unreadCount: raw.unreadCount,
    updatedAt: raw.updatedAt,
  };
  if (typeof raw.lastMessageAt === "string" && raw.lastMessageAt !== "") {
    summary.lastMessageAt = raw.lastMessageAt;
  }
  return summary;
}

function parseMessage(item: unknown): Message | null {
  if (typeof item !== "object" || item === null) {
    return null;
  }
  const raw = item as Record<string, unknown>;
  if (typeof raw.messageId !== "string" || raw.messageId === "") {
    return null;
  }
  if (typeof raw.conversationId !== "string" || raw.conversationId === "") {
    return null;
  }
  if (typeof raw.senderUserId !== "string" || raw.senderUserId === "") {
    return null;
  }
  if (typeof raw.body !== "string") {
    return null;
  }
  if (typeof raw.createdAt !== "string" || raw.createdAt === "") {
    return null;
  }
  return {
    messageId: raw.messageId,
    conversationId: raw.conversationId,
    senderUserId: raw.senderUserId,
    body: raw.body,
    createdAt: raw.createdAt,
  };
}

export async function createConversation(listingId: string): Promise<Conversation> {
  const response = await apiFetch("/v1/messaging/conversations", {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify({ listingId }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const conv = parseConversation(body);
  if (!conv) {
    throw new MessagingClientError("generic", GENERIC);
  }
  return conv;
}

export async function listConversations(): Promise<ConversationSummary[]> {
  const response = await apiFetch("/v1/messaging/conversations", { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new MessagingClientError("generic", GENERIC);
  }
  const rows = (body as { conversations?: unknown }).conversations;
  if (!Array.isArray(rows)) {
    throw new MessagingClientError("generic", GENERIC);
  }
  const out: ConversationSummary[] = [];
  for (const item of rows) {
    const parsed = parseSummary(item);
    if (parsed) {
      out.push(parsed);
    }
  }
  return out;
}

export async function getConversation(conversationId: string): Promise<Conversation> {
  const response = await apiFetch(`/v1/messaging/conversations/${encodeURIComponent(conversationId)}`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const conv = parseConversation(body);
  if (!conv) {
    throw new MessagingClientError("generic", GENERIC);
  }
  return conv;
}

export async function listMessages(conversationId: string): Promise<Message[]> {
  const response = await apiFetch(
    `/v1/messaging/conversations/${encodeURIComponent(conversationId)}/messages`,
    { method: "GET" },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new MessagingClientError("generic", GENERIC);
  }
  const rows = (body as { messages?: unknown }).messages;
  if (!Array.isArray(rows)) {
    throw new MessagingClientError("generic", GENERIC);
  }
  const out: Message[] = [];
  for (const item of rows) {
    const parsed = parseMessage(item);
    if (parsed) {
      out.push(parsed);
    }
  }
  return out;
}

export async function sendMessage(conversationId: string, text: string): Promise<Message> {
  const response = await apiFetch(
    `/v1/messaging/conversations/${encodeURIComponent(conversationId)}/messages`,
    {
      method: "POST",
      headers: mutateHeaders(),
      body: JSON.stringify({ body: text }),
    },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const msg = parseMessage(body);
  if (!msg) {
    throw new MessagingClientError("generic", GENERIC);
  }
  return msg;
}

export async function markConversationRead(conversationId: string): Promise<void> {
  const response = await apiFetch(
    `/v1/messaging/conversations/${encodeURIComponent(conversationId)}/read`,
    {
      method: "POST",
      headers: mutateHeaders(),
      body: JSON.stringify({}),
    },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
}
