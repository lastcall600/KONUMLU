type JsonObject = Record<string, unknown>;

function isRecord(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function base64urlToBuffer(value: string): ArrayBuffer {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/");
  const padLength = (4 - (padded.length % 4)) % 4;
  const binary = atob(padded + "=".repeat(padLength));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

function bufferToBase64url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

export function webAuthnLoginSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.PublicKeyCredential === "function" &&
    typeof navigator.credentials?.get === "function"
  );
}

function decodeAllowCredential(raw: unknown): PublicKeyCredentialDescriptor | null {
  if (!isRecord(raw) || typeof raw.id !== "string" || raw.type !== "public-key") {
    return null;
  }
  const descriptor: PublicKeyCredentialDescriptor = {
    type: "public-key",
    id: base64urlToBuffer(raw.id),
  };
  if (Array.isArray(raw.transports)) {
    const allowed = new Set<AuthenticatorTransport>(["ble", "hybrid", "internal", "nfc", "usb"]);
    const transports = raw.transports.filter(
      (item): item is AuthenticatorTransport =>
        typeof item === "string" && allowed.has(item as AuthenticatorTransport),
    );
    if (transports.length > 0) {
      descriptor.transports = transports;
    }
  }
  return descriptor;
}

export function publicKeyOptionsFromJson(raw: unknown): PublicKeyCredentialRequestOptions {
  if (!isRecord(raw) || typeof raw.challenge !== "string") {
    throw new Error("invalid_public_key");
  }

  const options: PublicKeyCredentialRequestOptions = {
    challenge: base64urlToBuffer(raw.challenge),
  };

  if (typeof raw.timeout === "number") {
    options.timeout = raw.timeout;
  }
  if (typeof raw.rpId === "string") {
    options.rpId = raw.rpId;
  }
  if (typeof raw.userVerification === "string") {
    options.userVerification = raw.userVerification as UserVerificationRequirement;
  }
  if (Array.isArray(raw.allowCredentials)) {
    options.allowCredentials = raw.allowCredentials
      .map(decodeAllowCredential)
      .filter((item): item is PublicKeyCredentialDescriptor => item !== null);
  }
  if (isRecord(raw.extensions)) {
    options.extensions = raw.extensions as AuthenticationExtensionsClientInputs;
  }

  return options;
}

export function assertionToJSON(credential: PublicKeyCredential): JsonObject {
  const response = credential.response;
  if (!(response instanceof AuthenticatorAssertionResponse)) {
    throw new Error("invalid_assertion");
  }

  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
      authenticatorData: bufferToBase64url(response.authenticatorData),
      signature: bufferToBase64url(response.signature),
      userHandle: response.userHandle ? bufferToBase64url(response.userHandle) : null,
    },
  };
}
