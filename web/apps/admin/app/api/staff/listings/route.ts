import { copyAllowedListingParams, proxyStaffRequest } from "@/lib/staff-server";

export async function GET(request: Request): Promise<Response> {
  const incoming = new URL(request.url);
  const query = copyAllowedListingParams(incoming.searchParams);
  return proxyStaffRequest("/v1/staff/listings", {
    method: "GET",
    search: query,
  });
}
