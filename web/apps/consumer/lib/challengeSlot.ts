import {
  CHALLENGE_EXPIRED_MESSAGE,
  CHALLENGE_UNAVAILABLE_MESSAGE,
  shouldShowTurnstile,
} from "./humanChallenge";
import type { TurnstileAction } from "./turnstileActions";

export type ChallengeSlotState = {
  operation: TurnstileAction;
  token: string | null;
  required: boolean;
  showWidget: boolean;
  widgetKey: number;
  message: string | null;
};

/**
 * In-memory challenge token for one auth operation.
 * Tokens must never be written to localStorage, sessionStorage, or logs.
 */
export function createChallengeSlot(operation: TurnstileAction): {
  snapshot(): ChallengeSlotState;
  setToken(token: string): void;
  consumeToken(): string | undefined;
  markRequired(): void;
  handleWidgetError(): void;
  handleExpired(): void;
  handleTimeout(): void;
} {
  let token: string | null = null;
  let required = false;
  let widgetKey = 0;
  let message: string | null = null;

  function state(): ChallengeSlotState {
    return {
      operation,
      token,
      required,
      showWidget: shouldShowTurnstile({ operation, challengeRequired: required }),
      widgetKey,
      message,
    };
  }

  return {
    snapshot: state,
    setToken(next: string) {
      const trimmed = next.trim();
      if (!trimmed) {
        return;
      }
      token = trimmed;
      message = null;
    },
    consumeToken() {
      const current = token ?? undefined;
      token = null;
      widgetKey += 1;
      message = null;
      return current;
    },
    markRequired() {
      required = true;
      token = null;
    },
    handleWidgetError() {
      token = null;
      widgetKey += 1;
      message = CHALLENGE_UNAVAILABLE_MESSAGE;
    },
    handleExpired() {
      token = null;
      widgetKey += 1;
      message = CHALLENGE_EXPIRED_MESSAGE;
    },
    handleTimeout() {
      token = null;
      widgetKey += 1;
      message = CHALLENGE_EXPIRED_MESSAGE;
    },
  };
}
