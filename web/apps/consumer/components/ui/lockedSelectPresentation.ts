import { Children, isValidElement, type ReactNode } from "react";

export type LockedSelectPresentation = {
  submittedValue?: string;
  label: string;
};

type SelectValue = string | number | readonly string[] | undefined;

type OptionProps = {
  value?: string | number;
  label?: string;
  children?: ReactNode;
};

type OptgroupProps = {
  children?: ReactNode;
};

export function resolveLockedSelectPresentation(
  children: ReactNode,
  value: SelectValue,
  defaultValue: SelectValue,
): LockedSelectPresentation {
  const submittedValue = scalarSelectValue(value ?? defaultValue);
  if (submittedValue === undefined) {
    return { label: "" };
  }
  return {
    submittedValue,
    label: findNativeOptionLabel(children, submittedValue) ?? submittedValue,
  };
}

function scalarSelectValue(value: SelectValue): string | undefined {
  if (typeof value === "number") {
    return String(value);
  }
  if (typeof value === "string") {
    return value;
  }
  return undefined;
}

function findNativeOptionLabel(children: ReactNode, selected: string): string | undefined {
  let matched: string | undefined;
  Children.forEach(children, (child) => {
    if (matched !== undefined || !isValidElement(child)) {
      return;
    }
    if (child.type === "optgroup") {
      matched = findNativeOptionLabel((child.props as OptgroupProps).children, selected);
      return;
    }
    if (child.type !== "option") {
      return;
    }
    const option = child.props as OptionProps;
    const optionValue = option.value !== undefined ? String(option.value) : flattenText(option.children);
    if (optionValue !== selected) {
      return;
    }
    if (typeof option.label === "string" && option.label.length > 0) {
      matched = option.label;
      return;
    }
    const text = flattenText(option.children);
    matched = text.length > 0 ? text : undefined;
  });
  return matched;
}

function flattenText(node: ReactNode): string {
  if (node == null || typeof node === "boolean") {
    return "";
  }
  if (typeof node === "string" || typeof node === "number") {
    return String(node);
  }
  if (Array.isArray(node)) {
    return node.map(flattenText).join("");
  }
  if (isValidElement<{ children?: ReactNode }>(node)) {
    return flattenText(node.props.children);
  }
  return "";
}
