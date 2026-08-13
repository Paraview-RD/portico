import { useCallback, useEffect, useState } from "react";

import { authApi, myExternalIdentitiesApi, userApi } from "../api/endpoints";
import type {
  ExternalIdentity,
  ExternalSignInOption,
  UserSession,
} from "../api/types";
import {
  Alert,
  Badge,
  Button,
  Card,
  ConfirmDialog,
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

      {/* Four sections in two rows of two, and the pairing is the layout.

          The previous version put three cards in a left column and one
          beside them, which reads as two columns until you notice that the
          right one runs out after the first card. It also broke the rule the
          layout guard checks: the device list ended 150px above where the
          password form began, so that form chained into no row and sat alone
          at 346–826 between two rows reaching 1786. A row stopping short of
          the one above it is the thing that looks like a mistake.

          Explicit grid cells fix it by construction rather than by luck. Two
          cards placed in the same grid row start at the same y whatever they
          contain, so the rows agree about the column no matter how many
          devices are signed in or how long a user agent string runs.

          The pairing is also the meaning. Row one is the account as it
          stands — who you are, and what is currently signed in as you. Row
          two is what you can do about it: change the password, or close the
          account. */}
      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,var(--form-width))_minmax(0,1fr)]">
        <ProfileDetailsForm onSaved={refresh} />

        {/* The device list, which is the one thing on this screen that wants
            width: an address, a user agent string, a timestamp, and a button
            on one row. At 30rem it wrapped to four lines per session. */}
        <SessionsCard />

        {/* Full width, and a row of its own.
            It renders nothing at all where no provider is configured, which
            is almost every deployment — so this is a row that usually does
            not exist rather than a fifth card leaving one of the four
            stranded alone on a half-width row. */}
        <div className="lg:col-span-2">
          <LinkedIdentitiesCard />
        </div>

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

  // The parent renders nothing without a user, so this cannot be reached —
  // but it is what lets the account facts below be read without a `?.` on
  // every one, and `role.undefined` is not a translation key.
  if (!user) return null;

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
      {/* What the server decides, above what you decide, separated by a rule
          rather than by a card of its own.

          The split is still the server's — a username, a role, and an
          organization are not things a user may change about themselves, and
          putting them in the form below would imply otherwise. But that
          distinction needed a divider, not a second surface: as a bare
          untitled card it read as three facts floating above the page with
          nothing saying what they were.

          Laid across rather than down. As a two-column list the labels and
          values huddled at the left edge of a 480px card and left the rest
          empty; three across fills the width and gives each fact the same
          weight. */}
      <dl className="mb-5 grid gap-4 border-b border-[var(--color-border)] pb-5 sm:grid-cols-3">
        <Detail label={t("profile.username")} value={user.username} />
        <div className="min-w-0">
          <dt className="text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]">
            {t("profile.role")}
          </dt>
          <dd className="mt-1">
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

              {/* Bordered, not ghost. Ghost is for an action that sits in a
                  group — a table's action column, a toolbar — where the
                  cluster is what says "these are buttons". This one is alone
                  at the end of a row, and borderless it reads as a caption
                  rather than as the control that ends a session. */}
              <Button
                size="sm"
                variant="secondary"
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

/**
 * The other ways this account can sign in.
 *
 * Linking is the only route an external identity has to an account, unless
 * an administrator has decided to trust a provider's addresses — so this
 * card is not a convenience. It is what a person is told to come here and do
 * after a first external sign-in is refused, and the sentence that refuses
 * them names this screen.
 *
 * The round trip is the same one a sign-in makes, and ends on the same
 * callback address. What makes it a binding rather than a sign-in is
 * remembered server-side when the request departs, so nothing here has to
 * carry it — and nothing coming back could forge it.
 */
function LinkedIdentitiesCard() {
  const t = useT();
  const describeError = useErrorMessage();

  const [identities, setIdentities] = useState<ExternalIdentity[] | null>(null);
  const [options, setOptions] = useState<ExternalSignInOption[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [unlinking, setUnlinking] = useState<ExternalIdentity | null>(null);

  const load = useCallback(async () => {
    try {
      setIdentities(await myExternalIdentitiesApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
    // The buttons this tenant offers. The same public list the sign-in
    // screen draws from, because it is the same question: which providers
    // would work if you clicked one.
    authApi
      .externalOptions()
      .then(setOptions)
      .catch(() => setOptions([]));
  }, [load]);

  async function link(providerId: string) {
    setError("");
    setBusy(providerId);
    try {
      const { authorizationUrl } =
        await myExternalIdentitiesApi.startBinding(providerId);
      window.location.assign(authorizationUrl);
    } catch (err) {
      setBusy("");
      setError(describeError(err));
    }
  }

  async function unlink() {
    if (!unlinking) return;
    setError("");
    try {
      await myExternalIdentitiesApi.unlink(unlinking.id);
      setUnlinking(null);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  // Nothing configured, and nothing linked: a deployment where this concept
  // does not exist should not be told about it.
  if (
    options.length === 0 &&
    (identities === null || identities.length === 0)
  ) {
    return null;
  }

  return (
    <Card title={t("profile.linkedTitle")}>
      <p className="mb-4 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("profile.linkedHelp")}
      </p>

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      {identities !== null && identities.length > 0 && (
        <ul className="mb-4 flex flex-col gap-2">
          {identities.map((identity) => (
            <li
              key={identity.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] px-3 py-2"
            >
              <span className="min-w-0">
                <span className="font-[weight:var(--font-weight-medium)]">
                  {identity.providerName}
                </span>{" "}
                <span className="text-[var(--color-fg-muted)]">
                  {/* The address the provider gave at binding time, which is
                      what makes one of these recognisable. It is not what
                      finds the account — the subject is — so it is shown as
                      a label and never as an identifier. */}
                  {identity.email || identity.subject}
                </span>
              </span>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setUnlinking(identity)}
              >
                {t("profile.unlink")}
              </Button>
            </li>
          ))}
        </ul>
      )}

      {options.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {options.map((option) => (
            <Button
              key={option.id}
              variant="secondary"
              disabled={busy !== ""}
              onClick={() => void link(option.id)}
            >
              {t("profile.link", option.label)}
            </Button>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={unlinking !== null}
        title={t("profile.unlink")}
        message={t("profile.unlinkConfirm", unlinking?.providerName ?? "")}
        destructive
        onConfirm={() => void unlink()}
        onCancel={() => setUnlinking(null)}
      />
    </Card>
  );
}

// One fact as a stacked pair — a small muted label over its value — which is
// what lets three of them sit across a row and line up. The previous version
// returned a fragment so the two halves became cells of the caller's grid;
// that was right for a two-column list and is wrong here, where each pair has
// to be one cell to be one of the three columns.
function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]">
        {label}
      </dt>
      <dd className="mt-1 truncate text-[var(--color-fg)]">{value}</dd>
    </div>
  );
}
