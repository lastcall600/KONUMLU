import { apiFetch } from "@/lib/api";
import { DEFAULT_LOCALE } from "@/lib/locale";

export type CatalogCategory = {
  id: string;
  code: string;
  parentId: string | null;
  label: string;
  hasPublishedForm: boolean;
  schemaVersion?: number;
};

export type CategoryTreeItem = CatalogCategory & {
  depth: number;
};

export type FormOption = {
  code: string;
  label: string;
};

export type FormField = {
  code: string;
  label: string;
  helpText?: string;
  valueType: "text" | "integer" | "decimal" | "boolean" | "enum";
  required: boolean;
  constraints: Record<string, unknown>;
  options: FormOption[];
};

export type CategoryForm = {
  categoryId: string;
  categoryCode: string;
  schemaVersion: number;
  label: string;
  fields: FormField[];
};

export type FieldValues = Record<string, string | boolean>;

export class MasterDataClientError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "MasterDataClientError";
    this.code = code;
  }
}

const GENERIC = "Katalog bilgisi alınamadı. Lütfen tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const BAD_REQUEST = "İstek geçersiz. Lütfen tekrar deneyin.";
const NO_FORM = "Bu kategori için yayımlanmış form yok.";
const NOT_FOUND = "Kategori bulunamadı.";

type ErrorBody = {
  error?: string;
};

function readErrorCode(body: unknown): string | undefined {
  if (typeof body !== "object" || body === null || !("error" in body)) {
    return undefined;
  }
  const error = (body as ErrorBody).error;
  return typeof error === "string" ? error : undefined;
}

async function readJson(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

function errorFromResponse(status: number, body: unknown, notFoundMessage: string): MasterDataClientError {
  const code = readErrorCode(body);
  if (status === 503 || code === "unavailable") {
    return new MasterDataClientError("unavailable", UNAVAILABLE);
  }
  if (status === 404 || code === "not_found") {
    return new MasterDataClientError("not_found", notFoundMessage);
  }
  if (status === 400 || code === "bad_request") {
    return new MasterDataClientError("bad_request", BAD_REQUEST);
  }
  return new MasterDataClientError("generic", GENERIC);
}

function parseParentId(raw: unknown): string | null {
  if (typeof raw === "string" && raw !== "") {
    return raw;
  }
  return null;
}

function parseCategory(raw: unknown): CatalogCategory | null {
  if (typeof raw !== "object" || raw === null) {
    return null;
  }
  const item = raw as Record<string, unknown>;
  if (typeof item.id !== "string" || item.id === "") {
    return null;
  }
  if (typeof item.code !== "string" || item.code === "") {
    return null;
  }
  if (typeof item.label !== "string") {
    return null;
  }
  if (typeof item.hasPublishedForm !== "boolean") {
    return null;
  }
  const category: CatalogCategory = {
    id: item.id,
    code: item.code,
    parentId: parseParentId(item.parentId),
    label: item.label,
    hasPublishedForm: item.hasPublishedForm,
  };
  if (typeof item.schemaVersion === "number" && Number.isInteger(item.schemaVersion)) {
    category.schemaVersion = item.schemaVersion;
  }
  return category;
}

function parseValueType(raw: unknown): FormField["valueType"] | null {
  if (raw === "text" || raw === "integer" || raw === "decimal" || raw === "boolean" || raw === "enum") {
    return raw;
  }
  return null;
}

function parseOptions(raw: unknown): FormOption[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const options: FormOption[] = [];
  for (const item of raw) {
    if (typeof item !== "object" || item === null) {
      continue;
    }
    const option = item as Record<string, unknown>;
    if (typeof option.code !== "string" || option.code === "") {
      continue;
    }
    options.push({
      code: option.code,
      label: typeof option.label === "string" ? option.label : option.code,
    });
  }
  return options;
}

function parseField(raw: unknown): FormField | null {
  if (typeof raw !== "object" || raw === null) {
    return null;
  }
  const item = raw as Record<string, unknown>;
  const valueType = parseValueType(item.valueType);
  if (typeof item.code !== "string" || item.code === "" || valueType === null) {
    return null;
  }
  if (typeof item.label !== "string" || typeof item.required !== "boolean") {
    return null;
  }
  const field: FormField = {
    code: item.code,
    label: item.label,
    valueType,
    required: item.required,
    constraints:
      typeof item.constraints === "object" && item.constraints !== null && !Array.isArray(item.constraints)
        ? (item.constraints as Record<string, unknown>)
        : {},
    options: valueType === "enum" ? parseOptions(item.options) : [],
  };
  if (typeof item.helpText === "string" && item.helpText !== "") {
    field.helpText = item.helpText;
  }
  return field;
}

function parseForm(body: unknown): CategoryForm | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const raw = body as Record<string, unknown>;
  if (typeof raw.categoryId !== "string" || raw.categoryId === "") {
    return null;
  }
  if (typeof raw.categoryCode !== "string" || raw.categoryCode === "") {
    return null;
  }
  if (typeof raw.schemaVersion !== "number" || !Number.isInteger(raw.schemaVersion) || raw.schemaVersion < 1) {
    return null;
  }
  if (typeof raw.label !== "string") {
    return null;
  }
  if (!Array.isArray(raw.fields)) {
    return null;
  }
  const fields: FormField[] = [];
  for (const item of raw.fields) {
    const field = parseField(item);
    if (!field) {
      return null;
    }
    fields.push(field);
  }
  return {
    categoryId: raw.categoryId,
    categoryCode: raw.categoryCode,
    schemaVersion: raw.schemaVersion,
    label: raw.label,
    fields,
  };
}

