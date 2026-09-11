import { staffApiFetch } from "@/lib/api";
import {
  isAbortError,
  kindFromStatus,
  optionalString,
  readErrorCode,
  readJson,
  type StaffOpKind,
} from "@/lib/moderation-http";

export const ACTION_TYPES = ["no_action", "warning", "restrict", "suspend", "remove"] as const;
export const ACTION_STATUSES = ["proposed", "approved", "executed", "cancelled"] as const;
export const ACTION_REASON_CODES = [
  "no_violation",
  "policy_violation",
  "repeated_violation",
  "safety_risk",
  "prohibited_content",
  "other",
] as const;
export const ACTION_LIST_LIMIT = 100;

export type ActionType = (typeof ACTION_TYPES)[number];
export type ActionStatus = (typeof ACTION_STATUSES)[number];
export type ActionReasonCode = (typeof ACTION_REASON_CODES)[number];

export type StaffAction = {
  actionId: string;
  caseId: string;
  targetType: string;
  targetId: string;
  actionType: string;
  status: string;
  reasonCode: string;
  rationale?: string;
  actorStaffId?: string;
  createdAt: string;
  updatedAt: string;
};

export type StaffActionList = {
  actions: StaffAction[];
};

export type ActionLoadResult =
  | { ok: true; data: StaffActionList }
  | { ok: false; kind: StaffOpKind; status: number; message: string };

export type ActionDetailResult =
  | { ok: true; data: StaffAction }
  | { ok: false; kind: StaffOpKind; status: number; message: string };

export type CreateActionInput = {
  targetType: string;
  targetId: string;
  actionType: string;
  reasonCode: string;
  rationale: string;
};

export function isActionType(value: string): value is ActionType {
  return (ACTION_TYPES as readonly string[]).includes(value);
}

export function isActionStatus(value: string): value is ActionStatus {
  return (ACTION_STATUSES as readonly string[]).includes(value);
}

export function isActionReasonCode(value: string): value is ActionReasonCode {
  return (ACTION_REASON_CODES as readonly string[]).includes(value);
}

export function actionTransitionNeedsConfirm(status: string): boolean {
  return status === "executed" || status === "cancelled";
}

function parseStaffAction(value: unknown): StaffAction | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.actionId !== "string" || row.actionId.trim() === "") {
    return null;
  }
  if (typeof row.caseId !== "string" || typeof row.targetType !== "string") {
    return null;
  }
  if (typeof row.targetId !== "string" || typeof row.actionType !== "string") {
    return null;
  }
  if (typeof row.status !== "string" || typeof row.reasonCode !== "string") {
    return null;
  }
  if (typeof row.createdAt !== "string" || typeof row.updatedAt !== "string") {
    return null;
  }
  const out: StaffAction = {
    actionId: row.actionId,
    caseId: row.caseId,
    targetType: row.targetType,
    targetId: row.targetId,
    actionType: row.actionType,
    status: row.status,
    reasonCode: row.reasonCode,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  };
  const rationale = optionalString(row.rationale);
  if (rationale) out.rationale = rationale;
  const actorStaffId = optionalString(row.actorStaffId);
  if (actorStaffId) out.actorStaffId = actorStaffId;
  return out;
}

function messageFor(
  kind: StaffOpKind,
  status: number,
  code: string | null,
  verb: "load" | "create" | "transition",
): string {
  switch (kind) {
    case "unauthenticated":
      return "Management Center oturumu yok. Staff IAM kimliği doğrulanamadı.";
    case "forbidden":
      if (verb === "transition") {
        return "Bu aksiyon geçişi için yetkiniz yok.";
      }
      return verb === "create" ? "Aksiyon oluşturma yetkiniz yok." : "Bu vakanın aksiyonlarını görüntüleme yetkiniz yok.";
    case "not_found":
      return "Vaka veya aksiyon bulunamadı.";
    case "bad_request":
      return code ? `İstek reddedildi (${code}).` : "İstek geçersiz.";
    case "conflict":
      return code ? `Aksiyon geçişi reddedildi (${code}).` : "Aksiyon geçişi reddedildi.";
    default:
      if (verb === "create") {
        return `Aksiyon oluşturulamadı (${status}).`;
      }
      if (verb === "transition") {
        return `Aksiyon geçişi başarısız oldu (${status}).`;
      }
      return `Aksiyonlar yüklenemedi (${status}).`;
  }
}

