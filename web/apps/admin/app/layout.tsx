import type { Metadata } from "next";
import type { ReactNode } from "react";

import { DEFAULT_LOCALE, dirForLocale } from "@/lib/locale";

import "./globals.css";

export const metadata: Metadata = {
  title: "KONUMLU Management Center",
  description: "Operations control plane",
};

const NAV_ITEMS = [
  "Overview",
  "Users",
  "Listings",
  "Companies",
  "Trust",
  "Moderation",
  "Cases",
  "Finance",
  "Compliance",
  "Platform",
] as const;

export default function RootLayout({ children }: { children: ReactNode }) {
  const locale = DEFAULT_LOCALE;

  return (
    <html lang={locale} dir={dirForLocale(locale)}>
      <body>
        <div className="shell">
          <nav className="shell-nav" aria-label="Management Center">
            <h1>Management Center</h1>
            <ul>
              {NAV_ITEMS.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </nav>
          <div className="shell-main">{children}</div>
        </div>
      </body>
    </html>
  );
}
