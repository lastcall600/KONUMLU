import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import { test } from "node:test";

import { loginWithPasskey, loginWithPassword, completeSignup, startSignupVerification, AuthClientError } from "./auth";
import { createChallengeSlot } from "./challengeSlot";
import { getPublicConfig } from "./config";
import {
  attachChallengeToken,
  challengeEffectForAuthError,
  classifyChallengeHttp,
  shouldShowTurnstile,
} from "./humanChallenge";
import {
  assertClosedAction,
  parseTurnstileAction,
  parseTurnstileOperations,
  turnstileActionFor,
} from "./turnstileActions";
import {
  loadTurnstileScript,
  resetTurnstileScriptLoaderForTests,
  TURNSTILE_SCRIPT_SRC,
  turnstileRenderOptions,
  type TurnstileAPI,
  type TurnstileScriptDocument,
} from "./turnstileScript";

const HERE = dirname(fileURLToPath(import.meta.url));

function source(name: string): string {
  return readFileSync(join(HERE, name), "utf8");
}

function withEnv(values: Record<string, string | undefined>, fn: () => void): void {
  const previous: Record<string, string | undefined> = {};
  for (const [key, value] of Object.entries(values)) {
    previous[key] = process.env[key];
    if (value === undefined) {
      delete process.env[key];
    } else {
      process.env[key] = value;
    }
  }
  try {
    fn();
  } finally {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    }
  }
}

test("script loader injects official explicit script only once", async () => {
  resetTurnstileScriptLoaderForTests();
  const appended: { src: string }[] = [];
  const api: TurnstileAPI = {
    render: () => "w1",
    reset: () => undefined,
    remove: () => undefined,
  };
  const host: { document: TurnstileScriptDocument; turnstile?: TurnstileAPI } = {
    document: {
      querySelector() {
        return appended[0]
          ? {
              src: appended[0].src,
              async: true,
              defer: true,
              dataset: { konumluTurnstile: "1" },
              onload: null,
              onerror: null,
            }
          : null;
      },
      createElement() {
        return {
          src: "",
          async: false,
          defer: false,
          dataset: {},
          onload: null,
          onerror: null,
        };
      },
      head: {
        appendChild(node) {
          appended.push(node);
          host.turnstile = api;
          queueMicrotask(() => node.onload?.(undefined));
        },
      },
    },
  };
  const first = loadTurnstileScript(host);
  const second = loadTurnstileScript(host);
  await Promise.all([first, second]);
  assert.equal(appended.length, 1);
  assert.equal(appended[0]?.src, TURNSTILE_SCRIPT_SRC);
  assert.match(TURNSTILE_SCRIPT_SRC, /render=explicit/);
});

test("explicit widget render uses closed action and public sitekey", () => {
  const calls: unknown[] = [];
  const options = turnstileRenderOptions({
    sitekey: "1x00000000000000000000AA",
    action: "password_login",
    onToken: (token) => calls.push(token),
    onError: () => calls.push("error"),
    onExpired: () => calls.push("expired"),
    onTimeout: () => calls.push("timeout"),
  });
  assert.equal(options.sitekey, "1x00000000000000000000AA");
  assert.equal(options.action, "password_login");
  options.callback("opaque-token");
  options["expired-callback"]();
  options["timeout-callback"]();
  options["error-callback"]();
  assert.deepEqual(calls, ["opaque-token", "expired", "timeout", "error"]);
});

test("production sitekey comes from public config and secret is absent", () => {
  withEnv(
    {
      NEXT_PUBLIC_TURNSTILE_SITEKEY: "1x00000000000000000000AA",
      NEXT_PUBLIC_TURNSTILE_OPERATIONS: "password_login,login,signup_complete",
    },
    () => {
      const cfg = getPublicConfig();
      assert.equal(cfg.turnstileSiteKey, "1x00000000000000000000AA");
      assert.equal(cfg.turnstileOperations.has("password_login"), true);
      assert.equal(cfg.turnstileOperations.has("signup_complete"), true);
      assert.equal(parseTurnstileAction("login"), null);
    },
  );
  const configSrc = source("config.ts");
  assert.match(configSrc, /NEXT_PUBLIC_TURNSTILE_SITEKEY/);
  assert.equal(configSrc.includes("TURNSTILE_SECRET"), false);
  assert.equal(configSrc.includes("IDENTITY_HUMAN_CHALLENGE_TURNSTILE_SECRET"), false);
  const envExample = source("../.env.example");
  assert.match(envExample, /NEXT_PUBLIC_TURNSTILE_SITEKEY=/);
  assert.equal(envExample.includes("NEXT_PUBLIC_TURNSTILE_SECRET"), false);
});

