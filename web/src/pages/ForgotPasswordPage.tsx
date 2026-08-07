import { useEffect, useState } from "react";

import { ApiError, tenantStore } from "../api/client";
import { authApi } from "../api/endpoints";
import type { RecoveryChannel } from "../api/types";
import { Alert, Button, Field, Input } from "../components/ui";
import { useT } from "../i18n";
import { useRouter } from "../router";

export function ForgotPasswordPage() {
  const t = useT();
  const { navigate } = useRouter();

  const [tenant] = useState(
    () =>
      new URLSearchParams(window.location.search).get("tenant") ??
      tenantStore.get() ??
      "",
  );
  const [channels, setChannels] = useState<RecoveryChannel[]>([]);
  const [ttlMinutes, setTtlMinutes] = useState(30);
  const [channel, setChannel] = useState<RecoveryChannel>("EMAIL");
  const [destination, setDestination] = useState("");
  const [error, setError] = useState("");
  const [sent, setSent] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  // Only channels the deployment can actually use are offered. A form that
  // fails on submit because nothing is configured wastes the one attempt
  // someone locked out of their account has patience for.
  useEffect(() => {
    tenantStore.set(tenant);
    const controller = new AbortController();
    authApi
      .recoveryChannels(controller.signal)
      .then(({ channels: available, tokenTtlMinutes }) => {
        setChannels(available);
        setTtlMinutes(tokenTtlMinutes);
        if (available.length > 0) setChannel(available[0]);
      })
      .catch(() => setChannels([]));
    return () => controller.abort();
  }, [tenant]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await authApi.requestPasswordRecovery(channel, destination);
      setSent(true);
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
          {t("recovery.title")}
        </h1>
        <p className="mb-5 text-[var(--color-fg-muted)]">
          {t("recovery.subtitle")}
        </p>

        {channels.length === 0 ? (
          <div className="flex flex-col gap-4">
            <Alert tone="danger">{t("recovery.unavailable")}</Alert>
            <Button onClick={() => navigate("/login")}>
              {t("recovery.backToSignIn")}
            </Button>
          </div>
        ) : sent ? (
          <div className="flex flex-col gap-4">
            {/* Says "if", not "we sent": the server will not reveal whether
                an account matched, and neither may this screen. */}
            <Alert tone="success">{t("recovery.sent", ttlMinutes)}</Alert>
            <Button onClick={() => navigate("/login")}>
              {t("recovery.backToSignIn")}
            </Button>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            {channels.length > 1 && (
              <Field label={t("recovery.channel")}>
                <div className="flex gap-2">
                  {channels.map((option) => (
                    <Button
                      key={option}
                      type="button"
                      variant={channel === option ? "primary" : "secondary"}
                      onClick={() => setChannel(option)}
                    >
                      {t(`recovery.channel.${option}`)}
                    </Button>
                  ))}
                </div>
              </Field>
            )}

            <Field
              label={
                channel === "EMAIL" ? t("recovery.email") : t("recovery.phone")
              }
              required
            >
              <Input
                value={destination}
                onChange={(e) => setDestination(e.target.value)}
                type={channel === "EMAIL" ? "email" : "tel"}
                autoComplete={channel === "EMAIL" ? "email" : "tel"}
                autoFocus
                required
              />
            </Field>

            {error && <Alert tone="danger">{error}</Alert>}

            <Button type="submit" disabled={submitting}>
              {submitting ? t("recovery.sending") : t("recovery.submit")}
            </Button>

            <button
              type="button"
              onClick={() => navigate("/login")}
              className="text-[length:var(--font-size-sm)] text-[var(--color-primary)] underline-offset-2 hover:underline"
            >
              {t("recovery.backToSignIn")}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
