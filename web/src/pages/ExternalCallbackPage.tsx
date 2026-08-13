import { useEffect, useState } from "react";

import { authApi } from "../api/endpoints";
import { isSession } from "../api/types";
import { AuthShell } from "../components/AuthShell";
import { Alert, Button } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { useRouter } from "../router";
import type { Route } from "../router";
import { useSession } from "../session";

/**
 * Where somebody else's provider sends a browser back to.
 *
 * The address registered at the provider is this screen and not the API
 * endpoint that does the work, because coming back is a top-level
 * navigation: whatever answers it is what a person is looking at. The
 * endpoint answers JSON. So the landing happens here, and this screen spends
 * the `state` and `code` out of its own address on the call that judges
 * them — which also keeps the session out of a URL that browser history and
 * every proxy in between would otherwise keep a copy of.
 *
 * It serves two journeys that look identical from here: a sign-in, and a
 * signed-in person linking an identity to their own account. Which one this
 * was is not read from anything the browser brought back — it was decided
 * when the request departed and remembered server-side — so this screen
 * learns which it was from the answer rather than deciding it.
 */

/** What the callback address carries. */
export interface ExternalCallback {
  /** From the path, empty for the default tenant. */
  tenant: string;
  state: string;
  code: string;
  /** Set when the provider itself refused, in place of a code. */
  error: string;
}

const CALLBACK_PATH = "/external/callback";

/**
 * Reads a callback out of the address, or null if this is not one.
 *
 * Matched on the path rather than added to the router's route union: it is
 * one of two shapes, one of them carrying a tenant code, and neither is a
 * screen anybody navigates to from inside the app.
 */
export function externalCallback(
  pathname: string,
  search: string,
): ExternalCallback | null {
  let tenant = "";
  if (pathname !== CALLBACK_PATH) {
    const prefixed = new RegExp(`^/t/([^/]+)${CALLBACK_PATH}$`).exec(pathname);
    if (!prefixed) return null;
    tenant = decodeURIComponent(prefixed[1]);
  }

  const params = new URLSearchParams(search);
  return {
    tenant,
    state: params.get("state") ?? "",
    code: params.get("code") ?? "",
    error: params.get("error") ?? "",
  };
}

/**
 * States already spent, so no second attempt is made on one.
 *
 * The server deletes the request row with the same statement that reads it,
 * so a second exchange of the same state fails — and would report a sign-in
 * that worked as one that did not. React's development mode mounts effects
 * twice on purpose, which is exactly this mistake, and a module-level record
 * also covers a person pressing reload on the callback address.
 */
const spent = new Set<string>();

export function ExternalCallbackPage({
  callback,
  onDone,
}: {
  callback: ExternalCallback;
  /** Hands the address back to the router once this landing is over. */
  onDone: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const { navigate } = useRouter();
  const { adoptIssuedSession } = useSession();

  const [state, setState] = useState<"working" | "failed">("working");
  const [error, setError] = useState("");

  useEffect(() => {
    if (callback.error) {
      setState("failed");
      setError(t("external.providerRefused", callback.error));
      return;
    }
    if (!callback.state || !callback.code) {
      setState("failed");
      setError(t("external.incomplete"));
      return;
    }
    if (spent.has(callback.state)) return;
    spent.add(callback.state);

    // The tenant comes from the path, which is why it is in the path: a
    // browser that has been to another site and back brings no header and no
    // cookie of ours, and taking it from storage instead would let the last
    // tenant somebody typed decide which tenant they just signed in to.
    //
    // Passed to the call rather than written to storage first. This exchange
    // fails routinely — a reload, a link opened twice — and a landing that
    // failed must not leave the browser pointed at a tenant nobody signed in
    // to. Storage is written on the way out, by the session that succeeded.
    let abandoned = false;
    authApi
      .completeExternalSignIn(callback.state, callback.code, callback.tenant)
      .then(async (result) => {
        if (abandoned) return;

        if (isSession(result)) {
          await adoptIssuedSession(result.token, callback.tenant);
          leave("/", navigate, onDone);
          return;
        }
        // A binding. The person already had a session; the profile screen is
        // where the new link is, and showing it is a better confirmation
        // than a sentence saying it happened.
        leave("/profile", navigate, onDone);
      })
      .catch((err) => {
        if (abandoned) return;
        setState("failed");
        setError(describeError(err));
      });

    return () => {
      abandoned = true;
    };
  }, [callback, t, describeError, navigate, adoptIssuedSession, onDone]);

  return (
    <AuthShell title={t("external.title")} subtitle={t("external.subtitle")}>
      {state === "working" && (
        <p className="text-[var(--color-fg-muted)]">{t("external.working")}</p>
      )}

      {state === "failed" && (
        <div className="flex flex-col items-start gap-4">
          <Alert tone="danger">{error}</Alert>
          <Button
            variant="secondary"
            onClick={() => leave("/login", navigate, onDone)}
          >
            {t("recovery.backToSignIn")}
          </Button>
        </div>
      )}
    </AuthShell>
  );
}

/**
 * Takes the callback address out of the history and hands over to the router.
 *
 * Replaced rather than pushed: the state on it has been spent, so a back
 * button that returned here would land on a page whose only possible outcome
 * is an error about a sign-in that in fact succeeded.
 */
function leave(to: Route, navigate: (to: Route) => void, onDone: () => void) {
  window.history.replaceState({}, "", to);
  onDone();
  navigate(to);
}
