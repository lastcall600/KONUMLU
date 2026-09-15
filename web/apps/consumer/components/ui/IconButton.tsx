import type { ButtonHTMLAttributes, ReactNode, Ref } from "react";

import { cx } from "./classNames";

export type IconButtonVariant = "primary" | "secondary" | "ghost";
export type IconButtonSize = "small" | "medium" | "large";

type AccessibleName =
  | { "aria-label": string; "aria-labelledby"?: string }
  | { "aria-label"?: string; "aria-labelledby": string };

export type IconButtonProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "aria-label" | "aria-labelledby" | "children"
> & {
  variant?: IconButtonVariant;
  size?: IconButtonSize;
  children: ReactNode;
  ref?: Ref<HTMLButtonElement>;
} & AccessibleName;

export function IconButton({
  variant = "primary",
  size = "medium",
  type = "button",
  className,
  children,
  ref,
  ...props
}: IconButtonProps) {
  const accessibleName = props["aria-label"] ?? props["aria-labelledby"];
  if (!accessibleName) {
    throw new Error("IconButton requires aria-label or aria-labelledby.");
  }

  return (
    <button
      {...props}
      ref={ref}
      type={type}
      className={cx(
        "ui-icon-button",
        `ui-icon-button--${variant}`,
        `ui-icon-button--${size}`,
        className,
      )}
    >
      <span className="ui-icon-button__icon" aria-hidden="true">
        {children}
      </span>
    </button>
  );
}