export async function listPublishedCategories(locale: string = DEFAULT_LOCALE): Promise<CatalogCategory[]> {
  const response = await apiFetch(`/v1/master-data/categories?locale=${encodeURIComponent(locale)}`, {
    method: "GET",
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body, NOT_FOUND);
  }
  if (typeof body !== "object" || body === null) {
    throw new MasterDataClientError("generic", GENERIC);
  }
  const list = (body as { categories?: unknown }).categories;
  if (!Array.isArray(list)) {
    throw new MasterDataClientError("generic", GENERIC);
  }
  const categories: CatalogCategory[] = [];
  for (const item of list) {
    const category = parseCategory(item);
    if (!category) {
      throw new MasterDataClientError("generic", GENERIC);
    }
    categories.push(category);
  }
  return categories;
}

export async function getPublishedCategoryForm(
  categoryId: string,
  locale: string = DEFAULT_LOCALE,
): Promise<CategoryForm> {
  const response = await apiFetch(
    `/v1/master-data/categories/${encodeURIComponent(categoryId)}/form?locale=${encodeURIComponent(locale)}`,
    { method: "GET" },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body, NO_FORM);
  }
  const form = parseForm(body);
  if (!form) {
    throw new MasterDataClientError("generic", GENERIC);
  }
  return form;
}

export async function getCategoryFormAtVersion(
  categoryId: string,
  schemaVersion: number,
  locale: string = DEFAULT_LOCALE,
): Promise<CategoryForm> {
  const response = await apiFetch(
    `/v1/master-data/categories/${encodeURIComponent(categoryId)}/schemas/${encodeURIComponent(String(schemaVersion))}/form?locale=${encodeURIComponent(locale)}`,
    { method: "GET" },
  );
  const body = await readJson(response);
  if (!response.ok) {
    throw errorFromResponse(response.status, body, NO_FORM);
  }
  const form = parseForm(body);
  if (!form) {
    throw new MasterDataClientError("generic", GENERIC);
  }
  return form;
}

export function flattenCategoryTree(categories: CatalogCategory[]): CategoryTreeItem[] {
  const byId = new Map(categories.map((category) => [category.id, category]));
  const children = new Map<string, CatalogCategory[]>();
  const roots: CatalogCategory[] = [];

  for (const category of categories) {
    const parentId = category.parentId;
    if (parentId && byId.has(parentId)) {
      const siblings = children.get(parentId) ?? [];
      siblings.push(category);
      children.set(parentId, siblings);
    } else {
      roots.push(category);
    }
  }

  const byLabel = (a: CatalogCategory, b: CatalogCategory) => a.label.localeCompare(b.label, "tr");
  roots.sort(byLabel);
  for (const siblings of children.values()) {
    siblings.sort(byLabel);
  }

  const out: CategoryTreeItem[] = [];
  const visit = (category: CatalogCategory, depth: number) => {
    out.push({ ...category, depth });
    const nested = children.get(category.id) ?? [];
    for (const child of nested) {
      visit(child, depth + 1);
    }
  };
  for (const root of roots) {
    visit(root, 0);
  }
  return out;
}

export function emptyFieldValues(fields: FormField[]): FieldValues {
  const values: FieldValues = {};
  for (const field of fields) {
    values[field.code] = field.valueType === "boolean" ? false : "";
  }
  return values;
}

