import { useState } from "react";

import { tenantStore } from "../api/client";
import { authApi } from "../api/endpoints";
import { AuthLink, AuthShell } from "../components/AuthShell";
import { Alert, Button, Field, Input } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { useRouter } from "../router";

export function RegisterPage() {
  const t = useT();
  const describeError = useErrorMessage();
  const { navigate } = useRouter();

  // Which tenant the account is being created in. Carried over from the
  // sign-in screen, or from a ?tenant= link for someone who arrived here
  // directly. Blank means the default tenant.
  const [tenant, setTenant] = useState(
    () =>
      new URLSearchParams(window.location.search).get("tenant") ??
      tenantStore.get() ??
      "",
  );
  const [form, setForm] = useState({
    username: "",
    displayName: "",
    password: "",
    confirmPassword: "",
    phone: "",
    email: "",
  });
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  function set(field: keyof typeof form, value: string) {
    setForm((previous) => ({ ...previous, [field]: value }));
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");

    // Checked here rather than server-side: the confirmation field exists
    // only to catch typing mistakes, so it never needs to reach the API.
    if (form.password !== form.confirmPassword) {
      setError(t("register.passwordMismatch"));
      return;
    }

    setSubmitting(true);
    try {
      // The client attaches this to anonymous requests as a header.
      tenantStore.set(tenant);
      await authApi.register({
        username: form.username,
        displayName: form.displayName,
        password: form.password,
        phone: form.phone,
        email: form.email,
      });
      setDone(true);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthShell
      title={t("register.title")}
      footer={
        <>
          {t("register.haveAccount")}{" "}
          <AuthLink onClick={() => navigate("/login")}>
            {t("register.signIn")}
          </AuthLink>
        </>
      }
    >
      {done ? (
        <div className="flex flex-col gap-4">
          <Alert tone="success">{t("register.success")}</Alert>
          <Button onClick={() => navigate("/login")}>
            {t("register.signIn")}
          </Button>
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Field label={t("login.tenant")} hint={t("login.tenantHint")}>
            <Input
              value={tenant}
              onChange={(e) => setTenant(e.target.value)}
              autoComplete="organization"
              placeholder="default"
            />
          </Field>

          <Field label={t("login.username")} required>
            <Input
              value={form.username}
              onChange={(e) => set("username", e.target.value)}
              autoComplete="username"
              autoFocus
              required
            />
          </Field>

          <Field label={t("register.displayName")} required>
            <Input
              value={form.displayName}
              onChange={(e) => set("displayName", e.target.value)}
              required
            />
          </Field>

          <Field label={t("login.password")} required>
            <Input
              type="password"
              value={form.password}
              onChange={(e) => set("password", e.target.value)}
              autoComplete="new-password"
              required
            />
          </Field>

          <Field label={t("register.confirmPassword")} required>
            <Input
              type="password"
              value={form.confirmPassword}
              onChange={(e) => set("confirmPassword", e.target.value)}
              autoComplete="new-password"
              required
            />
          </Field>

          <Field label={`${t("register.phone")} (${t("common.optional")})`}>
            <Input
              value={form.phone}
              onChange={(e) => set("phone", e.target.value)}
              autoComplete="tel"
            />
          </Field>

          <Field label={`${t("register.email")} (${t("common.optional")})`}>
            <Input
              type="email"
              value={form.email}
              onChange={(e) => set("email", e.target.value)}
              autoComplete="email"
            />
          </Field>

          {error && <Alert tone="danger">{error}</Alert>}

          <Button type="submit" disabled={submitting}>
            {t("register.submit")}
          </Button>
        </form>
      )}
    </AuthShell>
  );
}
