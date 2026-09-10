import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";

export type ReviewEligibility = {
  eligible: boolean;
  alreadyReviewed: boolean;
  expiresAt?: string;
};

export type ReviewEligibilityKind = "eligible" | "already_reviewed" | "expired";

export type Review = {
  reviewId: string;
  verifiedInteractionId: string;
  listingId: string;
  listingAccuracy: number;
  providerService: number;
  body?: string;
  createdAt: string;
  updatedAt: string;
};

export type CreateReviewInput = {
  verifiedInteractionId: string;
  listingAccuracy: number;
  providerService: number;
  body?: string;
};

export class ReviewsClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "ReviewsClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const BAD_REQUEST = "Girdi geçersiz. Lütfen tekrar deneyin.";
const NOT_FOUND = "Kayıt bulunamadı veya bu işlem için yetkiniz yok.";
const CONFLICT = "Bu doğrulanmış etkileşim için zaten değerlendirme yapılmış.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";
const MAX_BODY_BYTES = 4000;

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

function errorFromResponse(status: number, body: unknown): ReviewsClientError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new ReviewsClientError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 400 || code === "bad_request") {
    return new ReviewsClientError("bad_request", BAD_REQUEST);
  }
  if (status === 404 || status === 403 || code === "not_found" || code === "forbidden") {
    return new ReviewsClientError("not_found", NOT_FOUND);
  }
  if (status === 409 || code === "conflict") {
    return new ReviewsClientError("conflict", CONFLICT);
  }
  if (status === 503 || code === "unavailable") {
    return new ReviewsClientError("unavailable", UNAVAILABLE);
  }
  return new ReviewsClientError("generic", GENERIC);
}

function parseRating(raw: unknown): number | null {
  if (typeof raw !== "number" || !Number.isInteger(raw) || raw < 1 || raw > 5) {
    return null;
  }
  return raw;
}

function parseEligibility(body: unknown): ReviewEligibility | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.eligible !== "boolean") {
    return null;
  }
  if (typeof raw.alreadyReviewed !== "boolean") {
    return null;
  }
  const row: ReviewEligibility = {
    eligible: raw.eligible,
    alreadyReviewed: raw.alreadyReviewed,
  };
  if (typeof raw.expiresAt === "string" && raw.expiresAt !== "") {
    row.expiresAt = raw.expiresAt;
  }
  return row;
}

function parseReview(body: unknown): Review | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.reviewId !== "string" || raw.reviewId === "") {
    return null;
  }
  if (typeof raw.verifiedInteractionId !== "string" || raw.verifiedInteractionId === "") {
    return null;
  }
  if (typeof raw.listingId !== "string" || raw.listingId === "") {
    return null;
  }
  const listingAccuracy = parseRating(raw.listingAccuracy);
  const providerService = parseRating(raw.providerService);
  if (listingAccuracy === null || providerService === null) {
    return null;
  }
  if (typeof raw.createdAt !== "string" || raw.createdAt === "") {
    return null;
  }
  if (typeof raw.updatedAt !== "string" || raw.updatedAt === "") {
    return null;
  }
  const row: Review = {
    reviewId: raw.reviewId,
    verifiedInteractionId: raw.verifiedInteractionId,
    listingId: raw.listingId,
    listingAccuracy,
    providerService,
    createdAt: raw.createdAt,
    updatedAt: raw.updatedAt,
  };
  if (typeof raw.body === "string" && raw.body !== "") {
    row.body = raw.body;
  }
  return row;
}

export function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

export function bodyWithinLimit(value: string): boolean {
  return utf8ByteLength(value) <= MAX_BODY_BYTES;
}

export function classifyEligibility(eligibility: ReviewEligibility): ReviewEligibilityKind {
  if (eligibility.alreadyReviewed) {
    return "already_reviewed";
  }
  if (eligibility.eligible) {
    return "eligible";
  }
  return "expired";
}

export async function getReviewEligibility(
  verifiedInteractionId: string,
): Promise<ReviewEligibility> {
  const response = await apiFetch(
    `/v1/reviews/eligibility/${encodeURIComponent(verifiedInteractionId)}`,
    { method: "GET" },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const row = parseEligibility(body);
  if (!row) {
    throw new ReviewsClientError("generic", GENERIC);
  }
  return row;
}

export async function createReview(input: CreateReviewInput): Promise<Review> {
  const payload: Record<string, unknown> = {
    verifiedInteractionId: input.verifiedInteractionId,
    listingAccuracy: input.listingAccuracy,
    providerService: input.providerService,
  };
  if (input.body !== undefined && input.body !== "") {
    payload.body = input.body;
  }
  const response = await apiFetch("/v1/reviews", {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify(payload),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const row = parseReview(body);
  if (!row) {
    throw new ReviewsClientError("generic", GENERIC);
  }
  return row;
}

export async function listMyReviews(): Promise<Review[]> {
  const response = await apiFetch("/v1/reviews/mine", { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new ReviewsClientError("generic", GENERIC);
  }
  const rows = (body as { reviews?: unknown }).reviews;
  if (!Array.isArray(rows)) {
    throw new ReviewsClientError("generic", GENERIC);
  }
  const out: Review[] = [];
  for (const item of rows) {
    const parsed = parseReview(item);
    if (parsed) {
      out.push(parsed);
    }
  }
  return out;
}
