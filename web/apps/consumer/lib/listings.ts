import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";

export type ListingStatus = string;

export type Listing = {
  listingId: string;
  status: ListingStatus;
  categoryId: string;
  categorySchemaVersion: number;
  title: string;
  description: string;
  priceAmount?: string;
  priceCurrency?: string;
  attributes: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
  publishedAt?: string;
  archivedAt?: string;
};

export type ListingLocation = {
  listingId: string;
  latitude: number;
  longitude: number;
  catalogLocationId?: string;
};

export type CreateListingInput = {
  categoryId: string;
  categorySchemaVersion: number;
  title: string;
  description: string;
  priceAmount?: string;
  priceCurrency?: string;
  attributes?: Record<string, unknown>;
  location?: {
    latitude: number;
    longitude: number;
    catalogLocationId?: string;
  };
  mediaAssetIds?: string[];
};

export type PatchDraftInput = {
  title?: string;
  description?: string;
  priceAmount?: string | null;
  priceCurrency?: string | null;
  attributes?: Record<string, unknown>;
  updatedAt: string;
};

export class ListingsClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "ListingsClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "İlan işlemi tamamlanamadı. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";
const FORBIDDEN = "Bu işlem için yetki doğrulanamadı. Lütfen sayfayı yenileyip tekrar deneyin.";
const CONFLICT = "İlan güncel değil veya bu geçiş yapılamıyor.";
const PUBLISH_CONFLICT = "İlan şu anda yayımlanamıyor.";
const BAD_REQUEST = "Gönderilen bilgiler geçersiz. Lütfen kontrol edip tekrar deneyin.";
const NOT_FOUND = "İlan bulunamadı.";
const PUBLISH_NOT_FOUND = "İlan bulunamadı veya size ait değil.";

function mutateHeaders(): Headers {
  const headers = new Headers();
  headers.set("Content-Type", "application/json");
  const csrf = readCsrfToken();
  if (csrf) {
    headers.set(CSRF_HEADER_NAME, csrf);
  }
  return headers;
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

function errorCodeFromBody(body: unknown): string | undefined {
  if (typeof body !== "object" || body === null || !("error" in body)) {
    return undefined;
  }
  const error = (body as ErrorBody).error;
  return typeof error === "string" ? error : undefined;
}

function errorFromResponse(status: number, body: unknown): ListingsClientError {
  const code = errorCodeFromBody(body);
  if (status === 401 || code === "unauthenticated") {
    return new ListingsClientError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 403 || code === "forbidden") {
    return new ListingsClientError("forbidden", FORBIDDEN);
  }
  if (status === 404 || code === "not_found") {
    return new ListingsClientError("not_found", NOT_FOUND);
  }
  if (status === 409 || code === "conflict") {
    return new ListingsClientError("conflict", CONFLICT);
  }
  if (status === 400 || code === "bad_request") {
    return new ListingsClientError("bad_request", BAD_REQUEST);
  }
  if (status === 503 || code === "unavailable") {
    return new ListingsClientError("unavailable", UNAVAILABLE);
  }
  return new ListingsClientError("generic", GENERIC);
}

function parseListing(body: unknown): Listing | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.listingId !== "string" || raw.listingId === "") {
    return null;
  }
  if (typeof raw.status !== "string" || raw.status === "") {
    return null;
  }
  if (typeof raw.categoryId !== "string" || raw.categoryId === "") {
    return null;
  }
  if (typeof raw.categorySchemaVersion !== "number") {
    return null;
  }
  if (typeof raw.title !== "string" || typeof raw.description !== "string") {
    return null;
  }
  if (typeof raw.updatedAt !== "string" || raw.updatedAt === "") {
    return null;
  }
  const listing: Listing = {
    listingId: raw.listingId,
    status: raw.status,
    categoryId: raw.categoryId,
    categorySchemaVersion: raw.categorySchemaVersion,
    title: raw.title,
    description: raw.description,
    attributes:
      typeof raw.attributes === "object" && raw.attributes !== null && !Array.isArray(raw.attributes)
        ? (raw.attributes as Record<string, unknown>)
        : {},
    createdAt: typeof raw.createdAt === "string" ? raw.createdAt : "",
    updatedAt: raw.updatedAt,
  };
  if (typeof raw.priceAmount === "string") {
    listing.priceAmount = raw.priceAmount;
  }
  if (typeof raw.priceCurrency === "string") {
    listing.priceCurrency = raw.priceCurrency;
  }
  if (typeof raw.publishedAt === "string") {
    listing.publishedAt = raw.publishedAt;
  }
  if (typeof raw.archivedAt === "string") {
    listing.archivedAt = raw.archivedAt;
  }
  return listing;
}

async function listingMutation(path: string, init: RequestInit): Promise<Listing> {
  const response = await apiFetch(path, init);
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const listing = parseListing(body);
  if (!listing) {
    throw new ListingsClientError("generic", GENERIC);
  }
  return listing;
}

