import { proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ caseId: string; actionId: string }>;
};

export async function GET(_request: Request, context: RouteContext): Promise<Response> {
  const { caseId, actionId } = await context.params;
  const cid = caseId.trim();
  const aid = actionId.trim();
  if (!cid || !aid) {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }
  return proxyStaffRequest(
    `/v1/staff/moderation/cases/${encodeURIComponent(cid)}/actions/${encodeURIComponent(aid)}`,
    { method: "GET" },
  );
}
