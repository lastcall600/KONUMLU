import { proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ reportId: string }>;
};

export async function GET(_request: Request, context: RouteContext): Promise<Response> {
  const { reportId } = await context.params;
  const id = reportId.trim();
  if (!id) {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }
  return proxyStaffRequest(`/v1/staff/moderation/reports/${encodeURIComponent(id)}`, {
    method: "GET",
  });
}
