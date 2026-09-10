import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";

export type MediaAsset = {
  assetId: string;
  status: string;
  kind?: string;
  listingId?: string;
  originalFilename?: string;
};

export type UploadTarget = {
  assetId: string;
  uploadUrl: string;
  expiresAt: string;
  requiredHeaders: Record<string, string>;
  maxBytes: number;
};

export class MediaClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "MediaClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Görsel yükleme tamamlanamadı. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";
const FORBIDDEN = "Bu işlem için yetki doğrulanamadı. Lütfen sayfayı yenileyip tekrar deneyin.";
const CONFLICT = "Bu görsel şu anda kullanılamıyor veya zaten eklenmiş.";
const BAD_REQUEST = "Görsel bilgileri geçersiz. Lütfen kontrol edip tekrar deneyin.";
const NOT_FOUND = "Görsel bulunamadı.";
const TOO_LARGE = "Dosya boyutu izin verilen sınırı aşıyor.";
const NOT_IMAGE = "Yalnızca görsel dosyaları seçilebilir.";

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

function errorFromResponse(status: number, body: unknown): MediaClientError {
  const code = errorCodeFromBody(body);
  if (status === 401 || code === "unauthenticated") {
    return new MediaClientError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 403 || code === "forbidden") {
    return new MediaClientError("forbidden", FORBIDDEN);
  }
  if (status === 404 || code === "not_found") {
    return new MediaClientError("not_found", NOT_FOUND);
  }
  if (status === 409 || code === "conflict") {
    return new MediaClientError("conflict", CONFLICT);
  }
  if (status === 400 || code === "bad_request") {
    return new MediaClientError("bad_request", BAD_REQUEST);
  }
  if (status === 503 || code === "unavailable") {
    return new MediaClientError("unavailable", UNAVAILABLE);
  }
  return new MediaClientError("generic", GENERIC);
}

function parseUploadTarget(body: unknown): UploadTarget | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.assetId !== "string" || raw.assetId === "") {
    return null;
  }
  if (typeof raw.uploadUrl !== "string" || raw.uploadUrl === "") {
    return null;
  }
  if (typeof raw.expiresAt !== "string") {
    return null;
  }
  const requiredHeaders: Record<string, string> = {};
  if (typeof raw.requiredHeaders === "object" && raw.requiredHeaders !== null && !Array.isArray(raw.requiredHeaders)) {
    for (const [key, value] of Object.entries(raw.requiredHeaders as Record<string, unknown>)) {
      if (typeof value === "string") {
        requiredHeaders[key] = value;
      }
    }
  }
  const maxBytes = typeof raw.maxBytes === "number" ? raw.maxBytes : 0;
  return {
    assetId: raw.assetId,
    uploadUrl: raw.uploadUrl,
    expiresAt: raw.expiresAt,
    requiredHeaders,
    maxBytes,
  };
}

function parseAsset(body: unknown): MediaAsset | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.assetId !== "string" || raw.assetId === "") {
    return null;
  }
  if (typeof raw.status !== "string" || raw.status === "") {
    return null;
  }
  const asset: MediaAsset = {
    assetId: raw.assetId,
    status: raw.status,
  };
  if (typeof raw.kind === "string") {
    asset.kind = raw.kind;
  }
  if (typeof raw.listingId === "string" && raw.listingId !== "") {
    asset.listingId = raw.listingId;
  }
  if (typeof raw.originalFilename === "string" && raw.originalFilename !== "") {
    asset.originalFilename = raw.originalFilename;
  }
  return asset;
}

export function isImageFile(file: File): boolean {
  return file.type.startsWith("image/");
}

export async function initiateListingImage(input: {
  originalFilename?: string;
}): Promise<UploadTarget> {
  const payload: Record<string, unknown> = {};
  if (input.originalFilename) {
    payload.originalFilename = input.originalFilename;
  }
  const response = await apiFetch("/v1/media/listing-images", {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify(payload),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const target = parseUploadTarget(body);
  if (!target) {
    throw new MediaClientError("generic", GENERIC);
  }
  return target;
}

export async function putListingImage(target: UploadTarget, file: File): Promise<void> {
  if (!isImageFile(file)) {
    throw new MediaClientError("bad_request", NOT_IMAGE);
  }
  if (target.maxBytes > 0 && file.size > target.maxBytes) {
    throw new MediaClientError("too_large", TOO_LARGE);
  }
  const headers = new Headers();
  for (const [key, value] of Object.entries(target.requiredHeaders)) {
    headers.set(key, value);
  }
  const response = await fetch(target.uploadUrl, {
    method: "PUT",
    headers,
    body: file,
    credentials: "omit",
  });
  if (!response.ok) {
    throw new MediaClientError("generic", GENERIC);
  }
}

export async function confirmListingImage(assetId: string): Promise<{ assetId: string; status: string }> {
  const response = await apiFetch(`/v1/media/listing-images/${encodeURIComponent(assetId)}/confirm`, {
    method: "POST",
    headers: mutateHeaders(),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new MediaClientError("generic", GENERIC);
  }
  const raw = body as { assetId?: unknown; status?: unknown };
  if (typeof raw.assetId !== "string" || typeof raw.status !== "string") {
    throw new MediaClientError("generic", GENERIC);
  }
  return { assetId: raw.assetId, status: raw.status };
}

export async function getListingImage(assetId: string): Promise<MediaAsset> {
  const response = await apiFetch(`/v1/media/listing-images/${encodeURIComponent(assetId)}`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const asset = parseAsset(body);
  if (!asset) {
    throw new MediaClientError("generic", GENERIC);
  }
  return asset;
}

const POLL_INTERVAL_MS = 2000;
const POLL_MAX_ATTEMPTS = 20;

export function isReadyListingImage(status: string): boolean {
  return status.toLowerCase() === "ready";
}

export function isRejectedListingImage(status: string): boolean {
  const normalized = status.toLowerCase();
  return normalized === "rejected" || normalized === "failed";
}

export function isProcessingListingImage(status: string): boolean {
  const normalized = status.toLowerCase();
  return normalized === "uploaded" || normalized === "processing" || normalized === "pending";
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

/** Confirm is not ready. Polls GET until ready, rejected, or the attempt bound. */
export async function pollListingImageUntilSettled(assetId: string): Promise<MediaAsset> {
  let last = await getListingImage(assetId);
  if (!isProcessingListingImage(last.status)) {
    return last;
  }
  for (let attempt = 1; attempt < POLL_MAX_ATTEMPTS; attempt++) {
    await delay(POLL_INTERVAL_MS);
    last = await getListingImage(assetId);
    if (!isProcessingListingImage(last.status)) {
      return last;
    }
  }
  return last;
}

export async function uploadListingImage(file: File): Promise<MediaAsset> {
  if (!isImageFile(file)) {
    throw new MediaClientError("bad_request", NOT_IMAGE);
  }
  const target = await initiateListingImage({ originalFilename: file.name });
  await putListingImage(target, file);
  const confirmed = await confirmListingImage(target.assetId);
  try {
    const latest = await getListingImage(confirmed.assetId);
    return {
      ...latest,
      originalFilename: latest.originalFilename || file.name,
    };
  } catch {
    return {
      assetId: confirmed.assetId,
      status: confirmed.status,
      originalFilename: file.name,
    };
  }
}
