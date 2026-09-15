import {
  Children,
  cloneElement,
  isValidElement,
  useId,
  type ReactNode,
} from "react";

import { cx } from "./classNames";

export type FormFieldProps = {
  label?: ReactNode;
  htmlFor?: string;
  required?: boolean;
  supportingText?: ReactNode;
  error?: ReactNode;
  invalid?: boolean;
  lockedCategory?: boolean;
  className?: string;
  children: ReactNode;
};

type ControlA11yProps = {
  id?: string;
  "aria-invalid"?: boolean | "true" | "false";
  "aria-describedby"?: string;
  "aria-required"?: boolean | "true" | "false";
};

function mergeDescribedBy(...parts: Array<string | undefined>): string | undefined {
  const value = parts.filter(Boolean).join(" ");
  return value || undefined;
}

export function FormField({
  label,
  htmlFor,
  required = false,
  supportingText,
  error,
  invalid,
  lockedCategory = false,
  className,
  children,
}: FormFieldProps) {
  const generatedId = useId();
  const controlId = htmlFor ?? generatedId;
  const supportingId = supportingText ? `${controlId}-supporting` : undefined;
  const errorId = error ? `${controlId}-error` : undefined;
  const isInvalid = Boolean(invalid || error);
  const describedBy = mergeDescribedBy(supportingId, errorId);

  const child = Children.count(children) === 1 ? Children.only(children) : null;
  const control =
    child && isValidElement<ControlA11yProps>(child)
      ? cloneElement(child, {
          id: child.props.id ?? controlId,
          "aria-invalid": isInvalid ? true : child.props["aria-invalid"],
          "aria-describedby": mergeDescribedBy(child.props["aria-describedby"], describedBy),
          "aria-required": required ? true : child.props["aria-required"],
        })
      : children;

  return (
    <div
      className={cx(
        "ui-form-field",
        isInvalid && "ui-form-field--invalid",
        lockedCategory && "ui-form-field--locked",
        className,
      )}
      data-invalid={isInvalid ? "true" : undefined}
      data-locked-category={lockedCategory ? "true" : undefined}
    >
      {label ? (
        <label className="ui-form-field__label" htmlFor={controlId}>
          {label}
          {required ? (
            <span className="ui-form-field__required" aria-hidden="true">
              {" "}
              *
            </span>
          ) : null}
        </label>
      ) : null}
      <div className="ui-form-field__control">{control}</div>
      {supportingText ? (
        <p className="ui-form-field__supporting" id={supportingId}>
          {supportingText}
        </p>
      ) : null}
      {error ? (
        <p className="ui-form-field__error" id={errorId} role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}
