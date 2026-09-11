import Link from "next/link";

export default function AppealsIndexPage() {
  return (
    <main className="ops-page">
      <header className="ops-header">
        <h1>İtirazlar</h1>
        <p>Staff itiraz incelemesi vaka kapsamındadır. Global itiraz kuyruğu endpoint'i yok.</p>
      </header>
      <p className="ops-banner">
        Bir vaka açıp <strong>İtirazlar</strong> bölümüne gidin. Liste, detay ve karar uçları case appeals
        sözleşmesindedir.
      </p>
      <p className="ops-back">
        <Link href="/cases">Vaka kuyruğuna git</Link>
      </p>
    </main>
  );
}
