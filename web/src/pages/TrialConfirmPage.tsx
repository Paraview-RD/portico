import { useEffect, useRef, useState } from "react";

import type { TrialTenant } from "../api/types";
import { trialApi } from "../api/endpoints";
import { AuthShell } from "../components/AuthShell";
import { Alert, Button, Code, CopyField } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { useRouter } from "../router";

/**
 * Where the trial link lands, and the only time the password is shown.
 *
 * It spends the token on arrival rather than behind a button, on the same
 * reasoning as VerifyPage: somebody who clicked a link in an email has already
 * said yes, and a mail client that prefetches would consume the token whether
 * or not there is a button here.
 *
 * The guard against a second run is not politeness — React mounts effects
 * twice in development precisely to surface this, and without it the second
 * run reports a link that just worked as already spent.
 *
 * The password is displayed because this is the one moment it exists in
 * readable form: it was generated a request ago, it is in an email the visitor
 * may not have open, and it is stored nowhere but as a hash. A screen that
 * created a tenant and then sent somebody to their inbox to find out how to
 * open it would be adding a step and a way to fail.
 */
export function TrialConfirmPage() {
  const t = useT();
  const describeError = useErrorMessage();
  const { navigate } = useRouter();

  const [state, setState] = useState<"working" | "done" | "failed">("working");
  const [tenant, setTenant] = useState<TrialTenant | null>(null);
  const [error, setError] = useState("");
  const spent = useRef(false);

  useEffect(() => {
    if (spent.current) return;
    spent.current = true;

    const token =
      new URLSearchParams(window.location.search).get("token") ?? "";
    if (!token) {
      setState("failed");
      setError(t("trialConfirm.noToken"));
      return;
    }

    void (async () => {
      try {
        setTenant(await trialApi.confirmTrial(token));
        setState("done");
      } catch (err) {
        setError(describeError(err));
        setState("failed");
      }
    })();
  }, [describeError, t]);

  if (state === "working") {
    return (
      <AuthShell title={t("trialConfirm.workingTitle")}>
        <p className="text-[var(--color-fg-muted)]">
          {t("trialConfirm.working")}
        </p>
      </AuthShell>
    );
  }

  if (state === "failed" || tenant === null) {
    return (
      <AuthShell title={t("trialConfirm.failedTitle")}>
        <Alert tone="danger">{error}</Alert>
        <Button variant="secondary" onClick={() => navigate("/trial")}>
          {t("trialConfirm.startAgain")}
        </Button>
      </AuthShell>
    );
  }

  return (
    <AuthShell
      title={t("trialConfirm.readyTitle")}
      subtitle={t("trialConfirm.ready", tenant.tenantName)}
    >
      {/* Copyable rather than selectable. This is a generated password nobody
          will retype correctly, and the one place it can be had. */}
      <div className="flex flex-col gap-3">
        <CopyField label={t("trialConfirm.tenant")} value={tenant.tenantCode} />
        <CopyField
          label={t("trialConfirm.username")}
          value={tenant.adminUsername}
        />
        <CopyField
          label={t("trialConfirm.password")}
          value={tenant.adminPassword}
        />
        {/* A second password, for the accounts the pack created. Absent when
            the fill failed, which is the one case where a working tenant is
            handed over empty — see the note below it. */}
        {tenant.demoPassword && (
          <CopyField
            label={t("trialConfirm.demoPassword")}
            value={tenant.demoPassword}
          />
        )}
      </div>

      {tenant.demoPassword ? (
        <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
          {t("trialConfirm.demoAccounts")}
        </p>
      ) : (
        <Alert tone="warning">{t("trialConfirm.emptyTenant")}</Alert>
      )}

      <Alert tone="warning">{t("trialConfirm.onlyTime")}</Alert>

      {/* Said plainly, because a demonstration that looks like a product is
          how somebody ends up with real staff records in it. */}
      <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("trialConfirm.notReal")}
      </p>

      <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("trialConfirm.signInHint")} <Code>{tenant.tenantCode}</Code>
      </p>

      <Button onClick={() => navigate("/login")}>
        {t("trialConfirm.signIn")}
      </Button>
    </AuthShell>
  );
}
