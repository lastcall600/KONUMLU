import { proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ publicProfileId: string }>;
};

export async function GET(_request: Request, context: RouteContext): Promise<Response> {
  const { publicProfileId } = await context.params;
  const id = publicProfileId.trim();
  if (!id) {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }
  return proxyStaffRequest(`/v1/staff/identity/profiles/${encodeURIComponent(id)}`, {
    method: "GET",
  });
}
