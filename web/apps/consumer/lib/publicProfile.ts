import { apiFetch } from "@/lib/api";

export type PublicIdentityProfile = {
  publicProfileId: string;
  displayName: string | null;
  memberSince: string;
};

export class PublicProfileClientError extends Error {
  readonly code: "not_found" | "unavailable" | "generic";

  constructor(code: PublicProfileClientError["code"], message: string) {
    super(message);
    this.name = "PublicProfileClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const NOT_FOUND = "Profil bulunamadı.";

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

function errorFromResponse(status: number, body: unknown): PublicProfileClientError {
  const code = readErrorCode(body);
  if (status === 404 || code === "not_found") {
    return new PublicProfileClientError("not_found", NOT_FOUND);
  }
  if (status === 503 || code === "unavailable") {
    return new PublicProfileClientError("unavailable", UNAVAILABLE);
  }
  return new PublicProfileClientError("generic", GENERIC);
}

function parsePublicProfile(body: unknown): PublicIdentityProfile | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const row = body as Record<string, unknown>;
  if (typeof row.publicProfileId !== "string" || row.publicProfileId === "") {
    return null;
  }
  if (typeof row.memberSince !== "string" || row.memberSince === "") {
    return null;
  }
  let displayName: string | null = null;
  if (row.displayName === null || row.displayName === undefined) {
    displayName = null;
  } else if (typeof row.displayName === "string") {
    const trimmed = row.displayName.trim();
    displayName = trimmed === "" ? null : trimmed;
  } else {
    return null;
  }
  return {
    publicProfileId: row.publicProfileId,
    displayName,
    memberSince: row.memberSince,
  };
}

export async function getPublicProfile(publicProfileId: string): Promise<PublicIdentityProfile> {
  const response = await apiFetch(`/v1/public/profiles/${encodeURIComponent(publicProfileId)}`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const profile = parsePublicProfile(body);
  if (profile === null) {
    throw new PublicProfileClientError("generic", GENERIC);
  }
  return profile;
}
