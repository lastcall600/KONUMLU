import { staffApiFetch } from "@/lib/api";

export const REPORT_STATUSES = ["submitted", "triaged", "closed"] as const;
export const QUEUE_ORDERS = ["newest", "oldest"] as const;
export const TARGET_TYPES = ["listing", "public_profile"] as const;
export const REASON_CODES = [
  "spam",
  "scam_or_fraud",
  "prohibited_item",
  "harassment",
  "impersonation",
  "inappropriate_content",
  "other",
] as const;
export const QUEUE_LIMITS = [20, 50] as const;

export type ReportStatus = (typeof REPORT_STATUSES)[number];
export type QueueOrder = (typeof QUEUE_ORDERS)[number];
export type TargetType = (typeof TARGET_TYPES)[number];
export type ReasonCode = (typeof REASON_CODES)[number];
export type QueueLimit = (typeof QUEUE_LIMITS)[number];

export type StaffReport = {
  reportId: string;
  targetType: string;
  targetId: string;
  reasonCode: string;
  description?: string;
  status: string;
  staffNote?: string;
  statusChangedBy?: string;
  createdAt: string;
  updatedAt: string;
};

export type StaffReportList = {
  reports: StaffReport[];
  nextCursor?: string;
};

export type QueueFilters = {
  status: string;
  order: QueueOrder;
  limit: QueueLimit;
  targetType: string;
  reasonCode: string;
};

export const DEFAULT_QUEUE_FILTERS: QueueFilters = {
  status: "",
  order: "newest",
  limit: 20,
  targetType: "",
  reasonCode: "",
};

export type QueueLoadResult =
  | { ok: true; data: StaffReportList }
  | { ok: false; kind: "unauthenticated" | "forbidden" | "error"; status: number; message: string };

function isQueueLimit(value: number): value is QueueLimit {
  return (QUEUE_LIMITS as readonly number[]).includes(value);
}

export function parseQueueFilters(params: URLSearchParams): QueueFilters {
  const status = params.get("status")?.trim() ?? "";
  const orderRaw = params.get("order")?.trim() ?? "";
  const order: QueueOrder = QUEUE_ORDERS.includes(orderRaw as QueueOrder)
    ? (orderRaw as QueueOrder)
    : DEFAULT_QUEUE_FILTERS.order;
  const limitRaw = Number.parseInt(params.get("limit")?.trim() ?? "", 10);
  const limit: QueueLimit = isQueueLimit(limitRaw) ? limitRaw : DEFAULT_QUEUE_FILTERS.limit;
  const targetType = params.get("targetType")?.trim() ?? "";
  const reasonCode = params.get("reasonCode")?.trim() ?? "";
  return { status, order, limit, targetType, reasonCode };
}

export function filtersToSearchParams(filters: QueueFilters, cursor: string): URLSearchParams {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (filters.order && filters.order !== DEFAULT_QUEUE_FILTERS.order) {
    params.set("order", filters.order);
  }
  if (filters.limit !== DEFAULT_QUEUE_FILTERS.limit) {
    params.set("limit", String(filters.limit));
  }
  if (filters.targetType) params.set("targetType", filters.targetType);
  if (filters.reasonCode) params.set("reasonCode", filters.reasonCode);
  if (cursor) params.set("cursor", cursor);
  return params;
}

export async function fetchModerationQueue(
  filters: QueueFilters,
  cursor: string,
  signal?: AbortSignal,
): Promise<QueueLoadResult> {
  const params = filtersToSearchParams(filters, cursor);
  params.set("order", filters.order);
  params.set("limit", String(filters.limit));
  const query = params.toString();
  const path = query ? `/moderation/reports?${query}` : "/moderation/reports";
  let res: Response;
  try {
    res = await staffApiFetch(path, { method: "GET", signal });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw err;
    }
    return { ok: false, kind: "error", status: 0, message: "Kuyruk isteği başarısız oldu." };
  }

  if (res.status === 401) {
    return {
      ok: false,
      kind: "unauthenticated",
      status: 401,
      message: "Management Center oturumu yok. Staff IAM kimliği doğrulanamadı.",
    };
  }
  if (res.status === 403) {
    return {
      ok: false,
      kind: "forbidden",
      status: 403,
      message: "Bu kuyruğu görüntüleme yetkiniz yok.",
    };
  }
  if (!res.ok) {
    return {
      ok: false,
      kind: "error",
      status: res.status,
      message: `Moderasyon kuyruğu yüklenemedi (${res.status}).`,
    };
  }

  let parsed: unknown;
  try {
    parsed = await res.json();
  } catch {
    return { ok: false, kind: "error", status: res.status, message: "Kuyruk yanıtı okunamadı." };
  }

  if (!parsed || typeof parsed !== "object" || !Array.isArray((parsed as StaffReportList).reports)) {
    return { ok: false, kind: "error", status: res.status, message: "Kuyruk yanıtı geçersiz." };
  }

  const body = parsed as StaffReportList;
  return {
    ok: true,
    data: {
      reports: body.reports,
      nextCursor: typeof body.nextCursor === "string" && body.nextCursor !== "" ? body.nextCursor : undefined,
    },
  };
}
