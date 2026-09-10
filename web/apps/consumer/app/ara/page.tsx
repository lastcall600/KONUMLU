"use client";

import {
  Component,
  FormEvent,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";

import { ListingCard } from "@/components/ListingCard";
import { getSession } from "@/lib/auth";
import { listFavoriteListings } from "@/lib/favorites";
import {
  flattenCategoryTree,
  listPublishedCategories,
  MasterDataClientError,
  type CatalogCategory,
  type CategoryTreeItem,
} from "@/lib/masterdata";
import {
  SearchClientError,
  buildAraHref,
  formatViewportParam,
  isValidViewport,
  searchListings,
  viewportFromSearchParams,
  type SearchListing,
  type SearchViewport,
} from "@/lib/search";
import { SavedSearchClientError, createSavedSearch } from "@/lib/savedSearch";

const SearchMap = dynamic(() => import("@/components/SearchMap").then((mod) => mod.SearchMap), {
  ssr: false,
  loading: () => <p className="search-map-fallback">Harita yükleniyor…</p>,
});

type CatalogState =
  | { kind: "loading" }
  | { kind: "empty" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; categories: CatalogCategory[]; options: CategoryTreeItem[] };

type ResultsState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; listings: SearchListing[]; nextCursor?: string };

type SearchFilters = {
  q: string;
  categoryId: string;
  minPrice: string;
  maxPrice: string;
  currency: string;
};

const EMPTY_FILTERS: SearchFilters = {
  q: "",
  categoryId: "",
  minPrice: "",
  maxPrice: "",
  currency: "",
};

function filtersFromParams(params: URLSearchParams): SearchFilters {
  return {
    q: params.get("q") ?? "",
    categoryId: params.get("categoryId") ?? "",
    minPrice: params.get("minPrice") ?? "",
    maxPrice: params.get("maxPrice") ?? "",
    currency: params.get("currency") ?? "",
  };
}

function filtersToHref(filters: SearchFilters, viewport?: SearchViewport): string {
  return buildAraHref({
    q: filters.q.trim() || undefined,
    categoryId: filters.categoryId.trim() || undefined,
    minPrice: filters.minPrice.trim() || undefined,
    maxPrice: filters.maxPrice.trim() || undefined,
    currency: filters.currency.trim() || undefined,
    viewport,
  });
}

function filtersEqual(a: SearchFilters, b: SearchFilters): boolean {
  return (
    a.q.trim() === b.q.trim() &&
    a.categoryId.trim() === b.categoryId.trim() &&
    a.minPrice.trim() === b.minPrice.trim() &&
    a.maxPrice.trim() === b.maxPrice.trim() &&
    a.currency.trim() === b.currency.trim()
  );
}

function viewportEqual(a?: SearchViewport, b?: SearchViewport): boolean {
  if (!a && !b) {
    return true;
  }
  if (!a || !b) {
    return false;
  }
  return (
    formatViewportParam(a.north) === formatViewportParam(b.north) &&
    formatViewportParam(a.south) === formatViewportParam(b.south) &&
    formatViewportParam(a.east) === formatViewportParam(b.east) &&
    formatViewportParam(a.west) === formatViewportParam(b.west)
  );
}

function messageFromError(error: unknown): string {
  if (
    error instanceof SearchClientError ||
    error instanceof MasterDataClientError ||
    error instanceof SavedSearchClientError
  ) {
    return error.message;
  }
  return "Bir sorun oluştu. Lütfen tekrar deneyin.";
}

function categoryOptionLabel(option: CategoryTreeItem): string {
  const prefix = option.depth > 0 ? `${"— ".repeat(option.depth)}` : "";
  return `${prefix}${option.label || option.code}`;
}

class MapErrorBoundary extends Component<{ onError: () => void; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true };
  }

  componentDidCatch(): void {
    this.props.onError();
  }

  render(): ReactNode {
    if (this.state.failed) {
      return null;
    }
    return this.props.children;
  }
}

export default function SearchRoutePage() {
  return (
    <Suspense
      fallback={
        <main className="search-main">
          <p>Yükleniyor…</p>
        </main>
      }
    >
      <SearchPage />
    </Suspense>
  );
}

function SearchPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const urlFilters = useMemo(
    () => filtersFromParams(new URLSearchParams(searchParams.toString())),
    [searchParams],
  );
  const urlViewport = useMemo(
    () => viewportFromSearchParams(new URLSearchParams(searchParams.toString())),
    [searchParams],
  );

  const [form, setForm] = useState<SearchFilters>(urlFilters);
  const [applied, setApplied] = useState<SearchFilters>(urlFilters);
  const [appliedViewport, setAppliedViewport] = useState<SearchViewport | undefined>(urlViewport);
  const [catalog, setCatalog] = useState<CatalogState>({ kind: "loading" });
  const [results, setResults] = useState<ResultsState>({ kind: "loading" });
  const [loadingMore, setLoadingMore] = useState(false);
  const [selectedListingId, setSelectedListingId] = useState<string | null>(null);
  const [selectionFromMap, setSelectionFromMap] = useState(false);
  const [boundsDirty, setBoundsDirty] = useState(false);
  const [pendingViewport, setPendingViewport] = useState<SearchViewport | undefined>(undefined);
  const [mapFailed, setMapFailed] = useState(false);
  const [cameraKey, setCameraKey] = useState(0);
  const [sessionReady, setSessionReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set());
  const [saveOpen, setSaveOpen] = useState(false);
  const [saveName, setSaveName] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveMessage, setSaveMessage] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const currencyRef = useRef(form.currency);
  currencyRef.current = form.currency;

  const categoryLabels = useMemo(() => {
    if (catalog.kind !== "ready") {
      return new Map<string, string>();
    }
    return new Map(catalog.categories.map((category) => [category.id, category.label || category.code]));
  }, [catalog]);

  const loadCatalog = useCallback(async () => {
    setCatalog({ kind: "loading" });
    try {
      const categories = await listPublishedCategories("tr");
      if (categories.length === 0) {
        setCatalog({ kind: "empty" });
        return;
      }
      setCatalog({
        kind: "ready",
        categories,
        options: flattenCategoryTree(categories),
      });
    } catch (error) {
      setCatalog({ kind: "unavailable", message: messageFromError(error) });
    }
  }, []);

  const runSearch = useCallback(async (filters: SearchFilters, viewport?: SearchViewport) => {
    setResults({ kind: "loading" });
    setSelectedListingId(null);
    setSelectionFromMap(false);
    setBoundsDirty(false);
    setPendingViewport(undefined);
    try {
      const page = await searchListings({
        q: filters.q.trim() || undefined,
        categoryId: filters.categoryId.trim() || undefined,
        minPrice: filters.minPrice.trim() || undefined,
        maxPrice: filters.maxPrice.trim() || undefined,
        currency: filters.currency.trim() || undefined,
        viewport,
      });
      setResults({
        kind: "ready",
        listings: page.listings,
        nextCursor: page.nextCursor,
      });
      setCameraKey((current) => current + 1);
    } catch (error) {
      setResults({ kind: "unavailable", message: messageFromError(error) });
    }
  }, []);

  useEffect(() => {
    void loadCatalog();
  }, [loadCatalog]);

  useEffect(() => {
    let cancelled = false;
    async function loadFavoritesSession() {
      try {
        const session = await getSession();
        if (cancelled) {
          return;
        }
        if (session === null) {
          setAuthenticated(false);
          setFavoriteIds(new Set());
          return;
        }
        setAuthenticated(true);
        try {
          const records = await listFavoriteListings();
          if (!cancelled) {
            setFavoriteIds(new Set(records.map((record) => record.listingId)));
          }
        } catch {
          if (!cancelled) {
            setFavoriteIds(new Set());
          }
        }
      } catch {
        if (!cancelled) {
          setAuthenticated(false);
          setFavoriteIds(new Set());
        }
      } finally {
        if (!cancelled) {
          setSessionReady(true);
        }
      }
    }
    void loadFavoritesSession();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const next = { ...urlFilters, currency: currencyRef.current };
    setForm(next);
    setApplied(next);
    setAppliedViewport(urlViewport);
    void runSearch(next, urlViewport);
  }, [urlFilters, urlViewport, runSearch]);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const next = {
      q: form.q.trim(),
      categoryId: form.categoryId.trim(),
      minPrice: form.minPrice.trim(),
      maxPrice: form.maxPrice.trim(),
      currency: form.currency.trim(),
    };
    setApplied(next);
    const href = filtersToHref(next, appliedViewport);
    if (href !== filtersToHref(urlFilters, urlViewport)) {
      router.replace(href);
      return;
    }
    await runSearch(next, appliedViewport);
  }

  function onReset() {
    setForm(EMPTY_FILTERS);
    setApplied(EMPTY_FILTERS);
    setAppliedViewport(undefined);
    if (searchParams.toString() === "") {
      void runSearch(EMPTY_FILTERS);
      return;
    }
    router.replace("/ara");
  }

  async function onRetryResults() {
    await runSearch(applied, appliedViewport);
  }

  function onSearchThisArea() {
    if (!pendingViewport || !isValidViewport(pendingViewport)) {
      return;
    }
    const href = filtersToHref(applied, pendingViewport);
    if (href !== filtersToHref(urlFilters, urlViewport)) {
      router.replace(href);
      return;
    }
    setAppliedViewport(pendingViewport);
    void runSearch(applied, pendingViewport);
  }

  async function onSaveSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = saveName.trim();
    if (!name) {
      setSaveError("Kayıt için kısa bir ad girin.");
      setSaveMessage(null);
      return;
    }
    setSaving(true);
    setSaveError(null);
    setSaveMessage(null);
    try {
      await createSavedSearch({
        name,
        q: applied.q.trim() || undefined,
        categoryId: applied.categoryId.trim() || undefined,
        minPrice: applied.minPrice.trim() || undefined,
        maxPrice: applied.maxPrice.trim() || undefined,
        currency: applied.currency.trim() || undefined,
        viewport: appliedViewport && isValidViewport(appliedViewport) ? appliedViewport : undefined,
      });
      setSaveMessage("Arama kaydedildi.");
      setSaveName("");
      setSaveOpen(false);
    } catch (error) {
      setSaveError(messageFromError(error));
    } finally {
      setSaving(false);
    }
  }

  async function onLoadMore() {
    if (results.kind !== "ready" || !results.nextCursor || loadingMore) {
      return;
    }
    setLoadingMore(true);
    try {
      const page = await searchListings({
        q: applied.q.trim() || undefined,
        categoryId: applied.categoryId.trim() || undefined,
        minPrice: applied.minPrice.trim() || undefined,
        maxPrice: applied.maxPrice.trim() || undefined,
        currency: applied.currency.trim() || undefined,
        viewport: appliedViewport,
        cursor: results.nextCursor,
      });
      setResults({
        kind: "ready",
        listings: [...results.listings, ...page.listings],
        nextCursor: page.nextCursor,
      });
    } catch (error) {
      setResults({ kind: "unavailable", message: messageFromError(error) });
    } finally {
      setLoadingMore(false);
    }
  }

  function highlightFromList(listingId: string) {
    setSelectedListingId(listingId);
    setSelectionFromMap(false);
  }

  function highlightFromMap(listingId: string) {
    setSelectedListingId(listingId);
    setSelectionFromMap(true);
    const card = document.getElementById(`search-listing-${listingId}`);
    card?.scrollIntoView({ block: "nearest" });
  }

  const busy = results.kind === "loading";
  const dirty = !filtersEqual(form, applied);
  const listings = results.kind === "ready" ? results.listings : [];

  return (
    <main className="search-main">
      <p className="search-nav">
        <Link href="/">Ana sayfa</Link>
        {" · "}
        <Link href="/favoriler">Favoriler</Link>
        {" · "}
        <Link href="/kayitli-aramalar">Kayıtlı aramalar</Link>
      </p>
      <h1 className="brand">KONUMLU</h1>
      <h2 className="auth-title">İlan ara</h2>

      <form className="auth-card search-filters" onSubmit={(event) => void onSubmit(event)}>
        <label className="auth-field" htmlFor="q">
          Anahtar kelime
        </label>
        <input
          id="q"
          name="q"
          type="search"
          value={form.q}
          onChange={(event) => setForm((current) => ({ ...current, q: event.target.value }))}
          disabled={busy}
          autoComplete="off"
        />

        <CategoryField
          catalog={catalog}
          categoryId={form.categoryId}
          disabled={busy}
          onChange={(categoryId) => setForm((current) => ({ ...current, categoryId }))}
          onRetry={() => void loadCatalog()}
        />

        <div className="search-price-row">
          <div>
            <label className="auth-field" htmlFor="minPrice">
              En düşük fiyat
            </label>
            <input
              id="minPrice"
              name="minPrice"
              type="text"
              inputMode="decimal"
              value={form.minPrice}
              onChange={(event) => setForm((current) => ({ ...current, minPrice: event.target.value }))}
              disabled={busy}
              autoComplete="off"
            />
          </div>
          <div>
            <label className="auth-field" htmlFor="maxPrice">
              En yüksek fiyat
            </label>
            <input
              id="maxPrice"
              name="maxPrice"
              type="text"
              inputMode="decimal"
              value={form.maxPrice}
              onChange={(event) => setForm((current) => ({ ...current, maxPrice: event.target.value }))}
              disabled={busy}
              autoComplete="off"
            />
          </div>
        </div>

        <label className="auth-field" htmlFor="currency">
          Para birimi
        </label>
        <select
          id="currency"
          name="currency"
          value={form.currency}
          onChange={(event) => setForm((current) => ({ ...current, currency: event.target.value }))}
          disabled={busy}
        >
          <option value="">Tümü</option>
          <option value="TRY">TRY</option>
        </select>

        <div className="auth-actions">
          <button type="submit" disabled={busy}>
            Ara
          </button>
          <button type="button" onClick={onReset} disabled={busy}>
            Sıfırla
          </button>
        </div>
        {dirty ? <p className="field-help">Değişiklikleri uygulamak için Ara’ya basın.</p> : null}
      </form>

      {sessionReady && !authenticated ? (
        <p>
          Aramayı kaydetmek için <Link href="/giris">giriş</Link> yapın.
        </p>
      ) : null}
      {sessionReady && authenticated ? (
        <section className="auth-card" aria-labelledby="save-search-heading">
          <h2 id="save-search-heading" className="auth-title">
            Kayıtlı arama
          </h2>
          {!saveOpen ? (
            <button
              type="button"
              onClick={() => {
                setSaveOpen(true);
                setSaveError(null);
              }}
            >
              Aramayı Kaydet
            </button>
          ) : (
            <form onSubmit={(event) => void onSaveSearch(event)}>
              <label className="auth-field" htmlFor="savedSearchName">
                Kısa ad
              </label>
              <input
                id="savedSearchName"
                name="savedSearchName"
                type="text"
                value={saveName}
                onChange={(event) => setSaveName(event.target.value)}
                disabled={saving}
                autoComplete="off"
                maxLength={80}
              />
              <div className="auth-actions">
                <button type="submit" disabled={saving}>
                  {saving ? "Kaydediliyor…" : "Kaydet"}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setSaveOpen(false);
                    setSaveError(null);
                  }}
                  disabled={saving}
                >
                  Vazgeç
                </button>
              </div>
            </form>
          )}
          {saveMessage ? <p className="auth-success">{saveMessage}</p> : null}
          {saveError ? (
            <p className="auth-error" role="alert">
              {saveError}
            </p>
          ) : null}
        </section>
      ) : null}

      <div className="search-layout">
        <section aria-labelledby="search-results-heading" aria-busy={busy}>
          <h2 id="search-results-heading" className="auth-title">
            Sonuçlar
          </h2>
          {results.kind === "loading" ? <p>İlanlar yükleniyor…</p> : null}
          {results.kind === "unavailable" ? (
            <div className="page-status">
              <p className="auth-error" role="alert">
                {results.message}
              </p>
              <button type="button" onClick={() => void onRetryResults()}>
                Tekrar dene
              </button>
            </div>
          ) : null}
          {results.kind === "ready" && results.listings.length === 0 ? (
            <p>Bu aramaya uygun ilan bulunamadı.</p>
          ) : null}
          {results.kind === "ready" && results.listings.length > 0 ? (
            <>
              <p className="search-count">{results.listings.length} ilan gösteriliyor</p>
              <div className="search-results">
                {results.listings.map((listing) => (
                  <ListingCard
                    key={listing.listingId}
                    listing={listing}
                    categoryLabel={categoryLabels.get(listing.categoryId)}
                    selected={listing.listingId === selectedListingId}
                    onHighlight={() => highlightFromList(listing.listingId)}
                    sessionReady={sessionReady}
                    authenticated={authenticated}
                    favorited={favoriteIds.has(listing.listingId)}
                    onFavoriteChange={(next) => {
                      setFavoriteIds((current) => {
                        const copy = new Set(current);
                        if (next) {
                          copy.add(listing.listingId);
                        } else {
                          copy.delete(listing.listingId);
                        }
                        return copy;
                      });
                    }}
                  />
                ))}
              </div>
              {results.nextCursor ? (
                <div className="auth-actions">
                  <button type="button" onClick={() => void onLoadMore()} disabled={loadingMore}>
                    {loadingMore ? "Yükleniyor…" : "Daha fazla yükle"}
                  </button>
                </div>
              ) : null}
            </>
          ) : null}
        </section>

        <section className="search-map-panel" aria-labelledby="search-map-heading">
          <h2 id="search-map-heading" className="auth-title">
            Harita
          </h2>
          {mapFailed ? (
            <p className="search-map-fallback" role="status">
              Harita şu anda kullanılamıyor. İlan listesinden devam edebilirsiniz.
            </p>
          ) : (
            <>
              <div className="search-map-toolbar">
                <button
                  type="button"
                  onClick={onSearchThisArea}
                  disabled={busy || !boundsDirty || !pendingViewport}
                >
                  Bu alanda ara
                </button>
              </div>
              <div className="search-map">
                <MapErrorBoundary onError={() => setMapFailed(true)}>
                  <SearchMap
                    listings={listings}
                    selectedListingId={selectedListingId}
                    showPopup={selectionFromMap}
                    cameraKey={String(cameraKey)}
                    cameraViewport={appliedViewport}
                    onSelectListing={highlightFromMap}
                    onUserBoundsChange={(viewport) => {
                      if (!isValidViewport(viewport) || viewportEqual(viewport, appliedViewport)) {
                        return;
                      }
                      setPendingViewport(viewport);
                      setBoundsDirty(true);
                    }}
                    onUnavailable={() => setMapFailed(true)}
                  />
                </MapErrorBoundary>
              </div>
            </>
          )}
        </section>
      </div>
    </main>
  );
}

