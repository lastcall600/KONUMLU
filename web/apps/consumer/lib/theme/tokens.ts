/**
 * Semantic design-token consumption API for the KONUMLU consumer app.
 *
 * Names match frozen Figma semantics (slash → camelCase / nested keys):
 *   surface/default          → color.surface.default
 *   text/default             → color.text.default
 *   action/primary-pressed   → color.action.primaryPressed
 *   category/listings        → color.category.listings
 *   verification/eids        → color.verification.eids
 *   trust/established        → color.trust.established
 *
 * CSS: surface/default → --color-surface-default
 * Do not use --palette-* primitives in product UI.
 * Do not use a parallel vocabulary (e.g. text.primary when Figma says text/default).
 *
 * Trust is three labels only. There is no numeric 0–100 Trust token.
 * Domain mappings below reuse Figma tokens; they do not invent extra hues.
 */

export const HOVER_MEDIA = "(hover: hover) and (pointer: fine)";
export const REDUCED_MOTION_MEDIA = "(prefers-reduced-motion: reduce)";

/** Figma semantic → primitive custom property (alias contract). */
export const figmaColorAlias = {
  "surface/default": "--palette-neutral-0",
  "surface/subtle": "--palette-neutral-50",
  "surface/elevated": "--palette-neutral-0",
  "surface/raised": "--palette-neutral-0",
  "surface/inverse": "--palette-neutral-950",
  "surface/disabled": "--palette-neutral-100",
  "surface/interactive": "--palette-brand-blue-50",
  "surface/interactive-hover": "--palette-brand-blue-100",
  "surface/interactive-pressed": "--palette-brand-blue-200",
  "surface/selected": "--palette-brand-blue-50",
  "text/default": "--palette-neutral-950",
  "text/secondary": "--palette-neutral-700",
  "text/muted": "--palette-neutral-500",
  "text/subtle": "--palette-neutral-400",
  "text/inverse": "--palette-neutral-0",
  "text/disabled": "--palette-neutral-400",
  "text/link": "--palette-brand-blue-700",
  "text/critical": "--palette-status-red-700",
  "border/default": "--palette-neutral-200",
  "border/subtle": "--palette-neutral-100",
  "border/strong": "--palette-neutral-300",
  "border/focus": "--palette-brand-blue-600",
  "border/disabled": "--palette-neutral-200",
  "border/critical": "--palette-status-red-600",
  "action/primary": "--palette-brand-blue-600",
  "action/primary-hover": "--palette-brand-blue-700",
  "action/primary-pressed": "--palette-brand-blue-800",
  "action/secondary": "--palette-neutral-100",
  "action/secondary-hover": "--palette-neutral-200",
  "action/destructive": "--palette-status-red-600",
  "action/destructive-hover": "--palette-status-red-700",
  "action/disabled": "--palette-neutral-300",
  "status/success": "--palette-accent-green-700",
  "status/success-surface": "--palette-accent-green-50",
  "status/warning": "--palette-accent-orange-700",
  "status/warning-surface": "--palette-accent-orange-50",
  "status/error": "--palette-status-red-700",
  "status/error-surface": "--palette-status-red-50",
  "status/info": "--palette-accent-cyan-700",
  "status/info-surface": "--palette-accent-cyan-50",
  "status/neutral": "--palette-neutral-600",
  "status/neutral-surface": "--palette-neutral-100",
  "category/environment": "--palette-brand-blue-600",
  "category/listings": "--palette-status-red-500",
  "category/market": "--palette-status-red-600",
  "category/services": "--palette-accent-orange-600",
  "category/jobs": "--palette-accent-purple-600",
  "category/tourism": "--palette-accent-green-600",
  "category/events": "--palette-accent-pink-600",
  "category/business": "--palette-accent-green-700",
  "category/creator": "--palette-accent-purple-600",
  "verification/eids": "--palette-brand-blue-700",
  "verification/verified": "--palette-accent-green-700",
  "verification/pending": "--palette-accent-orange-700",
  "verification/rejected": "--palette-status-red-700",
  "trust/new": "--palette-neutral-500",
  "trust/verified": "--palette-brand-blue-700",
  "trust/established": "--palette-accent-green-700",
} as const;

