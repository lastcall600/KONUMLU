import { staffApiFetch } from "@/lib/api";
import { TARGET_TYPES } from "@/lib/moderation-queue";

export const CASE_STATUSES = ["open", "investigating", "resolved", "closed"] as const;
export const CASE_PRIORITIES = ["low", "normal", "high", "urgent"] as const;
export const CASE_LIMITS = [20, 50] as const;
export const CASE_SUBJECT_TYPES = TARGET_TYPES;

export type CaseStatus = (typeof CASE_STATUSES)[number];
export type CasePriority = (typeof CASE_PRIORITIES)[number];
export type CaseLimit = (typeof CASE_LIMITS)[number];

export type StaffCaseSummary = {
  caseId: string;
  status: string;
  priority: string;
  subjectType: string;
  subjectId: string;
  title: string;
  assignedStaffId?: string;
  createdAt: string;
  updatedAt: string;
};

export type StaffCaseHistory = {
  kind: string;
  actorStaffId?: string;
  reportId?: string;
  fromStatus?: string;
  toStatus?: string;
  fromPriority?: string;
  toPriority?: string;
  fromAssignedStaffId?: string;
  toAssignedStaffId?: string;
  note?: string;
  evidenceId?: string;
  actionId?: string;
  appealId?: string;
  createdAt: string;
};

export type StaffCaseDetail = StaffCaseSummary & {
  reportIds: string[];
  history: StaffCaseHistory[];
};

export type StaffCaseList = {
  cases: StaffCaseSummary[];
  nextCursor?: string;
};

export type CaseFilters = {
  status: string;
  priority: string;
  subjectType: string;
  limit: CaseLimit;
};

export const DEFAULT_CASE_FILTERS: CaseFilters = {
  status: "",
  priority: "",
  subjectType: "",
  limit: 20,
};

export type CaseQueueLoadResult =
  | { ok: true; data: StaffCaseList }
  | { ok: false; kind: "unauthenticated" | "forbidden" | "bad_request" | "error"; status: number; message: string };

export type CaseDetailLoadKind = "unauthenticated" | "forbidden" | "not_found" | "bad_request" | "error";

export type CaseDetailLoadResult =
  | { ok: true; data: StaffCaseDetail }
  | { ok: false; kind: CaseDetailLoadKind; status: number; message: string };

function isCaseLimit(value: number): value is CaseLimit {
  return (CASE_LIMITS as readonly number[]).includes(value);
}

export function parseCaseFilters(params: URLSearchParams): CaseFilters {
  const status = params.get("status")?.trim() ?? "";
  const priority = params.get("priority")?.trim() ?? "";
  const subjectType = params.get("subjectType")?.trim() ?? "";
  const limitRaw = Number.parseInt(params.get("limit")?.trim() ?? "", 10);
  const limit: CaseLimit = isCaseLimit(limitRaw) ? limitRaw : DEFAULT_CASE_FILTERS.limit;
  return { status, priority, subjectType, limit };
}

export function caseFiltersToSearchParams(filters: CaseFilters, cursor: string): URLSearchParams {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (filters.priority) params.set("priority", filters.priority);
  if (filters.subjectType) params.set("subjectType", filters.subjectType);
  if (filters.limit !== DEFAULT_CASE_FILTERS.limit) {
    params.set("limit", String(filters.limit));
  }
  if (cursor) params.set("cursor", cursor);
  return params;
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function parseStaffCaseSummary(value: unknown): StaffCaseSummary | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.caseId !== "string" || row.caseId.trim() === "") {
    return null;
  }
  if (typeof row.status !== "string" || typeof row.priority !== "string") {
    return null;
  }
  if (typeof row.subjectType !== "string" || typeof row.subjectId !== "string") {
    return null;
  }
  if (typeof row.title !== "string") {
    return null;
  }
  if (typeof row.createdAt !== "string" || typeof row.updatedAt !== "string") {
    return null;
  }
  const out: StaffCaseSummary = {
    caseId: row.caseId,
    status: row.status,
    priority: row.priority,
    subjectType: row.subjectType,
    subjectId: row.subjectId,
    title: row.title,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  };
  const assigned = optionalString(row.assignedStaffId);
  if (assigned) {
    out.assignedStaffId = assigned;
  }
  return out;
}

