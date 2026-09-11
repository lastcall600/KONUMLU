import { staffApiFetch } from "@/lib/api";
import {
  isAbortError,
  kindFromStatus,
  optionalString,
  readErrorCode,
  readJson,
  type StaffOpKind,
} from "@/lib/moderation-http";

export const APPEAL_STATUSES = [
  "submitted",
  "under_review",
  "accepted",
  "rejected",
  "withdrawn",
] as const;

export const STAFF_APPEAL_DECISIONS = ["under_review", "accepted", "rejected"] as const;

export const APPEAL_LIST_LIMIT = 100;

export type AppealStatus = (typeof APPEAL_STATUSES)[number];
export type StaffAppealDecision = (typeof STAFF_APPEAL_DECISIONS)[number];

export type StaffAppeal = {
  appealId: string;
  actionId: string;
  caseId: string;
  appellantUserId: string;
  statement: string;
  status: string;
  createdAt: string;
  updatedAt: string;
  decidedAt?: string;
  decidedByStaffId?: string;
};

export type StaffAppealList = {
  appeals: StaffAppeal[];
};

export type AppealLoadResult =
  | { ok: true; data: StaffAppealList }
  | { ok: false; kind: StaffOpKind; status: number; message: string };

export type AppealDetailResult =
  | { ok: true; data: StaffAppeal }
  | { ok: false; kind: StaffOpKind; status: number; message: string };

export function isStaffAppealDecision(value: string): value is StaffAppealDecision {
  return (STAFF_APPEAL_DECISIONS as readonly string[]).includes(value);
}

export function appealDecisionNeedsConfirm(status: string): boolean {
  return status === "accepted" || status === "rejected";
}

function parseStaffAppeal(value: unknown): StaffAppeal | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.appealId !== "string" || row.appealId.trim() === "") {
    return null;
  }
  if (typeof row.actionId !== "string" || typeof row.caseId !== "string") {
    return null;
  }
  if (typeof row.appellantUserId !== "string" || typeof row.statement !== "string") {
    return null;
  }
  if (typeof row.status !== "string") {
    return null;
  }
  if (typeof row.createdAt !== "string" || typeof row.updatedAt !== "string") {
    return null;
  }
  const out: StaffAppeal = {
    appealId: row.appealId,
    actionId: row.actionId,
    caseId: row.caseId,
    appellantUserId: row.appellantUserId,
    statement: row.statement,
    status: row.status,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  };
  const decidedAt = optionalString(row.decidedAt);
  if (decidedAt) out.decidedAt = decidedAt;
  const decidedByStaffId = optionalString(row.decidedByStaffId);
  if (decidedByStaffId) out.decidedByStaffId = decidedByStaffId;
  return out;
}

function messageFor(
  kind: StaffOpKind,
  status: number,
  code: string | null,
  verb: "load" | "decision",
): string {
  switch (kind) {
    case "unauthenticated":
      return "Management Center oturumu yok. Staff IAM kimliği doğrulanamadı.";
    case "forbidden":
      return verb === "decision"
        ? "Bu itiraz kararı için yetkiniz yok."
        : "Bu vakanın itirazlarını görüntüleme yetkiniz yok.";
    case "not_found":
      return "Vaka veya itiraz bulunamadı.";
    case "bad_request":
      return code ? `İstek reddedildi (${code}).` : "İstek geçersiz.";
    case "conflict":
      return code ? `İtiraz geçişi reddedildi (${code}).` : "İtiraz geçişi reddedildi.";
    default:
      return verb === "decision"
        ? `İtiraz kararı gönderilemedi (${status}).`
        : `İtirazlar yüklenemedi (${status}).`;
  }
}

function fail(
  status: number,
  payload: unknown,
  verb: "load" | "decision",
): { ok: false; kind: StaffOpKind; status: number; message: string } {
  const kind = kindFromStatus(status);
  const code = readErrorCode(payload);
  return { ok: false, kind, status, message: messageFor(kind, status, code, verb) };
}

export async function fetchCaseAppeals(
  caseId: string,
  signal?: AbortSignal,
): Promise<AppealLoadResult> {
  const id = caseId.trim();
  if (!id) {
    return { ok: false, kind: "bad_request", status: 400, message: "Vaka kimliği geçersiz." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(
      `/moderation/cases/${encodeURIComponent(id)}/appeals?limit=${APPEAL_LIST_LIMIT}`,
      { method: "GET", signal },
    );
  } catch (err) {
    if (isAbortError(err)) {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "İtiraz isteği başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "load");
  }
  if (!payload || typeof payload !== "object" || !Array.isArray((payload as StaffAppealList).appeals)) {
    return { ok: false, kind: "error", status: res.status, message: "İtiraz yanıtı geçersiz." };
  }

  const appeals: StaffAppeal[] = [];
  for (const row of (payload as StaffAppealList).appeals) {
    const parsed = parseStaffAppeal(row);
    if (!parsed) {
      return { ok: false, kind: "error", status: res.status, message: "İtiraz yanıtı geçersiz." };
    }
    appeals.push(parsed);
  }
  return { ok: true, data: { appeals } };
}

export async function fetchCaseAppeal(
  caseId: string,
  appealId: string,
  signal?: AbortSignal,
): Promise<AppealDetailResult> {
  const cid = caseId.trim();
  const aid = appealId.trim();
  if (!cid || !aid) {
    return { ok: false, kind: "bad_request", status: 400, message: "İtiraz kimliği geçersiz." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(
      `/moderation/cases/${encodeURIComponent(cid)}/appeals/${encodeURIComponent(aid)}`,
      { method: "GET", signal },
    );
  } catch (err) {
    if (isAbortError(err)) {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "İtiraz isteği başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "load");
  }
  const data = parseStaffAppeal(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "İtiraz yanıtı geçersiz." };
  }
  return { ok: true, data };
}

export async function transitionCaseAppeal(
  caseId: string,
  appealId: string,
  status: string,
): Promise<AppealDetailResult> {
  const cid = caseId.trim();
  const aid = appealId.trim();
  if (!cid || !aid) {
    return { ok: false, kind: "bad_request", status: 400, message: "İtiraz kimliği geçersiz." };
  }
  if (!isStaffAppealDecision(status)) {
    return { ok: false, kind: "bad_request", status: 400, message: "Geçerli bir karar seçin." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(
      `/moderation/cases/${encodeURIComponent(cid)}/appeals/${encodeURIComponent(aid)}/status`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      },
    );
  } catch {
    return { ok: false, kind: "error", status: 0, message: "İtiraz kararı gönderilemedi." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "decision");
  }
  const data = parseStaffAppeal(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "İtiraz kararı yanıtı geçersiz." };
  }
  return { ok: true, data };
}
