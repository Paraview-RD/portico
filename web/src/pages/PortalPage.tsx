import { useEffect, useState } from "react";

import { authApi, groupsApi, portalApi, userApi } from "../api/endpoints";
import type {
  GroupRef,
  PortalApplication,
  RecoveryChannel,
  UserSession,
} from "../api/types";
import {
  Alert,
  AppIcon,
  Badge,
  Button,
  Card,
  PageHeader,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { useRouter } from "../router";
import { useSession } from "../session";

/**
 * The home screen, for everybody.
 *
 * Until now a person who was not an administrator signed in and landed on
 * their profile — account details, a password form, a list of devices. All
 * true, and none of it the question they arrived with, which is what they can
 * use and whether anything is wrong with their account.
 *
 * The applications shown here are **the tenant's, not the reader's**. This
 * version has two fixed roles and no notion of who may use what, so everyone
 * can sign in to everything registered, and this list is identical for every
 * person. The heading says so. Implying an entitlement that does not exist
 * would be worse than showing nothing: somebody would reasonably conclude
 * that an application missing from a colleague's portal is one they were not
 * granted, when in fact nobody grants anything yet.
 *
 * Layout, because the first version got it wrong: the account facts and the
 * recent sign-ins were two full-width cards, each with its label hard left
 * and its value hard right across a 1440px column. Ten words of content
 * arranged as a metre of empty space reads as a screen that failed to load.
 * They are a pair of columns now, and nothing inside them is justified to
 * both edges — a label belongs next to its value, not at the other end of
 * the room.
 */
export function PortalPage() {
  const t = useT();
  const { user } = useSession();

  if (!user) return null;

  return (
    <>
      <PageHeader
        title={t("portal.greeting", user.displayName)}
        subtitle={t("portal.subtitle")}
      />

      <div className="flex flex-col gap-4">
        <AccountNotices />
        <Applications />

        {/* Two columns from the large breakpoint, one below it. Both cards
            are short, and side by side they read as one summary of "your
            account" rather than as two screens' worth of stacked panels. */}
        <div className="grid gap-4 lg:grid-cols-2">
          <AccountSummary />
          <RecentSignIns />
        </div>
      </div>
    </>
  );
}

/**
 * The two things about an account somebody can act on today, shown only when
 * they are true.
 *
 * A password about to expire, and contact details missing that they would
 * need to recover the account. Both are cheap to know and expensive to
 * discover the other way — at a sign-in screen on a morning they were busy,
 * or at the moment they have already forgotten the password.
 */
function AccountNotices() {
  const t = useT();
  const { user } = useSession();
  const { navigate } = useRouter();

  const [channels, setChannels] = useState<RecoveryChannel[] | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    authApi
      .recoveryChannels(controller.signal)
      .then((r) => setChannels(r.channels))
      // A deployment with no recovery configured answers this fine; a
      // failure here just means no nudge, which is the safe direction.
      .catch(() => setChannels([]));
    return () => controller.abort();
  }, []);

  if (!user) return null;

  const notices: { tone: "warning" | "danger"; text: string }[] = [];

  // --- Password expiry ---------------------------------------------------
  if (user.passwordExpiresAt) {
    const days = Math.ceil(
      (new Date(user.passwordExpiresAt).getTime() - Date.now()) / 86400000,
    );
    if (days <= 0) {
      notices.push({ tone: "danger", text: t("portal.passwordExpired") });
    } else if (days <= 14) {
      // Two weeks, not thirty: a warning that stands for a month becomes
      // part of the furniture, and by the time it matters nobody sees it.
      notices.push({
        tone: "warning",
        text: t("portal.passwordExpiring", String(days)),
      });
    }
  }

  // --- Contact details for recovery --------------------------------------
  //
  // Gated on the deployment actually having the channel. Telling somebody to
  // add an email address so they can recover their password, on a server with
  // no mail configured, promises a capability that does not exist — the same
  // mistake as implying these applications were assigned to them.
  if (channels) {
    const wantsEmail = channels.includes("EMAIL") && !user.email;
    const wantsPhone = channels.includes("SMS") && !user.phone;
    if (wantsEmail || wantsPhone) {
      notices.push({
        tone: "warning",
        text: t(
          wantsEmail && wantsPhone
            ? "portal.contactMissingBoth"
            : wantsEmail
              ? "portal.contactMissingEmail"
              : "portal.contactMissingPhone",
        ),
      });
    }
  }

  if (notices.length === 0) return null;

  return (
    <div className="flex flex-col gap-2">
      {notices.map((notice) => (
        <Alert key={notice.text} tone={notice.tone}>
          <span className="flex flex-wrap items-center justify-between gap-3">
            {notice.text}
            <Button
              size="sm"
              variant="ghost"
              onClick={() => navigate("/profile")}
            >
              {t("portal.goToProfile")}
            </Button>
          </span>
        </Alert>
      ))}
    </div>
  );
}