export async function createListing(input: CreateListingInput): Promise<Listing> {
  const payload: Record<string, unknown> = {
    categoryId: input.categoryId,
    categorySchemaVersion: input.categorySchemaVersion,
    title: input.title,
    description: input.description,
  };
  if (input.priceAmount) {
    payload.priceAmount = input.priceAmount;
  }
  if (input.priceCurrency) {
    payload.priceCurrency = input.priceCurrency;
  }
  if (input.attributes) {
    payload.attributes = input.attributes;
  }
  if (input.location) {
    const location: Record<string, unknown> = {
      latitude: input.location.latitude,
      longitude: input.location.longitude,
    };
    if (input.location.catalogLocationId) {
      location.catalogLocationId = input.location.catalogLocationId;
    }
    payload.location = location;
  }
  if (input.mediaAssetIds && input.mediaAssetIds.length > 0) {
    payload.mediaAssetIds = input.mediaAssetIds;
  }
  return listingMutation("/v1/listings", {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify(payload),
  });
}

export async function getOwnedListing(listingId: string): Promise<Listing> {
  const response = await apiFetch(`/v1/listings/${encodeURIComponent(listingId)}`, { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const listing = parseListing(body);
  if (!listing) {
    throw new ListingsClientError("generic", GENERIC);
  }
  return listing;
}

export async function patchDraft(listingId: string, input: PatchDraftInput): Promise<Listing> {
  const payload: Record<string, unknown> = {
    updatedAt: input.updatedAt,
  };
  if (input.title !== undefined) {
    payload.title = input.title;
  }
  if (input.description !== undefined) {
    payload.description = input.description;
  }
  if (input.priceAmount !== undefined) {
    payload.priceAmount = input.priceAmount;
  }
  if (input.priceCurrency !== undefined) {
    payload.priceCurrency = input.priceCurrency;
  }
  if (input.attributes !== undefined) {
    payload.attributes = input.attributes;
  }
  return listingMutation(`/v1/listings/${encodeURIComponent(listingId)}`, {
    method: "PATCH",
    headers: mutateHeaders(),
    body: JSON.stringify(payload),
  });
}

export async function replaceListingLocation(
  listingId: string,
  input: { latitude: number; longitude: number; catalogLocationId?: string },
): Promise<ListingLocation> {
  const payload: Record<string, unknown> = {
    latitude: input.latitude,
    longitude: input.longitude,
  };
  if (input.catalogLocationId) {
    payload.catalogLocationId = input.catalogLocationId;
  }
  const response = await apiFetch(`/v1/listings/${encodeURIComponent(listingId)}/location`, {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify(payload),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new ListingsClientError("generic", GENERIC);
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.listingId !== "string" || typeof raw.latitude !== "number" || typeof raw.longitude !== "number") {
    throw new ListingsClientError("generic", GENERIC);
  }
  const location: ListingLocation = {
    listingId: raw.listingId,
    latitude: raw.latitude,
    longitude: raw.longitude,
  };
  if (typeof raw.catalogLocationId === "string" && raw.catalogLocationId !== "") {
    location.catalogLocationId = raw.catalogLocationId;
  }
  return location;
}

export async function attachListingMedia(listingId: string, assetIds: string[]): Promise<string[]> {
  const response = await apiFetch(`/v1/listings/${encodeURIComponent(listingId)}/media`, {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify({ assetIds }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new ListingsClientError("generic", GENERIC);
  }
  const assetIdsOut = (body as { assetIds?: unknown }).assetIds;
  if (!Array.isArray(assetIdsOut) || !assetIdsOut.every((id) => typeof id === "string")) {
    throw new ListingsClientError("generic", GENERIC);
  }
  return assetIdsOut;
}

export async function markListingReady(listingId: string): Promise<Listing> {
  return listingMutation(`/v1/listings/${encodeURIComponent(listingId)}/ready`, {
    method: "POST",
    headers: mutateHeaders(),
  });
}

function errorFromPublishResponse(status: number, body: unknown): ListingsClientError {
  const code = errorCodeFromBody(body);
  if (status === 404 || code === "not_found") {
    return new ListingsClientError("not_found", PUBLISH_NOT_FOUND);
  }
  if (status === 409 || code === "conflict") {
    return new ListingsClientError("conflict", PUBLISH_CONFLICT);
  }
  return errorFromResponse(status, body);
}

export async function publishListing(listingId: string): Promise<Listing> {
  const response = await apiFetch(`/v1/listings/${encodeURIComponent(listingId)}/publish`, {
    method: "POST",
    headers: mutateHeaders(),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromPublishResponse(response.status, body);
  }
  const listing = parseListing(body);
  if (!listing) {
    throw new ListingsClientError("generic", GENERIC);
  }
  return listing;
}

export async function archiveListing(listingId: string): Promise<Listing> {
  return listingMutation(`/v1/listings/${encodeURIComponent(listingId)}/archive`, {
    method: "POST",
    headers: mutateHeaders(),
  });
}
