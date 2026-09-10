import { apiFetch } from "@/lib/api";

export type SearchListing = {
  listingId: string;
  categoryId: string;
  title: string;
  priceAmount?: string;
  priceCurrency?: string;
  latitude?: number;
  longitude?: number;
  publishedAt?: string;
};

export type SearchPage = {
  listings: SearchListing[];
  nextCursor?: string;
};

export type SearchViewport = {
  north: number;
  south: number;
  east: number;
  west: number;
};

export type SearchListingsInput = {
  q?: string;
  categoryId?: string;
  minPrice?: string;
  maxPrice?: string;
  currency?: string;
  cursor?: string;
  north?: string;
  south?: string;
  east?: string;
  west?: string;
  viewport?: SearchViewport;
};

export class SearchClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "SearchClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Arama tamamlanamadı. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const BAD_REQUEST = "Arama bilgileri geçersiz. Lütfen filtreleri kontrol edip tekrar deneyin.";

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

function errorFromResponse(status: number, body: unknown): SearchClientError {
  const code = readErrorCode(body);
  if (status === 503 || code === "unavailable") {
    return new SearchClientError("unavailable", UNAVAILABLE);
  }
  if (status === 400 || code === "bad_request") {
    return new SearchClientError("bad_request", BAD_REQUEST);
  }
  return new SearchClientError("generic", GENERIC);
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

export function hasValidCoordinates(listing: SearchListing): boolean {
  return (
    listing.latitude !== undefined &&
    listing.longitude !== undefined &&
    listing.latitude >= -90 &&
    listing.latitude <= 90 &&
    listing.longitude >= -180 &&
    listing.longitude <= 180
  );
}

export function isValidViewport(viewport: SearchViewport): boolean {
  return (
    Number.isFinite(viewport.north) &&
    Number.isFinite(viewport.south) &&
    Number.isFinite(viewport.east) &&
    Number.isFinite(viewport.west) &&
    viewport.north > viewport.south &&
    viewport.north <= 90 &&
    viewport.south >= -90 &&
    viewport.east >= -180 &&
    viewport.east <= 180 &&
    viewport.west >= -180 &&
    viewport.west <= 180
  );
}

function parseFiniteNumber(raw: string): number | undefined {
  if (raw.trim() === "") {
    return undefined;
  }
  const value = Number(raw);
  return Number.isFinite(value) ? value : undefined;
}

export function viewportFromSearchParams(params: URLSearchParams): SearchViewport | undefined {
  const north = parseFiniteNumber(params.get("north") ?? "");
  const south = parseFiniteNumber(params.get("south") ?? "");
  const east = parseFiniteNumber(params.get("east") ?? "");
  const west = parseFiniteNumber(params.get("west") ?? "");
  if (north === undefined || south === undefined || east === undefined || west === undefined) {
    return undefined;
  }
  const viewport = { north, south, east, west };
  return isValidViewport(viewport) ? viewport : undefined;
}

export function formatViewportParam(value: number): string {
  return value.toFixed(6);
}

function viewportQueryFields(input: SearchListingsInput): {
  north?: string;
  south?: string;
  east?: string;
  west?: string;
} {
  if (input.viewport && isValidViewport(input.viewport)) {
    return {
      north: formatViewportParam(input.viewport.north),
      south: formatViewportParam(input.viewport.south),
      east: formatViewportParam(input.viewport.east),
      west: formatViewportParam(input.viewport.west),
    };
  }
  return {
    north: input.north?.trim() || undefined,
    south: input.south?.trim() || undefined,
    east: input.east?.trim() || undefined,
    west: input.west?.trim() || undefined,
  };
}

function parseListing(raw: unknown): SearchListing | null {
  if (typeof raw !== "object" || raw === null) {
    return null;
  }
  const item = raw as Record<string, unknown>;
  if (typeof item.listingId !== "string" || item.listingId === "") {
    return null;
  }
  if (typeof item.categoryId !== "string" || item.categoryId === "") {
    return null;
  }
  if (typeof item.title !== "string") {
    return null;
  }
  const listing: SearchListing = {
    listingId: item.listingId,
    categoryId: item.categoryId,
    title: item.title,
  };
  const priceAmount = parseOptionalString(item.priceAmount);
  if (priceAmount) {
    listing.priceAmount = priceAmount;
  }
  const priceCurrency = parseOptionalString(item.priceCurrency);
  if (priceCurrency) {
    listing.priceCurrency = priceCurrency;
  }
  const latitude = parseOptionalNumber(item.latitude);
  if (latitude !== undefined) {
    listing.latitude = latitude;
  }
  const longitude = parseOptionalNumber(item.longitude);
  if (longitude !== undefined) {
    listing.longitude = longitude;
  }
  const publishedAt = parseOptionalString(item.publishedAt);
  if (publishedAt) {
    listing.publishedAt = publishedAt;
  }
  return listing;
}

export function buildSearchQueryString(input: SearchListingsInput): string {
  const params = new URLSearchParams();
  const q = input.q?.trim();
  if (q) {
    params.set("q", q);
  }
  const categoryId = input.categoryId?.trim();
  if (categoryId) {
    params.set("categoryId", categoryId);
  }
  const minPrice = input.minPrice?.trim();
  if (minPrice) {
    params.set("minPrice", minPrice);
  }
  const maxPrice = input.maxPrice?.trim();
  if (maxPrice) {
    params.set("maxPrice", maxPrice);
  }
  const currency = input.currency?.trim();
  if (currency) {
    params.set("currency", currency);
  }
  const cursor = input.cursor?.trim();
  if (cursor) {
    params.set("cursor", cursor);
  }
  const viewport = viewportQueryFields(input);
  if (viewport.north && viewport.south && viewport.east && viewport.west) {
    params.set("north", viewport.north);
    params.set("south", viewport.south);
    params.set("east", viewport.east);
    params.set("west", viewport.west);
  }
  const encoded = params.toString();
  return encoded === "" ? "" : `?${encoded}`;
}

export function buildAraHref(input: SearchListingsInput): string {
  const query = buildSearchQueryString({
    q: input.q,
    categoryId: input.categoryId,
    minPrice: input.minPrice,
    maxPrice: input.maxPrice,
    currency: input.currency,
    north: input.north,
    south: input.south,
    east: input.east,
    west: input.west,
    viewport: input.viewport,
  });
  return query === "" ? "/ara" : `/ara${query}`;
}

export async function searchListings(input: SearchListingsInput = {}): Promise<SearchPage> {
  const query = buildSearchQueryString(input);
  const response = await apiFetch(`/v1/search/listings${query}`, { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new SearchClientError("generic", GENERIC);
  }
  const list = (body as { listings?: unknown }).listings;
  if (!Array.isArray(list)) {
    throw new SearchClientError("generic", GENERIC);
  }
  const listings: SearchListing[] = [];
  for (const item of list) {
    const listing = parseListing(item);
    if (!listing) {
      throw new SearchClientError("generic", GENERIC);
    }
    listings.push(listing);
  }
  const page: SearchPage = { listings };
  const nextCursor = parseOptionalString((body as { nextCursor?: unknown }).nextCursor);
  if (nextCursor) {
    page.nextCursor = nextCursor;
  }
  return page;
}
