import { apiFetch } from "@/lib/api";

export type RatingSummary = {
  reviewCount: number;
  average: number | null;
};

export type ListingReviewSummary = {
  listingAccuracy: RatingSummary;
};

export type ProviderReviewSummary = {
  providerService: RatingSummary;
};

export class ReviewSummaryClientError extends Error {
  readonly code: "unauthenticated" | "unavailable" | "generic";

  constructor(code: ReviewSummaryClientError["code"], message: string) {
    super(message);
    this.name = "ReviewSummaryClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";

export const REVIEW_SUMMARY_EXPLAINABILITY =
  "Bu puan yalnızca Konumlu Verified ile doğrulanmış fiziksel etkileşimlerden sonra verilen değerlendirmelerden oluşur.";

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

function errorFromResponse(status: number, body: unknown): ReviewSummaryClientError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new ReviewSummaryClientError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 503 || code === "unavailable") {
    return new ReviewSummaryClientError("unavailable", UNAVAILABLE);
  }
  return new ReviewSummaryClientError("generic", GENERIC);
}

function parseCount(raw: unknown): number | null {
  if (typeof raw !== "number" || !Number.isFinite(raw) || !Number.isInteger(raw) || raw < 0) {
    return null;
  }
  return raw;
}

function parseAverage(raw: unknown): number | null | undefined {
  if (raw === null || raw === undefined) {
    return null;
  }
  if (typeof raw === "number") {
    return Number.isFinite(raw) ? raw : undefined;
  }
  if (typeof raw === "string") {
    const trimmed = raw.trim();
    if (trimmed === "") {
      return undefined;
    }
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  return undefined;
}

function parseRatingSummary(raw: unknown): RatingSummary | null {
  if (typeof raw !== "object" || raw === null) {
    return null;
  }
  const row = raw as Record<string, unknown>;
  const reviewCount = parseCount(row.reviewCount);
  if (reviewCount === null) {
    return null;
  }
  const average = parseAverage(row.average);
  if (average === undefined) {
    return null;
  }
  if (reviewCount === 0) {
    return { reviewCount, average: null };
  }
  if (average === null) {
    return null;
  }
  return { reviewCount, average };
}

function parseListingSummary(body: unknown): ListingReviewSummary | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const listingAccuracy = parseRatingSummary((body as Record<string, unknown>).listingAccuracy);
  if (listingAccuracy === null) {
    return null;
  }
  return { listingAccuracy };
}

function parseProviderSummary(body: unknown): ProviderReviewSummary | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const providerService = parseRatingSummary((body as Record<string, unknown>).providerService);
  if (providerService === null) {
    return null;
  }
  return { providerService };
}

export function formatAverageDisplay(value: number): string {
  const rounded = Math.round(value * 10) / 10;
  if (!Number.isFinite(rounded)) {
    return "";
  }
  if (Number.isInteger(rounded)) {
    return String(rounded);
  }
  return rounded.toFixed(1);
}

export async function getPublicListingReviewSummary(
  listingId: string,
): Promise<ListingReviewSummary> {
  const response = await apiFetch(
    `/v1/public/listings/${encodeURIComponent(listingId)}/review-summary`,
    { method: "GET" },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const summary = parseListingSummary(body);
  if (summary === null) {
    throw new ReviewSummaryClientError("generic", GENERIC);
  }
  return summary;
}

export async function getMyProviderReviewSummary(): Promise<ProviderReviewSummary> {
  const response = await apiFetch("/v1/review-summary/me", { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const summary = parseProviderSummary(body);
  if (summary === null) {
    throw new ReviewSummaryClientError("generic", GENERIC);
  }
  return summary;
}