function Applications() {
  const t = useT();
  const describeError = useErrorMessage();

  const [applications, setApplications] = useState<PortalApplication[] | null>(
    null,
  );
  const [error, setError] = useState("");

  useEffect(() => {
    portalApi
      .applications()
      .then(setApplications)
      .catch((err) => setError(describeError(err)));
  }, [describeError]);

  return (
    <Card title={t("portal.applications")}>
      {/* Said on the screen rather than left to be inferred. */}
      <p className="mb-4 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("portal.applicationsHint")}
      </p>

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      {applications === null ? (
        <p className="text-[var(--color-fg-muted)]">{t("common.loading")}</p>
      ) : applications.length === 0 ? (
        // Not "nothing to show": an empty portal on a deployment with
        // registered applications means none of them has a launch address,
        // and the person who can fix that is reading this.
        <p className="text-[var(--color-fg-muted)]">
          {t("portal.noneOpenable")}
        </p>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {applications.map((application) => (
            <li key={`${application.protocol}:${application.name}`}>
              <a
                href={application.launchUrl}
                // A new tab, and the two attributes that make one safe: the
                // opened page must not be able to reach back through
                // window.opener, and must not learn where its visitor came
                // from.
                target="_blank"
                rel="noopener noreferrer"
                className="flex h-full items-center gap-3 rounded-[var(--radius-sm)] border border-[var(--color-border)] p-3 transition-colors hover:border-[var(--color-primary)] hover:bg-[var(--color-bg-hover)]"
              >
                <AppIcon
                  name={application.name}
                  src={application.logoUri}
                  size={40}
                />
                <span className="flex min-w-0 flex-col">
                  <span className="truncate font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
                    {application.name}
                  </span>
                  <span className="truncate text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {hostOf(application.launchUrl)}
                  </span>
                </span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

/**
 * The host, not the whole address.
 *
 * A tile is scanned, not read: "wiki.example.com" identifies the system at a
 * glance where "https://wiki.example.com/" spends a third of its width on a
 * scheme every one of them shares. The full address is still what the link
 * goes to, and still what the browser shows on hover.
 */
function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    // Not reachable through the console, which validates the address — but a
    // row written straight into the database should degrade to showing what
    // is there rather than to a blank line.
    return url;
  }
}

function AccountSummary() {
  const t = useT();
  const describeError = useErrorMessage();
  const { user } = useSession();
  const { navigate } = useRouter();

  const [groups, setGroups] = useState<GroupRef[] | null>(null);

  useEffect(() => {
    if (!user) return;
    groupsApi
      .forMe()
      // Silent: this is context, and failing to load it must not put an
      // error banner on somebody's home screen.
      .then(setGroups)
      .catch(() => setGroups([]));
  }, [user, describeError]);

  if (!user) return null;

  return (
    <Card title={t("portal.account")}>
      {/* A two-column grid, not a row of justify-between: the value sits
          beside its label at a fixed gap, so a card that is half the page
          wide does not put "zhaoliu" a hand's width from "Sign-in name". */}
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2.5">
        <Row label={t("profile.username")}>{user.username}</Row>
        <Row label={t("profile.organization")}>
          {user.organizationName || "—"}
        </Row>
        <Row label={t("groups.ofUser")}>
          {groups === null ? (
            <span className="text-[var(--color-fg-muted)]">
              {t("common.loading")}
            </span>
          ) : groups.length === 0 ? (
            <span className="text-[var(--color-fg-muted)]">
              {t("groups.none")}
            </span>
          ) : (
            <span className="flex flex-wrap gap-1">
              {groups.map((group) => (
                <Badge key={group.id} tone="neutral">
                  {group.displayName}
                </Badge>
              ))}
            </span>
          )}
        </Row>
        {/* Shown only where it means something. On a tenant that does not
            expire passwords there is no date to show, and inventing "never"
            as a row would make an absent policy look like a configured one. */}
        {user.passwordExpiresAt && (
          <Row label={t("portal.passwordExpires")}>
            {new Date(user.passwordExpiresAt).toLocaleDateString()}
          </Row>
        )}
      </dl>

      <div className="mt-4 flex flex-wrap gap-2 border-t border-[var(--color-border)] pt-4">
        <Button
          size="sm"
          variant="secondary"
          onClick={() => navigate("/profile")}
        >
          {t("profile.changePassword")}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => navigate("/profile")}>
          {t("portal.manageDevices")}
        </Button>
      </div>
    </Card>
  );
}

/**
 * The last few sign-ins, which is the one security question a person can
 * answer about their own account: was that me.
 *
 * Ending a session is left on the profile screen. This is a home screen, and
 * a destructive control on one is a control somebody clicks while looking for
 * something else.
 */
function RecentSignIns() {
  const t = useT();
  const [sessions, setSessions] = useState<UserSession[] | null>(null);

  useEffect(() => {
    userApi
      .ownSessions()
      .then((all) => setSessions(all.slice(0, 4)))
      .catch(() => setSessions([]));
  }, []);

  return (
    <Card title={t("portal.recentSignIns")}>
      {sessions === null ? (
        <p className="text-[var(--color-fg-muted)]">{t("common.loading")}</p>
      ) : sessions.length === 0 ? (
        <p className="text-[var(--color-fg-muted)]">{t("common.empty")}</p>
      ) : (
        <ul className="flex flex-col gap-2.5">
          {sessions.map((session) => (
            <li
              key={session.id}
              className="border-b border-[var(--color-border)] pb-2.5 last:border-0 last:pb-0"
            >
              {/* Address and time on one line, in that order, because the
                  question this answers is "was that me" and the answer is
                  usually decided by the pair together. */}
              <div className="flex flex-wrap items-center gap-2">
                <code className="text-[length:var(--font-size-sm)]">
                  {session.ip || "—"}
                </code>
                <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                  {new Date(session.lastSeenAt).toLocaleString()}
                </span>
                {session.current && (
                  <Badge tone="success">{t("profile.sessionCurrent")}</Badge>
                )}
              </div>
              {/* Verbatim, not prettified into a browser name: a guess that
                  says "Chrome on macOS" when the truth is a script is worse
                  than the raw string for the one thing this answers. */}
              <div className="truncate text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]">
                {session.userAgent || "—"}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <>
      <dt className="text-[var(--color-fg-muted)]">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </>
  );
}
