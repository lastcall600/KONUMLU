import { Suspense } from "react";

import { ModerationQueue } from "./ModerationQueue";

export default function ModerationPage() {
  return (
    <Suspense
      fallback={
        <main className="ops-page">
          <p className="ops-banner">Kuyruk yükleniyor…</p>
        </main>
      }
    >
      <ModerationQueue />
    </Suspense>
  );
}
