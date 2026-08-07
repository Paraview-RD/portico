import { useEffect, useState } from "react";

import { ApiError, tenantStore } from "../api/client";
import { authApi } from "../api/endpoints";
import { Alert, Button, Field, Input } from "../components/ui";
import { useT } from "../i18n";
import { useRouter } from "../router";
import { useSession } from "../session";

export function LoginPage() {
  const t = useT();
  const { signIn, expired } = useSession();
  const { navigate } = useRouter();

  // Remembered from the last sign-in, or taken from a ?tenant= link, so an
  // operator can hand out a URL that lands on the right tenant. Blank means
  // the default tenant, which is all a single-tenant deployment ever needs.
  const initialTenant =
    new URLSearchParams(window.location.search).get("tenant") ??
    tenantStore.get() ??
    "";

  const [tenant, setTenant] = useState(initialTenant);
  // The tenant the lookup below has run for. It trails the field rather than
  // tracking it, because the field changes on every keystroke and typing
  // "acme" would otherwise issue four requests, three of them for tenants
  // that do not exist.
  const [lookedUpTenant, setLookedUpTenant] = useState(initialTenant);
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [registrationOpen, setRegistrationOpen] = useState(false);
  const [systemName, setSystemName] = useState("Portico");

  // The sign-in screen only offers registration when the server says it is
  // open, so a closed instance does not advertise a dead end. Both that and
  // the name in the header belong to the tenant being signed in to, so the
  // lookup runs again when the tenant settles.
  useEffect(() => {
    tenantStore.set(lookedUpTenant);

    const controller = new AbortController();
    authApi
      .registrationStatus(controller.signal)
      .then((status) => {
        setRegistrationOpen(status.registrationEnabled);
        setSystemName(status.systemName);
      })
      .catch(() => {
        // An unknown tenant lands here. Sign-in will say so plainly, which
        // is more useful than an error under a field they may not have
        // finished filling in — but the header must not keep showing the
        // previous tenant's name as though this one existed.
        setRegistrationOpen(false);
        setSystemName("Portico");
      });

    return () => controller.abort();
  }, [lookedUpTenant]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      // Submitting without leaving the field never fires onBlur, so settle
      // the lookup here too.
      setLookedUpTenant(tenant.trim());
      await signIn(tenant.trim(), identifier, password);
      navigate("/users");
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : t("common.unexpectedError"),
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-dvh items-center justify-center bg-[var(--color-bg-soft)] p-4">
      <div className="w-full max-w-sm rounded-[var(--radius-lg)] bg-[var(--color-bg)] p-6 shadow-[var(--shadow-md)]">
        <h1 className="mb-1 text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
          {systemName}
        </h1>
        <p className="mb-5 text-[var(--color-fg-muted)]">{t("login.title")}</p>

        {expired && (
          <div className="mb-4">
            <Alert tone="danger">{t("login.sessionExpired")}</Alert>
          </div>
        )}

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Field label={t("login.tenant")} hint={t("login.tenantHint")}>
            <Input
              value={tenant}
              onChange={(e) => setTenant(e.target.value)}
              onBlur={() => setLookedUpTenant(tenant.trim())}
              autoComplete="organization"
              placeholder="default"
            />
          </Field>

          <Field
            label={t("login.identifier")}
            hint={t("login.identifierHint")}
            required
          >
            <Input
              value={identifier}
              onChange={(e) => setIdentifier(e.target.value)}
              autoComplete="username"
              autoFocus
              required
            />
          </Field>

          <Field label={t("login.password")} required>
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              required
            />
          </Field>

          {error && <Alert tone="danger">{error}</Alert>}

          <Button type="submit" disabled={submitting}>
            {submitting ? t("login.signingIn") : t("login.submit")}
          </Button>
        </form>

        <p className="mt-4 text-center text-[length:var(--font-size-sm)]">
          <button
            type="button"
            onClick={() => navigate("/forgot-password")}
            className="text-[var(--color-primary)] underline-offset-2 hover:underline"
          >
            {t("login.forgotPassword")}
          </button>
        </p>

        {registrationOpen && (
          <p className="mt-4 text-center text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("login.noAccount")}{" "}
            <button
              type="button"
              onClick={() => navigate("/register")}
              className="text-[var(--color-primary)] underline-offset-2 hover:underline"
            >
              {t("login.register")}
            </button>
          </p>
        )}
      </div>
    </div>
  );
}