test("action comes from typed closed mapping and arbitrary action is rejected", () => {
  assert.equal(turnstileActionFor("passkey_login_begin"), "passkey_login_begin");
  assert.equal(parseTurnstileAction("password_login"), "password_login");
  assert.equal(parseTurnstileAction("login"), null);
  assert.throws(() => assertClosedAction("login"));
  const parsed = parseTurnstileOperations("password_login,not-an-action,step_up");
  assert.equal(parsed.has("password_login"), true);
  assert.equal(parsed.size, 1);
});

test("token stays in memory, is attached only to the intended body, and clears after consume", () => {
  withEnv(
    {
      NEXT_PUBLIC_TURNSTILE_SITEKEY: "1x00000000000000000000AA",
      NEXT_PUBLIC_TURNSTILE_OPERATIONS: "password_login",
    },
    () => {
      const slot = createChallengeSlot("password_login");
      const logs: string[] = [];
      const originalLog = console.log;
      console.log = (...args: unknown[]) => {
        logs.push(args.map(String).join(" "));
      };
      try {
        slot.setToken("opaque-turnstile-token");
        assert.equal(slot.snapshot().token, "opaque-turnstile-token");
        const passwordBody = attachChallengeToken(
          { kind: "email", identifier: "a@b.c", password: "x" },
          slot.snapshot().token ?? undefined,
        );
        const signupBody = attachChallengeToken({ signupProof: "proof" }, undefined);
        assert.equal(passwordBody.challengeToken, "opaque-turnstile-token");
        assert.equal("challengeToken" in signupBody, false);
        const taken = slot.consumeToken();
        assert.equal(taken, "opaque-turnstile-token");
        assert.equal(slot.snapshot().token, null);
        const replay = attachChallengeToken({ kind: "email" }, slot.consumeToken());
        assert.equal("challengeToken" in replay, false);
      } finally {
        console.log = originalLog;
      }
      assert.equal(logs.some((line) => line.includes("opaque-turnstile-token")), false);
    },
  );
});

test("token clears on expiry, timeout, and error", () => {
  withEnv({ NEXT_PUBLIC_TURNSTILE_SITEKEY: "1x00000000000000000000AA" }, () => {
    const slot = createChallengeSlot("signup_complete");
    slot.markRequired();
    slot.setToken("t1");
    slot.handleExpired();
    assert.equal(slot.snapshot().token, null);
    slot.setToken("t2");
    slot.handleTimeout();
    assert.equal(slot.snapshot().token, null);
    slot.setToken("t3");
    slot.handleWidgetError();
    assert.equal(slot.snapshot().token, null);
  });
});

test("widget is hidden until challenge is required when operations are empty", () => {
  withEnv(
    {
      NEXT_PUBLIC_TURNSTILE_SITEKEY: "1x00000000000000000000AA",
      NEXT_PUBLIC_TURNSTILE_OPERATIONS: "",
    },
    () => {
      assert.equal(shouldShowTurnstile({ operation: "password_login", challengeRequired: false }), false);
      assert.equal(shouldShowTurnstile({ operation: "password_login", challengeRequired: true }), true);
      const classified = classifyChallengeHttp({
        status: 403,
        errorCode: "challenge_required",
        sentToken: false,
        sitekeyPresent: true,
        operationEnabled: false,
      });
      assert.equal(classified, "challenge_required");
      const slot = createChallengeSlot("password_login");
      assert.equal(slot.snapshot().showWidget, false);
      slot.markRequired();
      assert.equal(slot.snapshot().showWidget, true);
    },
  );
});

test("generic 403 forbidden does not activate Turnstile", () => {
  withEnv(
    {
      NEXT_PUBLIC_TURNSTILE_SITEKEY: "1x00000000000000000000AA",
      NEXT_PUBLIC_TURNSTILE_OPERATIONS: "",
    },
    () => {
      assert.equal(
        classifyChallengeHttp({
          status: 403,
          errorCode: "forbidden",
          sentToken: false,
          sitekeyPresent: true,
          operationEnabled: false,
        }),
        null,
      );
      assert.equal(
        classifyChallengeHttp({
          status: 403,
          sentToken: false,
          sitekeyPresent: true,
          operationEnabled: false,
        }),
        null,
      );
      const slot = createChallengeSlot("password_login");
      const generic = new AuthClientError("generic", "Giriş başarısız oldu. Lütfen tekrar deneyin.");
      assert.equal(challengeEffectForAuthError(generic, "password_login"), null);
      assert.equal(slot.snapshot().showWidget, false);
    },
  );
});

