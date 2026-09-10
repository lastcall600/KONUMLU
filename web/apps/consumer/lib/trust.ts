import { apiFetch } from "@/lib/api";

export type TrustLevel = "new" | "verified" | "established";

export type TrustPassport = {
  level: TrustLevel;
  verifiedInteractionCount: number;
  requesterVerifiedInteractionCount: number;
  providerVerifiedInteractionCount: number;
  lastVerifiedInteractionAt?: string;
  verifiedReviewCount: number;
  providerServiceReviewCount: number;
  providerServiceAverage: number | null;
  lastVerifiedReviewAt?: string;
};

export class TrustClientError extends Error {
  readonly code: "unauthenticated" | "unavailable" | "generic";

  constructor(code: TrustClientError["code"], message: string) {
    super(message);
    this.name = "TrustClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";

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

function errorFromResponse(status: number, body: unknown): TrustClientError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new TrustClientError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 503 || code === "unavailable") {
    return new TrustClientError("unavailable", UNAVAILABLE);
  }
  return new TrustClientError("generic", GENERIC);
}

function parseLevel(raw: unknown): TrustLevel | null {
  if (raw === "new" || raw === "verified" || raw === "established") {
    return raw;
  }
  return null;
}

function parseCount(raw: unknown): number | null {
  if (typeof raw !== "number" || !Number.isFinite(raw) || !Number.isInteger(raw) || raw < 0) {
    return null;
  }
  return raw;
}

function parseNullableAverage(raw: unknown): number | null | undefined {
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

export function formatProviderServiceAverage(value: number): string {
  const rounded = Math.round(value * 10) / 10;
  if (!Number.isFinite(rounded)) {
    return "";
  }
  if (Number.isInteger(rounded)) {
    return String(rounded);
  }
  return rounded.toFixed(1);
}

function parsePassport(body: unknown): TrustPassport | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const row = body as Record<string, unknown>;
  const level = parseLevel(row.level);
  const verifiedInteractionCount = parseCount(row.verifiedInteractionCount);
  const requesterVerifiedInteractionCount = parseCount(row.requesterVerifiedInteractionCount);
  const providerVerifiedInteractionCount = parseCount(row.providerVerifiedInteractionCount);
  if (
    level === null ||
    verifiedInteractionCount === null ||
    requesterVerifiedInteractionCount === null ||
    providerVerifiedInteractionCount === null
  ) {
    return null;
  }
  const lastRaw = row.lastVerifiedInteractionAt;
  let lastVerifiedInteractionAt: string | undefined;
  if (lastRaw === null || lastRaw === undefined) {
    lastVerifiedInteractionAt = undefined;
  } else if (typeof lastRaw === "string" && lastRaw !== "") {
    lastVerifiedInteractionAt = lastRaw;
  } else {
    return null;
  }
  const verifiedReviewCount = parseCount(row.verifiedReviewCount);
  const providerServiceReviewCount = parseCount(row.providerServiceReviewCount);
  const providerServiceAverage = parseNullableAverage(row.providerServiceAverage);
  if (
    verifiedReviewCount === null ||
    providerServiceReviewCount === null ||
    providerServiceAverage === undefined
  ) {
    return null;
  }
  const lastReviewRaw = row.lastVerifiedReviewAt;
  let lastVerifiedReviewAt: string | undefined;
  if (lastReviewRaw === null || lastReviewRaw === undefined) {
    lastVerifiedReviewAt = undefined;
  } else if (typeof lastReviewRaw === "string" && lastReviewRaw !== "") {
    lastVerifiedReviewAt = lastReviewRaw;
  } else {
    return null;
  }
  return {
    level,
    verifiedInteractionCount,
    requesterVerifiedInteractionCount,
    providerVerifiedInteractionCount,
    lastVerifiedInteractionAt,
    verifiedReviewCount,
    providerServiceReviewCount,
    providerServiceAverage,
    lastVerifiedReviewAt,
  };
}

export function trustLevelLabel(level: TrustLevel): string {
  switch (level) {
    case "new":
      return "Yeni";
    case "verified":
      return "Doğrulanmış";
    case "established":
      return "Yerleşik Güven";
  }
}

export function isTrustZeroState(passport: TrustPassport): boolean {
  return passport.level === "new" && passport.verifiedInteractionCount === 0;
}

export function isReviewSignalsZeroState(passport: TrustPassport): boolean {
  return passport.verifiedReviewCount === 0 && passport.providerServiceReviewCount === 0;
}

export async function getMyTrustPassport(): Promise<TrustPassport> {
  const response = await apiFetch("/v1/trust/me", { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const passport = parsePassport(body);
  if (passport === null) {
    throw new TrustClientError("generic", GENERIC);
  }
  return passport;
}

export type PublicTrustPassport = {
  level: TrustLevel;
  verifiedInteractionCount: number;
  providerVerifiedInteractionCount: number;
  providerServiceReviewCount: number;
  providerServiceAverage: number | null;
  lastVerifiedInteractionAt?: string;
};

export function isPublicTrustZeroState(passport: PublicTrustPassport): boolean {
  return passport.level === "new" && passport.verifiedInteractionCount === 0;
}

function parsePublicPassport(body: unknown): PublicTrustPassport | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const row = body as Record<string, unknown>;
  const level = parseLevel(row.level);
  const verifiedInteractionCount = parseCount(row.verifiedInteractionCount);
  const providerVerifiedInteractionCount = parseCount(row.providerVerifiedInteractionCount);
  const providerServiceReviewCount = parseCount(row.providerServiceReviewCount);
  const providerServiceAverage = parseNullableAverage(row.providerServiceAverage);
  if (
    level === null ||
    verifiedInteractionCount === null ||
    providerVerifiedInteractionCount === null ||
    providerServiceReviewCount === null ||
    providerServiceAverage === undefined
  ) {
    return null;
  }
  const lastRaw = row.lastVerifiedInteractionAt;
  let lastVerifiedInteractionAt: string | undefined;
  if (lastRaw === null || lastRaw === undefined) {
    lastVerifiedInteractionAt = undefined;
  } else if (typeof lastRaw === "string" && lastRaw !== "") {
    lastVerifiedInteractionAt = lastRaw;
  } else {
    return null;
  }
  return {
    level,
    verifiedInteractionCount,
    providerVerifiedInteractionCount,
    providerServiceReviewCount,
    providerServiceAverage,
    lastVerifiedInteractionAt,
  };
}

export async function getPublicTrustPassport(publicProfileId: string): Promise<PublicTrustPassport> {
  const response = await apiFetch(`/v1/public/profiles/${encodeURIComponent(publicProfileId)}/trust`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromPublicResponse(response.status, body);
  }
  const passport = parsePublicPassport(body);
  if (passport === null) {
    throw new TrustClientError("generic", GENERIC);
  }
  return passport;
}

function errorFromPublicResponse(status: number, body: unknown): TrustClientError {
  const code = readErrorCode(body);
  if (status === 503 || code === "unavailable") {
    return new TrustClientError("unavailable", UNAVAILABLE);
  }
  return new TrustClientError("generic", GENERIC);
}
