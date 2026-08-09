import { useEffect, useState } from "react";

import { authApi } from "../api/endpoints";
import { AuthShell } from "../components/AuthShell";
import { Alert, Button } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { useRouter } from "../router";

/**
 * The screen a confirmation link lands on.
 *
 * It redeems the token on arrival rather than showing a button to press.
 * Somebody who has clicked a link in an email has already expressed the
 * intent; asking them to click again is a second chance to fail, and the
 * only thing it would protect against is a mail client prefetching links —
 * which would consume the token whether or not there is a button here.
 *
 * The link works once, so this must not run twice. React's development mode
 * mounts effects twice on purpose to surface exactly this, and the guard
 * below is why the second run does not report a valid link as spent.
 */
export function VerifyPage() {
  const t = useT();
  const describeError = useErrorMessage();
  const { navigate } = useRouter();

  const [state, setState] = useState<"working" | "done" | "failed">("working");
  const [error, setError] = useState("");

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const token = params.get("token") ?? "";
    const tenant = params.get("tenant") ?? "";

    if (!token) {
      setState("failed");
      setError(t("verify.noToken"));
      return;
    }

    // Not cancelled on unmount: the request has already been sent by then
    // and the token already spent, so abandoning it would leave somebody
    // looking at a screen that never resolves for a link that did work.
    let spent = false;
    authApi
      .confirmRegistration(token, tenant)
      .then(() => {
        if (!spent) setState("done");
      })
      .catch((err) => {
        if (spent) return;
        setState("failed");
        setError(describeError(err));
      });
    return () => {
      spent = true;
    };
  }, [t, describeError]);

  return (
    <AuthShell title={t("verify.title")} subtitle={t("verify.subtitle")}>
      {state === "working" && (
        <p className="text-[var(--color-fg-muted)]">{t("verify.working")}</p>
      )}

      {state === "done" && (
        <div className="flex flex-col items-start gap-4">
          <Alert tone="success">{t("verify.done")}</Alert>
          <Button onClick={() => navigate("/login")}>
            {t("recovery.backToSignIn")}
          </Button>
        </div>
      )}

      {state === "failed" && (
        <div className="flex flex-col items-start gap-4">
          <Alert tone="danger">{error}</Alert>
          {/* No resend form here. The link carries a token and not an
              address, so this screen does not know who to send another to —
              the sign-in screen does, because the person types it. */}
          <Button variant="secondary" onClick={() => navigate("/login")}>
            {t("recovery.backToSignIn")}
          </Button>
        </div>
      )}
    </AuthShell>
  );
}
