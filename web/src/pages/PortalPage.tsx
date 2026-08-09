import { useEffect, useState } from "react";

import { groupsApi, portalApi } from "../api/endpoints";
import type { GroupRef, PortalApplication, UserSession } from "../api/types";
import { userApi } from "../api/endpoints";
import { Alert, Badge, Card, PageHeader } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
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
        <Applications />
        <AccountSummary />
        <RecentSignIns />
      </div>
    </>
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
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
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
                className="flex h-full flex-col gap-1 rounded-[var(--radius-sm)] border border-[var(--color-border)] p-4 transition-colors hover:border-[var(--color-primary)] hover:bg-[var(--color-bg-hover)]"
              >
                <span className="font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
                  {application.name}
                </span>
                <span className="truncate text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                  {application.launchUrl}
                </span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function AccountSummary() {
  const t = useT();
  const describeError = useErrorMessage();
  const { user } = useSession();

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
      <dl className="flex flex-col gap-3">
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
            <span className="flex flex-wrap justify-end gap-1">
              {groups.map((group) => (
                <Badge key={group.id} tone="neutral">
                  {group.displayName}
                </Badge>
              ))}
            </span>
          )}
        </Row>
      </dl>
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
      .then((all) => setSessions(all.slice(0, 3)))
      .catch(() => setSessions([]));
  }, []);

  return (
    <Card title={t("portal.recentSignIns")}>
      {sessions === null ? (
        <p className="text-[var(--color-fg-muted)]">{t("common.loading")}</p>
      ) : sessions.length === 0 ? (
        <p className="text-[var(--color-fg-muted)]">{t("common.empty")}</p>
      ) : (
        <ul className="flex flex-col gap-3">
          {sessions.map((session) => (
            <li
              key={session.id}
              className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--color-border)] pb-3 last:border-0"
            >
              <div className="min-w-0">
                <div className="flex items-center gap-2 truncate text-[length:var(--font-size-sm)]">
                  {session.ip || "—"}
                  {session.current && (
                    <Badge tone="success">{t("profile.sessionCurrent")}</Badge>
                  )}
                </div>
                <div className="truncate text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                  {session.userAgent || "—"}
                </div>
              </div>
              <div className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                {new Date(session.lastSeenAt).toLocaleString()}
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
    <div className="flex items-start justify-between gap-4">
      <dt className="text-[var(--color-fg-muted)]">{label}</dt>
      <dd className="min-w-0 text-right">{children}</dd>
    </div>
  );
}
