"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import { AuthClientError, getSession } from "@/lib/auth";
import {
  archiveListing,
  attachListingMedia,
  createListing,
  ListingsClientError,
  markListingReady,
  patchDraft,
  publishListing,
  replaceListingLocation,
  type Listing,
} from "@/lib/listings";
import {
  buildAttributes,
  emptyFieldValues,
  flattenCategoryTree,
  getPublishedCategoryForm,
  inputMaxLength,
  isMultilineTextField,
  listPublishedCategories,
  MasterDataClientError,
  numericInputBounds,
  validateDynamicFields,
  type CatalogCategory,
  type CategoryForm,
  type CategoryTreeItem,
  type FieldValues,
  type FormField,
} from "@/lib/masterdata";
import {
  getListingImage,
  isImageFile,
  isProcessingListingImage,
  isReadyListingImage,
  isRejectedListingImage,
  MediaClientError,
  pollListingImageUntilSettled,
  uploadListingImage,
} from "@/lib/media";

type WizardStep = 1 | 2 | 3 | 4 | 5;

type Status =
  | { kind: "idle" }
  | { kind: "loading"; message: string }
  | { kind: "success"; message: string }
  | { kind: "error"; message: string };

type CatalogState =
  | { kind: "loading" }
  | { kind: "empty" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; categories: CatalogCategory[]; options: CategoryTreeItem[] };

type FormState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "no_form" }
  | { kind: "unavailable"; message: string }
  | { kind: "ready"; form: CategoryForm };

type PhotoItem = {
  assetId: string;
  filename: string;
  status: string;
  attached?: boolean;
  pollExhausted?: boolean;
};

const UUID_RE =
  /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;

function messageFromError(error: unknown): string {
  if (
    error instanceof AuthClientError ||
    error instanceof ListingsClientError ||
    error instanceof MediaClientError ||
    error instanceof MasterDataClientError
  ) {
    return error.message;
  }
  return "Bir sorun oluştu. Lütfen tekrar deneyin.";
}

function parseLatitude(raw: string): number | { error: string } {
  const trimmed = raw.trim();
  if (trimmed === "") {
    return { error: "Enlem gerekli." };
  }
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value < -90 || value > 90) {
    return { error: "Enlem -90 ile 90 arasında olmalıdır." };
  }
  return value;
}

function parseLongitude(raw: string): number | { error: string } {
  const trimmed = raw.trim();
  if (trimmed === "") {
    return { error: "Boylam gerekli." };
  }
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value < -180 || value > 180) {
    return { error: "Boylam -180 ile 180 arasında olmalıdır." };
  }
  return value;
}

function listingStatusLabel(status: string): string {
  if (status === "draft") {
    return "Taslak";
  }
  if (status === "ready") {
    return "Hazır (yayımlanmadı)";
  }
  if (status === "archived") {
    return "Arşivlendi";
  }
  if (status === "published") {
    return "Yayımlandı";
  }
  if (status === "verification_pending") {
    return "Doğrulama bekleniyor";
  }
  return `Durum: ${status}`;
}

function canPublishListing(status: string): boolean {
  return status === "ready" || status === "verification_pending";
}

function photoStatusLabel(status: string): string {
  if (isReadyListingImage(status)) {
    return "İşleme tamamlandı";
  }
  if (isRejectedListingImage(status)) {
    return "Görsel reddedildi";
  }
  if (isProcessingListingImage(status)) {
    return "Görsel hazırlanıyor";
  }
  return "Görsel durumu alınamadı";
}

function categoryOptionLabel(option: CategoryTreeItem): string {
  const prefix = option.depth > 0 ? `${"— ".repeat(option.depth)}` : "";
  return `${prefix}${option.label}`;
}

