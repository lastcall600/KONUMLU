import { useId, type InputHTMLAttributes, type ReactNode, type Ref } from "react";

import { cx } from "./classNames";

export type SwitchProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "size" | "role"> & {
  label?: ReactNode;
  ref?: Ref<HTMLInputElement>;
};

export function Switch({ ref, label, id, className, ...props }: SwitchProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;

  const control = (
    <input
      {...props}
      ref={ref}
      id={inputId}
      type="checkbox"
      role="switch"
      className={cx("ui-switch", className)}
    />
  );

  if (!label) {
    return control;
  }

  return (
    <label className="ui-choice ui-choice--switch" htmlFor={inputId}>
      {control}
      <span className="ui-choice__text">{label}</span>
    </label>
  );
}
