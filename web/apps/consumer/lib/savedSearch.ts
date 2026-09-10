import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";
import { buildAraHref, type SearchViewport } from "@/lib/search";

export type SavedSearch = {
  id: string;
  name: string;
  q?: string;
  categoryId?: string;
  minPrice?: string;
  maxPrice?: string;
  currency?: string;
  north?: number;
  south?: number;
  east?: number;
  west?: number;
  createdAt: string;
  updatedAt: string;
};

export type SavedSearchInput = {
  name: string;
  q?: string;
  categoryId?: string;
  minPrice?: string;
  maxPrice?: string;
  currency?: string;
  viewport?: SearchViewport;
};

export class SavedSearchClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "SavedSearchClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const BAD_REQUEST = "Arama bilgileri geçersiz. Lütfen filtreleri kontrol edip tekrar deneyin.";
const NOT_FOUND = "Kayıtlı arama bulunamadı.";

function mutateHeaders(): Headers {
  const headers = new Headers({ "Content-Type": "application/json" });
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

function errorFromResponse(status: number, body: unknown): SavedSearchClientError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new SavedSearchClientError("unauthenticated", GENERIC);
  }
  if (status === 404 || code === "not_found") {
    return new SavedSearchClientError("not_found", NOT_FOUND);
  }
  if (status === 400 || code === "bad_request") {
    return new SavedSearchClientError("bad_request", BAD_REQUEST);
  }
  if (status === 503 || code === "unavailable") {
    return new SavedSearchClientError("unavailable", UNAVAILABLE);
  }
  return new SavedSearchClientError("generic", GENERIC);
}

function parseOptionalString(raw: unknown): string | undefined {
  if (typeof raw === "string" && raw !== "") {
    return raw;
  }
  return undefined;
}

function parseOptionalNumber(raw: unknown): number | undefined {
  if (typeof raw === "number" && Number.isFinite(raw)) {
    return raw;
  }
  return undefined;
}

function parseSavedSearch(raw: unknown): SavedSearch | null {
  if (typeof raw !== "object" || raw === null) {
    return null;
  }
  const item = raw as Record<string, unknown>;
  if (typeof item.id !== "string" || item.id === "") {
    return null;
  }
  if (typeof item.name !== "string" || item.name === "") {
    return null;
  }
  if (typeof item.createdAt !== "string" || item.createdAt === "") {
    return null;
  }
  if (typeof item.updatedAt !== "string" || item.updatedAt === "") {
    return null;
  }
  const row: SavedSearch = {
    id: item.id,
    name: item.name,
    createdAt: item.createdAt,
    updatedAt: item.updatedAt,
  };
  const q = parseOptionalString(item.q);
  if (q) {
    row.q = q;
  }
  const categoryId = parseOptionalString(item.categoryId);
  if (categoryId) {
    row.categoryId = categoryId;
  }
  const minPrice = parseOptionalString(item.minPrice);
  if (minPrice) {
    row.minPrice = minPrice;
  }
  const maxPrice = parseOptionalString(item.maxPrice);
  if (maxPrice) {
    row.maxPrice = maxPrice;
  }
  const currency = parseOptionalString(item.currency);
  if (currency) {
    row.currency = currency;
  }
  const north = parseOptionalNumber(item.north);
  const south = parseOptionalNumber(item.south);
  const east = parseOptionalNumber(item.east);
  const west = parseOptionalNumber(item.west);
  if (north !== undefined) {
    row.north = north;
  }
  if (south !== undefined) {
    row.south = south;
  }
  if (east !== undefined) {
    row.east = east;
  }
  if (west !== undefined) {
    row.west = west;
  }
  return row;
}

export function savedSearchToAraHref(row: SavedSearch): string {
  const viewport =
    row.north !== undefined &&
    row.south !== undefined &&
    row.east !== undefined &&
    row.west !== undefined
      ? { north: row.north, south: row.south, east: row.east, west: row.west }
      : undefined;
  return buildAraHref({
    q: row.q,
    categoryId: row.categoryId,
    minPrice: row.minPrice,
    maxPrice: row.maxPrice,
    currency: row.currency,
    viewport,
  });
}

export function summarizeSavedSearch(row: SavedSearch): string {
  const parts: string[] = [];
  if (row.q) {
    parts.push(`Kelime: ${row.q}`);
  }
  if (row.categoryId) {
    parts.push("Kategori seçili");
  }
  if (row.minPrice || row.maxPrice) {
    const min = row.minPrice ?? "—";
    const max = row.maxPrice ?? "—";
    parts.push(`Fiyat: ${min}–${max}`);
  }
  if (row.currency) {
    parts.push(row.currency);
  }
  if (
    row.north !== undefined &&
    row.south !== undefined &&
    row.east !== undefined &&
    row.west !== undefined
  ) {
    parts.push("Harita alanı");
  }
  return parts.length === 0 ? "Tüm ilanlar" : parts.join(" · ");
}

export async function createSavedSearch(input: SavedSearchInput): Promise<SavedSearch> {
  const body: Record<string, unknown> = { name: input.name.trim() };
  const q = input.q?.trim();
  if (q) {
    body.q = q;
  }
  const categoryId = input.categoryId?.trim();
  if (categoryId) {
    body.categoryId = categoryId;
  }
  const minPrice = input.minPrice?.trim();
  if (minPrice) {
    body.minPrice = minPrice;
  }
  const maxPrice = input.maxPrice?.trim();
  if (maxPrice) {
    body.maxPrice = maxPrice;
  }
  const currency = input.currency?.trim();
  if (currency) {
    body.currency = currency;
  }
  if (input.viewport) {
    body.north = input.viewport.north;
    body.south = input.viewport.south;
    body.east = input.viewport.east;
    body.west = input.viewport.west;
  }
  const response = await apiFetch("/v1/saved-searches", {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify(body),
  });
  const parsed = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, parsed);
  }
  const row = parseSavedSearch(parsed);
  if (!row) {
    throw new SavedSearchClientError("generic", GENERIC);
  }
  return row;
}

export async function listSavedSearches(): Promise<SavedSearch[]> {
  const response = await apiFetch("/v1/saved-searches", { method: "GET" });
  const parsed = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, parsed);
  }
  if (typeof parsed !== "object" || parsed === null) {
    throw new SavedSearchClientError("generic", GENERIC);
  }
  const rows = (parsed as { savedSearches?: unknown }).savedSearches;
  if (!Array.isArray(rows)) {
    throw new SavedSearchClientError("generic", GENERIC);
  }
  const out: SavedSearch[] = [];
  for (const item of rows) {
    const row = parseSavedSearch(item);
    if (row) {
      out.push(row);
    }
  }
  return out;
}

export async function deleteSavedSearch(id: string): Promise<void> {
  const response = await apiFetch(`/v1/saved-searches/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: mutateHeaders(),
  });
  const parsed = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, parsed);
  }
}