export function fieldValuesFromAttributes(fields: FormField[], attributes: Record<string, unknown>): FieldValues {
  const values = emptyFieldValues(fields);
  for (const field of fields) {
    const raw = attributes[field.code];
    if (raw === undefined) {
      continue;
    }
    if (field.valueType === "boolean") {
      values[field.code] = raw === true;
      continue;
    }
    if (typeof raw === "string") {
      values[field.code] = raw;
      continue;
    }
    if (typeof raw === "number" && Number.isFinite(raw)) {
      values[field.code] = String(raw);
    }
  }
  return values;
}

function constraintNumber(constraints: Record<string, unknown>, keys: string[]): number | undefined {
  for (const key of keys) {
    const raw = constraints[key];
    if (typeof raw === "number" && Number.isFinite(raw)) {
      return raw;
    }
  }
  return undefined;
}

function textLength(value: string): number {
  return Array.from(value).length;
}

function useTextarea(field: FormField): boolean {
  const max = constraintNumber(field.constraints, ["maxLength", "max_length"]);
  return max === undefined || max > 80;
}

export function isMultilineTextField(field: FormField): boolean {
  return field.valueType === "text" && useTextarea(field);
}

export function validateDynamicFields(
  fields: FormField[],
  values: FieldValues,
): { ok: true } | { ok: false; errors: Record<string, string>; message: string } {
  const errors: Record<string, string> = {};
  for (const field of fields) {
    const error = validateField(field, values[field.code]);
    if (error) {
      errors[field.code] = error;
    }
  }
  const keys = Object.keys(errors);
  if (keys.length === 0) {
    return { ok: true };
  }
  return {
    ok: false,
    errors,
    message: "Zorunlu veya geçersiz alanlar var. Lütfen kontrol edin.",
  };
}

function validateField(field: FormField, raw: string | boolean | undefined): string | null {
  if (field.valueType === "boolean") {
    return null;
  }
  const text = typeof raw === "string" ? raw.trim() : "";
  if (text === "") {
    return field.required ? "Bu alan zorunludur." : null;
  }
  switch (field.valueType) {
    case "text":
      return validateTextConstraints(text, field.constraints);
    case "integer": {
      const parsed = Number(text);
      if (!Number.isInteger(parsed)) {
        return "Tam sayı girin.";
      }
      return validateNumericConstraints(parsed, field.constraints);
    }
    case "decimal": {
      const parsed = Number(text);
      if (!Number.isFinite(parsed)) {
        return "Sayı girin.";
      }
      return validateNumericConstraints(parsed, field.constraints);
    }
    case "enum":
      if (!field.options.some((option) => option.code === text)) {
        return "Geçerli bir seçenek seçin.";
      }
      return null;
    default:
      return null;
  }
}

function validateTextConstraints(value: string, constraints: Record<string, unknown>): string | null {
  const n = textLength(value);
  const min = constraintNumber(constraints, ["minLength", "min_length"]);
  const max = constraintNumber(constraints, ["maxLength", "max_length"]);
  if (min !== undefined && n < min) {
    return `En az ${min} karakter girin.`;
  }
  if (max !== undefined && n > max) {
    return `En fazla ${max} karakter girin.`;
  }
  return null;
}

function validateNumericConstraints(value: number, constraints: Record<string, unknown>): string | null {
  const min = constraintNumber(constraints, ["min"]);
  const max = constraintNumber(constraints, ["max"]);
  if (min !== undefined && value < min) {
    return `En az ${min} olmalıdır.`;
  }
  if (max !== undefined && value > max) {
    return `En fazla ${max} olmalıdır.`;
  }
  return null;
}

export function buildAttributes(fields: FormField[], values: FieldValues): Record<string, unknown> {
  const attributes: Record<string, unknown> = {};
  for (const field of fields) {
    const raw = values[field.code];
    if (field.valueType === "boolean") {
      attributes[field.code] = raw === true;
      continue;
    }
    const text = typeof raw === "string" ? raw.trim() : "";
    if (text === "") {
      continue;
    }
    if (field.valueType === "integer") {
      attributes[field.code] = Number(text);
      continue;
    }
    if (field.valueType === "decimal") {
      attributes[field.code] = Number(text);
      continue;
    }
    attributes[field.code] = text;
  }
  return attributes;
}

export function inputMaxLength(field: FormField): number | undefined {
  const max = constraintNumber(field.constraints, ["maxLength", "max_length"]);
  return max === undefined ? undefined : Math.trunc(max);
}

export function numericInputBounds(field: FormField): { min?: number; max?: number } {
  return {
    min: constraintNumber(field.constraints, ["min"]),
    max: constraintNumber(field.constraints, ["max"]),
  };
}
