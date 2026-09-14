"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { createChallengeSlot, type ChallengeSlotState } from "@/lib/challengeSlot";
import { getTurnstilePublicConfig } from "@/lib/config";
import { AuthClientError } from "@/lib/auth";
import { challengeEffectForAuthError } from "@/lib/humanChallenge";
import {
  loadTurnstileScript,
  turnstileRenderOptions,
  type TurnstileAPI,
} from "@/lib/turnstileScript";
import { assertClosedAction, type TurnstileAction } from "@/lib/turnstileActions";

type TurnstileWidgetProps = {
  sitekey: string;
  action: TurnstileAction;
  onToken: (token: string) => void;
  onExpired: () => void;
  onError: () => void;
  onTimeout?: () => void;
  resetKey?: number;
};

export function TurnstileWidget(props: TurnstileWidgetProps) {
  const action = assertClosedAction(props.action);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const widgetIdRef = useRef<string | null>(null);
  const apiRef = useRef<TurnstileAPI | null>(null);
  const callbacksRef = useRef(props);
  callbacksRef.current = props;

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !props.sitekey) {
      return;
    }
    let cancelled = false;

    void loadTurnstileScript()
      .then((api) => {
        if (cancelled || !containerRef.current) {
          return;
        }
        apiRef.current = api;
        widgetIdRef.current = api.render(
          containerRef.current,
          turnstileRenderOptions({
            sitekey: props.sitekey,
            action,
            onToken: (token) => callbacksRef.current.onToken(token),
            onError: () => callbacksRef.current.onError(),
            onExpired: () => callbacksRef.current.onExpired(),
            onTimeout: () =>
              (callbacksRef.current.onTimeout ?? callbacksRef.current.onExpired)(),
          }),
        );
      })
      .catch(() => {
        if (!cancelled) {
          callbacksRef.current.onError();
        }
      });

    return () => {
      cancelled = true;
      const id = widgetIdRef.current;
      widgetIdRef.current = null;
      if (id && apiRef.current) {
        try {
          apiRef.current.remove(id);
        } catch {
          try {
            apiRef.current.reset(id);
          } catch {
            /* widget already gone */
          }
        }
      }
    };
  }, [props.sitekey, action, props.resetKey]);

  return (
    <div className="auth-challenge" data-turnstile-action={action}>
      <p className="auth-field" id={`turnstile-label-${action}`}>
        Güvenlik doğrulaması
      </p>
      <div
        ref={containerRef}
        className="auth-challenge-widget"
        role="group"
        aria-labelledby={`turnstile-label-${action}`}
      />
    </div>
  );
}

export type TurnstileChallenge = ChallengeSlotState & {
  consumeToken: () => string | undefined;
  applyAuthError: (error: unknown) => void;
  widget: {
    onToken: (token: string) => void;
    onExpired: () => void;
    onError: () => void;
    onTimeout: () => void;
  };
  blocksSubmit: boolean;
};

export function useTurnstileChallenge(operation: TurnstileAction): TurnstileChallenge {
  const slotRef = useRef(createChallengeSlot(operation));
  const [state, setState] = useState(slotRef.current.snapshot());

  const sync = useCallback(() => {
    setState({ ...slotRef.current.snapshot() });
  }, []);

  const onToken = useCallback(
    (token: string) => {
      slotRef.current.setToken(token);
      sync();
    },
    [sync],
  );

  const onExpired = useCallback(() => {
    slotRef.current.handleExpired();
    sync();
  }, [sync]);

  const onError = useCallback(() => {
    slotRef.current.handleWidgetError();
    sync();
  }, [sync]);

  const onTimeout = useCallback(() => {
    slotRef.current.handleTimeout();
    sync();
  }, [sync]);

  const consumeToken = useCallback(() => {
    const token = slotRef.current.consumeToken();
    sync();
    return token;
  }, [sync]);

  const applyAuthError = useCallback(
    (error: unknown) => {
      if (!(error instanceof AuthClientError)) {
        return;
      }
      const effect = challengeEffectForAuthError(error, operation);
      if (effect === "mark_required") {
        slotRef.current.markRequired();
        sync();
        return;
      }
      if (effect === "expired") {
        slotRef.current.handleExpired();
        sync();
        return;
      }
      if (effect === "unavailable") {
        slotRef.current.handleWidgetError();
        sync();
      }
    },
    [operation, sync],
  );

  return {
    ...state,
    consumeToken,
    applyAuthError,
    widget: { onToken, onExpired, onError, onTimeout },
    blocksSubmit: state.showWidget && !state.token,
  };
}

export function AuthTurnstile({
  challenge,
}: {
  challenge: TurnstileChallenge;
}) {
  const sitekey = getTurnstilePublicConfig().turnstileSiteKey;
  if (!challenge.showWidget || !sitekey) {
    return null;
  }
  return (
    <>
      <TurnstileWidget
        sitekey={sitekey}
        action={challenge.operation}
        resetKey={challenge.widgetKey}
        onToken={challenge.widget.onToken}
        onExpired={challenge.widget.onExpired}
        onError={challenge.widget.onError}
        onTimeout={challenge.widget.onTimeout}
      />
      {challenge.message ? <p className="auth-error">{challenge.message}</p> : null}
    </>
  );
}
