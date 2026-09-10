"use client";

export default function Error({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <main className="page-status">
      <h1>Bir sorun oluştu</h1>
      <button type="button" onClick={() => reset()}>
        Yeniden dene
      </button>
    </main>
  );
}
