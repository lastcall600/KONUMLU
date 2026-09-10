import { apiFetch } from "@/lib/api";
import { CSRF_HEADER_NAME, readCsrfToken } from "@/lib/csrf";
import { assertionToJSON, publicKeyOptionsFromJson, webAuthnLoginSupported } from "@/lib/webauthn";

export type IdentifierKind = "email" | "phone";

export type AuthSession = {
  authenticated: true;
  userId: string;
};

export type AuthClientErrorCode =
  | "unauthenticated"
  | "rate_limited"
  | "unavailable"
  | "passkey_unsupported"
  | "passkey_cancelled"
  | "generic";

export class AuthClientError extends Error {
  readonly code: AuthClientErrorCode;

  constructor(code: AuthClientErrorCode, message: string) {
    super(message);
    this.name = "AuthClientError";
    this.code = code;
  }
}

type ErrorBody = {
  error?: string;
};

type AuthBody = {
  authenticated?: boolean;
  userId?: string;
};

type PasskeyBeginBody = {
  ceremonyToken?: string;
  publicKey?: unknown;
};

const LOGIN_FAILED = "Giriş başarısız oldu. Lütfen tekrar deneyin.";
const LOGOUT_FAILED = "Çıkış yapılamadı. Lütfen tekrar deneyin.";
const SIGNUP_START_FAILED = "İşlem tamamlanamadı. Lütfen bilgilerinizi kontrol edip tekrar deneyin.";
const SIGNUP_VERIFY_FAILED = "Doğrulama tamamlanamadı. Lütfen tekrar deneyin.";
const SIGNUP_COMPLETE_FAILED = "Hesap oluşturulamadı. Lütfen tekrar deneyin.";
const RESET_START_FAILED = "İşlem tamamlanamadı. Lütfen bilgilerinizi kontrol edip tekrar deneyin.";
const RESET_VERIFY_FAILED = "Doğrulama tamamlanamadı. Lütfen tekrar deneyin.";
const RESET_COMPLETE_FAILED = "Şifre güncellenemedi. Lütfen tekrar deneyin.";
const RATE_LIMITED = "Çok fazla deneme yapıldı. Lütfen daha sonra tekrar deneyin.";
const UNAVAILABLE = "Hizmet şu anda kullanılamıyor. Lütfen daha sonra tekrar deneyin.";
const PASSKEY_UNSUPPORTED = "Bu tarayıcı geçiş anahtarlarını desteklemiyor.";
const PASSKEY_CANCELLED = "Geçiş anahtarı işlemi iptal edildi veya tamamlanamadı.";

function mutateHeaders(): Headers {
  const headers = new Headers();
  const csrf = readCsrfToken();
  if (csrf) {
    headers.set(CSRF_HEADER_NAME, csrf);
  }
  return headers;
}

async function readJson(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

function errorCodeFromBody(body: unknown): string | undefined {
  if (typeof body !== "object" || body === null || !("error" in body)) {
    return undefined;
  }
  const error = (body as ErrorBody).error;
  return typeof error === "string" ? error : undefined;
}

function authErrorFromResponse(
  status: number,
  body: unknown,
  fallback: string,
  unauthenticatedMessage: string,
): AuthClientError {
  const code = errorCodeFromBody(body);
  if (status === 429 || code === "rate_limited") {
    return new AuthClientError("rate_limited", RATE_LIMITED);
  }
  if (status === 503 || code === "unavailable") {
    return new AuthClientError("unavailable", UNAVAILABLE);
  }
  if (status === 401 || code === "unauthenticated") {
    return new AuthClientError("unauthenticated", unauthenticatedMessage);
  }
  return new AuthClientError("generic", fallback);
}

function loginErrorFromResponse(status: number, body: unknown): AuthClientError {
  return authErrorFromResponse(status, body, LOGIN_FAILED, LOGIN_FAILED);
}

function parseSession(body: unknown): AuthSession | null {
  if (typeof body !== "object" || body === null) {
    return null;
  }
  const auth = body as AuthBody;
  if (auth.authenticated !== true || typeof auth.userId !== "string" || auth.userId === "") {
    return null;
  }
  return { authenticated: true, userId: auth.userId };
}

export async function getSession(): Promise<AuthSession | null> {
  const response = await apiFetch("/v1/auth/session", { method: "GET" });
  if (response.status === 401) {
    return null;
  }
  const body = await readJson(response);
  if (!response.ok) {
    if (response.status === 503) {
      throw new AuthClientError("unavailable", UNAVAILABLE);
    }
    throw new AuthClientError("generic", LOGIN_FAILED);
  }
  return parseSession(body);
}

export async function loginWithPassword(input: {
  kind: IdentifierKind;
  identifier: string;
  password: string;
}): Promise<AuthSession> {
  const response = await apiFetch("/v1/auth/password/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      kind: input.kind,
      identifier: input.identifier,
      password: input.password,
    }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw loginErrorFromResponse(response.status, body);
  }
  const session = parseSession(body);
  if (!session) {
    throw new AuthClientError("generic", LOGIN_FAILED);
  }
  return session;
}

