import { proxyStaffRequest } from "@/lib/staff-server";

type RouteContext = {
  params: Promise<{ reportId: string }>;
};

export async function POST(request: Request, context: RouteContext): Promise<Response> {
  const { reportId } = await context.params;
  const id = reportId.trim();
  if (!id) {
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
  const body: { status: string; staffNote?: string } = {
    status: typeof raw.status === "string" ? raw.status : "",
  };
  if (typeof raw.staffNote === "string") {
    body.staffNote = raw.staffNote;
  }

  return proxyStaffRequest(`/v1/staff/moderation/reports/${encodeURIComponent(id)}/status`, {
    method: "POST",
    jsonBody: body,
  });
}
