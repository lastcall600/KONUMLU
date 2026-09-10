import type { Metadata } from "next";
import type { ReactNode } from "react";

import { DEFAULT_LOCALE, dirForLocale } from "@/lib/locale";

import "./globals.css";

export const metadata: Metadata = {
  title: "KONUMLU",
  description: "Konum odaklı yerel pazaryeri",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  const locale = DEFAULT_LOCALE;

  return (
    <html lang={locale} dir={dirForLocale(locale)}>
      <body>{children}</body>
    </html>
  );
}
