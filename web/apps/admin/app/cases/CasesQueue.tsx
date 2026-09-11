"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import {
  CASE_LIMITS,
  CASE_PRIORITIES,
  CASE_STATUSES,
  CASE_SUBJECT_TYPES,
  caseFiltersToSearchParams,
  fetchModerationCases,
  parseCaseFilters,
  type CaseFilters,
  type CaseQueueLoadResult,
  type StaffCaseSummary,
} from "@/lib/moderation-cases";
import { TargetLink } from "@/components/TargetLink";

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

export function CasesQueue() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const requestKey = searchParams.toString();
  const filters = parseCaseFilters(searchParams);
  const cursor = searchParams.get("cursor")?.trim() ?? "";

  const [result, setResult] = useState<CaseQueueLoadResult | null>(null);
  const [loading, setLoading] = useState(true);

  const replaceQuery = useCallback(
    (nextFilters: CaseFilters, nextCursor: string) => {
      const params = caseFiltersToSearchParams(nextFilters, nextCursor);
      const qs = params.toString();
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [pathname, router],
  );

  useEffect(() => {
    const params = new URLSearchParams(requestKey);
    const nextFilters = parseCaseFilters(params);
    const nextCursor = params.get("cursor")?.trim() ?? "";
    const controller = new AbortController();
    setLoading(true);

    void fetchModerationCases(nextFilters, nextCursor, controller.signal)
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

  function onFilterChange<K extends keyof CaseFilters>(key: K, value: CaseFilters[K]) {
    replaceQuery({ ...filters, [key]: value }, "");
  }

  const cases: StaffCaseSummary[] = result?.ok ? result.data.cases : [];
  const nextCursor = result?.ok ? result.data.nextCursor : undefined;

  return (
    <main className="ops-page">
      <header className="ops-header">
        <h1>Vaka kuyruğu</h1>
        <p>Staff vaka kuyruğu. Kayıtlar backend sözleşmesinden gelir.</p>
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
            {CASE_STATUSES.map((status) => (
              <option key={status} value={status}>
                {status}
              </option>
            ))}
          </select>
        </label>
        <label>
          Öncelik
          <select
            value={filters.priority}
            onChange={(event) => onFilterChange("priority", event.target.value)}
          >
            <option value="">Tümü</option>
            {CASE_PRIORITIES.map((priority) => (
              <option key={priority} value={priority}>
                {priority}
              </option>
            ))}
          </select>
        </label>
        <label>
          Hedef türü
          <select
            value={filters.subjectType}
            onChange={(event) => onFilterChange("subjectType", event.target.value)}
          >
            <option value="">Tümü</option>
            {CASE_SUBJECT_TYPES.map((type) => (
              <option key={type} value={type}>
                {type}
              </option>
            ))}
          </select>
        </label>
        <label>
          Sayfa boyutu
          <select
            value={String(filters.limit)}
            onChange={(event) =>
              onFilterChange("limit", Number.parseInt(event.target.value, 10) as CaseFilters["limit"])
            }
          >
            {CASE_LIMITS.map((limit) => (
              <option key={limit} value={limit}>
                {limit}
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

      {!loading && result && !result.ok && (result.kind === "error" || result.kind === "bad_request") ? (
        <p className="ops-banner ops-banner-error" role="alert">
          {result.message}
        </p>
      ) : null}

      {!loading && result?.ok && cases.length === 0 ? (
        <p className="ops-banner">Kuyruk boş. Gösterilecek vaka yok.</p>
      ) : null}

      {!loading && result?.ok && cases.length > 0 ? (
        <div className="ops-table-wrap">
          <table className="ops-table">
            <thead>
              <tr>
                <th scope="col">Vaka</th>
                <th scope="col">Durum</th>
                <th scope="col">Öncelik</th>
                <th scope="col">Hedef türü</th>
                <th scope="col">Hedef</th>
                <th scope="col">Başlık</th>
                <th scope="col">Atanan</th>
                <th scope="col">Oluşturma</th>
                <th scope="col">Güncelleme</th>
                <th scope="col">Aç</th>
              </tr>
            </thead>
            <tbody>
              {cases.map((row) => (
                <tr key={row.caseId}>
                  <td>
                    <Link href={`/cases/${encodeURIComponent(row.caseId)}`}>
                      <code>{row.caseId}</code>
                    </Link>
                  </td>
                  <td>
                    <span className={`status-chip status-${row.status}`}>{row.status}</span>
                  </td>
                  <td>
                    <span className={`status-chip priority-${row.priority}`}>{row.priority}</span>
                  </td>
                  <td>{row.subjectType}</td>
                  <td>
                    <TargetLink type={row.subjectType} id={row.subjectId} />
                  </td>
                  <td title={row.title}>{truncate(row.title, 48)}</td>
                  <td>{row.assignedStaffId ? <code>{row.assignedStaffId}</code> : "—"}</td>
                  <td>{formatTimestamp(row.createdAt)}</td>
                  <td>{formatTimestamp(row.updatedAt)}</td>
                  <td>
                    <Link href={`/cases/${encodeURIComponent(row.caseId)}`}>Aç</Link>
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
