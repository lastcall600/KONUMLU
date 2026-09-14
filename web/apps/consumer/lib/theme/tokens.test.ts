import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import { test } from "node:test";

import {
  REDUCED_MOTION_MEDIA,
  collectCssVarNames,
  color,
  eidsStatusAppearance,
  figmaColorAlias,
  figmaCssVar,
  humanChallengeAppearance,
  motion,
  size,
  stepUpAppearance,
  tokens,
  trustLevelAppearance,
  verifiedFlowTypeAppearance,
} from "./tokens";

const HERE = dirname(fileURLToPath(import.meta.url));
const TOKEN_CSS_PATH = join(HERE, "../../styles/tokens.css");

function parseRootDeclarations(css: string): Map<string, string> {
  const rootBlock = css.match(/:root\s*\{([\s\S]*?)\n\}/);
  assert.ok(rootBlock?.[1], "tokens.css must declare :root custom properties");
  const declarations = new Map<string, string>();
  for (const match of rootBlock[1].matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/g)) {
    const name = match[1];
    const value = match[2]?.trim();
    if (name && value) {
      declarations.set(name, value);
    }
  }
  return declarations;
}

const CANONICAL_SEMANTIC_NAMES = [
  "surface/default",
  "surface/subtle",
  "surface/elevated",
  "surface/raised",
  "surface/inverse",
  "surface/disabled",
  "surface/interactive",
  "surface/interactive-hover",
  "surface/interactive-pressed",
  "surface/selected",
  "text/default",
  "text/secondary",
  "text/muted",
  "text/subtle",
  "text/inverse",
  "text/disabled",
  "text/link",
  "text/critical",
  "border/default",
  "border/subtle",
  "border/strong",
  "border/focus",
  "border/disabled",
  "border/critical",
  "action/primary",
  "action/primary-hover",
  "action/primary-pressed",
  "action/secondary",
  "action/secondary-hover",
  "action/destructive",
  "action/destructive-hover",
  "action/disabled",
  "status/success",
  "status/success-surface",
  "status/warning",
  "status/warning-surface",
  "status/error",
  "status/error-surface",
  "status/info",
  "status/info-surface",
  "status/neutral",
  "status/neutral-surface",
  "category/environment",
  "category/listings",
  "category/market",
  "category/services",
  "category/jobs",
  "category/tourism",
  "category/events",
  "category/business",
  "category/creator",
  "verification/eids",
  "verification/verified",
  "verification/pending",
  "verification/rejected",
  "trust/new",
  "trust/verified",
  "trust/established",
] as const;

const CANONICAL_CATEGORY_NAMES = [
  "environment",
  "listings",
  "market",
  "services",
  "jobs",
  "tourism",
  "events",
  "business",
  "creator",
] as const;