export async function loginWithPasskey(): Promise<AuthSession> {
  if (!webAuthnLoginSupported()) {
    throw new AuthClientError("passkey_unsupported", PASSKEY_UNSUPPORTED);
  }

  const begin = await apiFetch("/v1/auth/passkey/login/begin", { method: "POST" });
  const beginBody = await readJson(begin);
  if (!begin.ok) {
    throw loginErrorFromResponse(begin.status, beginBody);
  }

  const payload = beginBody as PasskeyBeginBody | null;
  const ceremonyToken = payload?.ceremonyToken;
  if (typeof ceremonyToken !== "string" || ceremonyToken === "" || payload?.publicKey == null) {
    throw new AuthClientError("generic", LOGIN_FAILED);
  }

  let credential: PublicKeyCredential;
  try {
    const publicKey = publicKeyOptionsFromJson(payload.publicKey);
    const result = await navigator.credentials.get({ publicKey });
    if (!(result instanceof PublicKeyCredential)) {
      throw new AuthClientError("generic", LOGIN_FAILED);
    }
    credential = result;
  } catch (error) {
    if (error instanceof AuthClientError) {
      throw error;
    }
    if (error instanceof DOMException && error.name === "NotAllowedError") {
      throw new AuthClientError("passkey_cancelled", PASSKEY_CANCELLED);
    }
    throw new AuthClientError("generic", LOGIN_FAILED);
  }

  const finish = await apiFetch("/v1/auth/passkey/login/finish", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      ceremonyToken,
      credential: assertionToJSON(credential),
    }),
  });
  const finishBody = await readJson(finish);
  if (!finish.ok) {
    throw loginErrorFromResponse(finish.status, finishBody);
  }
  const session = parseSession(finishBody);
  if (!session) {
    throw new AuthClientError("generic", LOGIN_FAILED);
  }
  return session;
}

export async function startSignupVerification(input: {
  kind: IdentifierKind;
  identifier: string;
  locale: string;
}): Promise<{ challengeId: string }> {
  const response = await apiFetch("/v1/auth/signup/verification/start", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      kind: input.kind,
      identifier: input.identifier,
      locale: input.locale,
    }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw authErrorFromResponse(response.status, body, SIGNUP_START_FAILED, SIGNUP_START_FAILED);
  }
  if (typeof body !== "object" || body === null) {
    throw new AuthClientError("generic", SIGNUP_START_FAILED);
  }
  const challengeId = (body as { challengeId?: unknown }).challengeId;
  if (typeof challengeId !== "string" || challengeId === "") {
    throw new AuthClientError("generic", SIGNUP_START_FAILED);
  }
  return { challengeId };
}

