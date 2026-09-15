import { useId, type InputHTMLAttributes, type ReactNode, type Ref } from "react";

import { cx } from "./classNames";

export type RadioProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "size"> & {
  label?: ReactNode;
  ref?: Ref<HTMLInputElement>;
};

export function Radio({ ref, label, id, className, ...props }: RadioProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;

  const control = (
    <input
      {...props}
      ref={ref}
      id={inputId}
      type="radio"
      className={cx("ui-radio", className)}
    />
  );

  if (!label) {
    return control;
  }

  return (
    <label className="ui-choice" htmlFor={inputId}>
      {control}
      <span className="ui-choice__text">{label}</span>
    </label>
  );
}
