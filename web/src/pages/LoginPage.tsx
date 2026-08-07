import { useEffect, useState } from "react";

import { ApiError } from "../api/client";
import { authApi } from "../api/endpoints";
import { Alert, Button, Field, Input } from "../components/ui";
import { useT } from "../i18n";
import { useRouter } from "../router";
import { useSession } from "../session";

export function LoginPage() {
  const t = useT();
  const { signIn, expired } = useSession();
  const { navigate } = useRouter();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [registrationOpen, setRegistrationOpen] = useState(false);
  const [systemName, setSystemName] = useState("Portico");

  // The sign-in screen only offers registration when the server says it is
  // open, so a closed instance does not advertise a dead end.
  useEffect(() => {
    authApi
      .registrationStatus()
      .then((status) => {
        setRegistrationOpen(status.registrationEnabled);
        setSystemName(status.systemName);
      })
      .catch(() => {
        // Not being able to read the toggle is not worth blocking sign-in.
      });
  }, []);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await signIn(username, password);
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
          <Field label={t("login.username")} required>
            <Input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
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
