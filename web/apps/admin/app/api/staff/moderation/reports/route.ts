import { NextResponse } from "next/server";

import {
  copyAllowedQueueParams,
  staffBackendBaseUrl,
  staffDevBearerToken,
  staffDevIdpEnabled,
} from "@/lib/staff-server";

function unauthenticated(): NextResponse {
  return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
}

export async function GET(request: Request): Promise<Response> {
  if (staffDevIdpEnabled() && staffDevBearerToken() === null) {
    return unauthenticated();
  }

  let backendBase: string;
  try {
    backendBase = staffBackendBaseUrl();
  } catch {
    return NextResponse.json({ error: "unavailable" }, { status: 503 });
  }

  const incoming = new URL(request.url);
  const query = copyAllowedQueueParams(incoming.searchParams);
  const target = new URL("/v1/staff/moderation/reports", `${backendBase}/`);
  target.search = query.toString();

  const headers = new Headers({ Accept: "application/json" });
  const token = staffDevBearerToken();
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  let upstream: Response;
  try {
    upstream = await fetch(target, {
      method: "GET",
      headers,
      cache: "no-store",
    });
  } catch {
    return NextResponse.json({ error: "unavailable" }, { status: 503 });
  }

  const body = await upstream.text();
  const contentType = upstream.headers.get("content-type") ?? "application/json";
  return new Response(body, {
    status: upstream.status,
    headers: { "Content-Type": contentType },
  });
}
