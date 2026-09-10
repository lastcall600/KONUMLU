import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";

export type AppointmentStatus =
  | "requested"
  | "accepted"
  | "rejected"
  | "cancelled"
  | "completed"
  | "no_show";

export type VerificationMethod = "qr" | "otp";

export type FlowInteractionType = "transaction" | "delivery";

export const FLOW_INTERACTION_TRANSACTION: FlowInteractionType = "transaction";
export const FLOW_INTERACTION_DELIVERY: FlowInteractionType = "delivery";

export type Appointment = {
  appointmentId: string;
  listingId: string;
  requesterUserId: string;
  providerUserId: string;
  status: AppointmentStatus;
  requestedAt: string;
  scheduledAt?: string;
  updatedAt: string;
  verifiedInteractionId: string | null;
};

export type IssuedChallenge = {
  challengeId: string;
  appointmentId?: string;
  flowId?: string;
  method: VerificationMethod;
  token: string;
  expiresAt: string;
  qrPayload?: string;
  interactionType?: string;
};

export type VerifiedInteraction = {
  interactionId: string;
  appointmentId?: string;
  flowId?: string;
  listingId: string;
  requesterUserId: string;
  providerUserId: string;
  interactionType: string;
  verificationMethod: string;
  verifiedAt: string;
};

export type VerificationFlowStatus = "open" | "completed";

export type VerificationFlow = {
  flowId: string;
  listingId: string;
  interactionType: string;
  status: VerificationFlowStatus;
  createdAt: string;
  completedInteractionId: string | null;
};

export class VerifiedClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "VerifiedClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const BAD_REQUEST = "İstek veya jeton geçersiz.";
const NOT_FOUND = "Kayıt bulunamadı veya bu işlem için yetkiniz yok.";
const CONFLICT = "Bu işlem şu anda yapılamıyor.";
const UNAUTHENTICATED = "Oturum gerekli. Lütfen giriş yapın.";

function mutateHeaders(): Headers {
  const headers = new Headers();
  headers.set("Content-Type", "application/json");
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

function errorFromResponse(status: number, body: unknown): VerifiedClientError {
  const code = readErrorCode(body);
  if (status === 401 || code === "unauthenticated") {
    return new VerifiedClientError("unauthenticated", UNAUTHENTICATED);
  }
  if (status === 400 || code === "bad_request") {
    return new VerifiedClientError("bad_request", BAD_REQUEST);
  }
  if (status === 404 || status === 403 || code === "not_found" || code === "forbidden") {
    return new VerifiedClientError("not_found", NOT_FOUND);
  }
  if (status === 409 || code === "conflict") {
    return new VerifiedClientError("conflict", CONFLICT);
  }
  if (status === 503 || code === "unavailable") {
    return new VerifiedClientError("unavailable", UNAVAILABLE);
  }
  return new VerifiedClientError("generic", GENERIC);
}

function parseStatus(raw: unknown): AppointmentStatus | null {
  if (typeof raw !== "string") {
    return null;
  }
  switch (raw) {
    case "requested":
    case "accepted":
    case "rejected":
    case "cancelled":
    case "completed":
    case "no_show":
      return raw;
    default:
      return null;
  }
}

function parseMethod(raw: unknown): VerificationMethod | null {
  if (raw === "qr" || raw === "otp") {
    return raw;
  }
  return null;
}

function parseAppointment(body: unknown): Appointment | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.appointmentId !== "string" || raw.appointmentId === "") {
    return null;
  }
  if (typeof raw.listingId !== "string" || raw.listingId === "") {
    return null;
  }
  if (typeof raw.requesterUserId !== "string" || raw.requesterUserId === "") {
    return null;
  }
  if (typeof raw.providerUserId !== "string" || raw.providerUserId === "") {
    return null;
  }
  const status = parseStatus(raw.status);
  if (status === null) {
    return null;
  }
  if (typeof raw.requestedAt !== "string" || raw.requestedAt === "") {
    return null;
  }
  if (typeof raw.updatedAt !== "string" || raw.updatedAt === "") {
    return null;
  }
  const row: Appointment = {
    appointmentId: raw.appointmentId,
    listingId: raw.listingId,
    requesterUserId: raw.requesterUserId,
    providerUserId: raw.providerUserId,
    status,
    requestedAt: raw.requestedAt,
    updatedAt: raw.updatedAt,
    verifiedInteractionId:
      typeof raw.verifiedInteractionId === "string" && raw.verifiedInteractionId !== ""
        ? raw.verifiedInteractionId
        : null,
  };
  if (typeof raw.scheduledAt === "string" && raw.scheduledAt !== "") {
    row.scheduledAt = raw.scheduledAt;
  }
  return row;
}

