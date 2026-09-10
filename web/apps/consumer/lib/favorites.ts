import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";

export type FavoriteRecord = {
  listingId: string;
  createdAt: string;
};

export class FavoritesClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "FavoritesClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const NOT_FOUND = "İlan bulunamadı veya artık yayında değil.";

function mutateHeaders(): Headers {
  const headers = new Headers();
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

function errorFromResponse(status: number, body: unknown): FavoritesClientError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new FavoritesClientError("unauthenticated", GENERIC);
  }
  if (status === 404 || code === "not_found") {
    return new FavoritesClientError("not_found", NOT_FOUND);
  }
  if (status === 503 || code === "unavailable") {
    return new FavoritesClientError("unavailable", UNAVAILABLE);
  }
  return new FavoritesClientError("generic", GENERIC);
}

function parseState(body: unknown): boolean {
  if (typeof body !== "object" || body === null) {
    return false;
  }
  return (body as { favorited?: unknown }).favorited === true;
}

export async function listFavoriteListings(): Promise<FavoriteRecord[]> {
  const response = await apiFetch("/v1/favorites/listings", { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new FavoritesClientError("generic", GENERIC);
  }
  const listings = (body as { listings?: unknown }).listings;
  if (!Array.isArray(listings)) {
    throw new FavoritesClientError("generic", GENERIC);
  }
  const out: FavoriteRecord[] = [];
  for (const item of listings) {
    if (typeof item !== "object" || item === null) {
      continue;
    }
    const row = item as { listingId?: unknown; createdAt?: unknown };
    if (typeof row.listingId !== "string" || row.listingId === "") {
      continue;
    }
    if (typeof row.createdAt !== "string" || row.createdAt === "") {
      continue;
    }
    out.push({ listingId: row.listingId, createdAt: row.createdAt });
  }
  return out;
}

export async function getFavoriteState(listingId: string): Promise<boolean> {
  const response = await apiFetch(`/v1/favorites/listings/${encodeURIComponent(listingId)}`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  return parseState(body);
}

export async function addFavorite(listingId: string): Promise<void> {
  const response = await apiFetch(`/v1/favorites/listings/${encodeURIComponent(listingId)}`, {
    method: "POST",
    headers: mutateHeaders(),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
}

export async function removeFavorite(listingId: string): Promise<void> {
  const response = await apiFetch(`/v1/favorites/listings/${encodeURIComponent(listingId)}`, {
    method: "DELETE",
    headers: mutateHeaders(),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
}