test("unrelated forbidden does not retry Turnstile on another operation", () => {
  withEnv(
    {
      NEXT_PUBLIC_TURNSTILE_SITEKEY: "1x00000000000000000000AA",
      NEXT_PUBLIC_TURNSTILE_OPERATIONS: "",
    },
    () => {
      const signup = createChallengeSlot("signup_complete");
      const passwordRequired = new AuthClientError(
        "challenge_required",
        "Güvenlik doğrulaması gerekli.",
        "password_login",
      );
      assert.equal(challengeEffectForAuthError(passwordRequired, "signup_complete"), null);
      assert.equal(challengeEffectForAuthError(passwordRequired, "password_login"), "mark_required");
      const stepUpForbidden = new AuthClientError("generic", "İşlem tamamlanamadı.");
      assert.equal(challengeEffectForAuthError(stepUpForbidden, "passkey_register_begin"), null);
      assert.equal(signup.snapshot().showWidget, false);
    },
  );
});

test("configured challenge operation still shows the widget before submit", () => {
  withEnv(
    {
      NEXT_PUBLIC_TURNSTILE_SITEKEY: "1x00000000000000000000AA",
      NEXT_PUBLIC_TURNSTILE_OPERATIONS: "password_login",
    },
    () => {
      assert.equal(shouldShowTurnstile({ operation: "password_login", challengeRequired: false }), true);
      assert.equal(shouldShowTurnstile({ operation: "signup_complete", challengeRequired: false }), false);
      const slot = createChallengeSlot("password_login");
      assert.equal(slot.snapshot().showWidget, true);
    },
  );
});

test("backend unavailable after a challenge token is handled as challenge_unavailable", () => {
  assert.equal(
    classifyChallengeHttp({
      status: 503,
      errorCode: "unavailable",
      sentToken: true,
      sitekeyPresent: true,
      operationEnabled: true,
    }),
    "challenge_unavailable",
  );
});