function parseIssuedChallenge(body: unknown): IssuedChallenge | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.challengeId !== "string" || raw.challengeId === "") {
    return null;
  }
  const appointmentId =
    typeof raw.appointmentId === "string" && raw.appointmentId !== "" ? raw.appointmentId : undefined;
  const flowId = typeof raw.flowId === "string" && raw.flowId !== "" ? raw.flowId : undefined;
  if (appointmentId === undefined && flowId === undefined) {
    return null;
  }
  const method = parseMethod(raw.method);
  if (method === null) {
    return null;
  }
  if (typeof raw.token !== "string" || raw.token === "") {
    return null;
  }
  if (typeof raw.expiresAt !== "string" || raw.expiresAt === "") {
    return null;
  }
  const issued: IssuedChallenge = {
    challengeId: raw.challengeId,
    method,
    token: raw.token,
    expiresAt: raw.expiresAt,
  };
  if (appointmentId !== undefined) {
    issued.appointmentId = appointmentId;
  }
  if (flowId !== undefined) {
    issued.flowId = flowId;
  }
  if (typeof raw.qrPayload === "string" && raw.qrPayload !== "") {
    issued.qrPayload = raw.qrPayload;
  }
  if (typeof raw.interactionType === "string" && raw.interactionType !== "") {
    issued.interactionType = raw.interactionType;
  }
  return issued;
}

export function qrPayloadForChallenge(issued: IssuedChallenge): string | null {
  if (issued.method !== "qr") {
    return null;
  }
  if (typeof issued.qrPayload === "string" && issued.qrPayload !== "") {
    return issued.qrPayload;
  }
  if (issued.token !== "") {
    return issued.token;
  }
  return null;
}

export const VERIFIED_OTP_DIGITS = 6;

export function digitsOnly(value: string): string {
  return value.replace(/\D/g, "");
}

function parseInteraction(body: unknown): VerifiedInteraction | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.interactionId !== "string" || raw.interactionId === "") {
    return null;
  }
  const appointmentId =
    typeof raw.appointmentId === "string" && raw.appointmentId !== "" ? raw.appointmentId : undefined;
  const flowId = typeof raw.flowId === "string" && raw.flowId !== "" ? raw.flowId : undefined;
  if (typeof raw.listingId !== "string" || raw.listingId === "") {
    return null;
  }
  if (typeof raw.requesterUserId !== "string" || raw.requesterUserId === "") {
    return null;
  }
  if (typeof raw.providerUserId !== "string" || raw.providerUserId === "") {
    return null;
  }
  if (typeof raw.interactionType !== "string" || raw.interactionType === "") {
    return null;
  }
  if (typeof raw.verificationMethod !== "string" || raw.verificationMethod === "") {
    return null;
  }
  if (typeof raw.verifiedAt !== "string" || raw.verifiedAt === "") {
    return null;
  }
  const row: VerifiedInteraction = {
    interactionId: raw.interactionId,
    listingId: raw.listingId,
    requesterUserId: raw.requesterUserId,
    providerUserId: raw.providerUserId,
    interactionType: raw.interactionType,
    verificationMethod: raw.verificationMethod,
    verifiedAt: raw.verifiedAt,
  };
  if (appointmentId !== undefined) {
    row.appointmentId = appointmentId;
  }
  if (flowId !== undefined) {
    row.flowId = flowId;
  }
  return row;
}

export function flowInteractionTypeLabel(type: string): string {
  switch (type) {
    case FLOW_INTERACTION_TRANSACTION:
      return "İşlem Doğrulaması";
    case FLOW_INTERACTION_DELIVERY:
      return "Teslimat Doğrulaması";
    default:
      return "Doğrulama";
  }
}

export function appointmentStatusLabel(status: AppointmentStatus): string {
  switch (status) {
    case "requested":
      return "Talep edildi";
    case "accepted":
      return "Kabul edildi";
    case "rejected":
      return "Reddedildi";
    case "cancelled":
      return "İptal edildi";
    case "completed":
      return "Tamamlandı";
    case "no_show":
      return "Gelinmedi";
  }
}

