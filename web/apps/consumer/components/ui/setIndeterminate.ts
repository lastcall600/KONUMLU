/** Sets the native DOM `indeterminate` property. The HTML attribute cannot express this state. */
export function setIndeterminate(
  element: { indeterminate: boolean } | null,
  indeterminate: boolean,
): void {
  if (!element) {
    return;
  }
  element.indeterminate = indeterminate;
}
