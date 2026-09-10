import { apiFetch } from "@/lib/api";

export type PublicListingReview = {
  reviewId: string;
  body: string | null;
  listingAccuracy: number;
  providerService: number;
  createdAt: string;
};

export type PublicListingReviewsPage = {
  reviews: PublicListingReview[];
  nextCursor?: string;
};

export class PublicReviewsClientError extends Error {
  readonly code: "not_found" | "unavailable" | "generic";

  constructor(code: PublicReviewsClientError["code"], message: string) {
    super(message);
    this.name = "PublicReviewsClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";

export const PUBLIC_REVIEWS_EXPLAINABILITY =
  "Bu yorumlar yalnızca Konumlu Verified ile doğrulanmış fiziksel etkileşimlerden sonra oluşturulabilir.";

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

function errorFromResponse(status: number, body: unknown): PublicReviewsClientError {
  const code = readErrorCode(body);
  if (status === 404 || code === "not_found") {
    return new PublicReviewsClientError("not_found", GENERIC);
  }
  if (status === 503 || code === "unavailable") {
    return new PublicReviewsClientError("unavailable", UNAVAILABLE);
  }
  return new PublicReviewsClientError("generic", GENERIC);
}

function parseRating(raw: unknown): number | null {
  if (typeof raw !== "number" || !Number.isInteger(raw) || raw < 1 || raw > 5) {
    return null;
  }
  return raw;
}

function parsePublicReview(body: unknown): PublicListingReview | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.reviewId !== "string" || raw.reviewId === "") {
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
  let bodyText: string | null = null;
  if (raw.body === null || raw.body === undefined) {
    bodyText = null;
  } else if (typeof raw.body === "string") {
    const trimmed = raw.body.trim();
    bodyText = trimmed === "" ? null : raw.body;
  } else {
    return null;
  }
  return {
    reviewId: raw.reviewId,
    body: bodyText,
    listingAccuracy,
    providerService,
    createdAt: raw.createdAt,
  };
}

export async function getPublicListingReviews(
  listingId: string,
  input: { cursor?: string } = {},
): Promise<PublicListingReviewsPage> {
  const params = new URLSearchParams();
  if (input.cursor) {
    params.set("cursor", input.cursor);
  }
  const query = params.toString();
  const path = `/v1/public/listings/${encodeURIComponent(listingId)}/reviews${query ? `?${query}` : ""}`;
  const response = await apiFetch(path, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new PublicReviewsClientError("generic", GENERIC);
  }
  const rows = (body as { reviews?: unknown }).reviews;
  if (!Array.isArray(rows)) {
    throw new PublicReviewsClientError("generic", GENERIC);
  }
  const out: PublicListingReview[] = [];
  for (const item of rows) {
    const parsed = parsePublicReview(item);
    if (parsed) {
      out.push(parsed);
    }
  }
  const page: PublicListingReviewsPage = { reviews: out };
  const nextCursor = (body as { nextCursor?: unknown }).nextCursor;
  if (typeof nextCursor === "string" && nextCursor !== "") {
    page.nextCursor = nextCursor;
  }
  return page;
}
