import { proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ caseId: string; appealId: string }>;
};

export async function POST(request: Request, context: RouteContext): Promise<Response> {
  const { caseId, appealId } = await context.params;
  const cid = caseId.trim();
  const aid = appealId.trim();
  if (!cid || !aid) {
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
  const body = {
    status: typeof raw.status === "string" ? raw.status : "",
  };

  return proxyStaffRequest(
    `/v1/staff/moderation/cases/${encodeURIComponent(cid)}/appeals/${encodeURIComponent(aid)}/status`,
    { method: "POST", jsonBody: body },
  );
}
