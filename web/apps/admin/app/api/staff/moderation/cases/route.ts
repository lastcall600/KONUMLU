import { copyAllowedCaseParams, proxyStaffRequest } from "@/lib/staff-server";

export async function GET(request: Request): Promise<Response> {
  const incoming = new URL(request.url);
  const query = copyAllowedCaseParams(incoming.searchParams);
  return proxyStaffRequest("/v1/staff/moderation/cases", {
    method: "GET",
    search: query,
  });
}
