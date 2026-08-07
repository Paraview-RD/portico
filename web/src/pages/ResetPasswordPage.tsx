import { useEffect, useState } from "react";

import { ApiError, tenantStore } from "../api/client";
import { authApi } from "../api/endpoints";
import { Alert, Button, Field, Input } from "../components/ui";
import { useT } from "../i18n";
import { useRouter } from "../router";

export function ResetPasswordPage() {
  const t = useT();
  const { navigate } = useRouter();

  // Both come from the link in the message. The tenant travels with the
  // token so redeeming it is a tenant-scoped lookup on the server.
  const params = new URLSearchParams(window.location.search);
  const token = params.get("token") ?? "";
  const linkTenant = params.get("tenant") ?? "";

  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (linkTenant) tenantStore.set(linkTenant);
  }, [linkTenant]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");

    // Checked here rather than server-side: the confirmation field exists
    // only to catch typing mistakes, so it never needs to reach the API.
    if (password !== confirmPassword) {
      setError(t("register.passwordMismatch"));
      return;
    }

    setSubmitting(true);
    try {
      await authApi.confirmPasswordRecovery(token, password);
      setDone(true);
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
        <h1 className="mb-5 text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
          {t("reset.title")}
        </h1>

        {!token ? (
          <div className="flex flex-col gap-4">
            <Alert tone="danger">{t("reset.missingToken")}</Alert>
            <Button onClick={() => navigate("/forgot-password")}>
              {t("reset.requestAnother")}
            </Button>
          </div>
        ) : done ? (
          <div className="flex flex-col gap-4">
            <Alert tone="success">{t("reset.done")}</Alert>
            <Button onClick={() => navigate("/login")}>
              {t("recovery.backToSignIn")}
            </Button>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <Field label={t("profile.newPassword")} required>
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                autoFocus
                required
              />
            </Field>

            <Field label={t("profile.confirmNewPassword")} required>
              <Input
                type="password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                autoComplete="new-password"
                required
              />
            </Field>

            {error && <Alert tone="danger">{error}</Alert>}

            <Button type="submit" disabled={submitting}>
              {submitting ? t("reset.saving") : t("reset.submit")}
            </Button>

            <button
              type="button"
              onClick={() => navigate("/forgot-password")}
              className="text-[length:var(--font-size-sm)] text-[var(--color-primary)] underline-offset-2 hover:underline"
            >
              {t("reset.requestAnother")}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
