"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";

import {
  VerifiedClientError,
  createAppointment,
  scheduledAtFromDatetimeLocal,
} from "@/lib/verified";

const GENERIC = "Bir sorun oluştu. Lütfen tekrar deneyin.";

export function AppointmentAction({
  listingId,
  sessionReady,
  authenticated,
  isOwner,
}: {
  listingId: string;
  sessionReady: boolean;
  authenticated: boolean;
  isOwner: boolean;
}) {
  const [scheduledLocal, setScheduledLocal] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createdId, setCreatedId] = useState<string | null>(null);

  if (!sessionReady || isOwner) {
    return null;
  }

  if (!authenticated) {
    return (
      <p className="listing-card-meta">
        <Link href="/giris">Randevu Talep Et</Link>
      </p>
    );
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const scheduledAt = scheduledAtFromDatetimeLocal(scheduledLocal);
    if (scheduledAt === null) {
      setError("Geçerli bir tarih ve saat seçin.");
      setBusy(false);
      return;
    }
    try {
      const row = await createAppointment(listingId, scheduledAt);
      setCreatedId(row.appointmentId);
    } catch (err) {
      if (err instanceof VerifiedClientError) {
        setError(err.message);
      } else {
        setError(GENERIC);
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="listing-card-meta">
      <form onSubmit={(event) => void onSubmit(event)}>
        <p>Randevu Talep Et</p>
        <label>
          Tarih ve saat
          <input
            type="datetime-local"
            name="scheduledAt"
            value={scheduledLocal}
            onChange={(event) => setScheduledLocal(event.target.value)}
            required
            disabled={busy}
          />
        </label>
        <button type="submit" disabled={busy}>
          Randevu Talep Et
        </button>
      </form>
      {error ? (
        <p className="auth-error" role="alert">
          {error}
        </p>
      ) : null}
      {createdId ? (
        <p className="auth-success">
          Randevu talebi gönderildi.{" "}
          <Link href="/randevular">Randevulara git</Link>
        </p>
      ) : null}
    </div>
  );
}
