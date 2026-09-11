import { staffApiFetch } from "@/lib/api";
import { REPORT_STATUSES, type StaffReport } from "@/lib/moderation-queue";

export type ReportLoadKind =
  | "unauthenticated"
  | "forbidden"
  | "not_found"
  | "bad_request"
  | "conflict"
  | "error";

export type ReportLoadResult =
  | { ok: true; data: StaffReport }
  | { ok: false; kind: ReportLoadKind; status: number; message: string };

const SAFE_ERROR_CODES = new Set([
  "unauthenticated",
  "forbidden",
  "not_found",
  "bad_request",
  "conflict",
  "unavailable",
]);

function readErrorCode(value: unknown): string | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const code = (value as { error?: unknown }).error;
  return typeof code === "string" && SAFE_ERROR_CODES.has(code) ? code : null;
}

function messageFor(kind: ReportLoadKind, status: number, code: string | null): string {
  switch (kind) {
    case "unauthenticated":
      return "Management Center oturumu yok. Staff IAM kimliği doğrulanamadı.";
    case "forbidden":
      return "Bu işlem için yetkiniz yok.";
    case "not_found":
      return "Rapor bulunamadı.";
    case "bad_request":
      return code ? `İstek reddedildi (${code}).` : "İstek geçersiz.";
    case "conflict":
      return code ? `Durum geçişi reddedildi (${code}).` : "Durum geçişi reddedildi.";
    default:
      return `İstek başarısız oldu (${status}).`;
  }
}

function kindFromStatus(status: number): ReportLoadKind {
  if (status === 401) return "unauthenticated";
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 400) return "bad_request";
  if (status === 409) return "conflict";
  return "error";
}

function parseStaffReport(value: unknown): StaffReport | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.reportId !== "string" || row.reportId.trim() === "") {
    return null;
  }
  if (typeof row.targetType !== "string" || typeof row.targetId !== "string") {
    return null;
  }
  if (typeof row.reasonCode !== "string" || typeof row.status !== "string") {
    return null;
  }
  if (typeof row.createdAt !== "string" || typeof row.updatedAt !== "string") {
    return null;
  }
  const out: StaffReport = {
    reportId: row.reportId,
    targetType: row.targetType,
    targetId: row.targetId,
    reasonCode: row.reasonCode,
    status: row.status,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
  };
  if (typeof row.description === "string") {
    out.description = row.description;
  }
  if (typeof row.staffNote === "string") {
    out.staffNote = row.staffNote;
  }
  if (typeof row.statusChangedBy === "string") {
    out.statusChangedBy = row.statusChangedBy;
  }
  return out;
}

async function readJson(res: Response): Promise<unknown> {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

function failFromResponse(status: number, payload: unknown): ReportLoadResult {
  const kind = kindFromStatus(status);
  const code = readErrorCode(payload);
  return { ok: false, kind, status, message: messageFor(kind, status, code) };
}

export async function fetchModerationReport(
  reportId: string,
  signal?: AbortSignal,
): Promise<ReportLoadResult> {
  const id = reportId.trim();
  if (!id) {
    return { ok: false, kind: "bad_request", status: 400, message: "Rapor kimliği geçersiz." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(`/moderation/reports/${encodeURIComponent(id)}`, {
      method: "GET",
      signal,
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "Rapor isteği başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return failFromResponse(res.status, payload);
  }
  const data = parseStaffReport(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "Rapor yanıtı geçersiz." };
  }
  return { ok: true, data };
}

export async function transitionModerationReport(
  reportId: string,
  status: string,
  staffNote: string,
): Promise<ReportLoadResult> {
  const id = reportId.trim();
  if (!id) {
    return { ok: false, kind: "bad_request", status: 400, message: "Rapor kimliği geçersiz." };
  }

  const body: { status: string; staffNote?: string } = { status };
  const note = staffNote.trim();
  if (note !== "") {
    body.staffNote = note;
  }

  let res: Response;
  try {
    res = await staffApiFetch(`/moderation/reports/${encodeURIComponent(id)}/status`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch {
    return { ok: false, kind: "error", status: 0, message: "Durum geçişi başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return failFromResponse(res.status, payload);
  }
  const data = parseStaffReport(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "Durum geçişi yanıtı geçersiz." };
  }
  return { ok: true, data };
}

export function isReportStatus(value: string): boolean {
  return (REPORT_STATUSES as readonly string[]).includes(value);
}
