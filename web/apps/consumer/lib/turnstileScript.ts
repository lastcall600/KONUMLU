import { turnstileActionFor, type TurnstileAction } from "./turnstileActions";

export const TURNSTILE_SCRIPT_SRC =
  "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

export const TURNSTILE_SCRIPT_MARKER = "konumlu-turnstile";

export type TurnstileRenderOptions = {
  sitekey: string;
  action: TurnstileAction;
  callback: (token: string) => void;
  "error-callback": () => void;
  "expired-callback": () => void;
  "timeout-callback": () => void;
};

export type TurnstileAPI = {
  render: (container: HTMLElement, options: TurnstileRenderOptions) => string;
  reset: (widgetId: string) => void;
  remove: (widgetId: string) => void;
};

type ScriptElementLike = {
  src: string;
  async: boolean;
  defer: boolean;
  dataset: Record<string, string>;
  onload: ((this: unknown, ev?: unknown) => void) | null;
  onerror: ((this: unknown, ev?: unknown) => void) | null;
};

export type TurnstileScriptDocument = {
  querySelector(selector: string): ScriptElementLike | null;
  createElement(tag: string): ScriptElementLike;
  head: { appendChild(node: ScriptElementLike): void };
};

export type TurnstileScriptHost = {
  document?: TurnstileScriptDocument;
  turnstile?: TurnstileAPI;
};

let loadPromise: Promise<TurnstileAPI> | null = null;

export function resetTurnstileScriptLoaderForTests(): void {
  loadPromise = null;
}

export function turnstileRenderOptions(input: {
  sitekey: string;
  action: TurnstileAction;
  onToken: (token: string) => void;
  onError: () => void;
  onExpired: () => void;
  onTimeout: () => void;
}): TurnstileRenderOptions {
  return {
    sitekey: input.sitekey,
    action: turnstileActionFor(input.action),
    callback: input.onToken,
    "error-callback": input.onError,
    "expired-callback": input.onExpired,
    "timeout-callback": input.onTimeout,
  };
}

function hostWindow(): TurnstileScriptHost {
  return globalThis as unknown as TurnstileScriptHost;
}

export function loadTurnstileScript(
  host: TurnstileScriptHost = hostWindow(),
): Promise<TurnstileAPI> {
  if (host.turnstile) {
    return Promise.resolve(host.turnstile);
  }
  if (loadPromise) {
    return loadPromise;
  }
  const doc = host.document;
  if (!doc) {
    return Promise.reject(new Error("turnstile_unavailable"));
  }

  loadPromise = new Promise<TurnstileAPI>((resolve, reject) => {
    const finish = (): void => {
      if (host.turnstile) {
        resolve(host.turnstile);
        return;
      }
      loadPromise = null;
      reject(new Error("turnstile_unavailable"));
    };
    const fail = (): void => {
      loadPromise = null;
      reject(new Error("turnstile_unavailable"));
    };

    const existing = doc.querySelector(`script[data-${TURNSTILE_SCRIPT_MARKER}="1"]`);
    if (existing) {
      if (host.turnstile) {
        resolve(host.turnstile);
        return;
      }
      existing.onload = finish;
      existing.onerror = fail;
      return;
    }

    const script = doc.createElement("script");
    script.src = TURNSTILE_SCRIPT_SRC;
    script.async = true;
    script.defer = true;
    script.dataset.konumluTurnstile = "1";
    script.onload = finish;
    script.onerror = fail;
    doc.head.appendChild(script);
  });

  return loadPromise;
}
