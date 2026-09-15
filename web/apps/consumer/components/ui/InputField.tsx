import { useId, type InputHTMLAttributes, type ReactNode, type Ref } from "react";

import { cx } from "./classNames";
import { FormField } from "./FormField";

export type InputFieldSize = "medium" | "large";

export type InputFieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, "size"> & {
  size?: InputFieldSize;
  label?: ReactNode;
  supportingText?: ReactNode;
  error?: ReactNode;
  lockedCategory?: boolean;
  ref?: Ref<HTMLInputElement>;
};

export function InputField({
  size = "medium",
  label,
  supportingText,
  error,
  lockedCategory = false,
  id,
  className,
  placeholder,
  readOnly,
  required,
  ref,
  ...props
}: InputFieldProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;

  return (
    <FormField
      label={label}
      htmlFor={inputId}
      required={required}
      supportingText={supportingText}
      error={error}
      lockedCategory={lockedCategory}
    >
      <input
        {...props}
        ref={ref}
        id={inputId}
        required={required}
        readOnly={Boolean(lockedCategory) || readOnly}
        placeholder={placeholder ?? ""}
        className={cx("ui-input", `ui-input--${size}`, className)}
      />
    </FormField>
  );
}
