import { copyAllowedNestedListParams, proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ caseId: string }>;
};

function casePath(caseId: string): string | null {
  const id = caseId.trim();
  if (!id) {
    return null;
  }
  return `/v1/staff/moderation/cases/${encodeURIComponent(id)}/evidence`;
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
    evidenceType: typeof raw.evidenceType === "string" ? raw.evidenceType : "",
    title: typeof raw.title === "string" ? raw.title : "",
  };
  if (typeof raw.description === "string") {
    body.description = raw.description;
  }
  if (typeof raw.referenceValue === "string") {
    body.referenceValue = raw.referenceValue;
  }

  return proxyStaffRequest(path, { method: "POST", jsonBody: body });
}
