import { useCallback, useEffect, useState } from "react";

import { authApi, userApi } from "../api/endpoints";
import type { UserSession } from "../api/types";
import {
  Alert,
  Badge,
  Button,
  Card,
  Field,
  Input,
  PageHeader,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { useSession } from "../session";

export function ProfilePage() {
  const t = useT();
  const describeError = useErrorMessage();
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
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      <PageHeader title={t("profile.title")} subtitle={t("profile.subtitle")} />

      {/* Two columns on a wide display: the forms at the width a form wants,
          and the device list beside them taking whatever is left.

          The previous version stacked four cards all clamped to 30rem, which
          on a 1440px column left two thirds of the screen empty and read as
          a page that had failed rather than as a deliberate measure. The
          fix is not to widen the forms — an input is no more usable for
          being a metre across — it is to put something beside them. */}
      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,var(--form-width))_minmax(0,1fr)]">
        <div className="flex flex-col gap-4">
          {/* Read-only above, editable below. The split is the server's:
              username, role, and organization are not things a user may
              change about themselves, and showing them in a form would
              imply otherwise. */}
          <Card>
            <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-3">
              <Detail label={t("profile.username")} value={user.username} />
              <dt className="text-[var(--color-fg-muted)]">
                {t("profile.role")}
              </dt>
              <dd>
                <Badge
                  tone={user.role === "SUPER_ADMIN" ? "warning" : "neutral"}
                >
                  {t(`role.${user.role}`)}
                </Badge>
              </dd>
              <Detail
                label={t("profile.organization")}
                value={user.organizationName || "—"}
              />
            </dl>
          </Card>

          <ProfileDetailsForm onSaved={refresh} />

          <Card title={t("profile.changePassword")}>
            {changed ? (
              <div className="flex flex-col items-start gap-4">
                <Alert tone="success">{t("profile.passwordChanged")}</Alert>
                <Button onClick={endSession}>{t("login.submit")}</Button>
              </div>
            ) : (
              <form onSubmit={handleSubmit} className="flex flex-col gap-4">
                <Field label={t("profile.currentPassword")} required>
                  <Input
                    type="password"
                    value={form.current}
                    onChange={(e) =>
                      setForm({ ...form, current: e.target.value })
                    }
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
                    onChange={(e) =>
                      setForm({ ...form, confirm: e.target.value })
                    }
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
          </Card>
        </div>

        {/* The device list, which is the one thing on this screen that wants
            width: an address, a user agent string, a timestamp, and a button
            on one row. At 30rem it wrapped to four lines per session. */}
        <SessionsCard />
      </div>

      {/* Below both columns rather than at the foot of one of them.
          Underneath the right column it was a card floating in half the
          width with empty space beside it — which the layout guard called
          ragged, correctly. Full width also reads as what it is: a final
          section, separate from the account maintenance above it. */}
      <div className="mt-4">
        <CloseAccountCard />
      </div>
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
  const describeError = useErrorMessage();
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
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Card title={t("profile.details")}>
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
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
    </Card>
  );
}

/**
 * What is signed in as you, and a way to end any of it.
 *
 * The reason this screen exists is the question "is that me?" — somebody
 * looking at an address or a browser they do not recognize. So the address
 * and the user agent are shown verbatim rather than prettified into a
 * browser name: a guess that says "Chrome on Windows" when the truth is a
 * script is worse than the raw string for the one thing this answers.
 */