test("challenge_required HTTP activates only that operation and generic 403 stays generic", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ error: "challenge_required" }), {
      status: 403,
      headers: { "Content-Type": "application/json" },
    })) as typeof fetch;
  try {
    await assert.rejects(
      () => loginWithPassword({ kind: "email", identifier: "a@b.c", password: "x" }),
      (err: unknown) => {
        assert.equal(err instanceof AuthClientError, true);
        const authErr = err as AuthClientError;
        assert.equal(authErr.code, "challenge_required");
        assert.equal(authErr.operation, "password_login");
        return true;
      },
    );
  } finally {
    globalThis.fetch = originalFetch;
  }

  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ error: "forbidden" }), {
      status: 403,
      headers: { "Content-Type": "application/json" },
    })) as typeof fetch;
  try {
    await assert.rejects(
      () => loginWithPassword({ kind: "email", identifier: "a@b.c", password: "x" }),
      (err: unknown) => {
        assert.equal(err instanceof AuthClientError, true);
        const authErr = err as AuthClientError;
        assert.equal(authErr.code, "generic");
        assert.equal(authErr.operation, undefined);
        return true;
      },
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("known and unknown challenge_required bodies classify the same", async () => {
  const originalFetch = globalThis.fetch;
  const results: AuthClientError[] = [];
  for (const identifier of ["owner@example.com", "nobody@example.com"]) {
    globalThis.fetch = (async () =>
      new Response(JSON.stringify({ error: "challenge_required" }), {
        status: 403,
        headers: { "Content-Type": "application/json" },
      })) as typeof fetch;
    try {
      await loginWithPassword({ kind: "email", identifier, password: "x" });
    } catch (err) {
      results.push(err as AuthClientError);
    }
  }
  globalThis.fetch = originalFetch;
  assert.equal(results.length, 2);
  assert.equal(results[0]?.code, "challenge_required");
  assert.equal(results[1]?.code, "challenge_required");
  assert.equal(results[0]?.message, results[1]?.message);
});

test("sent challengeToken maps invalid token forbidden to challenge_failed, not required", () => {
  assert.equal(
    classifyChallengeHttp({
      status: 403,
      errorCode: "forbidden",
      sentToken: true,
      sitekeyPresent: true,
      operationEnabled: false,
    }),
    "challenge_failed",
  );
  assert.equal(
    classifyChallengeHttp({
      status: 403,
      errorCode: "challenge_required",
      sentToken: false,
      sitekeyPresent: true,
      operationEnabled: false,
    }),
    "challenge_required",
  );
});

test("unchallenged auth JSON omits challengeToken and 401 copy stays generic", async () => {
  const bodies: unknown[] = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
    bodies.push(init?.body ? JSON.parse(String(init.body)) : null);
    return new Response(JSON.stringify({ error: "unauthenticated" }), {
      status: 401,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  try {
    await assert.rejects(
      () => loginWithPassword({ kind: "email", identifier: "a@b.c", password: "x" }),
      (err: unknown) => {
        assert.equal((err as { message: string }).message, "Giriş başarısız oldu. Lütfen tekrar deneyin.");
        return true;
      },
    );
    assert.equal("challengeToken" in (bodies[0] as object), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("password login sends challengeToken and other operations do not reuse it", async () => {
  const posts: { url: string; body: Record<string, unknown> }[] = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {};
    posts.push({ url, body });
    if (url.includes("/password/login")) {
      return new Response(JSON.stringify({ authenticated: true, userId: "user-1" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    return new Response(JSON.stringify({ challengeId: "c1" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  try {
    await loginWithPassword({
      kind: "email",
      identifier: "a@b.c",
      password: "secret",
      challengeToken: "opaque-turnstile-token",
    });
    await startSignupVerification({
      kind: "email",
      identifier: "a@b.c",
      locale: "tr",
    });
    assert.equal(posts[0]?.body.challengeToken, "opaque-turnstile-token");
    assert.equal("challengeToken" in (posts[1]?.body ?? {}), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("passkey begin sends challengeToken when provided and finish still uses ceremonyToken", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = (globalThis as { window?: unknown }).window;
  (globalThis as { window?: unknown }).window = globalThis;
  (globalThis as { PublicKeyCredential?: unknown }).PublicKeyCredential = function PublicKeyCredential() {};
  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: { credentials: { get: async () => null } },
  });
  const posts: Record<string, unknown>[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    posts.push(init?.body ? JSON.parse(String(init.body)) : {});
    return new Response(JSON.stringify({ error: "forbidden" }), {
      status: 403,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  try {
    await assert.rejects(() => loginWithPasskey({ beginChallengeToken: "begin-token" }));
    assert.equal(posts[0]?.challengeToken, "begin-token");
    assert.equal(posts.length, 1);
  } finally {
    globalThis.fetch = originalFetch;
    (globalThis as { window?: unknown }).window = originalWindow;
  }
  const auth = source("auth.ts");
  assert.match(auth, /navigator\.credentials\.get/);
  assert.match(auth, /assertionToJSON/);
  assert.match(auth, /finishChallengeToken/);
});

test("completeSignup does not receive a password_login token unless passed", async () => {
  const originalFetch = globalThis.fetch;
  let body: Record<string, unknown> = {};
  globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
    body = JSON.parse(String(init?.body));
    return new Response(JSON.stringify({ authenticated: true, userId: "user-1" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
  try {
    await completeSignup({ signupProof: "proof" });
    assert.equal("challengeToken" in body, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("challenge sources never persist or log tokens and do not touch Step-Up or Web Push", () => {
  const files = [
    "auth.ts",
    "humanChallenge.ts",
    "challengeSlot.ts",
    "turnstileScript.ts",
    "config.ts",
    "../components/TurnstileWidget.tsx",
  ];
  for (const file of files) {
    const src = source(file);
    assert.equal(src.includes("localStorage.setItem"), false, file);
    assert.equal(src.includes("sessionStorage.setItem"), false, file);
    assert.equal(src.toLowerCase().includes("step_up"), false, file);
    assert.equal(src.includes("console.log"), false, file);
  }
  assert.equal(source("auth.ts").includes("push-endpoints"), false);
  assert.equal(source("webPush.ts").includes("challengeToken"), false);
  assert.equal(source("../next.config.ts").includes("script-src *"), false);
});
