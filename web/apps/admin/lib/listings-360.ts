import {
  kindFromStatus,
  readErrorCode,
  readJson,
  type StaffOpKind,
} from "@/lib/moderation-http";
import { staffApiFetch } from "@/lib/api";

export type StaffListingOwner = {
  publicProfileId: string;
  displayName?: string;
};

export type StaffListingLocation = {
  latitude: number;
  longitude: number;
  catalogLocationId?: string;
};

export type StaffListingMedia = {
  assetId: string;
  order: number;
  width?: number;
  height?: number;
};

export type StaffListing = {
  listingId: string;
  title: string;
  description: string;
  categoryId: string;
  categorySchemaVersion: number;
  status: string;
  moderationState: string;
  createdAt: string;
  updatedAt: string;
  publishedAt?: string;
  archivedAt?: string;
  owner?: StaffListingOwner;
  location?: StaffListingLocation;
  media: StaffListingMedia[];
};

export type StaffListingSummary = {
  listingId: string;
  title: string;
  categoryId: string;
  status: string;
  moderationState: string;
  createdAt: string;
  updatedAt: string;
  owner?: StaffListingOwner;
};

export type StaffListingAccuracy = {
  reviewCount: number;
  average?: string;
};

export type OpsLoadKind = StaffOpKind;

export type OpsLoadResult<T> =
  | { ok: true; data: T }
  | { ok: false; kind: OpsLoadKind; status: number; message: string };

function messageFor(kind: OpsLoadKind, status: number, code: string | null, notFound: string): string {
  switch (kind) {
    case "unauthenticated":
      return "Management Center oturumu yok. Staff IAM kimliği doğrulanamadı.";
    case "forbidden":
      return "Bu işlem için yetkiniz yok.";
    case "not_found":
      return notFound;
    case "bad_request":
      return code ? `İstek reddedildi (${code}).` : "İstek geçersiz.";
    case "conflict":
      return code ? `İstek reddedildi (${code}).` : "İstek reddedildi.";
    default:
      return status === 0 ? "Ağ isteği başarısız oldu." : `İstek başarısız oldu (${status}).`;
  }
}

function fail<T>(status: number, body: unknown, notFound: string): OpsLoadResult<T> {
  const kind = kindFromStatus(status);
  return {
    ok: false,
    kind,
    status,
    message: messageFor(kind, status, readErrorCode(body), notFound),
  };
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function parseOwner(value: unknown): StaffListingOwner | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.publicProfileId !== "string" || row.publicProfileId.trim() === "") {
    return undefined;
  }
  return {
    publicProfileId: row.publicProfileId,
    displayName: optionalString(row.displayName),
  };
}

function parseLocation(value: unknown): StaffListingLocation | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.latitude !== "number" || typeof row.longitude !== "number") {
    return undefined;
  }
  return {
    latitude: row.latitude,
    longitude: row.longitude,
    catalogLocationId: optionalString(row.catalogLocationId),
  };
}

function parseMedia(value: unknown): StaffListingMedia[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const out: StaffListingMedia[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const row = item as Record<string, unknown>;
    if (typeof row.assetId !== "string" || typeof row.order !== "number") {
      continue;
    }
    const media: StaffListingMedia = { assetId: row.assetId, order: row.order };
    if (typeof row.width === "number") {
      media.width = row.width;
    }
    if (typeof row.height === "number") {
      media.height = row.height;
    }
    out.push(media);
  }
  return out;
}

function parseListing(value: unknown): StaffListing | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.listingId !== "string" || row.listingId.trim() === "") {
    return null;
  }
  if (
    typeof row.title !== "string" ||
    typeof row.categoryId !== "string" ||
    typeof row.categorySchemaVersion !== "number" ||
    typeof row.status !== "string" ||
    typeof row.moderationState !== "string" ||
    typeof row.createdAt !== "string" ||
    typeof row.updatedAt !== "string"
  ) {
    return null;
  }
  return {
    listingId: row.listingId,
    title: row.title,
    description: typeof row.description === "string" ? row.description : "",
    categoryId: row.categoryId,
    categorySchemaVersion: row.categorySchemaVersion,
    status: row.status,
    moderationState: row.moderationState,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
    publishedAt: optionalString(row.publishedAt),
    archivedAt: optionalString(row.archivedAt),
    owner: parseOwner(row.owner),
    location: parseLocation(row.location),
    media: parseMedia(row.media),
  };
}

