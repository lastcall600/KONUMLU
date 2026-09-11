"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import {
  DEFAULT_QUEUE_FILTERS,
  QUEUE_LIMITS,
  QUEUE_ORDERS,
  REASON_CODES,
  REPORT_STATUSES,
  TARGET_TYPES,
  fetchModerationQueue,
  filtersToSearchParams,
  parseQueueFilters,
  type QueueFilters,
  type QueueLoadResult,
  type StaffReport,
} from "@/lib/moderation-queue";

function formatTimestamp(value: string): string {
  if (!value) {
    return "—";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toISOString().replace("T", " ").replace(/\.\d+Z$/, " UTC");
}

function truncate(value: string | undefined, max: number): string {
  if (!value) {
    return "—";
  }
  return value.length > max ? `${value.slice(0, max)}…` : value;
}

export function ModerationQueue() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const requestKey = searchParams.toString();
  const filters = parseQueueFilters(searchParams);
  const cursor = searchParams.get("cursor")?.trim() ?? "";

  const [result, setResult] = useState<QueueLoadResult | null>(null);
  const [loading, setLoading] = useState(true);

  const replaceQuery = useCallback(
    (nextFilters: QueueFilters, nextCursor: string) => {
      const params = filtersToSearchParams(nextFilters, nextCursor);
      const qs = params.toString();
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [pathname, router],
  );

  useEffect(() => {
    const params = new URLSearchParams(requestKey);
    const nextFilters = parseQueueFilters(params);
    const nextCursor = params.get("cursor")?.trim() ?? "";
    const controller = new AbortController();
    setLoading(true);

    void fetchModerationQueue(nextFilters, nextCursor, controller.signal)
      .then((next) => {
        if (controller.signal.aborted) {
          return;
        }
        setResult(next);
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === "AbortError") {
          return;
        }
        if (!controller.signal.aborted) {
          setResult({
            ok: false,
            kind: "error",
            status: 0,
            message: "Kuyruk isteği başarısız oldu.",
          });
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });

    return () => {
      controller.abort();
    };
  }, [requestKey]);

  function onFilterChange<K extends keyof QueueFilters>(key: K, value: QueueFilters[K]) {
    replaceQuery({ ...filters, [key]: value }, "");
  }

  const reports: StaffReport[] = result?.ok ? result.data.reports : [];
  const nextCursor = result?.ok ? result.data.nextCursor : undefined;

  return (
    <main className="ops-page">
      <header className="ops-header">
        <h1>Moderasyon kuyruğu</h1>
        <p>Staff rapor kuyruğu. Kayıtlar backend sözleşmesinden gelir.</p>
      </header>

      <form
        className="ops-filters"
        onSubmit={(event) => {
          event.preventDefault();
        }}
      >
        <label>
          Durum
          <select
            value={filters.status}
            onChange={(event) => onFilterChange("status", event.target.value)}
          >
            <option value="">Tümü</option>
            {REPORT_STATUSES.map((status) => (
              <option key={status} value={status}>
                {status}
              </option>
            ))}
          </select>
        </label>
        <label>
          Sıra
          <select
            value={filters.order}
            onChange={(event) => onFilterChange("order", event.target.value as QueueFilters["order"])}
          >
            {QUEUE_ORDERS.map((order) => (
              <option key={order} value={order}>
                {order}
              </option>
            ))}
          </select>
        </label>
        <label>
          Sayfa boyutu
          <select
            value={String(filters.limit)}
            onChange={(event) =>
              onFilterChange("limit", Number.parseInt(event.target.value, 10) as QueueFilters["limit"])
            }
          >
            {QUEUE_LIMITS.map((limit) => (
              <option key={limit} value={limit}>
                {limit}
              </option>
            ))}
          </select>
        </label>
        <label>
          Hedef türü
          <select
            value={filters.targetType}
            onChange={(event) => onFilterChange("targetType", event.target.value)}
          >
            <option value="">Tümü</option>
            {TARGET_TYPES.map((type) => (
              <option key={type} value={type}>
                {type}
              </option>
            ))}
          </select>
        </label>
        <label>
          Gerekçe
          <select
            value={filters.reasonCode}
            onChange={(event) => onFilterChange("reasonCode", event.target.value)}
          >
            <option value="">Tümü</option>
            {REASON_CODES.map((code) => (
              <option key={code} value={code}>
                {code}
              </option>
            ))}
          </select>
        </label>
      </form>

      {loading ? <p className="ops-banner">Kuyruk yükleniyor…</p> : null}

      {!loading && result && !result.ok && result.kind === "unauthenticated" ? (
        <p className="ops-banner ops-banner-auth" role="alert">
          {result.message}
        </p>
      ) : null}

      {!loading && result && !result.ok && result.kind === "forbidden" ? (
        <p className="ops-banner ops-banner-auth" role="alert">
          {result.message}
        </p>
      ) : null}

      {!loading && result && !result.ok && result.kind === "error" ? (
        <p className="ops-banner ops-banner-error" role="alert">
          {result.message}
        </p>
      ) : null}

      {!loading && result?.ok && reports.length === 0 ? (
        <p className="ops-banner">Kuyruk boş. Gösterilecek rapor yok.</p>
      ) : null}

      {!loading && result?.ok && reports.length > 0 ? (
        <div className="ops-table-wrap">
          <table className="ops-table">
            <thead>
              <tr>
                <th scope="col">Rapor</th>
                <th scope="col">Hedef türü</th>
                <th scope="col">Hedef</th>
                <th scope="col">Gerekçe</th>
                <th scope="col">Durum</th>
                <th scope="col">Açıklama</th>
                <th scope="col">Oluşturma</th>
                <th scope="col">Güncelleme</th>
                <th scope="col">Personel notu</th>
                <th scope="col">Durumu değiştiren</th>
                <th scope="col">Aç</th>
              </tr>
            </thead>
            <tbody>
              {reports.map((row) => (
                <tr key={row.reportId}>
                  <td>
                    <Link href={`/moderation/${encodeURIComponent(row.reportId)}`}>
                      <code>{row.reportId}</code>
                    </Link>
                  </td>
                  <td>{row.targetType}</td>
                  <td>
                    <code>{row.targetId}</code>
                  </td>
                  <td>{row.reasonCode}</td>
                  <td>
                    <span className={`status-chip status-${row.status}`}>{row.status}</span>
                  </td>
                  <td title={row.description ?? ""}>{truncate(row.description, 48)}</td>
                  <td>{formatTimestamp(row.createdAt)}</td>
                  <td>{formatTimestamp(row.updatedAt)}</td>
                  <td title={row.staffNote ?? ""}>{truncate(row.staffNote, 48)}</td>
                  <td>
                    {row.statusChangedBy ? <code>{row.statusChangedBy}</code> : "—"}
                  </td>
                  <td>
                    <Link href={`/moderation/${encodeURIComponent(row.reportId)}`}>Aç</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      <div className="ops-pager">
        <button
          type="button"
          disabled={loading || cursor === ""}
          onClick={() => replaceQuery(filters, "")}
        >
          İlk sayfa
        </button>
        <button
          type="button"
          disabled={loading || !nextCursor}
          onClick={() => {
            if (!nextCursor || loading) {
              return;
            }
            replaceQuery(filters, nextCursor);
          }}
        >
          Sonraki sayfa
        </button>
      </div>
    </main>
  );
}