export type FigmaColorToken = keyof typeof figmaColorAlias;

/** Figma semantic → CSS custom property consumed by UI. */
export function figmaCssVar(token: FigmaColorToken): `--color-${string}` {
  return `--color-${token.replaceAll("/", "-")}` as `--color-${string}`;
}

export const color = {
  surface: {
    default: "var(--color-surface-default)",
    subtle: "var(--color-surface-subtle)",
    elevated: "var(--color-surface-elevated)",
    raised: "var(--color-surface-raised)",
    inverse: "var(--color-surface-inverse)",
    disabled: "var(--color-surface-disabled)",
    interactive: "var(--color-surface-interactive)",
    interactiveHover: "var(--color-surface-interactive-hover)",
    interactivePressed: "var(--color-surface-interactive-pressed)",
    selected: "var(--color-surface-selected)",
  },
  text: {
    default: "var(--color-text-default)",
    secondary: "var(--color-text-secondary)",
    muted: "var(--color-text-muted)",
    subtle: "var(--color-text-subtle)",
    inverse: "var(--color-text-inverse)",
    disabled: "var(--color-text-disabled)",
    link: "var(--color-text-link)",
    critical: "var(--color-text-critical)",
  },
  border: {
    default: "var(--color-border-default)",
    subtle: "var(--color-border-subtle)",
    strong: "var(--color-border-strong)",
    focus: "var(--color-border-focus)",
    disabled: "var(--color-border-disabled)",
    critical: "var(--color-border-critical)",
  },
  action: {
    primary: "var(--color-action-primary)",
    primaryHover: "var(--color-action-primary-hover)",
    primaryPressed: "var(--color-action-primary-pressed)",
    secondary: "var(--color-action-secondary)",
    secondaryHover: "var(--color-action-secondary-hover)",
    destructive: "var(--color-action-destructive)",
    destructiveHover: "var(--color-action-destructive-hover)",
    disabled: "var(--color-action-disabled)",
  },
  status: {
    success: "var(--color-status-success)",
    successSurface: "var(--color-status-success-surface)",
    warning: "var(--color-status-warning)",
    warningSurface: "var(--color-status-warning-surface)",
    error: "var(--color-status-error)",
    errorSurface: "var(--color-status-error-surface)",
    info: "var(--color-status-info)",
    infoSurface: "var(--color-status-info-surface)",
    neutral: "var(--color-status-neutral)",
    neutralSurface: "var(--color-status-neutral-surface)",
  },
  category: {
    environment: "var(--color-category-environment)",
    listings: "var(--color-category-listings)",
    market: "var(--color-category-market)",
    services: "var(--color-category-services)",
    jobs: "var(--color-category-jobs)",
    tourism: "var(--color-category-tourism)",
    events: "var(--color-category-events)",
    business: "var(--color-category-business)",
    creator: "var(--color-category-creator)",
  },
  verification: {
    eids: "var(--color-verification-eids)",
    verified: "var(--color-verification-verified)",
    pending: "var(--color-verification-pending)",
    rejected: "var(--color-verification-rejected)",
  },
  trust: {
    new: "var(--color-trust-new)",
    verified: "var(--color-trust-verified)",
    established: "var(--color-trust-established)",
  },
} as const;

export const space = {
  0: "var(--space-0)",
  2: "var(--space-2)",
  4: "var(--space-4)",
  6: "var(--space-6)",
  8: "var(--space-8)",
  12: "var(--space-12)",
  16: "var(--space-16)",
  20: "var(--space-20)",
  24: "var(--space-24)",
  32: "var(--space-32)",
  40: "var(--space-40)",
  48: "var(--space-48)",
  64: "var(--space-64)",
  80: "var(--space-80)",
  96: "var(--space-96)",
} as const;

