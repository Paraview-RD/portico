import { type ReactNode, useEffect, useRef, useState } from "react";

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
        {/* What is actually happening, rather than "please wait". Creating
            the tenant and filling it with a pack takes a second or two on a
            developer's machine and several on a small instance, and a wait
            with nothing to read is indistinguishable from a page that has
            stopped working. */}
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
      <div className="flex flex-col gap-6">
        {/* The way in, first and on its own.

            These four fields used to be one flat list, which made the
            administrator's password and a shared demonstration password look
            equally important. They are not: one of them is the only way into
            this tenant, and the other is a convenience for looking around. */}
        <section className="flex flex-col gap-3">
          <SectionLabel>{t("trialConfirm.adminSection")}</SectionLabel>
          {/* Copyable rather than selectable. This is a generated password
              nobody will retype correctly, and the one place it can be had. */}
          <CopyField
            label={t("trialConfirm.tenant")}
            value={tenant.tenantCode}
          />
          <CopyField
            label={t("trialConfirm.username")}
            value={tenant.adminUsername}
          />
          <CopyField
            label={t("trialConfirm.password")}
            value={tenant.adminPassword}
          />
          <Alert tone="warning">{t("trialConfirm.onlyTime")}</Alert>
        </section>

        {/* The accounts the pack created. Absent when the fill failed, which
            is the one case where a working tenant is handed over empty. */}
        {tenant.demoPassword ? (
          <section className="flex flex-col gap-3 border-t border-[var(--color-border)] pt-6">
            <SectionLabel>{t("trialConfirm.demoSection")}</SectionLabel>
            <CopyField
              label={t("trialConfirm.demoPassword")}
              value={tenant.demoPassword}
            />
            <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
              {t("trialConfirm.demoAccounts")}
            </p>
          </section>
        ) : (
          <Alert tone="warning">{t("trialConfirm.emptyTenant")}</Alert>
        )}

        <section className="flex flex-col gap-3 border-t border-[var(--color-border)] pt-6">
          <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("trialConfirm.signInHint")} <Code>{tenant.tenantCode}</Code>
          </p>
          {/* Carries the tenant, so the sign-in form arrives filled in.
              navigate() takes a route and no query string, and giving it one
              would change how every other redirect in the app treats the
              parameters it is currently leaving alone — including the
              authorization flow, whose whole state is in its query string. A
              full navigation is the honest way to do it here: this is a
              one-way door out of a page whose state is spent.

              Relative rather than the signInUrl the server returned, which is
              built from PORTICO_PUBLIC_URL — right for the copy in the email
              and needlessly dependent on configuration for a link in the page
              that configuration is already serving. */}
          <Button
            onClick={() =>
              window.location.assign(
                `/login?tenant=${encodeURIComponent(tenant.tenantCode)}`,
              )
            }
          >
            {t("trialConfirm.signIn")}
          </Button>
          {/* Last, and quietly. Said plainly because a demonstration that
              looks like a product is how somebody ends up with real staff
              records in it — but it is not what they came here to read. */}
          <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("trialConfirm.notReal")}
          </p>
        </section>
      </div>
    </AuthShell>
  );
}

/**
 * A heading for one group of fields on this page.
 *
 * Local rather than in ui.tsx: the other signed-out screens are single forms
 * with nothing to group, and a shared component with one caller is a
 * component nobody can change safely.
 */
function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <h2 className="text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
      {children}
    </h2>
  );
}
