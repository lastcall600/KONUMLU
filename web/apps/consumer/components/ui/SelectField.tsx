import { useId, type ReactNode, type Ref, type SelectHTMLAttributes } from "react";

import { cx } from "./classNames";
import { FormField } from "./FormField";
import { resolveLockedSelectPresentation } from "./lockedSelectPresentation";

export type SelectFieldSize = "medium" | "large";

export type SelectFieldProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, "size"> & {
  size?: SelectFieldSize;
  label?: ReactNode;
  supportingText?: ReactNode;
  error?: ReactNode;
  lockedCategory?: boolean;
  ref?: Ref<HTMLSelectElement>;
};

export function SelectField({
  size = "medium",
  label,
  supportingText,
  error,
  lockedCategory = false,
  id,
  className,
  required,
  children,
  ref,
  name,
  value,
  defaultValue,
  ...props
}: SelectFieldProps) {
  const generatedId = useId();
  const selectId = id ?? generatedId;

  if (lockedCategory) {
    const presentation = resolveLockedSelectPresentation(children, value, defaultValue);
    const submittedName = typeof name === "string" && name.length > 0 ? name : undefined;

    return (
      <FormField
        label={label}
        htmlFor={selectId}
        required={required}
        supportingText={supportingText}
        error={error}
        lockedCategory={lockedCategory}
      >
        <output id={selectId} className={cx("ui-select", `ui-select--${size}`, className)}>
          {presentation.label}
          {submittedName !== undefined && presentation.submittedValue !== undefined ? (
            <input type="hidden" name={submittedName} value={presentation.submittedValue} />
          ) : null}
        </output>
      </FormField>
    );
  }

  return (
    <FormField
      label={label}
      htmlFor={selectId}
      required={required}
      supportingText={supportingText}
      error={error}
      lockedCategory={lockedCategory}
    >
      <select
        {...props}
        ref={ref}
        id={selectId}
        name={name}
        value={value}
        defaultValue={defaultValue}
        required={required}
        className={cx("ui-select", `ui-select--${size}`, className)}
      >
        {children}
      </select>
    </FormField>
  );
}