const KNOWN_PRIMITIVE_HEX: Record<string, string> = {
  "--palette-neutral-0": "#ffffff",
  "--palette-neutral-50": "#f8fafc",
  "--palette-neutral-100": "#f1f5f9",
  "--palette-neutral-200": "#e2e8f0",
  "--palette-neutral-300": "#cbd5e1",
  "--palette-neutral-400": "#94a3b8",
  "--palette-neutral-500": "#64748b",
  "--palette-neutral-600": "#475569",
  "--palette-neutral-700": "#334155",
  "--palette-neutral-800": "#1e293b",
  "--palette-neutral-900": "#0f172a",
  "--palette-neutral-950": "#071426",
  "--palette-brand-blue-50": "#f3f6fc",
  "--palette-brand-blue-100": "#e5eefb",
  "--palette-brand-blue-200": "#c6dbfb",
  "--palette-brand-blue-300": "#9bc3fd",
  "--palette-brand-blue-400": "#6aa4fb",
  "--palette-brand-blue-500": "#3886fa",
  "--palette-brand-blue-600": "#0869f9",
  "--palette-brand-blue-700": "#0557d1",
  "--palette-brand-blue-800": "#0447a9",
  "--palette-brand-blue-900": "#033681",
  "--palette-brand-blue-950": "#072655",
  "--palette-accent-cyan-50": "#f3f9fc",
  "--palette-accent-cyan-100": "#e5f4fa",
  "--palette-accent-cyan-200": "#c7eafa",
  "--palette-accent-cyan-300": "#9ddefb",
  "--palette-accent-cyan-400": "#6dcdf8",
  "--palette-accent-cyan-500": "#3cbcf6",
  "--palette-accent-cyan-600": "#26b5f5",
  "--palette-accent-cyan-700": "#0990cd",
  "--palette-accent-cyan-800": "#0875a6",
  "--palette-accent-cyan-900": "#06597f",
  "--palette-accent-cyan-950": "#083c54",
  "--palette-accent-purple-50": "#f6f4fb",
  "--palette-accent-purple-100": "#ece8f7",
  "--palette-accent-purple-200": "#d8cef3",
  "--palette-accent-purple-300": "#bdaaee",
  "--palette-accent-purple-400": "#9b80e5",
  "--palette-accent-purple-500": "#7a56dc",
  "--palette-accent-purple-600": "#805dde",
  "--palette-accent-purple-700": "#4b24b2",
  "--palette-accent-purple-800": "#3c1d90",
  "--palette-accent-purple-900": "#2e166e",
  "--palette-accent-purple-950": "#211349",
  "--palette-accent-pink-50": "#fcf3f4",
  "--palette-accent-pink-100": "#fae5e8",
  "--palette-accent-pink-200": "#fac7cd",
  "--palette-accent-pink-300": "#fc9ca9",
  "--palette-accent-pink-400": "#fa6b7d",
  "--palette-accent-pink-500": "#f83a52",
  "--palette-accent-pink-600": "#f95469",
  "--palette-accent-pink-700": "#cf0721",
  "--palette-accent-pink-800": "#a8061a",
  "--palette-accent-pink-900": "#800414",
  "--palette-accent-pink-950": "#540711",
  "--palette-accent-orange-50": "#fcf7f3",
  "--palette-accent-orange-100": "#fbf0e5",
  "--palette-accent-orange-200": "#fae0c6",
  "--palette-accent-orange-300": "#fccc9c",
  "--palette-accent-orange-400": "#fbb26a",
  "--palette-accent-orange-500": "#f99839",
  "--palette-accent-orange-600": "#f99533",
  "--palette-accent-orange-700": "#d06a06",
  "--palette-accent-orange-800": "#a85605",
  "--palette-accent-orange-900": "#814204",
  "--palette-accent-orange-950": "#552e07",
  "--palette-accent-green-50": "#f4faf9",
  "--palette-accent-green-100": "#e9f7f3",
  "--palette-accent-green-200": "#d0f1e8",
  "--palette-accent-green-300": "#adebd9",
  "--palette-accent-green-400": "#84e1c6",
  "--palette-accent-green-500": "#5bd7b4",
  "--palette-accent-green-600": "#2cb78f",
  "--palette-accent-green-700": "#2aad87",
  "--palette-accent-green-800": "#228c6d",
  "--palette-accent-green-900": "#1a6b54",
  "--palette-accent-green-950": "#154739",
  "--palette-status-red-50": "#fbf4f4",
  "--palette-status-red-100": "#f8e7e8",
  "--palette-status-red-200": "#f5cccd",
  "--palette-status-red-300": "#f2a6a8",
  "--palette-status-red-400": "#ec797d",
  "--palette-status-red-500": "#e64c51",
  "--palette-status-red-600": "#e5484d",
  "--palette-status-red-700": "#bc1b20",
  "--palette-status-red-800": "#98161a",
  "--palette-status-red-900": "#741014",
  "--palette-status-red-950": "#4d0f11",
};

const FIGMA_PRIMITIVE_PREFIXES = [
  "--palette-neutral-",
  "--palette-brand-blue-",
  "--palette-accent-cyan-",
  "--palette-accent-purple-",
  "--palette-accent-pink-",
  "--palette-accent-orange-",
  "--palette-accent-green-",
  "--palette-status-red-",
];

const REPRESENTATIVE_PRIMITIVE_HEX: Record<string, string> = {
  "--palette-accent-cyan-700": "#0990cd",
  "--palette-accent-purple-600": "#805dde",
  "--palette-accent-pink-600": "#f95469",
  "--palette-accent-orange-700": "#d06a06",
  "--palette-accent-green-700": "#2aad87",
  "--palette-status-red-600": "#e5484d",
  "--palette-status-red-700": "#bc1b20",
};

const css = readFileSync(TOKEN_CSS_PATH, "utf8");
const declarations = parseRootDeclarations(css);

test("canonical semantic token names exist as CSS custom properties and TS API", () => {
  assert.deepEqual(Object.keys(figmaColorAlias), [...CANONICAL_SEMANTIC_NAMES]);
  for (const token of CANONICAL_SEMANTIC_NAMES) {
    const cssName = figmaCssVar(token);
    assert.ok(declarations.has(cssName), `CSS is missing ${cssName} (${token})`);
    assert.match(declarations.get(cssName) ?? "", /^var\(--palette-[a-z0-9-]+\)$/);
  }
  const names = collectCssVarNames(tokens);
  for (const name of names) {
    assert.ok(declarations.has(name), `CSS is missing ${name}`);
  }
  assert.equal("primary" in color.text, false);
  assert.equal("page" in color.surface, false);
  assert.equal("danger" in color.action, false);
  assert.equal("danger" in color.status, false);
  assert.equal("accent1" in color.category, false);
});

test("semantic aliases resolve to the expected primitive variables", () => {
  for (const [token, primitive] of Object.entries(figmaColorAlias)) {
    const cssName = figmaCssVar(token as keyof typeof figmaColorAlias);
    assert.equal(declarations.get(cssName), `var(${primitive})`, token);
  }
});

test("known primitive hex values match the frozen Figma palette", () => {
  for (const [name, hex] of Object.entries(KNOWN_PRIMITIVE_HEX)) {
    assert.equal((declarations.get(name) ?? "").toLowerCase(), hex);
  }
});

