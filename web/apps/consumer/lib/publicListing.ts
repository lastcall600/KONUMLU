import { apiFetch } from "@/lib/api";

export type PublicListingMedia = {
  url: string;
  width?: number;
  height?: number;
};

export type PublicListingLocation = {
  latitude: number;
  longitude: number;
};

export type PublicListingSeller = {
  publicProfileId: string;
  displayName: string | null;
};

export type PublicListing = {
  listingId: string;
  categoryId: string;
  categorySchemaVersion: number;
  title: string;
  description: string;
  priceAmount?: string;
  priceCurrency?: string;
  attributes: Record<string, unknown>;
  location?: PublicListingLocation;
  media: PublicListingMedia[];
  seller?: PublicListingSeller;
  publishedAt?: string;
  updatedAt: string;
};

export class PublicListingClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "PublicListingClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const NOT_FOUND = "İlan bulunamadı veya artık yayında değil.";

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

function errorFromResponse(status: number, body: unknown): PublicListingClientError {
  const code = readErrorCode(body);
  if (status === 404 || code === "not_found") {
    return new PublicListingClientError("not_found", NOT_FOUND);
  }
  if (status === 503 || code === "unavailable") {
    return new PublicListingClientError("unavailable", UNAVAILABLE);
  }
  return new PublicListingClientError("generic", GENERIC);
}

function parseOptionalString(raw: unknown): string | undefined {
  if (typeof raw === "string" && raw !== "") {
    return raw;
  }
  return undefined;
}

function parseOptionalInt(raw: unknown): number | undefined {
  if (typeof raw === "number" && Number.isInteger(raw) && raw > 0) {
    return raw;
  }
  return undefined;
}

function parseLocation(raw: unknown): PublicListingLocation | undefined {
  if (typeof raw !== "object" || raw === null) {
    return undefined;
  }
  const item = raw as Record<string, unknown>;
  if (typeof item.latitude !== "number" || !Number.isFinite(item.latitude)) {
    return undefined;
  }
  if (typeof item.longitude !== "number" || !Number.isFinite(item.longitude)) {
    return undefined;
  }
  return { latitude: item.latitude, longitude: item.longitude };
}

function parseMedia(raw: unknown): PublicListingMedia[] | null {
  if (!Array.isArray(raw)) {
    return null;
  }
  const media: PublicListingMedia[] = [];
  for (const item of raw) {
    if (typeof item !== "object" || item === null) {
      continue;
    }
    const row = item as Record<string, unknown>;
    if (typeof row.url !== "string" || row.url === "") {
      continue;
    }
    const entry: PublicListingMedia = { url: row.url };
    const width = parseOptionalInt(row.width);
    if (width !== undefined) {
      entry.width = width;
    }
    const height = parseOptionalInt(row.height);
    if (height !== undefined) {
      entry.height = height;
    }
    media.push(entry);
  }
  return media;
}

function parseSeller(raw: unknown): PublicListingSeller | undefined {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return undefined;
  }
  const item = raw as Record<string, unknown>;
  if (typeof item.publicProfileId !== "string" || item.publicProfileId === "") {
    return undefined;
  }
  let displayName: string | null = null;
  if (item.displayName === null || item.displayName === undefined) {
    displayName = null;
  } else if (typeof item.displayName === "string") {
    const trimmed = item.displayName.trim();
    displayName = trimmed === "" ? null : trimmed;
  } else {
    displayName = null;
  }
  return { publicProfileId: item.publicProfileId, displayName };
}

function parsePublicListing(body: unknown): PublicListing | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.listingId !== "string" || raw.listingId === "") {
    return null;
  }
  if (typeof raw.categoryId !== "string" || raw.categoryId === "") {
    return null;
  }
  if (typeof raw.categorySchemaVersion !== "number" || !Number.isInteger(raw.categorySchemaVersion)) {
    return null;
  }
  if (typeof raw.title !== "string" || typeof raw.description !== "string") {
    return null;
  }
  if (typeof raw.updatedAt !== "string" || raw.updatedAt === "") {
    return null;
  }
  const media = parseMedia(raw.media);
  if (media === null) {
    return null;
  }
  const listing: PublicListing = {
    listingId: raw.listingId,
    categoryId: raw.categoryId,
    categorySchemaVersion: raw.categorySchemaVersion,
    title: raw.title,
    description: raw.description,
    attributes:
      typeof raw.attributes === "object" && raw.attributes !== null && !Array.isArray(raw.attributes)
        ? (raw.attributes as Record<string, unknown>)
        : {},
    media,
    updatedAt: raw.updatedAt,
  };
  const priceAmount = parseOptionalString(raw.priceAmount);
  if (priceAmount) {
    listing.priceAmount = priceAmount;
  }
  const priceCurrency = parseOptionalString(raw.priceCurrency);
  if (priceCurrency) {
    listing.priceCurrency = priceCurrency;
  }
  const location = parseLocation(raw.location);
  if (location) {
    listing.location = location;
  }
  const publishedAt = parseOptionalString(raw.publishedAt);
  if (publishedAt) {
    listing.publishedAt = publishedAt;
  }
  const seller = parseSeller(raw.seller);
  if (seller) {
    listing.seller = seller;
  }
  return listing;
}

export async function getPublicListing(listingId: string): Promise<PublicListing> {
  const response = await apiFetch(`/v1/public/listings/${encodeURIComponent(listingId)}`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const listing = parsePublicListing(body);
  if (!listing) {
    throw new PublicListingClientError("generic", GENERIC);
  }
  return listing;
}