function CategoryField({
  catalog,
  categoryId,
  disabled,
  onChange,
  onRetry,
}: {
  catalog: CatalogState;
  categoryId: string;
  disabled: boolean;
  onChange: (categoryId: string) => void;
  onRetry: () => void;
}) {
  if (catalog.kind === "loading") {
    return <p>Kategoriler yükleniyor…</p>;
  }
  if (catalog.kind === "empty") {
    return (
      <p>
        Henüz kategori kataloğu yok. Anahtar kelime ile arama yapabilirsiniz.
      </p>
    );
  }
  if (catalog.kind === "unavailable") {
    return (
      <div>
        <p className="auth-error">{catalog.message}</p>
        <p>Kategori seçilemiyor. Anahtar kelime ile arama yapabilirsiniz.</p>
        <button type="button" onClick={onRetry} disabled={disabled}>
          Kategorileri tekrar dene
        </button>
      </div>
    );
  }
  return (
    <>
      <label className="auth-field" htmlFor="categoryId">
        Kategori
      </label>
      <select
        id="categoryId"
        name="categoryId"
        value={categoryId}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
      >
        <option value="">Tüm kategoriler</option>
        {categoryId && !catalog.options.some((option) => option.id === categoryId) ? (
          <option value={categoryId}>Seçili kategori</option>
        ) : null}
        {catalog.options.map((option) => (
          <option key={option.id} value={option.id}>
            {categoryOptionLabel(option)}
          </option>
        ))}
      </select>
    </>
  );
}
