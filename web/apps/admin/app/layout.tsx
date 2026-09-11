import type { Metadata } from "next";
import type { ReactNode } from "react";

import { ShellNav } from "@/components/ShellNav";
import { DEFAULT_LOCALE, dirForLocale } from "@/lib/locale";

import "./globals.css";

export const metadata: Metadata = {
  title: "KONUMLU Management Center",
  description: "Operations control plane",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  const locale = DEFAULT_LOCALE;

  return (
    <html lang={locale} dir={dirForLocale(locale)}>
      <body>
        <div className="shell">
          <ShellNav />
          <div className="shell-main">{children}</div>
        </div>
      </body>
    </html>
  );
}