function fail(
  status: number,
  payload: unknown,
  verb: "load" | "create" | "transition",
): { ok: false; kind: StaffOpKind; status: number; message: string } {
  const kind = kindFromStatus(status);
  const code = readErrorCode(payload);
  return { ok: false, kind, status, message: messageFor(kind, status, code, verb) };
}

export async function fetchCaseActions(
  caseId: string,
  signal?: AbortSignal,
): Promise<ActionLoadResult> {
  const id = caseId.trim();
  if (!id) {
    return { ok: false, kind: "bad_request", status: 400, message: "Vaka kimliği geçersiz." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(
      `/moderation/cases/${encodeURIComponent(id)}/actions?limit=${ACTION_LIST_LIMIT}`,
      { method: "GET", signal },
    );
  } catch (err) {
    if (isAbortError(err)) {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "Aksiyon isteği başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "load");
  }
  if (!payload || typeof payload !== "object" || !Array.isArray((payload as StaffActionList).actions)) {
    return { ok: false, kind: "error", status: res.status, message: "Aksiyon yanıtı geçersiz." };
  }

  const actions: StaffAction[] = [];
  for (const row of (payload as StaffActionList).actions) {
    const parsed = parseStaffAction(row);
    if (!parsed) {
      return { ok: false, kind: "error", status: res.status, message: "Aksiyon yanıtı geçersiz." };
    }
    actions.push(parsed);
  }
  return { ok: true, data: { actions } };
}

export async function fetchCaseAction(
  caseId: string,
  actionId: string,
  signal?: AbortSignal,
): Promise<ActionDetailResult> {
  const cid = caseId.trim();
  const aid = actionId.trim();
  if (!cid || !aid) {
    return { ok: false, kind: "bad_request", status: 400, message: "Aksiyon kimliği geçersiz." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(
      `/moderation/cases/${encodeURIComponent(cid)}/actions/${encodeURIComponent(aid)}`,
      { method: "GET", signal },
    );
  } catch (err) {
    if (isAbortError(err)) {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "Aksiyon isteği başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "load");
  }
  const data = parseStaffAction(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "Aksiyon yanıtı geçersiz." };
  }
  return { ok: true, data };
}

export async function createCaseAction(
  caseId: string,
  input: CreateActionInput,
): Promise<ActionDetailResult> {
  const id = caseId.trim();
  if (!id) {
    return { ok: false, kind: "bad_request", status: 400, message: "Vaka kimliği geçersiz." };
  }
  if (!isActionType(input.actionType) || !isActionReasonCode(input.reasonCode)) {
    return { ok: false, kind: "bad_request", status: 400, message: "Geçerli aksiyon alanları seçin." };
  }
  const targetType = input.targetType.trim();
  const targetId = input.targetId.trim();
  if (!targetType || !targetId) {
    return { ok: false, kind: "bad_request", status: 400, message: "Hedef bilgisi eksik." };
  }

  const body: Record<string, string> = {
    targetType,
    targetId,
    actionType: input.actionType,
    reasonCode: input.reasonCode,
  };
  const rationale = input.rationale.trim();
  if (rationale) {
    body.rationale = rationale;
  }

  let res: Response;
  try {
    res = await staffApiFetch(`/moderation/cases/${encodeURIComponent(id)}/actions`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch {
    return { ok: false, kind: "error", status: 0, message: "Aksiyon oluşturulamadı." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "create");
  }
  const data = parseStaffAction(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "Aksiyon yanıtı geçersiz." };
  }
  return { ok: true, data };
}

export async function transitionCaseAction(
  caseId: string,
  actionId: string,
  status: string,
): Promise<ActionDetailResult> {
  const cid = caseId.trim();
  const aid = actionId.trim();
  if (!cid || !aid) {
    return { ok: false, kind: "bad_request", status: 400, message: "Aksiyon kimliği geçersiz." };
  }
  if (!isActionStatus(status)) {
    return { ok: false, kind: "bad_request", status: 400, message: "Geçerli bir durum seçin." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(
      `/moderation/cases/${encodeURIComponent(cid)}/actions/${encodeURIComponent(aid)}/status`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      },
    );
  } catch {
    return { ok: false, kind: "error", status: 0, message: "Aksiyon geçişi başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "transition");
  }
  const data = parseStaffAction(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "Aksiyon geçişi yanıtı geçersiz." };
  }
  return { ok: true, data };
}
