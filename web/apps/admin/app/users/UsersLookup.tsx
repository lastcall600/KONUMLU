"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

export function UsersLookup() {
  const router = useRouter();
  const [publicProfileId, setPublicProfileId] = useState("");
  const [error, setError] = useState<string | null>(null);

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const id = publicProfileId.trim();
    if (!id) {
      setError("Public profile kimliği girin.");
      return;
    }
    setError(null);
    router.push(`/users/${encodeURIComponent(id)}`);
  }

  return (
    <main className="ops-page">
      <header className="ops-header">
        <h1>Users</h1>
        <p>Staff user listesi yok. Public profile kimliği ile doğrudan bakın.</p>
      </header>
      <form className="ops-action" onSubmit={onSubmit}>
        <label>
          Public profile ID
          <input
            value={publicProfileId}
            onChange={(event) => setPublicProfileId(event.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        {error ? (
          <p className="ops-banner ops-banner-error" role="alert">
            {error}
          </p>
        ) : null}
        <button type="submit">Aç</button>
      </form>
    </main>
  );
}