export const radius = {
  none: "var(--radius-none)",
  control: "var(--radius-control)",
  card: "var(--radius-card)",
  container: "var(--radius-container)",
  modal: "var(--radius-modal)",
  sheet: "var(--radius-sheet)",
  pill: "var(--radius-pill)",
  full: "var(--radius-full)",
} as const;

export const font = {
  familySans: "var(--font-family-sans)",
  familyMono: "var(--font-family-mono)",
  sizeDisplay: "var(--font-size-display)",
  sizeTitle: "var(--font-size-title)",
  sizeHeading: "var(--font-size-heading)",
  sizeBody: "var(--font-size-body)",
  sizeBodySm: "var(--font-size-body-sm)",
  sizeCaption: "var(--font-size-caption)",
  sizeLabel: "var(--font-size-label)",
  weightRegular: "var(--font-weight-regular)",
  weightMedium: "var(--font-weight-medium)",
  weightSemibold: "var(--font-weight-semibold)",
  weightBold: "var(--font-weight-bold)",
  lineHeightTight: "var(--font-line-height-tight)",
  lineHeightSnug: "var(--font-line-height-snug)",
  lineHeightBody: "var(--font-line-height-body)",
  letterSpacingNormal: "var(--font-letter-spacing-normal)",
  letterSpacingDisplay: "var(--font-letter-spacing-display)",
  numeric: "var(--font-numeric)",
} as const;

export const elevation = {
  none: "var(--elevation-none)",
  sm: "var(--elevation-sm)",
  md: "var(--elevation-md)",
  lg: "var(--elevation-lg)",
} as const;

export const motion = {
  durationInstant: "var(--motion-duration-instant)",
  durationFast: "var(--motion-duration-fast)",
  durationNormal: "var(--motion-duration-normal)",
  durationSlow: "var(--motion-duration-slow)",
  easingStandard: "var(--motion-easing-standard)",
} as const;

export const focus = {
  ringWidth: "var(--focus-ring-width)",
  ringOffset: "var(--focus-ring-offset)",
  ringColor: "var(--focus-ring-color)",
} as const;

export const size = {
  touchMin: "var(--size-touch-min)",
} as const;

export const tokens = {
  color,
  space,
  radius,
  font,
  elevation,
  motion,
  focus,
  size,
} as const;

export type TrustLevelToken = "new" | "verified" | "established";

export const trustLevelAppearance = {
  new: color.trust.new,
  verified: color.trust.verified,
  established: color.trust.established,
} as const satisfies Record<TrustLevelToken, string>;

export type EidsStatusToken =
  | "required"
  | "pending"
  | "in_progress"
  | "verified"
  | "failed"
  | "expired"
  | "unavailable";

/** Repository EİDS states → Figma verification/status tokens (no extra hues). */
export const eidsStatusAppearance = {
  required: color.verification.eids,
  pending: color.verification.pending,
  in_progress: color.verification.pending,
  verified: color.verification.verified,
  failed: color.verification.rejected,
  expired: color.verification.pending,
  unavailable: color.status.neutral,
} as const satisfies Record<EidsStatusToken, string>;

export type VerifiedFlowTypeToken = "listing_inspection" | "transaction" | "delivery";

/** Repository verified-interaction types → Figma verification tokens (shared hues). */
export const verifiedFlowTypeAppearance = {
  listing_inspection: color.verification.pending,
  transaction: color.verification.pending,
  delivery: color.verification.pending,
} as const satisfies Record<VerifiedFlowTypeToken, string>;

export const humanChallengeAppearance = {
  idle: color.status.neutral,
  required: color.status.neutral,
  expired: color.status.warning,
} as const;

export const stepUpAppearance = {
  required: color.action.primary,
} as const;

export function collectCssVarNames(value: unknown, into: Set<string> = new Set()): Set<string> {
  if (typeof value === "string") {
    for (const match of value.matchAll(/var\((--[a-z0-9-]+)\)/g)) {
      const name = match[1];
      if (name) {
        into.add(name);
      }
    }
    return into;
  }
  if (value && typeof value === "object") {
    for (const nested of Object.values(value)) {
      collectCssVarNames(nested, into);
    }
  }
  return into;
}