export async function createAppointment(listingId: string, scheduledAt: string): Promise<Appointment> {
  const response = await apiFetch("/v1/verified/appointments", {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify({ listingId, scheduledAt }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const row = parseAppointment(body);
  if (!row) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  return row;
}

export async function listAppointments(): Promise<Appointment[]> {
  const response = await apiFetch("/v1/verified/appointments", { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  const rows = (body as { appointments?: unknown }).appointments;
  if (!Array.isArray(rows)) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  const out: Appointment[] = [];
  for (const item of rows) {
    const parsed = parseAppointment(item);
    if (parsed) {
      out.push(parsed);
    }
  }
  return out;
}

export async function acceptAppointment(appointmentId: string): Promise<Appointment> {
  return mutateAppointment(appointmentId, "accept");
}

export async function rejectAppointment(appointmentId: string): Promise<Appointment> {
  return mutateAppointment(appointmentId, "reject");
}

export async function cancelAppointment(appointmentId: string): Promise<Appointment> {
  return mutateAppointment(appointmentId, "cancel");
}

async function mutateAppointment(
  appointmentId: string,
  action: "accept" | "reject" | "cancel",
): Promise<Appointment> {
  const response = await apiFetch(
    `/v1/verified/appointments/${encodeURIComponent(appointmentId)}/${action}`,
    {
      method: "POST",
      headers: mutateHeaders(),
      body: JSON.stringify({}),
    },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const row = parseAppointment(body);
  if (!row) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  return row;
}

export async function startVerification(
  appointmentId: string,
  method: VerificationMethod,
): Promise<IssuedChallenge> {
  const response = await apiFetch(
    `/v1/verified/appointments/${encodeURIComponent(appointmentId)}/verification/start`,
    {
      method: "POST",
      headers: mutateHeaders(),
      body: JSON.stringify({ method }),
    },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const issued = parseIssuedChallenge(body);
  if (!issued) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  return issued;
}

export async function startTransactionVerification(
  listingId: string,
  requesterUserId: string,
  method: VerificationMethod,
): Promise<IssuedChallenge> {
  return startFlowVerification("/v1/verified/transaction/verification/start", listingId, requesterUserId, method);
}

export async function startDeliveryVerification(
  listingId: string,
  requesterUserId: string,
  method: VerificationMethod,
): Promise<IssuedChallenge> {
  return startFlowVerification("/v1/verified/delivery/verification/start", listingId, requesterUserId, method);
}

async function startFlowVerification(
  path: string,
  listingId: string,
  requesterUserId: string,
  method: VerificationMethod,
): Promise<IssuedChallenge> {
  const response = await apiFetch(path, {
    method: "POST",
    headers: mutateHeaders(),
    body: JSON.stringify({ listingId, requesterUserId, method }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const issued = parseIssuedChallenge(body);
  if (!issued) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  return issued;
}

export async function finishFlowVerification(flowId: string, token: string): Promise<VerifiedInteraction> {
  const response = await apiFetch(
    `/v1/verified/verification-flows/${encodeURIComponent(flowId)}/verification/finish`,
    {
      method: "POST",
      headers: mutateHeaders(),
      body: JSON.stringify({ token }),
    },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const row = parseInteraction(body);
  if (!row) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  return row;
}

function parseFlowStatus(raw: unknown): VerificationFlowStatus | null {
  if (raw === "open" || raw === "completed") {
    return raw;
  }
  return null;
}

function parseVerificationFlow(body: unknown): VerificationFlow | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.flowId !== "string" || raw.flowId === "") {
    return null;
  }
  if (typeof raw.listingId !== "string" || raw.listingId === "") {
    return null;
  }
  if (typeof raw.interactionType !== "string" || raw.interactionType === "") {
    return null;
  }
  const status = parseFlowStatus(raw.status);
  if (status === null) {
    return null;
  }
  if (typeof raw.createdAt !== "string" || raw.createdAt === "") {
    return null;
  }
  return {
    flowId: raw.flowId,
    listingId: raw.listingId,
    interactionType: raw.interactionType,
    status,
    createdAt: raw.createdAt,
    completedInteractionId:
      typeof raw.completedInteractionId === "string" && raw.completedInteractionId !== ""
        ? raw.completedInteractionId
        : null,
  };
}

export async function listVerificationFlows(listingId?: string): Promise<VerificationFlow[]> {
  const query =
    listingId !== undefined && listingId !== ""
      ? `?listingId=${encodeURIComponent(listingId)}`
      : "";
  const response = await apiFetch(`/v1/verified/verification-flows${query}`, { method: "GET" });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  if (typeof body !== "object" || body === null) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  const rows = (body as { flows?: unknown }).flows;
  if (!Array.isArray(rows)) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  const out: VerificationFlow[] = [];
  for (const item of rows) {
    const parsed = parseVerificationFlow(item);
    if (parsed) {
      out.push(parsed);
    }
  }
  return out;
}

export async function getVerificationFlow(flowId: string): Promise<VerificationFlow> {
  const response = await apiFetch(`/v1/verified/verification-flows/${encodeURIComponent(flowId)}`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const row = parseVerificationFlow(body);
  if (!row) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  return row;
}

export async function getVerifiedInteraction(interactionId: string): Promise<VerifiedInteraction> {
  const response = await apiFetch(`/v1/verified/interactions/${encodeURIComponent(interactionId)}`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const row = parseInteraction(body);
  if (!row) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  return row;
}

export async function finishVerification(
  appointmentId: string,
  token: string,
): Promise<VerifiedInteraction> {
  const response = await apiFetch(
    `/v1/verified/appointments/${encodeURIComponent(appointmentId)}/verification/finish`,
    {
      method: "POST",
      headers: mutateHeaders(),
      body: JSON.stringify({ token }),
    },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body);
  }
  const row = parseInteraction(body);
  if (!row) {
    throw new VerifiedClientError("generic", GENERIC);
  }
  return row;
}

export function scheduledAtFromDatetimeLocal(value: string): string | null {
  const trimmed = value.trim();
  if (trimmed === "") {
    return null;
  }
  const parsed = new Date(trimmed);
  if (!Number.isFinite(parsed.getTime())) {
    return null;
  }
  return parsed.toISOString();
}
