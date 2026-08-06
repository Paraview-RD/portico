import { useState } from "react";

import { ApiError } from "../api/client";
import { userApi } from "../api/endpoints";
import {
  Alert,
  Badge,
  Button,
  Field,
  Input,
  PageHeader,
} from "../components/ui";
import { useT } from "../i18n";
import { useSession } from "../session";

export function ProfilePage() {
  const t = useT();
  const { user, endSession } = useSession();

  const [form, setForm] = useState({ current: "", next: "", confirm: "" });
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [changed, setChanged] = useState(false);

  if (!user) return null;

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");

    if (form.next !== form.confirm) {
      setError(t("register.passwordMismatch"));
      return;
    }

    setSubmitting(true);
    try {
      await userApi.changeOwnPassword(form.current, form.next);

      // Changing a password revokes every token, including the one this
      // request was made with. The session has to end — leaving the user on
      // this screen would make their next click fail with an unexplained
      // authentication error. Show why first, then end it on acknowledgement
      // rather than yanking them to the sign-in screen mid-sentence.
      setChanged(true);
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : t("common.unexpectedError"),
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      <PageHeader title={t("profile.title")} subtitle={t("profile.subtitle")} />

      <div className="mb-8 max-w-md rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-bg)] p-4">
        <dl className="flex flex-col gap-3">
          <Detail label={t("profile.username")} value={user.username} />
          <Detail label={t("profile.displayName")} value={user.displayName} />
          <div className="flex justify-between gap-4">
            <dt className="text-[var(--color-fg-muted)]">
              {t("profile.role")}
            </dt>
            <dd>
              <Badge tone={user.role === "SUPER_ADMIN" ? "warning" : "neutral"}>
                {t(`role.${user.role}`)}
              </Badge>
            </dd>
          </div>
          <Detail
            label={t("profile.organization")}
            value={user.organizationName || "—"}
          />
        </dl>
      </div>

      <h2 className="mb-3 text-[length:var(--font-size-base)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
        {t("profile.changePassword")}
      </h2>

      {changed ? (
        <div className="flex max-w-md flex-col items-start gap-4">
          <Alert tone="success">{t("profile.passwordChanged")}</Alert>
          <Button onClick={endSession}>{t("login.submit")}</Button>
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="flex max-w-md flex-col gap-4">
          <Field label={t("profile.currentPassword")} required>
            <Input
              type="password"
              value={form.current}
              onChange={(e) => setForm({ ...form, current: e.target.value })}
              autoComplete="current-password"
              required
            />
          </Field>

          <Field label={t("profile.newPassword")} required>
            <Input
              type="password"
              value={form.next}
              onChange={(e) => setForm({ ...form, next: e.target.value })}
              autoComplete="new-password"
              required
            />
          </Field>

          <Field label={t("profile.confirmNewPassword")} required>
            <Input
              type="password"
              value={form.confirm}
              onChange={(e) => setForm({ ...form, confirm: e.target.value })}
              autoComplete="new-password"
              required
            />
          </Field>

          {error && <Alert tone="danger">{error}</Alert>}

          <div>
            <Button type="submit" disabled={submitting}>
              {t("profile.changePassword")}
            </Button>
          </div>
        </form>
      )}
    </>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="text-[var(--color-fg-muted)]">{label}</dt>
      <dd className="text-[var(--color-fg)]">{value}</dd>
    </div>
  );
}
