"use client";

import {
  useId,
  useLayoutEffect,
  useRef,
  type InputHTMLAttributes,
  type ReactNode,
  type Ref,
} from "react";

import { cx } from "./classNames";
import { setIndeterminate } from "./setIndeterminate";

export type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "size"> & {
  label?: ReactNode;
  indeterminate?: boolean;
  ref?: Ref<HTMLInputElement>;
};

function assignRef<T>(ref: Ref<T> | undefined, value: T | null): void {
  if (!ref) {
    return;
  }
  if (typeof ref === "function") {
    ref(value);
  } else {
    ref.current = value;
  }
}

export function Checkbox({
  ref,
  label,
  indeterminate = false,
  id,
  className,
  ...props
}: CheckboxProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;
  const inputRef = useRef<HTMLInputElement>(null);

  useLayoutEffect(() => {
    setIndeterminate(inputRef.current, Boolean(indeterminate));
  }, [indeterminate]);

  const control = (
    <input
      {...props}
      ref={(element) => {
        inputRef.current = element;
        setIndeterminate(element, Boolean(indeterminate));
        assignRef(ref, element);
      }}
      id={inputId}
      type="checkbox"
      className={cx("ui-checkbox", className)}
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
