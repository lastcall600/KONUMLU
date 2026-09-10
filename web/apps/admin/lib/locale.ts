export const PRODUCT_LOCALES = ["tr", "en", "ru", "ar"] as const;

export type ProductLocale = (typeof PRODUCT_LOCALES)[number];

export type TextDirection = "ltr" | "rtl";

/** Operator chrome default (ADR-009 / ADR-013). Not an English-centric fallback. */
export const DEFAULT_LOCALE: ProductLocale = "tr";

export function isProductLocale(value: string): value is ProductLocale {
  return (PRODUCT_LOCALES as readonly string[]).includes(value);
}

export function dirForLocale(locale: ProductLocale): TextDirection {
  return locale === "ar" ? "rtl" : "ltr";
}
