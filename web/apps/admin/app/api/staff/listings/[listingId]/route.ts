import { proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ listingId: string }>;
};

export async function GET(_request: Request, context: RouteContext): Promise<Response> {
  const { listingId } = await context.params;
  const id = listingId.trim();
  if (!id) {
    return Response.json({ error: "bad_request" }, { status: 400 });
  }
  return proxyStaffRequest(`/v1/staff/listings/${encodeURIComponent(id)}`, {
    method: "GET",
  });
}