export default function CreateListingPage() {
  const router = useRouter();
  const [gateReady, setGateReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [step, setStep] = useState<WizardStep>(1);
  const [status, setStatus] = useState<Status>({ kind: "idle" });

  const [catalog, setCatalog] = useState<CatalogState>({ kind: "loading" });
  const [categoryId, setCategoryId] = useState("");
  const [formState, setFormState] = useState<FormState>({ kind: "idle" });
  const [fieldValues, setFieldValues] = useState<FieldValues>({});
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [priceAmount, setPriceAmount] = useState("");
  const [priceCurrency, setPriceCurrency] = useState("TRY");

  const [latitude, setLatitude] = useState("");
  const [longitude, setLongitude] = useState("");
  const [catalogLocationId, setCatalogLocationId] = useState("");
  const [includeLocation, setIncludeLocation] = useState(true);

  const [photos, setPhotos] = useState<PhotoItem[]>([]);
  const [listing, setListing] = useState<Listing | null>(null);
  const [savedLocation, setSavedLocation] = useState<{
    latitude: number;
    longitude: number;
    catalogLocationId?: string;
  } | null>(null);

  const busy = status.kind === "loading";
  const categoryLocked = listing !== null;
  const pollingRef = useRef(new Set<string>());
  const attachingRef = useRef(new Set<string>());
  const formRequestRef = useRef(0);

  useEffect(() => {
    let cancelled = false;

    async function loadSession() {
      setStatus({ kind: "loading", message: "Oturum kontrol ediliyor…" });
      try {
        const current = await getSession();
        if (cancelled) {
          return;
        }
        setAuthenticated(current !== null);
        setStatus({ kind: "idle" });
        if (current === null) {
          router.replace("/giris");
        }
      } catch (error) {
        if (cancelled) {
          return;
        }
        setAuthenticated(false);
        setStatus({ kind: "error", message: messageFromError(error) });
      } finally {
        if (!cancelled) {
          setGateReady(true);
        }
      }
    }

    void loadSession();
    return () => {
      cancelled = true;
    };
  }, [router]);

  useEffect(() => {
    if (!authenticated) {
      return;
    }
    let cancelled = false;

    async function loadCatalog() {
      setCatalog({ kind: "loading" });
      try {
        const categories = await listPublishedCategories();
        if (cancelled) {
          return;
        }
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
        if (cancelled) {
          return;
        }
        setCatalog({ kind: "unavailable", message: messageFromError(error) });
      }
    }

    void loadCatalog();
    return () => {
      cancelled = true;
    };
  }, [authenticated]);

  useEffect(() => {
    for (const photo of photos) {
      if (!isProcessingListingImage(photo.status) || photo.pollExhausted) {
        continue;
      }
      if (pollingRef.current.has(photo.assetId)) {
        continue;
      }
      pollingRef.current.add(photo.assetId);
      const assetId = photo.assetId;
      void (async () => {
        try {
          const asset = await pollListingImageUntilSettled(assetId);
          setPhotos((current) =>
            current.map((item) =>
              item.assetId === assetId
                ? {
                    ...item,
                    status: asset.status,
                    pollExhausted: isProcessingListingImage(asset.status),
                  }
                : item,
            ),
          );
        } catch {
          setPhotos((current) =>
            current.map((item) => (item.assetId === assetId ? { ...item, pollExhausted: true } : item)),
          );
        } finally {
          pollingRef.current.delete(assetId);
        }
      })();
    }
  }, [photos]);

  useEffect(() => {
    const currentListing = listing;
    if (!currentListing) {
      return;
    }
    for (const photo of photos) {
      if (!isReadyListingImage(photo.status) || photo.attached) {
        continue;
      }
      if (attachingRef.current.has(photo.assetId)) {
        continue;
      }
      attachingRef.current.add(photo.assetId);
      const assetId = photo.assetId;
      void (async () => {
        try {
          await attachListingMedia(currentListing.listingId, [assetId]);
          setPhotos((current) =>
            current.map((item) => (item.assetId === assetId ? { ...item, attached: true } : item)),
          );
        } catch (error) {
          attachingRef.current.delete(assetId);
          setStatus({ kind: "error", message: messageFromError(error) });
        }
      })();
    }
  }, [photos, listing]);

  function clearDynamicForm() {
    setFormState({ kind: "idle" });
    setFieldValues({});
    setFieldErrors({});
  }

  async function loadFormForCategory(nextCategoryId: string, hasPublishedForm: boolean) {
    const requestId = formRequestRef.current + 1;
    formRequestRef.current = requestId;
    setFieldValues({});
    setFieldErrors({});
    if (!hasPublishedForm) {
      setFormState({ kind: "no_form" });
      return;
    }
    setFormState({ kind: "loading" });
    try {
      const form = await getPublishedCategoryForm(nextCategoryId);
      if (formRequestRef.current !== requestId) {
        return;
      }
      setFormState({ kind: "ready", form });
      setFieldValues(emptyFieldValues(form.fields));
    } catch (error) {
      if (formRequestRef.current !== requestId) {
        return;
      }
      if (error instanceof MasterDataClientError && error.code === "not_found") {
        setFormState({ kind: "no_form" });
        return;
      }
      setFormState({ kind: "unavailable", message: messageFromError(error) });
    }
  }

  function onCategoryChange(nextId: string) {
    if (categoryLocked) {
      return;
    }
    setCategoryId(nextId);
    if (nextId === "") {
      clearDynamicForm();
      return;
    }
    const selected =
      catalog.kind === "ready" ? catalog.categories.find((category) => category.id === nextId) : undefined;
    void loadFormForCategory(nextId, selected?.hasPublishedForm === true);
  }

  function setFieldValue(code: string, value: string | boolean) {
    setFieldValues((current) => ({ ...current, [code]: value }));
    setFieldErrors((current) => {
      if (!(code in current)) {
        return current;
      }
      const next = { ...current };
      delete next[code];
      return next;
    });
  }

  function validateBasic(): string | null {
    if (catalog.kind === "loading") {
      return "Kategoriler yükleniyor…";
    }
    if (catalog.kind === "empty") {
      return "Henüz kategori tanımlanmadı.";
    }
    if (catalog.kind === "unavailable") {
      return catalog.message;
    }
    if (categoryId === "") {
      return "Kategori seçin.";
    }
    if (formState.kind === "loading") {
      return "Kategori formu yükleniyor…";
    }
    if (formState.kind === "no_form") {
      return "Bu kategori için yayımlanmış form yok.";
    }
    if (formState.kind === "unavailable") {
      return formState.message;
    }
    if (formState.kind !== "ready") {
      return "Kategori formu gerekli.";
    }
    const fieldsCheck = validateDynamicFields(formState.form.fields, fieldValues);
    if (!fieldsCheck.ok) {
      setFieldErrors(fieldsCheck.errors);
      return fieldsCheck.message;
    }
    setFieldErrors({});
    if (title.trim() === "") {
      return "Başlık gerekli.";
    }
    if (description.trim() === "") {
      return "Açıklama gerekli.";
    }
    if ((priceAmount.trim() === "") !== (priceCurrency.trim() === "")) {
      return "Fiyat tutarı ve para birimi birlikte doldurulmalıdır.";
    }
    return null;
  }

  function validateLocation(): string | null {
    if (!includeLocation) {
      return null;
    }
    const lat = parseLatitude(latitude);
    if (typeof lat !== "number") {
      return lat.error;
    }
    const lon = parseLongitude(longitude);
    if (typeof lon !== "number") {
      return lon.error;
    }
    const catalogId = catalogLocationId.trim();
    if (catalogId !== "" && !UUID_RE.test(catalogId)) {
      return "Katalog konum kimliği boş veya geçerli bir UUID olmalıdır.";
    }
    return null;
  }

  function onBasicNext(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const error = validateBasic();
    if (error) {
      setStatus({ kind: "error", message: error });
      return;
    }
    setStatus({ kind: "idle" });
    setStep(2);
  }

  function onLocationNext(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const error = validateLocation();
    if (error) {
      setStatus({ kind: "error", message: error });
      return;
    }
    setStatus({ kind: "idle" });
    setStep(3);
  }

  async function onPickPhotos(files: FileList | null) {
    if (!files || files.length === 0) {
      return;
    }
    for (const file of Array.from(files)) {
      if (!isImageFile(file)) {
        setStatus({ kind: "error", message: "Yalnızca görsel dosyaları seçilebilir." });
        continue;
      }
      setStatus({ kind: "loading", message: `Yükleme sürüyor: ${file.name}` });
      try {
        const asset = await uploadListingImage(file);
        setPhotos((current) => [
          ...current,
          {
            assetId: asset.assetId,
            filename: asset.originalFilename || file.name,
            status: asset.status,
          },
        ]);
        setStatus({
          kind: "success",
          message: isReadyListingImage(asset.status)
            ? "Yükleme onaylandı ve görsel hazır."
            : isRejectedListingImage(asset.status)
              ? "Görsel reddedildi."
              : "Yükleme onaylandı. Görsel hazırlanıyor.",
        });
      } catch (error) {
        setStatus({ kind: "error", message: messageFromError(error) });
        return;
      }
    }
  }

  async function refreshPhoto(assetId: string) {
    setStatus({ kind: "loading", message: "Görsel durumu alınıyor…" });
    try {
      const asset = await getListingImage(assetId);
      setPhotos((current) =>
        current.map((item) =>
          item.assetId === assetId
            ? {
                ...item,
                status: asset.status,
                pollExhausted: isProcessingListingImage(asset.status) ? false : item.pollExhausted,
              }
            : item,
        ),
      );
      setStatus({ kind: "success", message: photoStatusLabel(asset.status) });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onCreate() {
    const basicError = validateBasic();
    if (basicError) {
      setStatus({ kind: "error", message: basicError });
      setStep(1);
      return;
    }
    const locationError = validateLocation();
    if (locationError) {
      setStatus({ kind: "error", message: locationError });
      setStep(2);
      return;
    }
    if (formState.kind !== "ready") {
      setStatus({ kind: "error", message: "Kategori formu gerekli." });
      setStep(1);
      return;
    }
    const attributes = buildAttributes(formState.form.fields, fieldValues);
    const mediaAssetIds = photos.filter((photo) => isReadyListingImage(photo.status)).map((photo) => photo.assetId);
    setStatus({ kind: "loading", message: "İlan kaydediliyor…" });
    try {
      const created = await createListing({
        categoryId: formState.form.categoryId,
        categorySchemaVersion: formState.form.schemaVersion,
        title: title.trim(),
        description: description.trim(),
        priceAmount: priceAmount.trim() || undefined,
        priceCurrency: priceCurrency.trim() || undefined,
        attributes,
        location: includeLocation
          ? {
              latitude: parseLatitude(latitude) as number,
              longitude: parseLongitude(longitude) as number,
              catalogLocationId: catalogLocationId.trim() || undefined,
            }
          : undefined,
        mediaAssetIds,
      });
      setPhotos((current) =>
        current.map((photo) => ({
          ...photo,
          attached: mediaAssetIds.includes(photo.assetId) ? true : photo.attached,
        })),
      );
      setListing(created);
      if (includeLocation) {
        setSavedLocation({
          latitude: parseLatitude(latitude) as number,
          longitude: parseLongitude(longitude) as number,
          catalogLocationId: catalogLocationId.trim() || undefined,
        });
      }
      setStep(5);
      const pendingCount = photos.filter((photo) => isProcessingListingImage(photo.status)).length;
      const createdLabel =
        created.status === "draft"
          ? "Taslak oluşturuldu."
          : created.status === "ready"
            ? "İlan hazır olarak kaydedildi (yayımlanmadı)."
            : "İlan oluşturuldu.";
      setStatus({
        kind: "success",
        message:
          pendingCount > 0 ? `${createdLabel} Görsel hazırlanıyor; hazır olunca ilana eklenecek.` : createdLabel,
      });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onPatchDraft(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!listing) {
      return;
    }
    const basicError = validateBasic();
    if (basicError) {
      setStatus({ kind: "error", message: basicError });
      return;
    }
    if (formState.kind !== "ready") {
      setStatus({ kind: "error", message: "Kategori formu gerekli." });
      return;
    }
    setStatus({ kind: "loading", message: "İlan kaydediliyor…" });
    try {
      const updated = await patchDraft(listing.listingId, {
        title: title.trim(),
        description: description.trim(),
        priceAmount: priceAmount.trim() === "" ? null : priceAmount.trim(),
        priceCurrency: priceCurrency.trim() === "" ? null : priceCurrency.trim(),
        attributes: buildAttributes(formState.form.fields, fieldValues),
        updatedAt: listing.updatedAt,
      });
      setListing(updated);
      setStatus({ kind: "success", message: "Taslak içeriği güncellendi." });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onReplaceLocation(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!listing) {
      return;
    }
    const lat = parseLatitude(latitude);
    if (typeof lat !== "number") {
      setStatus({ kind: "error", message: lat.error });
      return;
    }
    const lon = parseLongitude(longitude);
    if (typeof lon !== "number") {
      setStatus({ kind: "error", message: lon.error });
      return;
    }
    const catalogId = catalogLocationId.trim();
    if (catalogId !== "" && !UUID_RE.test(catalogId)) {
      setStatus({ kind: "error", message: "Katalog konum kimliği boş veya geçerli bir UUID olmalıdır." });
      return;
    }
    setStatus({ kind: "loading", message: "Konum kaydediliyor…" });
    try {
      const point = await replaceListingLocation(listing.listingId, {
        latitude: lat,
        longitude: lon,
        catalogLocationId: catalogId || undefined,
      });
      setSavedLocation({
        latitude: point.latitude,
        longitude: point.longitude,
        catalogLocationId: point.catalogLocationId,
      });
      setStatus({ kind: "success", message: "Konum güncellendi." });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onAttachMore(files: FileList | null) {
    if (!listing || !files || files.length === 0) {
      return;
    }
    for (const file of Array.from(files)) {
      if (!isImageFile(file)) {
        setStatus({ kind: "error", message: "Yalnızca görsel dosyaları seçilebilir." });
        continue;
      }
      setStatus({ kind: "loading", message: `Yükleme sürüyor: ${file.name}` });
      try {
        const asset = await uploadListingImage(file);
        const item: PhotoItem = {
          assetId: asset.assetId,
          filename: asset.originalFilename || file.name,
          status: asset.status,
        };
        setPhotos((current) => [...current, item]);
        setStatus({
          kind: "success",
          message: isReadyListingImage(asset.status)
            ? "Yükleme onaylandı. Görsel ilana ekleniyor."
            : isRejectedListingImage(asset.status)
              ? "Görsel reddedildi."
              : "Yükleme onaylandı. Görsel hazırlanıyor.",
        });
      } catch (error) {
        setStatus({ kind: "error", message: messageFromError(error) });
        return;
      }
    }
  }

  async function onReady() {
    if (!listing) {
      return;
    }
    setStatus({ kind: "loading", message: "İlan hazır olarak işaretleniyor…" });
    try {
      const updated = await markListingReady(listing.listingId);
      setListing(updated);
      setStatus({
        kind: "success",
        message: "İlan hazır. Yayımlamak için İlanı Yayınla’yı kullanın.",
      });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onArchive() {
    if (!listing) {
      return;
    }
    setStatus({ kind: "loading", message: "İlan arşivleniyor…" });
    try {
      const updated = await archiveListing(listing.listingId);
      setListing(updated);
      setStatus({ kind: "success", message: "İlan arşivlendi." });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function onPublish() {
    if (!listing) {
      return;
    }
    setStatus({ kind: "loading", message: "İlan yayımlanıyor…" });
    try {
      const updated = await publishListing(listing.listingId);
      setListing(updated);
      setStatus({ kind: "success", message: "İlan yayımlandı." });
    } catch (error) {
      setStatus({ kind: "error", message: messageFromError(error) });
    }
  }

  async function retryCatalog() {
    setCatalog({ kind: "loading" });
    try {
      const categories = await listPublishedCategories();
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
  }

  if (!gateReady) {
    return (
      <main>
        <h1 className="brand">KONUMLU</h1>
        <p>Oturum kontrol ediliyor…</p>
      </main>
    );
  }

  if (!authenticated) {
    return (
      <main>
        <h1 className="brand">KONUMLU</h1>
        <h2 className="auth-title">İlan ver</h2>
        <p>Bu sayfa için giriş yapmanız gerekir.</p>
        <p>
          <Link href="/giris">Girişe git</Link>
        </p>
        {status.kind === "error" ? <p className="auth-error">{status.message}</p> : null}
      </main>
    );
  }

  const selectedCategoryLabel =
    formState.kind === "ready"
      ? formState.form.label
      : catalog.kind === "ready"
        ? (catalog.categories.find((category) => category.id === categoryId)?.label ?? "")
        : "";
  const canAdvanceBasic = catalog.kind === "ready" && formState.kind === "ready" && !busy;

  return (
    <main>
      <h1 className="brand">KONUMLU</h1>
      <h2 className="auth-title">İlan ver</h2>
      <p className="auth-lead">
        Kategori ve form Master Data kataloğundan gelir. Harita, yayımlama ve EİDS yok.{" "}
        <Link href="/">Ana sayfa</Link>
      </p>

      {step < 5 ? (
        <ol className="auth-steps listing-steps">
          <li className={step === 1 ? "listing-step-current" : undefined}>1. Temel bilgiler</li>
          <li className={step === 2 ? "listing-step-current" : undefined}>2. Konum</li>
          <li className={step === 3 ? "listing-step-current" : undefined}>3. Fotoğraflar</li>
          <li className={step === 4 ? "listing-step-current" : undefined}>4. Oluştur</li>
        </ol>
      ) : null}

      <div className="auth-status" aria-live="polite">
        {status.kind === "loading" ? <p>{status.message}</p> : null}
        {status.kind === "success" ? <p className="auth-success">{status.message}</p> : null}
        {status.kind === "error" ? <p className="auth-error">{status.message}</p> : null}
      </div>

      {step === 1 ? (
        <section className="auth-card" aria-labelledby="basic-heading">
          <h3 id="basic-heading">Temel bilgiler</h3>
          <form onSubmit={onBasicNext}>
            <CategoryPicker
              catalog={catalog}
              categoryId={categoryId}
              locked={categoryLocked}
              disabled={busy}
              onChange={onCategoryChange}
              onRetry={() => void retryCatalog()}
            />
            <FormFieldsPanel
              formState={formState}
              values={fieldValues}
              errors={fieldErrors}
              disabled={busy}
              onChange={setFieldValue}
              onRetry={() => {
                if (categoryId === "") {
                  return;
                }
                const selected =
                  catalog.kind === "ready"
                    ? catalog.categories.find((category) => category.id === categoryId)
                    : undefined;
                void loadFormForCategory(categoryId, selected?.hasPublishedForm === true);
              }}
            />

            <label className="auth-field" htmlFor="title">
              Başlık
            </label>
            <input
              id="title"
              name="title"
              type="text"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder="İlan başlığı"
              required
              disabled={busy}
            />

            <label className="auth-field" htmlFor="description">
              Açıklama
            </label>
            <textarea
              id="description"
              name="description"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Kısa açıklama"
              required
              disabled={busy}
              rows={5}
            />

            <label className="auth-field" htmlFor="priceAmount">
              Fiyat tutarı (isteğe bağlı)
            </label>
            <input
              id="priceAmount"
              name="priceAmount"
              type="text"
              inputMode="decimal"
              value={priceAmount}
              onChange={(event) => setPriceAmount(event.target.value)}
              placeholder="Örn. 1500.00"
              disabled={busy}
            />

            <label className="auth-field" htmlFor="priceCurrency">
              Para birimi (isteğe bağlı)
            </label>
            <input
              id="priceCurrency"
              name="priceCurrency"
              type="text"
              value={priceCurrency}
              onChange={(event) => setPriceCurrency(event.target.value)}
              placeholder="TRY"
              disabled={busy}
            />

            <button type="submit" disabled={!canAdvanceBasic}>
              Konuma geç
            </button>
          </form>
        </section>
      ) : null}

      {step === 2 ? (
        <section className="auth-card" aria-labelledby="location-heading">
          <h3 id="location-heading">Konum</h3>
          <p>Harita yok. Enlem ve boylamı sayı olarak girin.</p>
          <form onSubmit={onLocationNext}>
            <label className="auth-field">
              <input
                type="checkbox"
                checked={includeLocation}
                onChange={(event) => setIncludeLocation(event.target.checked)}
                disabled={busy}
              />{" "}
              Konumu ilana ekle
            </label>

            <label className="auth-field" htmlFor="latitude">
              Enlem
            </label>
            <input
              id="latitude"
              name="latitude"
              type="text"
              inputMode="decimal"
              value={latitude}
              onChange={(event) => setLatitude(event.target.value)}
              placeholder="Örn. 41.0082"
              disabled={busy || !includeLocation}
            />

            <label className="auth-field" htmlFor="longitude">
              Boylam
            </label>
            <input
              id="longitude"
              name="longitude"
              type="text"
              inputMode="decimal"
              value={longitude}
              onChange={(event) => setLongitude(event.target.value)}
              placeholder="Örn. 28.9784"
              disabled={busy || !includeLocation}
            />

            <label className="auth-field" htmlFor="catalogLocationId">
              Katalog konum kimliği (isteğe bağlı UUID)
            </label>
            <input
              id="catalogLocationId"
              name="catalogLocationId"
              type="text"
              value={catalogLocationId}
              onChange={(event) => setCatalogLocationId(event.target.value)}
              placeholder="Boş bırakılabilir"
              disabled={busy || !includeLocation}
            />

            <div className="auth-actions">
              <button type="button" onClick={() => setStep(1)} disabled={busy}>
                Geri
              </button>
              <button type="submit" disabled={busy}>
                Fotoğraflara geç
              </button>
            </div>
          </form>
        </section>
      ) : null}

      {step === 3 ? (
        <section className="auth-card" aria-labelledby="photos-heading">
          <h3 id="photos-heading">Fotoğraflar</h3>
          <p>
            Görseller önce imzalı yükleme adresine gider. Onay, görselin işlenmiş veya herkese açık olduğu anlamına
            gelmez. Genel görsel adresi bu aşamada yok.
          </p>
          <label className="auth-field" htmlFor="photos">
            Görsel seç
          </label>
          <input
            id="photos"
            name="photos"
            type="file"
            accept="image/*"
            multiple
            disabled={busy}
            onChange={(event) => {
              void onPickPhotos(event.target.files);
              event.target.value = "";
            }}
          />
          <PhotoList photos={photos} busy={busy} onRefresh={(id) => void refreshPhoto(id)} />
          <div className="auth-actions">
            <button type="button" onClick={() => setStep(2)} disabled={busy}>
              Geri
            </button>
            <button type="button" onClick={() => setStep(4)} disabled={busy}>
              Oluşturmaya geç
            </button>
          </div>
        </section>
      ) : null}

      {step === 4 ? (
        <section className="auth-card" aria-labelledby="create-heading">
          <h3 id="create-heading">İlanı oluştur</h3>
          <p>Sahip kimliği gönderilmez. Durum sunucu tarafından belirlenir.</p>
          <dl className="listing-summary">
            <dt>Başlık</dt>
            <dd>{title || "—"}</dd>
            <dt>Kategori</dt>
            <dd>{selectedCategoryLabel || "—"}</dd>
            <dt>Konum</dt>
            <dd>
              {includeLocation
                ? `${latitude.trim() || "?"}, ${longitude.trim() || "?"}`
                : "Eklenmeyecek"}
            </dd>
            <dt>Görseller</dt>
            <dd>
              {photos.filter((photo) => isReadyListingImage(photo.status)).length} hazır görsel
              {photos.some((photo) => isProcessingListingImage(photo.status))
                ? " — diğerleri hazırlanıyor, taslak onlarsız oluşturulur"
                : ""}
            </dd>
          </dl>
          <div className="auth-actions">
            <button type="button" onClick={() => setStep(3)} disabled={busy}>
              Geri
            </button>
            <button type="button" className="auth-primary-action" onClick={() => void onCreate()} disabled={busy}>
              Taslak oluştur
            </button>
          </div>
        </section>
      ) : null}

      {step === 5 && listing ? (
        <section className="auth-card" aria-labelledby="manage-heading">
          <h3 id="manage-heading">Taslak yönetimi</h3>
          <p>
            <strong>İlan kimliği:</strong> {listing.listingId}
          </p>
          <p>
            <strong>Durum:</strong> {listingStatusLabel(listing.status)}
          </p>
          <p>
            <strong>Kategori:</strong> {selectedCategoryLabel || "—"}
          </p>
          <p>Kategori bu taslakta değiştirilemez.</p>
          {listing.status === "published" ? (
            <p className="auth-success">
              İlan yayımlandı.{" "}
              <Link href={`/ilan/${listing.listingId}`}>İlan sayfasına git</Link>
              {" · "}
              <Link href="/ara">Aramaya git</Link>
            </p>
          ) : null}
          {savedLocation ? (
            <p>
              Kayıtlı konum: {savedLocation.latitude}, {savedLocation.longitude}
              {savedLocation.catalogLocationId ? ` (${savedLocation.catalogLocationId})` : ""}
            </p>
          ) : (
            <p>Konum eklenmedi.</p>
          )}

          <form onSubmit={(event) => void onPatchDraft(event)}>
            <h4>Temel içeriği güncelle</h4>
            <label className="auth-field" htmlFor="edit-title">
              Başlık
            </label>
            <input
              id="edit-title"
              type="text"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              disabled={busy}
            />
            <label className="auth-field" htmlFor="edit-description">
              Açıklama
            </label>
            <textarea
              id="edit-description"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              disabled={busy}
              rows={4}
            />
            <label className="auth-field" htmlFor="edit-priceAmount">
              Fiyat tutarı
            </label>
            <input
              id="edit-priceAmount"
              type="text"
              value={priceAmount}
              onChange={(event) => setPriceAmount(event.target.value)}
              disabled={busy}
            />
            <label className="auth-field" htmlFor="edit-priceCurrency">
              Para birimi
            </label>
            <input
              id="edit-priceCurrency"
              type="text"
              value={priceCurrency}
              onChange={(event) => setPriceCurrency(event.target.value)}
              disabled={busy}
            />
            {formState.kind === "ready" ? (
              <DynamicFields
                idPrefix="edit-"
                fields={formState.form.fields}
                values={fieldValues}
                errors={fieldErrors}
                disabled={busy}
                onChange={setFieldValue}
              />
            ) : null}
            <button type="submit" disabled={busy}>
              Taslağı kaydet
            </button>
          </form>

          <form onSubmit={(event) => void onReplaceLocation(event)}>
            <h4>Konumu değiştir</h4>
            <label className="auth-field" htmlFor="edit-latitude">
              Enlem
            </label>
            <input
              id="edit-latitude"
              type="text"
              value={latitude}
              onChange={(event) => setLatitude(event.target.value)}
              disabled={busy}
            />
            <label className="auth-field" htmlFor="edit-longitude">
              Boylam
            </label>
            <input
              id="edit-longitude"
              type="text"
              value={longitude}
              onChange={(event) => setLongitude(event.target.value)}
              disabled={busy}
            />
            <label className="auth-field" htmlFor="edit-catalogLocationId">
              Katalog konum kimliği
            </label>
            <input
              id="edit-catalogLocationId"
              type="text"
              value={catalogLocationId}
              onChange={(event) => setCatalogLocationId(event.target.value)}
              disabled={busy}
            />
            <button type="submit" disabled={busy}>
              Konumu kaydet
            </button>
          </form>

          <h4>Ek görsel</h4>
          <input
            type="file"
            accept="image/*"
            multiple
            disabled={busy}
            onChange={(event) => {
              void onAttachMore(event.target.files);
              event.target.value = "";
            }}
          />
          <PhotoList photos={photos} busy={busy} onRefresh={(id) => void refreshPhoto(id)} />

          <div className="auth-actions">
            {listing.status === "draft" ? (
              <button type="button" onClick={() => void onReady()} disabled={busy}>
                Hazır olarak işaretle
              </button>
            ) : null}
            {canPublishListing(listing.status) ? (
              <button type="button" className="auth-primary-action" onClick={() => void onPublish()} disabled={busy}>
                İlanı Yayınla
              </button>
            ) : null}
            {listing.status !== "archived" ? (
              <button type="button" onClick={() => void onArchive()} disabled={busy}>
                Arşivle
              </button>
            ) : null}
          </div>
        </section>
      ) : null}
    </main>
  );
}

function CategoryPicker({
  catalog,
  categoryId,
  locked,
  disabled,
  onChange,
  onRetry,
}: {
  catalog: CatalogState;
  categoryId: string;
  locked: boolean;
  disabled: boolean;
  onChange: (categoryId: string) => void;
  onRetry: () => void;
}) {
  if (catalog.kind === "loading") {
    return <p>Kategoriler yükleniyor…</p>;
  }
  if (catalog.kind === "empty") {
    return <p>Henüz kategori tanımlanmadı</p>;
  }
  if (catalog.kind === "unavailable") {
    return (
      <div>
        <p className="auth-error">{catalog.message}</p>
        <button type="button" onClick={onRetry} disabled={disabled}>
          Tekrar dene
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
        required
        disabled={disabled || locked}
      >
        <option value="">Kategori seçin</option>
        {catalog.options.map((option) => (
          <option key={option.id} value={option.id}>
            {categoryOptionLabel(option)}
          </option>
        ))}
      </select>
      {locked ? <p className="field-help">Kategori oluşturulmuş taslakta değiştirilemez.</p> : null}
    </>
  );
}

function FormFieldsPanel({
  formState,
  values,
  errors,
  disabled,
  onChange,
  onRetry,
}: {
  formState: FormState;
  values: FieldValues;
  errors: Record<string, string>;
  disabled: boolean;
  onChange: (code: string, value: string | boolean) => void;
  onRetry: () => void;
}) {
  if (formState.kind === "idle") {
    return <p>Kategori seçince forma ait alanlar burada görünür.</p>;
  }
  if (formState.kind === "loading") {
    return <p>Form yükleniyor…</p>;
  }
  if (formState.kind === "no_form") {
    return <p>Bu kategori için yayımlanmış form yok.</p>;
  }
  if (formState.kind === "unavailable") {
    return (
      <div>
        <p className="auth-error">{formState.message}</p>
        <button type="button" onClick={onRetry} disabled={disabled}>
          Formu tekrar yükle
        </button>
      </div>
    );
  }
  if (formState.form.fields.length === 0) {
    return <p>Bu kategoride ek alan yok.</p>;
  }
  return (
    <DynamicFields
      idPrefix=""
      fields={formState.form.fields}
      values={values}
      errors={errors}
      disabled={disabled}
      onChange={onChange}
    />
  );
}

function DynamicFields({
  idPrefix,
  fields,
  values,
  errors,
  disabled,
  onChange,
}: {
  idPrefix: string;
  fields: FormField[];
  values: FieldValues;
  errors: Record<string, string>;
  disabled: boolean;
  onChange: (code: string, value: string | boolean) => void;
}) {
  return (
    <>
      {fields.map((field) => {
        const fieldId = `${idPrefix}attr-${field.code}`;
        const error = errors[field.code];
        const value = values[field.code];
        return (
          <div key={field.code}>
            {field.valueType === "boolean" ? (
              <label className="auth-field" htmlFor={fieldId}>
                <input
                  id={fieldId}
                  type="checkbox"
                  checked={value === true}
                  onChange={(event) => onChange(field.code, event.target.checked)}
                  disabled={disabled}
                />{" "}
                {field.label}
                {field.required ? (
                  <span className="field-required" aria-hidden="true">
                    {" "}
                    *
                  </span>
                ) : null}
              </label>
            ) : (
              <>
                <label className="auth-field" htmlFor={fieldId}>
                  {field.label}
                  {field.required ? (
                    <span className="field-required" aria-hidden="true">
                      {" "}
                      *
                    </span>
                  ) : null}
                </label>
                <FieldControl
                  field={field}
                  id={fieldId}
                  value={typeof value === "string" ? value : ""}
                  disabled={disabled}
                  onChange={(next) => onChange(field.code, next)}
                />
              </>
            )}
            {field.helpText ? <span className="field-help">{field.helpText}</span> : null}
            {error ? (
              <span className="field-error" role="alert">
                {error}
              </span>
            ) : null}
          </div>
        );
      })}
    </>
  );
}

function FieldControl({
  field,
  id,
  value,
  disabled,
  onChange,
}: {
  field: FormField;
  id: string;
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  if (field.valueType === "enum") {
    return (
      <select id={id} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} required={field.required}>
        <option value="">{field.required ? "Seçin" : "Seçim yok"}</option>
        {field.options.map((option) => (
          <option key={option.code} value={option.code}>
            {option.label}
          </option>
        ))}
      </select>
    );
  }
  if (field.valueType === "integer" || field.valueType === "decimal") {
    const bounds = numericInputBounds(field);
    return (
      <input
        id={id}
        type="number"
        inputMode={field.valueType === "integer" ? "numeric" : "decimal"}
        step={field.valueType === "integer" ? 1 : "any"}
        min={bounds.min}
        max={bounds.max}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder="Değer girin"
        required={field.required}
        disabled={disabled}
      />
    );
  }
  const maxLength = inputMaxLength(field);
  if (isMultilineTextField(field)) {
    return (
      <textarea
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder="Metin girin"
        required={field.required}
        disabled={disabled}
        maxLength={maxLength}
        rows={4}
      />
    );
  }
  return (
    <input
      id={id}
      type="text"
      value={value}
      onChange={(event) => onChange(event.target.value)}
      placeholder="Metin girin"
      required={field.required}
      disabled={disabled}
      maxLength={maxLength}
    />
  );
}

function PhotoList({
  photos,
  busy,
  onRefresh,
}: {
  photos: PhotoItem[];
  busy: boolean;
  onRefresh: (assetId: string) => void;
}) {
  if (photos.length === 0) {
    return <p>Henüz görsel yok.</p>;
  }
  return (
    <ul className="listing-photos">
      {photos.map((photo) => (
        <li key={photo.assetId}>
          <span>{photo.filename}</span>
          <span> — {photoStatusLabel(photo.status)}</span>
          {photo.attached ? <span> — ilana bağlandı</span> : null}
          <button type="button" onClick={() => onRefresh(photo.assetId)} disabled={busy}>
            Durumu yenile
          </button>
        </li>
      ))}
    </ul>
  );
}