function parseStaffCaseHistory(value: unknown): StaffCaseHistory | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.kind !== "string" || typeof row.createdAt !== "string") {
    return null;
  }
  const out: StaffCaseHistory = {
    kind: row.kind,
    createdAt: row.createdAt,
  };
  const actorStaffId = optionalString(row.actorStaffId);
  if (actorStaffId) out.actorStaffId = actorStaffId;
  const reportId = optionalString(row.reportId);
  if (reportId) out.reportId = reportId;
  const fromStatus = optionalString(row.fromStatus);
  if (fromStatus) out.fromStatus = fromStatus;
  const toStatus = optionalString(row.toStatus);
  if (toStatus) out.toStatus = toStatus;
  const fromPriority = optionalString(row.fromPriority);
  if (fromPriority) out.fromPriority = fromPriority;
  const toPriority = optionalString(row.toPriority);
  if (toPriority) out.toPriority = toPriority;
  const fromAssignedStaffId = optionalString(row.fromAssignedStaffId);
  if (fromAssignedStaffId) out.fromAssignedStaffId = fromAssignedStaffId;
  const toAssignedStaffId = optionalString(row.toAssignedStaffId);
  if (toAssignedStaffId) out.toAssignedStaffId = toAssignedStaffId;
  const note = optionalString(row.note);
  if (note) out.note = note;
  const evidenceId = optionalString(row.evidenceId);
  if (evidenceId) out.evidenceId = evidenceId;
  const actionId = optionalString(row.actionId);
  if (actionId) out.actionId = actionId;
  const appealId = optionalString(row.appealId);
  if (appealId) out.appealId = appealId;
  return out;
}

function parseStaffCaseDetail(value: unknown): StaffCaseDetail | null {
  const summary = parseStaffCaseSummary(value);
  if (!summary || !value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (!Array.isArray(row.reportIds) || !Array.isArray(row.history)) {
    return null;
  }
  const reportIds: string[] = [];
  for (const id of row.reportIds) {
    if (typeof id !== "string" || id.trim() === "") {
      return null;
    }
    reportIds.push(id);
  }
  const history: StaffCaseHistory[] = [];
  for (const item of row.history) {
    const parsed = parseStaffCaseHistory(item);
    if (!parsed) {
      return null;
    }
    history.push(parsed);
  }
  return { ...summary, reportIds, history };
}

async function readJson(res: Response): Promise<unknown> {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

function queueMessage(
  kind: "unauthenticated" | "forbidden" | "bad_request" | "error",
  status: number,
): string {
  switch (kind) {
    case "unauthenticated":
      return "Management Center oturumu yok. Staff IAM kimliği doğrulanamadı.";
    case "forbidden":
      return "Bu kuyruğu görüntüleme yetkiniz yok.";
    case "bad_request":
      return "Kuyruk süzgeçleri geçersiz.";
    default:
      return `Vaka kuyruğu yüklenemedi (${status}).`;
  }
}

function detailMessage(kind: CaseDetailLoadKind, status: number): string {
  switch (kind) {
    case "unauthenticated":
      return "Management Center oturumu yok. Staff IAM kimliği doğrulanamadı.";
    case "forbidden":
      return "Bu vakayı görüntüleme yetkiniz yok.";
    case "not_found":
      return "Vaka bulunamadı.";
    case "bad_request":
      return "Vaka kimliği geçersiz.";
    default:
      return `Vaka yüklenemedi (${status}).`;
  }
}

function queueKindFromStatus(status: number): Extract<CaseQueueLoadResult, { ok: false }>["kind"] {
  if (status === 401) return "unauthenticated";
  if (status === 403) return "forbidden";
  if (status === 400) return "bad_request";
  return "error";
}

function detailKindFromStatus(status: number): CaseDetailLoadKind {
  if (status === 401) return "unauthenticated";
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 400) return "bad_request";
  return "error";
}

export async function fetchModerationCases(
  filters: CaseFilters,
  cursor: string,
  signal?: AbortSignal,
): Promise<CaseQueueLoadResult> {
  const params = caseFiltersToSearchParams(filters, cursor);
  params.set("limit", String(filters.limit));
  const query = params.toString();
  const path = query ? `/moderation/cases?${query}` : "/moderation/cases";
  let res: Response;
  try {
    res = await staffApiFetch(path, { method: "GET", signal });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "Kuyruk isteği başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    const kind = queueKindFromStatus(res.status);
    return { ok: false, kind, status: res.status, message: queueMessage(kind, res.status) };
  }

  if (!payload || typeof payload !== "object" || !Array.isArray((payload as StaffCaseList).cases)) {
    return { ok: false, kind: "error", status: res.status, message: "Kuyruk yanıtı geçersiz." };
  }

  const cases: StaffCaseSummary[] = [];
  for (const row of (payload as StaffCaseList).cases) {
    const parsed = parseStaffCaseSummary(row);
    if (!parsed) {
      return { ok: false, kind: "error", status: res.status, message: "Kuyruk yanıtı geçersiz." };
    }
    cases.push(parsed);
  }

  const nextCursor = optionalString((payload as StaffCaseList).nextCursor);
  return { ok: true, data: { cases, nextCursor } };
}

export async function fetchModerationCase(
  caseId: string,
  signal?: AbortSignal,
): Promise<CaseDetailLoadResult> {
  const id = caseId.trim();
  if (!id) {
    return { ok: false, kind: "bad_request", status: 400, message: "Vaka kimliği geçersiz." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(`/moderation/cases/${encodeURIComponent(id)}`, {
      method: "GET",
      signal,
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "Vaka isteği başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    const kind = detailKindFromStatus(res.status);
    return { ok: false, kind, status: res.status, message: detailMessage(kind, res.status) };
  }

  const data = parseStaffCaseDetail(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "Vaka yanıtı geçersiz." };
  }
  return { ok: true, data };
}
