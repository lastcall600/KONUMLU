import { staffApiFetch } from "@/lib/api";
import {
  isAbortError,
  kindFromStatus,
  optionalString,
  readErrorCode,
  readJson,
  type StaffOpKind,
} from "@/lib/moderation-http";

export const EVIDENCE_TYPES = [
  "staff_note",
  "external_reference",
  "internal_reference",
  "snapshot_reference",
] as const;

export const EVIDENCE_LIST_LIMIT = 100;

export type EvidenceType = (typeof EVIDENCE_TYPES)[number];

export type StaffEvidence = {
  evidenceId: string;
  caseId: string;
  evidenceType: string;
  title: string;
  description?: string;
  referenceValue?: string;
  actorStaffId?: string;
  createdAt: string;
};

export type StaffEvidenceList = {
  evidence: StaffEvidence[];
};

export type EvidenceLoadResult =
  | { ok: true; data: StaffEvidenceList }
  | { ok: false; kind: StaffOpKind; status: number; message: string };

export type EvidenceMutationResult =
  | { ok: true; data: StaffEvidence }
  | { ok: false; kind: StaffOpKind; status: number; message: string };

export type AddEvidenceInput = {
  evidenceType: string;
  title: string;
  description: string;
  referenceValue: string;
};

export function isEvidenceType(value: string): value is EvidenceType {
  return (EVIDENCE_TYPES as readonly string[]).includes(value);
}

export function evidenceNeedsReference(evidenceType: string): boolean {
  return evidenceType !== "staff_note";
}

function parseStaffEvidence(value: unknown): StaffEvidence | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const row = value as Record<string, unknown>;
  if (typeof row.evidenceId !== "string" || row.evidenceId.trim() === "") {
    return null;
  }
  if (typeof row.caseId !== "string" || typeof row.evidenceType !== "string") {
    return null;
  }
  if (typeof row.title !== "string" || typeof row.createdAt !== "string") {
    return null;
  }
  const out: StaffEvidence = {
    evidenceId: row.evidenceId,
    caseId: row.caseId,
    evidenceType: row.evidenceType,
    title: row.title,
    createdAt: row.createdAt,
  };
  const description = optionalString(row.description);
  if (description) out.description = description;
  const referenceValue = optionalString(row.referenceValue);
  if (referenceValue) out.referenceValue = referenceValue;
  const actorStaffId = optionalString(row.actorStaffId);
  if (actorStaffId) out.actorStaffId = actorStaffId;
  return out;
}

function messageFor(kind: StaffOpKind, status: number, code: string | null, verb: "load" | "append"): string {
  switch (kind) {
    case "unauthenticated":
      return "Management Center oturumu yok. Staff IAM kimliği doğrulanamadı.";
    case "forbidden":
      return verb === "append"
        ? "Kanıt ekleme yetkiniz yok."
        : "Bu vakanın kanıtını görüntüleme yetkiniz yok.";
    case "not_found":
      return "Vaka veya kanıt bulunamadı.";
    case "bad_request":
      return code ? `İstek reddedildi (${code}).` : "İstek geçersiz.";
    case "conflict":
      return code ? `Kanıt işlemi reddedildi (${code}).` : "Kanıt işlemi reddedildi.";
    default:
      return verb === "append"
        ? `Kanıt eklenemedi (${status}).`
        : `Kanıt listesi yüklenemedi (${status}).`;
  }
}

function fail(status: number, payload: unknown, verb: "load" | "append"): EvidenceLoadResult & EvidenceMutationResult {
  const kind = kindFromStatus(status);
  const code = readErrorCode(payload);
  return { ok: false, kind, status, message: messageFor(kind, status, code, verb) };
}

export async function fetchCaseEvidence(
  caseId: string,
  signal?: AbortSignal,
): Promise<EvidenceLoadResult> {
  const id = caseId.trim();
  if (!id) {
    return { ok: false, kind: "bad_request", status: 400, message: "Vaka kimliği geçersiz." };
  }

  let res: Response;
  try {
    res = await staffApiFetch(
      `/moderation/cases/${encodeURIComponent(id)}/evidence?limit=${EVIDENCE_LIST_LIMIT}`,
      { method: "GET", signal },
    );
  } catch (err) {
    if (isAbortError(err)) {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "Kanıt isteği başarısız oldu." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "load");
  }
  if (!payload || typeof payload !== "object" || !Array.isArray((payload as StaffEvidenceList).evidence)) {
    return { ok: false, kind: "error", status: res.status, message: "Kanıt yanıtı geçersiz." };
  }

  const evidence: StaffEvidence[] = [];
  for (const row of (payload as StaffEvidenceList).evidence) {
    const parsed = parseStaffEvidence(row);
    if (!parsed) {
      return { ok: false, kind: "error", status: res.status, message: "Kanıt yanıtı geçersiz." };
    }
    evidence.push(parsed);
  }
  return { ok: true, data: { evidence } };
}

export async function appendCaseEvidence(
  caseId: string,
  input: AddEvidenceInput,
): Promise<EvidenceMutationResult> {
  const id = caseId.trim();
  if (!id) {
    return { ok: false, kind: "bad_request", status: 400, message: "Vaka kimliği geçersiz." };
  }
  if (!isEvidenceType(input.evidenceType)) {
    return { ok: false, kind: "bad_request", status: 400, message: "Geçerli bir kanıt türü seçin." };
  }
  const title = input.title.trim();
  if (!title) {
    return { ok: false, kind: "bad_request", status: 400, message: "Başlık gerekli." };
  }

  const body: Record<string, string> = {
    evidenceType: input.evidenceType,
    title,
  };
  const description = input.description.trim();
  if (description) {
    body.description = description;
  }
  const referenceValue = input.referenceValue.trim();
  if (evidenceNeedsReference(input.evidenceType)) {
    if (!referenceValue) {
      return {
        ok: false,
        kind: "bad_request",
        status: 400,
        message: "Bu kanıt türü için referans değeri gerekli.",
      };
    }
    body.referenceValue = referenceValue;
  }

  let res: Response;
  try {
    res = await staffApiFetch(`/moderation/cases/${encodeURIComponent(id)}/evidence`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch {
    return { ok: false, kind: "error", status: 0, message: "Kanıt eklenemedi." };
  }

  const payload = await readJson(res);
  if (!res.ok) {
    return fail(res.status, payload, "append");
  }
  const data = parseStaffEvidence(payload);
  if (!data) {
    return { ok: false, kind: "error", status: res.status, message: "Kanıt yanıtı geçersiz." };
  }
  return { ok: true, data };
}
