"use client";

import { DEFAULT_LOCALE, dirForLocale } from "@/lib/locale";

export default function GlobalError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  const locale = DEFAULT_LOCALE;

  return (
    <html lang={locale} dir={dirForLocale(locale)}>
      <body>
        <main className="page-status">
          <h1>Bir sorun oluştu</h1>
          <button type="button" onClick={() => reset()}>
            Yeniden dene
          </button>
        </main>
      </body>
    </html>
  );
}
