import {
  kindFromStatus,
  readErrorCode,
  readJson,
  type StaffOpKind,
} from "@/lib/moderation-http";
import { staffApiFetch } from "@/lib/api";

export type StaffProfile = {
  publicProfileId: string;
  displayName?: string;
  moderationState: string;
  memberSince: string;
  createdAt: string;
  updatedAt: string;
  accountEligible: boolean;
  disabled: boolean;
  deleted: boolean;
};

export type StaffTrustHistory = {
  interactionId: string;
  listingId: string;
  role: string;
  interactionType: string;
  verificationMethod: string;
  verifiedAt: string;
};

export type StaffTrust = {
  level: string;
  verifiedInteractionCount: number;
  requesterVerifiedInteractionCount: number;
  providerVerifiedInteractionCount: number;
  lastVerifiedInteractionAt?: string;
  verifiedReviewCount: number;
  providerServiceReviewCount: number;
  providerServiceAverage?: string;
  lastVerifiedReviewAt?: string;
  history: StaffTrustHistory[];
};

export type StaffOwnedListing = {
  listingId: string;
  title: string;
  categoryId: string;
  status: string;
  moderationState: string;
  createdAt: string;
  updatedAt: string;
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

function parseProfile(value: unknown): StaffProfile | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.publicProfileId !== "string" || row.publicProfileId.trim() === "") {
    return null;
  }
  if (typeof row.moderationState !== "string") {
    return null;
  }
  if (
    typeof row.memberSince !== "string" ||
    typeof row.createdAt !== "string" ||
    typeof row.updatedAt !== "string"
  ) {
    return null;
  }
  if (typeof row.accountEligible !== "boolean" || typeof row.disabled !== "boolean" || typeof row.deleted !== "boolean") {
    return null;
  }
  return {
    publicProfileId: row.publicProfileId,
    displayName: optionalString(row.displayName),
    moderationState: row.moderationState,
    memberSince: row.memberSince,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
    accountEligible: row.accountEligible,
    disabled: row.disabled,
    deleted: row.deleted,
  };
}

function parseHistory(value: unknown): StaffTrustHistory[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const out: StaffTrustHistory[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const row = item as Record<string, unknown>;
    if (
      typeof row.interactionId !== "string" ||
      typeof row.listingId !== "string" ||
      typeof row.role !== "string" ||
      typeof row.interactionType !== "string" ||
      typeof row.verificationMethod !== "string" ||
      typeof row.verifiedAt !== "string"
    ) {
      continue;
    }
    out.push({
      interactionId: row.interactionId,
      listingId: row.listingId,
      role: row.role,
      interactionType: row.interactionType,
      verificationMethod: row.verificationMethod,
      verifiedAt: row.verifiedAt,
    });
  }
  return out;
}

function parseTrust(value: unknown): StaffTrust | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.level !== "string") {
    return null;
  }
  if (
    typeof row.verifiedInteractionCount !== "number" ||
    typeof row.requesterVerifiedInteractionCount !== "number" ||
    typeof row.providerVerifiedInteractionCount !== "number" ||
    typeof row.verifiedReviewCount !== "number" ||
    typeof row.providerServiceReviewCount !== "number"
  ) {
    return null;
  }
  let average: string | undefined;
  if (row.providerServiceAverage !== null && row.providerServiceAverage !== undefined) {
    average = String(row.providerServiceAverage);
  }
  return {
    level: row.level,
    verifiedInteractionCount: row.verifiedInteractionCount,
    requesterVerifiedInteractionCount: row.requesterVerifiedInteractionCount,
    providerVerifiedInteractionCount: row.providerVerifiedInteractionCount,
    lastVerifiedInteractionAt: optionalString(row.lastVerifiedInteractionAt),
    verifiedReviewCount: row.verifiedReviewCount,
    providerServiceReviewCount: row.providerServiceReviewCount,
    providerServiceAverage: average,
    lastVerifiedReviewAt: optionalString(row.lastVerifiedReviewAt),
    history: parseHistory(row.history),
  };
}

function parseOwnedListings(value: unknown): StaffOwnedListing[] | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const rows = (value as { listings?: unknown }).listings;
  if (!Array.isArray(rows)) {
    return null;
  }
  const out: StaffOwnedListing[] = [];
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
    });
  }
  return out;
}

export async function fetchStaffProfile(
  publicProfileId: string,
  signal?: AbortSignal,
): Promise<OpsLoadResult<StaffProfile>> {
  try {
    const res = await staffApiFetch(`/identity/profiles/${encodeURIComponent(publicProfileId)}`, { signal });
    const body = await readJson(res);
    if (!res.ok) {
      return fail(res.status, body, "Kullanıcı profili bulunamadı.");
    }
    const data = parseProfile(body);
    if (!data) {
      return fail(502, { error: "unavailable" }, "Kullanıcı profili bulunamadı.");
    }
    return { ok: true, data };
  } catch {
    return fail(0, null, "Kullanıcı profili bulunamadı.");
  }
}

export async function fetchStaffTrust(
  publicProfileId: string,
  signal?: AbortSignal,
): Promise<OpsLoadResult<StaffTrust>> {
  try {
    const res = await staffApiFetch(`/trust/profiles/${encodeURIComponent(publicProfileId)}`, { signal });
    const body = await readJson(res);
    if (!res.ok) {
      return fail(res.status, body, "Güven Pasaportu bulunamadı.");
    }
    const data = parseTrust(body);
    if (!data) {
      return fail(502, { error: "unavailable" }, "Güven Pasaportu bulunamadı.");
    }
    return { ok: true, data };
  } catch {
    return fail(0, null, "Güven Pasaportu bulunamadı.");
  }
}

export async function fetchOwnedListings(
  publicProfileId: string,
  signal?: AbortSignal,
): Promise<OpsLoadResult<StaffOwnedListing[]>> {
  try {
    const query = new URLSearchParams({ ownerPublicProfileId: publicProfileId });
    const res = await staffApiFetch(`/listings?${query.toString()}`, { signal });
    const body = await readJson(res);
    if (!res.ok) {
      return fail(res.status, body, "İlan bulunamadı.");
    }
    const data = parseOwnedListings(body);
    if (!data) {
      return fail(502, { error: "unavailable" }, "İlan bulunamadı.");
    }
    return { ok: true, data };
  } catch {
    return fail(0, null, "İlan bulunamadı.");
  }
}
