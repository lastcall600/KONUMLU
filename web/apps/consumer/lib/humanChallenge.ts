import { getTurnstilePublicConfig } from "./config";
import type { TurnstileAction } from "./turnstileActions";

export const CHALLENGE_REQUIRED_MESSAGE = "Güvenlik doğrulaması gerekli.";
export const CHALLENGE_EXPIRED_MESSAGE = "Doğrulamanın süresi doldu. Lütfen tekrar deneyin.";
export const CHALLENGE_UNAVAILABLE_MESSAGE = "Güvenlik doğrulaması şu anda tamamlanamıyor.";

export type ChallengeErrorCode = "challenge_required" | "challenge_failed" | "challenge_unavailable";

export function isTurnstileOperationEnabled(operation: TurnstileAction): boolean {
  return getTurnstilePublicConfig().turnstileOperations.has(operation);
}

export function turnstileSitekey(): string {
  return getTurnstilePublicConfig().turnstileSiteKey;
}

export function shouldShowTurnstile(input: {
  operation: TurnstileAction;
  challengeRequired: boolean;
}): boolean {
  if (!turnstileSitekey()) {
    return false;
  }
  return input.challengeRequired || isTurnstileOperationEnabled(input.operation);
}

export function attachChallengeToken<T extends Record<string, unknown>>(
  body: T,
  token: string | undefined,
): T {
  const trimmed = token?.trim() ?? "";
  if (!trimmed) {
    return body;
  }
  return { ...body, challengeToken: trimmed };
}

export function classifyChallengeHttp(input: {
  status: number;
  errorCode?: string;
  sentToken: boolean;
  sitekeyPresent: boolean;
  operationEnabled: boolean;
}): ChallengeErrorCode | null {
  if (input.status === 503 || input.errorCode === "unavailable") {
    if (input.sentToken || input.operationEnabled) {
      return "challenge_unavailable";
    }
    return null;
  }
  if (input.errorCode === "challenge_required") {
    return "challenge_required";
  }
  if (input.sentToken && (input.status === 403 || input.errorCode === "forbidden")) {
    return "challenge_failed";
  }
  return null;
}

export function challengeEffectForAuthError(
  error: { code: string; operation?: TurnstileAction },
  operation: TurnstileAction,
): "mark_required" | "expired" | "unavailable" | null {
  if (error.operation !== operation) {
    return null;
  }
  switch (error.code) {
    case "challenge_required":
      return "mark_required";
    case "challenge_failed":
      return "expired";
    case "challenge_unavailable":
      return "unavailable";
    default:
      return null;
  }
}

export function messageForChallengeCode(code: ChallengeErrorCode): string {
  switch (code) {
    case "challenge_required":
      return CHALLENGE_REQUIRED_MESSAGE;
    case "challenge_failed":
      return CHALLENGE_EXPIRED_MESSAGE;
    case "challenge_unavailable":
      return CHALLENGE_UNAVAILABLE_MESSAGE;
  }
}
