import { copyAllowedNestedListParams, proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ caseId: string }>;
};

function casePath(caseId: string): string | null {
  const id = caseId.trim();
  if (!id) {
    return null;
  }
  return `/v1/staff/moderation/cases/${encodeURIComponent(id)}/appeals`;
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