test("Figma primitive palettes have exact hex and no remaining unset", () => {
  for (const [name, hex] of Object.entries(REPRESENTATIVE_PRIMITIVE_HEX)) {
    assert.equal((declarations.get(name) ?? "").toLowerCase(), hex, name);
  }
  for (const [name, value] of declarations) {
    if (FIGMA_PRIMITIVE_PREFIXES.some((prefix) => name.startsWith(prefix))) {
      assert.notEqual(value, "unset", `${name} must not remain unset`);
      assert.match(value, /^#[0-9a-fA-F]{6}$/, `${name} must be an exact 6-digit hex`);
    }
  }
  assert.equal(/--palette-navy-|#061018|#0b5c34|#9b1c1c/.test(css), false);
});

test("category names remain canonical Figma product-navigation tokens", () => {
  assert.deepEqual(Object.keys(color.category), [...CANONICAL_CATEGORY_NAMES]);
  for (const name of CANONICAL_CATEGORY_NAMES) {
    assert.ok(declarations.has(`--color-category-${name}`));
  }
  assert.equal(declarations.has("--color-category-accent-1"), false);
  assert.equal(declarations.has("--color-category-accent-2"), false);
  assert.notEqual(color.category.listings, color.status.success);
});

test("trust tokens are only new/verified/established and never a score", () => {
  const trustNames = [...declarations.keys()].filter((name) => name.startsWith("--color-trust-"));
  assert.deepEqual(trustNames.sort(), [
    "--color-trust-established",
    "--color-trust-new",
    "--color-trust-verified",
  ]);
  assert.deepEqual(Object.keys(color.trust).sort(), ["established", "new", "verified"]);
  assert.deepEqual(Object.keys(trustLevelAppearance).sort(), ["established", "new", "verified"]);
  assert.equal(/--color-trust-(score|percent|percentage|meter|ring|\d+)\b/.test(css), false);
  assert.equal(/\btrust[-_]?(score|percent|percentage)\b/i.test(css), false);
});

test("domain mappings reuse Figma tokens instead of inventing unique hues", () => {
  assert.equal(eidsStatusAppearance.required, color.verification.eids);
  assert.equal(eidsStatusAppearance.verified, color.verification.verified);
  assert.equal(eidsStatusAppearance.failed, color.verification.rejected);
  assert.equal(eidsStatusAppearance.in_progress, color.verification.pending);
  assert.equal(verifiedFlowTypeAppearance.listing_inspection, color.verification.pending);
  assert.equal(verifiedFlowTypeAppearance.transaction, color.verification.pending);
  assert.equal(verifiedFlowTypeAppearance.delivery, color.verification.pending);
  assert.equal(humanChallengeAppearance.required, color.status.neutral);
  assert.equal(stepUpAppearance.required, color.action.primary);
  assert.equal(declarations.has("--color-verification-turnstile"), false);
  assert.equal(declarations.has("--color-verification-inspection"), false);
  assert.equal(declarations.has("--color-verification-transaction"), false);
  assert.equal(declarations.has("--color-verification-delivery"), false);
  assert.equal(declarations.has("--color-verification-step-up"), false);
});

test("reduced-motion override zeros Figma duration tokens", () => {
  assert.equal(REDUCED_MOTION_MEDIA, "(prefers-reduced-motion: reduce)");
  assert.match(css, /prefers-reduced-motion:\s*reduce/);
  assert.match(css, /--motion-duration-fast:\s*0ms/);
  assert.match(css, /--motion-duration-normal:\s*0ms/);
  assert.match(css, /--motion-duration-slow:\s*0ms/);
  assert.equal(declarations.get("--motion-duration-instant"), "0ms");
  assert.equal(declarations.get("--motion-duration-fast"), "120ms");
  assert.equal(declarations.get("--motion-duration-normal"), "200ms");
  assert.equal(declarations.get("--motion-duration-slow"), "320ms");
  assert.ok(motion.durationNormal.startsWith("var("));
});

test("minimum touch size remains 44px as a code-only accessibility token", () => {
  assert.equal(declarations.get("--size-touch-min"), "44px");
  assert.equal(size.touchMin, "var(--size-touch-min)");
});

test("foundation spacing and radius scales match Figma", () => {
  const spaces = [0, 2, 4, 6, 8, 12, 16, 20, 24, 32, 40, 48, 64, 80, 96];
  for (const n of spaces) {
    assert.equal(declarations.get(`--space-${n}`), `${n}px`);
  }
  assert.equal(declarations.get("--radius-none"), "0px");
  assert.equal(declarations.get("--radius-control"), "10px");
  assert.equal(declarations.get("--radius-card"), "16px");
  assert.equal(declarations.get("--radius-container"), "20px");
  assert.equal(declarations.get("--radius-modal"), "24px");
  assert.equal(declarations.get("--radius-sheet"), "24px");
  assert.equal(declarations.get("--radius-pill"), "999px");
  assert.equal(declarations.get("--radius-full"), "9999px");
});