function SessionsCard() {
  const t = useT();
  const describeError = useErrorMessage();
  const { signOut } = useSession();

  const [sessions, setSessions] = useState<UserSession[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setSessions(await userApi.ownSessions());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  async function revoke(session: UserSession) {
    setError("");
    setBusy(true);
    try {
      await userApi.revokeOwnSession(session.id);
      // Ending your own session means signing out, and the token in hand is
      // already dead — so go through signOut rather than reloading a list
      // every request for which would now fail.
      if (session.current) {
        await signOut();
        return;
      }
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setBusy(false);
    }
  }

  async function endEverywhere() {
    setError("");
    setBusy(true);
    try {
      await authApi.logoutEverywhere();
      await signOut();
    } catch (err) {
      setError(describeError(err));
      setBusy(false);
    }
  }

  return (
    <Card title={t("profile.sessionsTitle")}>
      <p className="mb-4 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("profile.sessionsHelp")}
      </p>

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      {sessions === null ? (
        <p className="text-[var(--color-fg-muted)]">{t("common.loading")}</p>
      ) : (
        <ul className="flex flex-col gap-3">
          {sessions.map((session) => (
            <li
              key={session.id}
              className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--color-border)] pb-3 last:border-0"
            >
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <code className="text-[length:var(--font-size-sm)]">
                    {session.ip || t("profile.sessionNoAddress")}
                  </code>
                  {session.current && (
                    <Badge tone="success">{t("profile.sessionCurrent")}</Badge>
                  )}
                </div>
                <p className="mt-0.5 break-all text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]">
                  {session.userAgent || t("profile.sessionNoAgent")}
                </p>
                <p className="mt-0.5 text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]">
                  {t(
                    "profile.sessionLastSeen",
                    new Date(session.lastSeenAt).toLocaleString(),
                  )}
                </p>
              </div>

              <Button
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={() => void revoke(session)}
              >
                {session.current
                  ? t("profile.sessionEndMine")
                  : t("profile.sessionEnd")}
              </Button>
            </li>
          ))}
        </ul>
      )}

      <div className="mt-4">
        <Button variant="secondary" disabled={busy} onClick={endEverywhere}>
          {t("profile.sessionEndAll")}
        </Button>
      </div>
    </Card>
  );
}

// A label and its value as two cells of the surrounding grid, rather than as
// a row that pushes them to opposite edges. The fragment is what lets the
// grid see both: wrapping them in a div would make the pair one cell and put
// the justification problem straight back.
/**
 * Closing your own account.
 *
 * Last on the screen and behind a confirmation, because it is the one
 * destructive thing here — and unlike the buttons above it, the person doing
 * it cannot undo it themselves.
 *
 * The copy says what actually happens rather than being vague to seem gentle:
 * the account stops signing in, everything signed in as it ends now, and an
 * administrator can put it back. "Delete" would be the wrong word and is
 * avoided; nothing is deleted.
 */
function CloseAccountCard() {
  const t = useT();
  const describeError = useErrorMessage();
  const { endSession } = useSession();

  const [confirming, setConfirming] = useState(false);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function close(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setBusy(true);
    try {
      await userApi.closeOwnAccount(password);
      // The token is already dead; going through endSession is what stops
      // the next render making a request that fails without explanation.
      endSession();
    } catch (err) {
      setError(describeError(err));
      setBusy(false);
    }
  }

  return (
    <Card title={t("profile.closeAccount")}>
      <p className="mb-4 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("profile.closeAccountHelp")}
      </p>

      {confirming ? (
        <form onSubmit={close} className="flex flex-col gap-4">
          <Alert tone="warning">{t("profile.closeAccountConfirm")}</Alert>
          <Field label={t("profile.closeAccountPassword")} required>
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              required
            />
          </Field>
          {error && <Alert tone="danger">{error}</Alert>}
          <div className="flex gap-2">
            <Button type="submit" variant="danger" disabled={busy}>
              {t("profile.closeAccountAction")}
            </Button>
            <Button
              variant="secondary"
              onClick={() => {
                setConfirming(false);
                setPassword("");
                setError("");
              }}
            >
              {t("common.cancel")}
            </Button>
          </div>
        </form>
      ) : (
        <Button variant="secondary" onClick={() => setConfirming(true)}>
          {t("profile.closeAccount")}
        </Button>
      )}
    </Card>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt className="text-[var(--color-fg-muted)]">{label}</dt>
      <dd className="min-w-0 text-[var(--color-fg)]">{value}</dd>
    </>
  );
}