export async function finishSignupVerification(input: {
  challengeId: string;
  code: string;
}): Promise<{ signupProof: string }> {
  const response = await apiFetch("/v1/auth/signup/verification/finish", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      challengeId: input.challengeId,
      code: input.code,
    }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw authErrorFromResponse(response.status, body, SIGNUP_VERIFY_FAILED, SIGNUP_VERIFY_FAILED);
  }
  if (typeof body !== "object" || body === null) {
    throw new AuthClientError("generic", SIGNUP_VERIFY_FAILED);
  }
  const payload = body as { verified?: unknown; signupProof?: unknown };
  if (payload.verified !== true || typeof payload.signupProof !== "string" || payload.signupProof === "") {
    throw new AuthClientError("generic", SIGNUP_VERIFY_FAILED);
  }
  return { signupProof: payload.signupProof };
}

export async function completeSignup(input: {
  signupProof: string;
  password?: string;
}): Promise<AuthSession> {
  const payload: { signupProof: string; password?: string } = {
    signupProof: input.signupProof,
  };
  if (input.password) {
    payload.password = input.password;
  }
  const response = await apiFetch("/v1/auth/signup/complete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw authErrorFromResponse(response.status, body, SIGNUP_COMPLETE_FAILED, SIGNUP_COMPLETE_FAILED);
  }
  const session = parseSession(body);
  if (!session) {
    throw new AuthClientError("generic", SIGNUP_COMPLETE_FAILED);
  }
  return session;
}

export async function startPasswordReset(input: {
  kind: IdentifierKind;
  identifier: string;
  locale: string;
}): Promise<{ challengeId: string }> {
  const response = await apiFetch("/v1/auth/password/reset/start", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      kind: input.kind,
      identifier: input.identifier,
      locale: input.locale,
    }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw authErrorFromResponse(response.status, body, RESET_START_FAILED, RESET_START_FAILED);
  }
  if (typeof body !== "object" || body === null) {
    throw new AuthClientError("generic", RESET_START_FAILED);
  }
  const challengeId = (body as { challengeId?: unknown }).challengeId;
  if (typeof challengeId !== "string" || challengeId === "") {
    throw new AuthClientError("generic", RESET_START_FAILED);
  }
  return { challengeId };
}

export async function verifyPasswordReset(input: {
  challengeId: string;
  code: string;
}): Promise<{ resetProof: string }> {
  const response = await apiFetch("/v1/auth/password/reset/verify", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      challengeId: input.challengeId,
      code: input.code,
    }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw authErrorFromResponse(response.status, body, RESET_VERIFY_FAILED, RESET_VERIFY_FAILED);
  }
  if (typeof body !== "object" || body === null) {
    throw new AuthClientError("generic", RESET_VERIFY_FAILED);
  }
  const resetProof = (body as { resetProof?: unknown }).resetProof;
  if (typeof resetProof !== "string" || resetProof === "") {
    throw new AuthClientError("generic", RESET_VERIFY_FAILED);
  }
  return { resetProof };
}

export async function completePasswordReset(input: {
  resetProof: string;
  newPassword: string;
}): Promise<void> {
  const response = await apiFetch("/v1/auth/password/reset/complete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      resetProof: input.resetProof,
      newPassword: input.newPassword,
    }),
  });
  const body = await readJson(response);
  if (!response.ok) {
    throw authErrorFromResponse(response.status, body, RESET_COMPLETE_FAILED, RESET_COMPLETE_FAILED);
  }
  const ok =
    typeof body === "object" &&
    body !== null &&
    "ok" in body &&
    (body as { ok?: unknown }).ok === true;
  if (!ok) {
    throw new AuthClientError("generic", RESET_COMPLETE_FAILED);
  }
}

export async function logout(): Promise<void> {
  const headers = mutateHeaders();
  const response = await apiFetch("/v1/auth/logout", {
    method: "POST",
    headers,
  });
  const body = await readJson(response);
  if (!response.ok) {
    if (response.status === 503) {
      throw new AuthClientError("unavailable", UNAVAILABLE);
    }
    throw new AuthClientError("generic", LOGOUT_FAILED);
  }
  const ok =
    typeof body === "object" &&
    body !== null &&
    "ok" in body &&
    (body as { ok?: unknown }).ok === true;
  if (!ok) {
    throw new AuthClientError("generic", LOGOUT_FAILED);
  }
}
