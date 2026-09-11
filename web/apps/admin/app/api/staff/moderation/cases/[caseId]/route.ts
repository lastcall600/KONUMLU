import { proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ caseId: string }>;
};

export async function GET(_request: Request, context: RouteContext): Promise<Response> {
  const { caseId } = await context.params;
  const id = caseId.trim();
  if (!id) {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }
  return proxyStaffRequest(`/v1/staff/moderation/cases/${encodeURIComponent(id)}`, {
    method: "GET",
  });
}
