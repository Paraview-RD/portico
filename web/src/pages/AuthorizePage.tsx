import { useEffect, useRef, useState } from "react";

import { ApiError } from "../api/client";
import { oauthApi } from "../api/endpoints";
import { AuthShell } from "../components/AuthShell";
import { Alert, Button } from "../components/ui";
import { useT } from "../i18n";
import { useSession } from "../session";
import { LoginPage } from "./LoginPage";

/**
 * The hand-off back to the OpenID Provider.
 *
 * An application sent the browser to /authorize; the provider needed to know
 * who is at the keyboard and redirected here. Once somebody is signed in,
 * this tells the server which request they are completing and follows the
 * redirect it answers with — after which the protocol library takes over
 * again and the person is back in the application they started from.
 *
 * There is no consent step. Every client is registered out of band by an
 * administrator, so there is no third party to consent to; see
 * docs/federation.md.
 */
export function AuthorizePage({ authRequestId }: { authRequestId: string }) {
  const t = useT();
  const { user, loading, signOut } = useSession();
  const [error, setError] = useState("");
  const [wrongTenant, setWrongTenant] = useState(false);

  // The redirect is a side effect that must happen exactly once. Without
  // this, React's development double-render would complete the request
  // twice, and the second attempt would fail against a request the first
  // one had already consumed.
  const started = useRef(false);

  useEffect(() => {
    if (loading || !user || started.current) return;
    started.current = true;

    oauthApi
      .authorize(authRequestId)
      .then((authorization) => {
        // A full navigation, not a router push: the destination belongs to
        // the provider, which is outside this application.
        window.location.assign(authorization.redirectTo);
      })
      .catch((err: unknown) => {
        if (err instanceof ApiError) {
          setWrongTenant(err.code === "AUTH_REQUEST_WRONG_TENANT");
          setError(err.message);
        } else {
          setError(t("common.unexpectedError"));
        }
      });
  }, [authRequestId, loading, user, t]);

  if (loading) {
    return (
      <div className="flex min-h-dvh items-center justify-center text-[var(--color-fg-muted)]">
        {t("common.loading")}
      </div>
    );
  }

  // Not signed in yet: the ordinary sign-in screen does its job, and this
  // component re-renders into the branch below the moment it succeeds.
  if (!user) {
    return <LoginPage />;
  }

  return (
    <AuthShell title={t("authorize.title")}>
      {error ? (
        <div className="flex flex-col gap-4">
          <Alert tone="danger">{error}</Alert>
          {wrongTenant && (
            <Button variant="secondary" onClick={() => void signOut()}>
              {t("authorize.signOutAndRetry")}
            </Button>
          )}
        </div>
      ) : (
        <p className="text-center text-[var(--color-fg-muted)]">
          {t("authorize.redirecting")}
        </p>
      )}
    </AuthShell>
  );
}
