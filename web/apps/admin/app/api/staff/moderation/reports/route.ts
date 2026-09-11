import { copyAllowedQueueParams, proxyStaffRequest } from "@/lib/staff-server";

export async function GET(request: Request): Promise<Response> {
  const incoming = new URL(request.url);
  const query = copyAllowedQueueParams(incoming.searchParams);
  return proxyStaffRequest("/v1/staff/moderation/reports", {
    method: "GET",
    search: query,
  });
}
