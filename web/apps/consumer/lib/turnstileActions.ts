/**
 * Closed Turnstile action mapping. Values equal Identity AuthOperation
 * strings the backend Siteverify expects. The user cannot type an action.
 */
export const TURNSTILE_ACTIONS = [
  "password_login",
  "passkey_login_begin",
  "passkey_login_finish",
  "signup_start",
  "signup_finish",
  "signup_complete",
  "reset_start",
  "reset_verify",
  "reset_complete",
  "passkey_register_begin",
  "passkey_register_finish",
] as const;

export type TurnstileAction = (typeof TURNSTILE_ACTIONS)[number];

const ACTION_SET: ReadonlySet<string> = new Set(TURNSTILE_ACTIONS);

export function isTurnstileAction(value: string): value is TurnstileAction {
  return ACTION_SET.has(value);
}

export function parseTurnstileAction(value: string): TurnstileAction | null {
  const trimmed = value.trim().toLowerCase();
  return isTurnstileAction(trimmed) ? trimmed : null;
}

export function assertClosedAction(operation: string): TurnstileAction {
  const parsed = parseTurnstileAction(operation);
  if (!parsed) {
    throw new Error("invalid_turnstile_action");
  }
  return parsed;
}

/** Widget `action` must match the backend operation exactly. */
export function turnstileActionFor(operation: TurnstileAction): TurnstileAction {
  return operation;
}

export function parseTurnstileOperations(raw: string | undefined): ReadonlySet<TurnstileAction> {
  const out = new Set<TurnstileAction>();
  if (!raw) {
    return out;
  }
  for (const part of raw.split(",")) {
    const parsed = parseTurnstileAction(part);
    if (parsed) {
      out.add(parsed);
    }
  }
  return out;
}
