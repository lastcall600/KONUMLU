export type PublicConfig = {
  apiBaseUrl: string;
  /** VAPID public key only. Never a private key or server credential. */
  webPushVapidPublicKey: string;
};

const LOCAL_API_BASE_URL = "http://localhost:8080";

function trimTrailingSlash(url: string): string {
  return url.replace(/\/+$/, "");
}

function assertAbsoluteHttpUrl(url: string): void {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    throw new Error("NEXT_PUBLIC_API_BASE_URL must be an absolute URL");
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("NEXT_PUBLIC_API_BASE_URL must use http or https");
  }
}

/**
 * Browser-safe public config only. Secrets must never be placed in NEXT_PUBLIC_* .
 */
export function getPublicConfig(): PublicConfig {
  const fromEnv = process.env.NEXT_PUBLIC_API_BASE_URL?.trim();
  const apiBaseUrl = fromEnv
    ? trimTrailingSlash(fromEnv)
    : process.env.NODE_ENV === "production"
      ? ""
      : LOCAL_API_BASE_URL;

  if (!apiBaseUrl) {
    throw new Error("NEXT_PUBLIC_API_BASE_URL must be set in production");
  }

  assertAbsoluteHttpUrl(apiBaseUrl);
  return {
    apiBaseUrl,
    webPushVapidPublicKey: process.env.NEXT_PUBLIC_WEBPUSH_VAPID_PUBLIC_KEY?.trim() ?? "",
  };
}