function parseListingList(value: unknown): StaffListingSummary[] | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const rows = (value as { listings?: unknown }).listings;
  if (!Array.isArray(rows)) {
    return null;
  }
  const out: StaffListingSummary[] = [];
  for (const item of rows) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const row = item as Record<string, unknown>;
    if (
      typeof row.listingId !== "string" ||
      typeof row.title !== "string" ||
      typeof row.categoryId !== "string" ||
      typeof row.status !== "string" ||
      typeof row.moderationState !== "string" ||
      typeof row.createdAt !== "string" ||
      typeof row.updatedAt !== "string"
    ) {
      continue;
    }
    out.push({
      listingId: row.listingId,
      title: row.title,
      categoryId: row.categoryId,
      status: row.status,
      moderationState: row.moderationState,
      createdAt: row.createdAt,
      updatedAt: row.updatedAt,
      owner: parseOwner(row.owner),
    });
  }
  return out;
}

function parseAccuracy(value: unknown): StaffListingAccuracy | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const accuracy = (value as { listingAccuracy?: unknown }).listingAccuracy;
  if (!accuracy || typeof accuracy !== "object") {
    return null;
  }
  const row = accuracy as Record<string, unknown>;
  if (typeof row.reviewCount !== "number") {
    return null;
  }
  let average: string | undefined;
  if (row.average !== null && row.average !== undefined) {
    average = String(row.average);
  }
  return { reviewCount: row.reviewCount, average };
}

export async function fetchStaffListing(
  listingId: string,
  signal?: AbortSignal,
): Promise<OpsLoadResult<StaffListing>> {
  try {
    const res = await staffApiFetch(`/listings/${encodeURIComponent(listingId)}`, { signal });
    const body = await readJson(res);
    if (!res.ok) {
      return fail(res.status, body, "İlan bulunamadı.");
    }
    const data = parseListing(body);
    if (!data) {
      return fail(502, { error: "unavailable" }, "İlan bulunamadı.");
    }
    return { ok: true, data };
  } catch {
    return fail(0, null, "İlan bulunamadı.");
  }
}

export async function fetchOwnedListingsByOwner(
  ownerPublicProfileId: string,
  signal?: AbortSignal,
): Promise<OpsLoadResult<StaffListingSummary[]>> {
  try {
    const query = new URLSearchParams({ ownerPublicProfileId });
    const res = await staffApiFetch(`/listings?${query.toString()}`, { signal });
    const body = await readJson(res);
    if (!res.ok) {
      return fail(res.status, body, "İlan bulunamadı.");
    }
    const data = parseListingList(body);
    if (!data) {
      return fail(502, { error: "unavailable" }, "İlan bulunamadı.");
    }
    return { ok: true, data };
  } catch {
    return fail(0, null, "İlan bulunamadı.");
  }
}

export async function fetchListingAccuracy(
  listingId: string,
  signal?: AbortSignal,
): Promise<OpsLoadResult<StaffListingAccuracy>> {
  try {
    const res = await staffApiFetch(`/review-aggregates/listings/${encodeURIComponent(listingId)}`, {
      signal,
    });
    const body = await readJson(res);
    if (!res.ok) {
      return fail(res.status, body, "Değerlendirme özeti bulunamadı.");
    }
    const data = parseAccuracy(body);
    if (!data) {
      return fail(502, { error: "unavailable" }, "Değerlendirme özeti bulunamadı.");
    }
    return { ok: true, data };
  } catch {
    return fail(0, null, "Değerlendirme özeti bulunamadı.");
  }
}
