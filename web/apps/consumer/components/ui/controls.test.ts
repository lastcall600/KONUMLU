import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import { test } from "node:test";

import { createElement } from "react";

import { resolveLockedSelectPresentation } from "./lockedSelectPresentation";
import { setIndeterminate } from "./setIndeterminate";

const HERE = dirname(fileURLToPath(import.meta.url));

function source(name: string): string {
  return readFileSync(join(HERE, name), "utf8");
}

function rawHexColors(css: string): string[] {
  const withoutComments = css.replace(/\/\*[\s\S]*?\*\//g, "");
  return [...withoutComments.matchAll(/#[0-9a-fA-F]{3,8}\b/g)].map((match) => match[0]);
}

const button = source("Button.tsx");
const iconButton = source("IconButton.tsx");
const inputField = source("InputField.tsx");
const checkbox = source("Checkbox.tsx");
const radio = source("Radio.tsx");
const switchSource = source("Switch.tsx");
const selectField = source("SelectField.tsx");
const formField = source("FormField.tsx");
const css = source("controls.css");

test("Button type defaults safely and disabled/variant/size stay native/class contracts", () => {
  assert.match(button, /<button/);
  assert.match(button, /type = "button"/);
  assert.match(button, /\.\.\.props/);
  assert.match(button, /ui-button--\$\{variant\}/);
  assert.match(button, /ui-button--\$\{size\}/);
  assert.match(button, /variant = "primary"/);
  assert.match(button, /size = "medium"/);
  assert.equal(/state\s*[:=]\s*["']Hover["']/.test(button), false);
  assert.equal(button.includes("forwardRef"), false);
});

test("IconButton requires an accessible name and keeps native disabled/variant/size", () => {
  assert.match(iconButton, /<button/);
  assert.match(iconButton, /type = "button"/);
  assert.match(iconButton, /"aria-label": string/);
  assert.match(iconButton, /"aria-labelledby": string/);
  assert.match(iconButton, /requires aria-label or aria-labelledby/);
  assert.match(iconButton, /ui-icon-button--\$\{variant\}/);
  assert.match(iconButton, /ui-icon-button--\$\{size\}/);
  assert.match(iconButton, /\.\.\.props/);
  assert.equal(/placeholder icon|svg path/i.test(iconButton), false);
});

test("InputField associates label, error, and helper text", () => {
  assert.match(inputField, /<input/);
  assert.match(inputField, /<FormField/);
  assert.match(inputField, /label=\{label\}/);
  assert.match(inputField, /supportingText=\{supportingText\}/);
  assert.match(inputField, /error=\{error\}/);
  assert.match(inputField, /htmlFor=\{inputId\}/);
  assert.match(inputField, /id=\{inputId\}/);
  assert.match(formField, /aria-invalid/);
  assert.match(formField, /aria-describedby/);
  assert.match(formField, /id=\{errorId\}/);
  assert.match(formField, /id=\{supportingId\}/);
  assert.match(formField, /htmlFor=\{controlId\}/);
  assert.equal(/state\s*[:=]\s*["']Focus["']/.test(inputField), false);
  assert.equal(/state\s*[:=]\s*["']Filled["']/.test(inputField), false);
  assert.equal(/\bstate\?:/.test(inputField), false);
});

test("InputField filled styling applies only when placeholder behavior exists", () => {
  const withoutComments = css.replace(/\/\*[\s\S]*?\*\//g, "");
  assert.match(withoutComments, /\.ui-input\[placeholder\]:not\(:placeholder-shown\)/);
  assert.equal(/\.ui-input:not\(:placeholder-shown\)/.test(withoutComments), false);
  assert.equal(/state\s*[:=]\s*["']Filled["']/.test(inputField), false);
});

test("Checkbox uses native type and checked/disabled attributes", () => {
  assert.match(checkbox, /type="checkbox"/);
  assert.match(checkbox, /\.\.\.props/);
  assert.match(checkbox, /<label/);
  assert.match(checkbox, /htmlFor=\{inputId\}/);
  assert.equal(/onKeyDown|onKeyUp/.test(checkbox), false);
});

test("Checkbox indeterminate is applied through the DOM property", () => {
  const element = { indeterminate: false };
  setIndeterminate(element, true);
  assert.equal(element.indeterminate, true);
  setIndeterminate(element, false);
  assert.equal(element.indeterminate, false);
  assert.doesNotThrow(() => setIndeterminate(null, true));
  assert.match(checkbox, /setIndeterminate/);
  assert.match(checkbox, /indeterminate/);
});

test("Radio uses native type/name/value semantics", () => {
  assert.match(radio, /type="radio"/);
  assert.match(radio, /\.\.\.props/);
  assert.match(radio, /<label/);
  assert.equal(/role="radio"/.test(radio), false);
  assert.equal(/onKeyDown/.test(radio), false);
});

test("Switch is a native checkbox with checked and disabled behavior", () => {
  assert.match(switchSource, /type="checkbox"/);
  assert.match(switchSource, /role="switch"/);
  assert.match(switchSource, /\.\.\.props/);
  assert.equal(/<div/.test(switchSource), false);
  assert.equal(/onKeyDown/.test(switchSource), false);
});

test("SelectField keeps native select semantics, selection, error, and disabled", () => {
  assert.match(selectField, /<select/);
  assert.match(selectField, /\.\.\.props/);
  assert.match(selectField, /<FormField/);
  assert.match(selectField, /error=\{error\}/);
  assert.match(selectField, /htmlFor=\{selectId\}/);
  assert.match(selectField, /name=\{name\}/);
  assert.match(selectField, /value=\{value\}/);
  assert.match(selectField, /defaultValue=\{defaultValue\}/);
  assert.equal(/state\s*[:=]\s*["']open["']/i.test(selectField), false);
  assert.equal(/role="listbox"/.test(selectField), false);
  assert.equal(/role="combobox"/.test(selectField), false);
  assert.equal(selectField.includes("disabled={lockedCategory}"), false);
});

test("SelectField lockedCategory is a static field, not an operable or disabled select", () => {
  const lockedStart = selectField.indexOf("if (lockedCategory)");
  const selectStart = selectField.indexOf("<select");
  const lockedBlock =
    lockedStart >= 0 && selectStart > lockedStart
      ? selectField.slice(lockedStart, selectStart)
      : "";
  assert.match(lockedBlock, /<output/);
  assert.match(lockedBlock, /presentation\.label/);
  assert.match(lockedBlock, /type="hidden"/);
  assert.match(lockedBlock, /name=\{submittedName\}/);
  assert.match(lockedBlock, /value=\{presentation\.submittedValue\}/);
  assert.equal(/<select/.test(lockedBlock), false);
  assert.equal(/disabled/.test(lockedBlock), false);
  assert.equal(/preventDefault/.test(selectField), false);
  assert.equal(/onPointerDown|onMouseDown|onKeyDown|onClick/.test(selectField), false);
  assert.equal(/restoreLockedSelectValue|preventLockedInteraction/.test(selectField), false);
  assert.equal(/role="listbox"|role="combobox"/.test(selectField), false);
  assert.equal(/\.ui-form-field--locked \.ui-select\s*\{[^}]*pointer-events/.test(css), false);
  assert.equal(/disabled=\{lockedCategory\}/.test(selectField), false);
  assert.equal(/disabled=\{true\}/.test(selectField), false);
});

test("locked SelectField resolves option labels and form values from native options", () => {
  const options = [
    createElement("option", { value: "", key: "empty" }, "Seçin"),
    createElement("option", { value: "emlak", key: "emlak" }, "Emlak"),
    createElement("option", { value: "vasita", key: "vasita", label: "Vasıta" }, "ignored"),
  ];
  const grouped = createElement("optgroup", { label: "Grup" }, [
    createElement("option", { value: "hizmet", key: "hizmet" }, "Hizmet"),
  ]);

  assert.deepEqual(resolveLockedSelectPresentation(options, "", undefined), {
    submittedValue: "",
    label: "Seçin",
  });
  assert.deepEqual(resolveLockedSelectPresentation(options, "emlak", undefined), {
    submittedValue: "emlak",
    label: "Emlak",
  });
  assert.deepEqual(resolveLockedSelectPresentation(options, undefined, "vasita"), {
    submittedValue: "vasita",
    label: "Vasıta",
  });
  assert.deepEqual(resolveLockedSelectPresentation(grouped, "hizmet", undefined), {
    submittedValue: "hizmet",
    label: "Hizmet",
  });
  assert.deepEqual(resolveLockedSelectPresentation(options, "missing", undefined), {
    submittedValue: "missing",
    label: "missing",
  });
  assert.deepEqual(resolveLockedSelectPresentation(options, undefined, undefined), {
    label: "",
  });
});

test("FormField required and invalid semantics stay on the field wrapper", () => {
  assert.match(formField, /required/);
  assert.match(formField, /aria-required/);
  assert.match(formField, /aria-invalid/);
  assert.match(formField, /data-invalid/);
  assert.match(formField, /ui-form-field--invalid/);
  assert.match(formField, /role="alert"/);
  assert.match(formField, /htmlFor=\{controlId\}/);
});

test("FormField lockedCategory is presentation and does not become disabled or a backend state", () => {
  assert.match(formField, /lockedCategory/);
  assert.match(formField, /data-locked-category/);
  assert.match(formField, /ui-form-field--locked/);
  assert.equal(/disabled=\{lockedCategory\}/.test(formField), false);
  assert.equal(/disabled=\{true\}/.test(formField), false);
  assert.equal(/aria-disabled/.test(formField), false);
  assert.equal(/moderation_state|archived|restricted/.test(formField), false);
  assert.match(inputField, /readOnly=\{Boolean\(lockedCategory\) \|\| readOnly\}/);
  assert.equal(/disabled=\{lockedCategory\}/.test(inputField), false);
  assert.equal(/disabled=\{lockedCategory\}/.test(selectField), false);
});

test("Batch 1 control styles use semantic tokens and no raw hex", () => {
  const withoutComments = css.replace(/\/\*[\s\S]*?\*\//g, "");
  assert.deepEqual(rawHexColors(css), []);
  assert.equal(/--palette-/.test(withoutComments), false);
  assert.match(withoutComments, /var\(--color-action-primary\)/);
  assert.match(withoutComments, /var\(--color-action-primary-hover\)/);
  assert.match(withoutComments, /var\(--color-action-primary-pressed\)/);
  assert.match(withoutComments, /var\(--color-action-secondary\)/);
  assert.match(withoutComments, /var\(--radius-control\)/);
  assert.match(withoutComments, /var\(--size-touch-min\)/);
  assert.match(withoutComments, /pointer:\s*coarse/);
  assert.equal(withoutComments.includes("120px"), false);
  assert.equal(withoutComments.includes("320px"), false);
  assert.equal(withoutComments.includes("380px"), false);
});
