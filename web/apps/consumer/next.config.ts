import type { NextConfig } from "next";

const LOCAL_API_ORIGIN = "http://localhost:8080";

function trimTrailingSlash(url: string): string {
  return url.replace(/\/+$/, "");
}

function apiProxyTarget(): string {
  const fromEnv =
    process.env.API_PROXY_TARGET?.trim() || process.env.NEXT_PUBLIC_API_BASE_URL?.trim();
  return trimTrailingSlash(fromEnv || LOCAL_API_ORIGIN);
}

const nextConfig: NextConfig = {
  reactStrictMode: true,
  async rewrites() {
    return [
      {
        source: "/v1/:path*",
        destination: `${apiProxyTarget()}/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
