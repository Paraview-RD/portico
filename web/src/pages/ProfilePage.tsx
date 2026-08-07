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
  const { user, endSession, refresh } = useSession();

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

      {/* Read-only above, editable below. The split is the server's: username,
          role, and organization are not things a user may change about
          themselves, and showing them in a form would imply otherwise. */}
      <div className="mb-8 max-w-md rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-bg)] p-4">
        <dl className="flex flex-col gap-3">
          <Detail label={t("profile.username")} value={user.username} />
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

      <ProfileDetailsForm onSaved={refresh} />

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

// ProfileDetailsForm is the editable half (§3.5).
//
// It holds its own state rather than lifting it, because the two forms on
// this screen have unrelated lifecycles: saving details leaves you signed in,
// while changing a password ends the session.
function ProfileDetailsForm({ onSaved }: { onSaved: () => Promise<void> }) {
  const t = useT();
  const { user } = useSession();

  const [form, setForm] = useState({
    displayName: user?.displayName ?? "",
    phone: user?.phone ?? "",
    email: user?.email ?? "",
  });
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSaved(false);
    setSubmitting(true);
    try {
      await userApi.updateOwnProfile(form);
      // Refresh rather than trusting the local copy: the server trims and
      // may reject in ways the form does not model, so what it stored is the
      // only accurate answer.
      await onSaved();
      setSaved(true);
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
      <h2 className="mb-3 text-[length:var(--font-size-base)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
        {t("profile.details")}
      </h2>

      <form
        onSubmit={handleSubmit}
        className="mb-8 flex max-w-md flex-col gap-4"
      >
        <Field label={t("profile.displayName")} required>
          <Input
            value={form.displayName}
            onChange={(e) => setForm({ ...form, displayName: e.target.value })}
            required
          />
        </Field>

        <Field label={t("profile.email")} hint={t("profile.contactHint")}>
          <Input
            type="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            autoComplete="email"
          />
        </Field>

        <Field label={t("profile.phone")} hint={t("profile.contactHint")}>
          <Input
            type="tel"
            value={form.phone}
            onChange={(e) => setForm({ ...form, phone: e.target.value })}
            autoComplete="tel"
          />
        </Field>

        {error && <Alert tone="danger">{error}</Alert>}
        {saved && <Alert tone="success">{t("profile.detailsSaved")}</Alert>}

        <div>
          <Button type="submit" disabled={submitting}>
            {t("common.save")}
          </Button>
        </div>
      </form>
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
