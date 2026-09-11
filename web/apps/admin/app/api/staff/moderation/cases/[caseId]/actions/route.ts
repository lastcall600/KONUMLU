import { copyAllowedNestedListParams, proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ caseId: string }>;
};

function casePath(caseId: string): string | null {
  const id = caseId.trim();
  if (!id) {
    return null;
  }
  return `/v1/staff/moderation/cases/${encodeURIComponent(id)}/actions`;
}

export async function GET(request: Request, context: RouteContext): Promise<Response> {
  const { caseId } = await context.params;
  const path = casePath(caseId);
  if (!path) {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }
  const incoming = new URL(request.url);
  return proxyStaffRequest(path, {
    method: "GET",
    search: copyAllowedNestedListParams(incoming.searchParams),
  });
}

export async function POST(request: Request, context: RouteContext): Promise<Response> {
  const { caseId } = await context.params;
  const path = casePath(caseId);
  if (!path) {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }

  let parsed: unknown;
  try {
    parsed = await request.json();
  } catch {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }
  if (!parsed || typeof parsed !== "object") {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }

  const raw = parsed as Record<string, unknown>;
  const body: Record<string, string> = {
    targetType: typeof raw.targetType === "string" ? raw.targetType : "",
    targetId: typeof raw.targetId === "string" ? raw.targetId : "",
    actionType: typeof raw.actionType === "string" ? raw.actionType : "",
    reasonCode: typeof raw.reasonCode === "string" ? raw.reasonCode : "",
  };
  if (typeof raw.rationale === "string") {
    body.rationale = raw.rationale;
  }

  return proxyStaffRequest(path, { method: "POST", jsonBody: body });
}
