import { Suspense } from "react";

import { CasesQueue } from "./CasesQueue";

export default function CasesPage() {
  return (
    <Suspense
      fallback={
        <main className="ops-page">
          <p className="ops-banner">Kuyruk yükleniyor…</p>
        </main>
      }
    >
      <CasesQueue />
    </Suspense>
  );
}
