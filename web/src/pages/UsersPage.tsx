import { useCallback, useEffect, useState } from "react";

import {
  UNASSIGNED_ORGANIZATION,
  groupsApi,
  organizationApi,
  userApi,
} from "../api/endpoints";
import type {
  BulkResult,
  GroupRef,
  Organization,
  Role,
  Status,
  User,
  UserProfile,
} from "../api/types";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  EmptyRow,
  LoadingRow,
  Field,
  Input,
  Modal,
  PageHeader,
  Pagination,
  Select,
  Table,
  Td,
  Th,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { ImportDialog } from "./ImportDialog";
// Borrowed rather than reimplemented. Two functions turning a flat list into
// a chart would drift, and the subtle half — a row whose parent is not in the
// list still has to appear — is the half that would be got wrong the second
// time. It is exported, and OrganizationsPage.test.ts holds it.
import { arrangeAsTree } from "./OrganizationsPage";

const PAGE_SIZE = 20;

/**
 * The organization filter, as the chart rather than as a list of names.
 *
 * A flat dropdown could name every organization and could not say how any of
 * them relate, which is most of what somebody knows about their own company:
 * "everybody in Engineering" is a question about a branch, and a list of
 * names cannot express a branch. Picking a node here selects it and
 * everything under it — the server walks the subtree — so the shape on
 * screen and the shape of the answer are the same shape.
 *
 * Always expanded. Collapsing would be state to keep, and a filter somebody
 * has to open three times before finding the department they are standing in
 * is worse than one they scroll. The server bounds the chart at ten deep.
 *
 * Deliberately no member counts. The list endpoint reports the members filed
 * directly against each organization, and selecting one now answers with its
 * whole branch — so a count beside the name would disagree with the number
 * of rows that appear when you click it. A wrong number is worse than none.
 */
function OrganizationTree({
  organizations,
  value,
  onChange,
}: {
  organizations: Organization[];
  value: string;
  onChange: (organizationId: string) => void;
}) {
  const t = useT();
  const rows = arrangeAsTree(organizations);

  function item(key: string, id: string, depth: number, label: string) {
    const selected = value === id;
    return (
      <li key={key}>
        <button
          type="button"
          aria-current={selected ? "true" : undefined}
          onClick={() => onChange(id)}
          style={{ paddingLeft: `${0.5 + depth * 1.25}rem` }}
          className={`w-full truncate rounded-[var(--radius-sm)] py-1.5 pr-2 text-left text-[length:var(--font-size-sm)] ${
            selected
              ? "bg-[var(--color-primary-soft)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]"
              : "text-[var(--color-fg-muted)] hover:bg-[var(--color-bg-soft)] hover:text-[var(--color-fg)]"
          }`}
        >
          {depth > 0 && (
            <span
              aria-hidden="true"
              className="mr-1.5 text-[var(--color-fg-subtle)]"
            >
              └
            </span>
          )}
          {label}
        </button>
      </li>
    );
  }

  return (
    <nav
      aria-label={t("users.filterOrganization")}
      className="rounded-[var(--radius-sm)] border border-[var(--color-border)] bg-[var(--color-bg)] p-2"
    >
      <div className="px-2 pt-1 pb-2">
        <div className="text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)]">
          {t("users.filterOrganization")}
        </div>
        {/* The one thing about this control that cannot be seen by looking
            at it: a parent means the parent and everything under it. */}
        <div className="text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]">
          {t("users.filterOrganizationHint")}
        </div>
      </div>
      {/* Bounded and scrollable rather than as tall as the chart. On a narrow
          screen this sits above the table, and a deep chart would otherwise
          push every account below the fold. */}
      <ul className="max-h-72 overflow-y-auto lg:max-h-[32rem]">
        {item("all", "", 0, t("common.all"))}
        {rows.map(({ org, depth }) => item(org.id, org.id, depth, org.name))}
        {/* Kept whatever the chart looks like: the accounts nobody has filed
            are the ones somebody comes here to find, and they are in no
            branch, so there is nowhere else this could sit. */}
        {item(
          "unassigned",
          UNASSIGNED_ORGANIZATION,
          0,
          t("users.filterNoOrganization"),
        )}
      </ul>
    </nav>
  );
}

export function UsersPage() {
  const t = useT();

  const describeError = useErrorMessage();
  const [users, setUsers] = useState<User[]>([]);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(true);

  const [keyword, setKeyword] = useState("");
  const [roleFilter, setRoleFilter] = useState<Role | "">("");
  const [statusFilter, setStatusFilter] = useState<Status | "">("");
  const [organizationFilter, setOrganizationFilter] = useState("");
  const [exporting, setExporting] = useState(false);
  // The accounts ticked for a bulk action, by id. Cleared whenever the list
  // reloads: a selection that survived a filter change would act on people
  // who are no longer on screen.
  const [selected, setSelected] = useState<string[]>([]);
  const [bulkResult, setBulkResult] = useState<BulkResult | null>(null);

  const [editing, setEditing] = useState<User | null>(null);
  const [creating, setCreating] = useState(false);
  const [importing, setImporting] = useState(false);
  const [resettingFor, setResettingFor] = useState<User | null>(null);
  const [confirming, setConfirming] = useState<{
    user: User;
    enable: boolean;
  } | null>(null);

  // Runs a bulk call and keeps its per-account report on screen. The call
  // succeeds even when some accounts were refused, so there is nothing to
  // catch in the ordinary case — the outcomes are the answer.
  async function runBulk(action: () => Promise<BulkResult>) {
    setError("");
    setBulkResult(null);
    try {
      setBulkResult(await action());
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await userApi.list({
        page,
        pageSize: PAGE_SIZE,
        keyword,
        role: roleFilter,
        status: statusFilter,
        organizationId: organizationFilter,
      });
      setUsers(result.items);
      setTotal(result.total);
      // A selection that survived a filter change would act on people who
      // are no longer on screen — exactly the mistake a bulk control makes
      // expensive.
      setSelected([]);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setLoading(false);
    }
  }, [
    page,
    keyword,
    roleFilter,
    statusFilter,
    organizationFilter,
    describeError,
  ]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    organizationApi
      .list()
      .then(setOrganizations)
      .catch(() => setOrganizations([]));
  }, []);

  async function unlock(user: User) {
    setError("");
    try {
      await userApi.unlock(user.id);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function toggleStatus(user: User, enable: boolean) {
    setConfirming(null);
    setError("");
    try {
      if (enable) {
        await userApi.enable(user.id);
      } else {
        await userApi.disable(user.id);
      }
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  return (
    <>
      <PageHeader
        title={t("users.title")}
        subtitle={t("users.subtitle")}
        actions={
          <>
            {/* Exports what is on screen, not everything: the same filters
                the list is using. "Export what I am looking at" is what
                somebody means, and a button that quietly ignored the filters
                would hand them a file with the wrong people in it. */}
            <Button
              variant="secondary"
              disabled={exporting}
              onClick={() => {
                setExporting(true);
                void userApi
                  .exportUsers({
                    keyword,
                    role: roleFilter,
                    status: statusFilter,
                    organizationId: organizationFilter,
                  })
                  .catch((err) => setError(describeError(err)))
                  .finally(() => setExporting(false));
              }}
            >
              {exporting ? t("users.exporting") : t("users.export")}
            </Button>
            <Button variant="secondary" onClick={() => setImporting(true)}>
              {t("users.import")}
            </Button>
            <Button onClick={() => setCreating(true)}>
              {t("users.create")}
            </Button>
          </>
        }
      />

      {/* Two columns inside the page's own column, rather than a second
          sidebar beside the navigation. The chart is a filter for this
          screen, not a place of its own — and every screen is laid out in
          the same column, which web/e2e/layout.spec.ts holds to. Widening
          one screen for one control would either break that or make it an
          exception, and the table has 1440px to give 240 of. */}
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start">
        <aside className="lg:w-60 lg:shrink-0">
          <OrganizationTree
            organizations={organizations}
            value={organizationFilter}
            onChange={(organizationId) => {
              setOrganizationFilter(organizationId);
              setPage(1);
            }}
          />
        </aside>

        <div className="min-w-0 flex-1">
          <div className="mb-4 flex flex-wrap items-end gap-3">
            <div className="w-64">
              <Input
                placeholder={t("users.searchPlaceholder")}
                value={keyword}
                onChange={(e) => {
                  setKeyword(e.target.value);
                  setPage(1);
                }}
              />
            </div>
            <div className="w-44">
              <Select
                value={roleFilter}
                onChange={(e) => {
                  setRoleFilter(e.target.value as Role | "");
                  setPage(1);
                }}
              >
                <option value="">
                  {t("users.filterRole")}: {t("common.all")}
                </option>
                <option value="SUPER_ADMIN">{t("role.SUPER_ADMIN")}</option>
                <option value="USER">{t("role.USER")}</option>
              </Select>
            </div>
            <div className="w-44">
              <Select
                value={statusFilter}
                onChange={(e) => {
                  setStatusFilter(e.target.value as Status | "");
                  setPage(1);
                }}
              >
                <option value="">
                  {t("users.filterStatus")}: {t("common.all")}
                </option>
                <option value="ACTIVE">{t("status.ACTIVE")}</option>
                <option value="DISABLED">{t("status.DISABLED")}</option>
              </Select>
            </div>
          </div>

          {error && (
            <div className="mb-4">
              <Alert tone="danger">{error}</Alert>
            </div>
          )}
          {notice && (
            <div className="mb-4">
              <Alert tone="success">{notice}</Alert>
            </div>
          )}

          {selected.length > 0 && (
            <div className="mb-3 flex flex-wrap items-center gap-3 rounded-[var(--radius-sm)] border border-[var(--color-border)] bg-[var(--color-bg)] p-3">
              <span className="font-[weight:var(--font-weight-medium)]">
                {t("users.selectedCount", String(selected.length))}
              </span>
              <Button
                size="sm"
                variant="secondary"
                onClick={() =>
                  void runBulk(() => userApi.bulkSetStatus(selected, "ACTIVE"))
                }
              >
                {t("common.enable")}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onClick={() =>
                  void runBulk(() =>
                    userApi.bulkSetStatus(selected, "DISABLED"),
                  )
                }
              >
                {t("common.disable")}
              </Button>
              <div className="w-56">
                <Select
                  value=""
                  onChange={(e) => {
                    const organizationId = e.target.value;
                    if (organizationId === "") return;
                    void runBulk(() =>
                      userApi.bulkSetOrganization(
                        selected,
                        organizationId === "__none__" ? "" : organizationId,
                      ),
                    );
                  }}
                >
                  <option value="">{t("users.bulkMoveTo")}</option>
                  <option value="__none__">
                    {t("users.bulkNoOrganization")}
                  </option>
                  {organizations.map((org) => (
                    <option key={org.id} value={org.id}>
                      {org.name}
                    </option>
                  ))}
                </Select>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setSelected([])}>
                {t("common.cancel")}
              </Button>
            </div>
          )}

          {bulkResult && (
            <div className="mb-3">
              <Alert tone={bulkResult.failed === 0 ? "success" : "warning"}>
                <div>
                  {t(
                    "users.bulkSummary",
                    String(bulkResult.succeeded),
                    String(bulkResult.failed),
                  )}
                </div>
                {/* Which ones, not just how many. Somebody who selected forty
                people and had one refused needs to know it was the last
                administrator, and which account that was. */}
                {bulkResult.outcomes
                  .filter((outcome) => outcome.code)
                  .map((outcome) => (
                    <div
                      key={outcome.userId}
                      className="mt-1 text-[length:var(--font-size-sm)]"
                    >
                      {users.find((u) => u.id === outcome.userId)?.username ??
                        outcome.userId}
                      : {outcome.message}
                    </div>
                  ))}
              </Alert>
            </div>
          )}

          <Table>
            <thead>
              <tr>
                <Th>
                  {/* Selects what is on this page, not every account matching the
                  filter. A control that silently reached beyond what somebody
                  can see is how a bulk disable becomes an incident. */}
                  <input
                    type="checkbox"
                    aria-label={t("users.selectAll")}
                    checked={
                      users.length > 0 && selected.length === users.length
                    }
                    onChange={(e) =>
                      setSelected(
                        e.target.checked ? users.map((u) => u.id) : [],
                      )
                    }
                  />
                </Th>
                <Th>{t("users.colUsername")}</Th>
                <Th>{t("users.colDisplayName")}</Th>
                <Th>{t("users.colRole")}</Th>
                <Th>{t("users.colOrganization")}</Th>
                <Th>{t("users.colStatus")}</Th>
                <Th>{t("common.actions")}</Th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <LoadingRow colSpan={7} />
              ) : users.length === 0 ? (
                <EmptyRow colSpan={7} />
              ) : (
                users.map((user) => (
                  <tr key={user.id}>
                    <Td>
                      <input
                        type="checkbox"
                        aria-label={user.username}
                        checked={selected.includes(user.id)}
                        onChange={(e) =>
                          setSelected((current) =>
                            e.target.checked
                              ? [...current, user.id]
                              : current.filter((id) => id !== user.id),
                          )
                        }
                      />
                    </Td>
                    <Td>
                      <div className="flex flex-wrap items-center gap-1">
                        {user.username}
                        {/* Only the directory-managed case gets a mark. The
                        other three sources say how the account was born,
                        which is history; this one says who owns it now,
                        which changes what an edit here will do. A column
                        showing all four would give them equal weight. */}
                        {(user.source === "SCIM" || user.source === "LDAP") && (
                          <Badge tone="warning">
                            {t(`source.${user.source}`)}
                          </Badge>
                        )}
                      </div>
                    </Td>
                    <Td>{user.displayName}</Td>
                    <Td>
                      <Badge
                        tone={
                          user.role === "SUPER_ADMIN" ? "warning" : "neutral"
                        }
                      >
                        {t(`role.${user.role}`)}
                      </Badge>
                    </Td>
                    <Td>
                      {user.organizationName || t("users.noOrganization")}
                    </Td>
                    <Td>
                      <div className="flex flex-wrap items-center gap-1">
                        <Badge
                          tone={user.status === "ACTIVE" ? "success" : "danger"}
                        >
                          {t(`status.${user.status}`)}
                        </Badge>
                        {/* Shown next to the status rather than instead of it:
                        locked and disabled are different situations with
                        different remedies, and an account can be both. */}
                        {user.lockedUntil && (
                          <Badge tone="warning">
                            {t(
                              "users.lockedUntil",
                              new Date(user.lockedUntil).toLocaleString(),
                            )}
                          </Badge>
                        )}
                      </div>
                    </Td>
                    <Td>
                      <div className="flex gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setEditing(user)}
                        >
                          {t("common.edit")}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setResettingFor(user)}
                        >
                          {t("users.resetPassword")}
                        </Button>
                        {user.lockedUntil && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => void unlock(user)}
                          >
                            {t("users.unlock")}
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() =>
                            setConfirming({
                              user,
                              enable: user.status !== "ACTIVE",
                            })
                          }
                        >
                          {user.status === "ACTIVE"
                            ? t("common.disable")
                            : t("common.enable")}
                        </Button>
                      </div>
                    </Td>
                  </tr>
                ))
              )}
            </tbody>
          </Table>

          <Pagination
            page={page}
            pageSize={PAGE_SIZE}
            total={total}
            onChange={setPage}
          />
        </div>
      </div>

      <UserFormDialog
        open={creating || editing !== null}
        user={editing}
        organizations={organizations}
        onClose={() => {
          setCreating(false);
          setEditing(null);
        }}
        onSaved={() => {
          setCreating(false);
          setEditing(null);
          void load();
        }}
      />

      <ResetPasswordDialog
        user={resettingFor}
        onClose={() => setResettingFor(null)}
        onDone={() => {
          setResettingFor(null);
          setNotice(t("users.passwordResetDone"));
        }}
      />

      <ImportDialog
        open={importing}
        onClose={() => setImporting(false)}
        onImported={() => void load()}
      />

      <ConfirmDialog
        open={confirming !== null}
        title={confirming?.enable ? t("common.enable") : t("common.disable")}
        message={
          confirming
            ? confirming.enable
              ? t("users.confirmEnable", confirming.user.username)
              : t("users.confirmDisable", confirming.user.username)
            : ""
        }
        destructive={confirming?.enable === false}
        onConfirm={() =>
          confirming && void toggleStatus(confirming.user, confirming.enable)
        }
        onCancel={() => setConfirming(null)}
      />
    </>
  );
}

// Every attribute empty, which is what an account with none looks like.
// Spread over whatever the server returned rather than trusting the response
// to carry all of them, so a field added on the server and not yet here does
// not make this object undefined-valued.
const emptyProfile: UserProfile = {
  nameFormatted: "",
  familyName: "",
  givenName: "",
  middleName: "",
  honorificPrefix: "",
  honorificSuffix: "",
  nickName: "",
  profileUrl: "",
  photoUrl: "",
  title: "",
  userType: "",
  preferredLanguage: "",
  locale: "",
  timezone: "",
  addressFormatted: "",
  streetAddress: "",
  locality: "",
  region: "",
  postalCode: "",
  country: "",
  employeeNumber: "",
  costCenter: "",
  department: "",
  managerId: "",
  managerName: "",
};

function UserFormDialog({
  open,
  user,
  organizations,
  onClose,
  onSaved,
}: {
  open: boolean;
  user: User | null;
  organizations: Organization[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const isEdit = user !== null;

  const [form, setForm] = useState({
    username: "",
    displayName: "",
    password: "",
    phone: "",
    email: "",
    role: "USER" as Role,
    organizationId: "",
  });
  // The descriptive attributes, held apart from the fields above because
  // they go to a different endpoint — the one that cannot change a role or a
  // status. Keeping them in one object here and splitting them on submit
  // would put the two back together in the one place it matters.
  const [profile, setProfile] = useState<UserProfile>(emptyProfile);
  const [showProfile, setShowProfile] = useState(false);

  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [groups, setGroups] = useState<GroupRef[] | null>(null);

  // Reset the form whenever the dialog opens, so a previous edit does not
  // leak into the next one.
  useEffect(() => {
    if (!open) return;
    setError("");
    setForm({
      username: user?.username ?? "",
      displayName: user?.displayName ?? "",
      password: "",
      phone: user?.phone ?? "",
      email: user?.email ?? "",
      role: user?.role ?? "USER",
      organizationId: user?.organizationId ?? "",
    });
    setProfile({ ...emptyProfile, ...(user?.profile ?? {}) });
    // Collapsed by default. Most edits here are a display name or an
    // organization; twenty-four fields open every time would bury them.
    setShowProfile(false);
  }, [open, user]);

  // Read-only, and fetched rather than carried on the user: membership is
  // edited from the groups screen, because that is where somebody deciding
  // who is in a group is looking. Shown here because the opposite question —
  // what is this person in — is asked while looking at the person, and until
  // now the only way to answer it was to open every group in turn.
  useEffect(() => {
    if (!open || !user) {
      setGroups(null);
      return;
    }
    let current = true;
    groupsApi
      .forUser(user.id)
      .then((found) => {
        if (current) setGroups(found);
      })
      // Deliberately silent. This is context beside the fields being
      // edited, and failing to load it must not stop somebody changing a
      // display name — the empty state reads the same as "no groups", which
      // is the one cost, and it is smaller than a blocked edit.
      .catch(() => {
        if (current) setGroups([]);
      });
    return () => {
      current = false;
    };
  }, [open, user]);

  function set(field: keyof typeof form, value: string) {
    setForm((previous) => ({ ...previous, [field]: value }));
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      if (isEdit && user) {
        await userApi.update(user.id, {
          displayName: form.displayName,
          phone: form.phone,
          email: form.email,
          role: form.role,
          organizationId: form.organizationId,
        });
        // Second, and only when it was opened. An account whose profile
        // section was never expanded should not have its attributes
        // rewritten with what the form happened to be holding.
        if (showProfile) {
          await userApi.setProfile(user.id, profile);
        }
      } else {
        const created = await userApi.create({
          username: form.username,
          displayName: form.displayName,
          password: form.password,
          phone: form.phone,
          email: form.email,
          role: form.role,
          organizationId: form.organizationId,
        });
        if (showProfile) {
          await userApi.setProfile(created.id, profile);
        }
      }
      onSaved();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal
      open={open}
      title={isEdit ? t("users.editTitle") : t("users.createTitle")}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button form="user-form" type="submit" disabled={submitting}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form
        id="user-form"
        onSubmit={handleSubmit}
        className="flex flex-col gap-4"
      >
        {/* Where the harm actually happens. The badge in the list warns
            before the click; this warns while the edit is being typed,
            which is the last moment it is still free to abandon. */}
        {isEdit && user?.source === "SCIM" && (
          <Alert tone="warning">{t("users.directoryManaged")}</Alert>
        )}

        {!isEdit && (
          <Field label={t("login.username")} required>
            <Input
              value={form.username}
              onChange={(e) => set("username", e.target.value)}
              required
            />
          </Field>
        )}

        <Field label={t("register.displayName")} required>
          <Input
            value={form.displayName}
            onChange={(e) => set("displayName", e.target.value)}
            required
          />
        </Field>

        {!isEdit && (
          <Field label={t("login.password")} required>
            <Input
              type="password"
              value={form.password}
              onChange={(e) => set("password", e.target.value)}
              autoComplete="new-password"
              required
            />
          </Field>
        )}

        <Field label={t("users.colRole")} required>
          <Select
            value={form.role}
            onChange={(e) => set("role", e.target.value)}
          >
            <option value="USER">{t("role.USER")}</option>
            <option value="SUPER_ADMIN">{t("role.SUPER_ADMIN")}</option>
          </Select>
        </Field>

        <Field label={t("users.colOrganization")}>
          <Select
            value={form.organizationId}
            onChange={(e) => set("organizationId", e.target.value)}
          >
            <option value="">{t("users.noOrganization")}</option>
            {organizations
              .filter(
                (org) =>
                  org.status === "ACTIVE" || org.id === form.organizationId,
              )
              .map((org) => (
                <option key={org.id} value={org.id}>
                  {org.name}
                </option>
              ))}
          </Select>
        </Field>

        <Field label={`${t("register.phone")} (${t("common.optional")})`}>
          <Input
            value={form.phone}
            onChange={(e) => set("phone", e.target.value)}
          />
        </Field>

        <Field label={`${t("register.email")} (${t("common.optional")})`}>
          <Input
            type="email"
            value={form.email}
            onChange={(e) => set("email", e.target.value)}
          />
        </Field>

        {/* The descriptive attributes, behind a disclosure.
            Collapsed because most edits here are a display name or an
            organization, and twenty-four fields open every time would bury
            them. Expanded, it writes through the endpoint that cannot reach
            a role — the same split the server draws. */}
        <div className="rounded-[var(--radius-sm)] border border-[var(--color-border)]">
          <button
            type="button"
            aria-expanded={showProfile}
            onClick={() => setShowProfile(!showProfile)}
            className="flex w-full items-center justify-between px-4 py-3 text-left"
          >
            <span className="font-[weight:var(--font-weight-medium)]">
              {t("users.profileSection")}
            </span>
            <span className="text-[var(--color-fg-muted)]">
              {showProfile ? "−" : "+"}
            </span>
          </button>

          {showProfile && (
            <div className="flex flex-col gap-4 border-t border-[var(--color-border)] p-4">
              <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                {t("users.profileHint")}
              </p>

              <div className="grid gap-4 sm:grid-cols-2">
                {(
                  [
                    ["givenName", "users.attr.givenName"],
                    ["familyName", "users.attr.familyName"],
                    ["title", "users.attr.title"],
                    ["department", "users.attr.department"],
                    ["employeeNumber", "users.attr.employeeNumber"],
                    ["costCenter", "users.attr.costCenter"],
                    ["userType", "users.attr.userType"],
                    ["nickName", "users.attr.nickName"],
                    ["preferredLanguage", "users.attr.preferredLanguage"],
                    ["timezone", "users.attr.timezone"],
                    ["locality", "users.attr.locality"],
                    ["country", "users.attr.country"],
                  ] as const
                ).map(([field, label]) => (
                  <Field key={field} label={t(label)}>
                    <Input
                      value={profile[field]}
                      onChange={(e) =>
                        setProfile((current) => ({
                          ...current,
                          [field]: e.target.value,
                        }))
                      }
                    />
                  </Field>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Not a Field: there is no control here and nothing to submit.
            Wrapping it in one would give it a label pointing at an input
            that does not exist, which is worse for a screen reader than
            the plain heading it actually is. */}
        {isEdit && groups !== null && (
          <div className="flex flex-col gap-1.5">
            <div className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
              {t("groups.ofUser")}
            </div>
            {groups.length === 0 ? (
              <div className="text-[length:var(--font-size-sm)]">
                {t("groups.none")}
              </div>
            ) : (
              <div className="flex flex-wrap gap-1">
                {groups.map((group) => (
                  <Badge key={group.id} tone="neutral">
                    {group.displayName}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        )}

        {error && <Alert tone="danger">{error}</Alert>}
      </form>
    </Modal>
  );
}

function ResetPasswordDialog({
  user,
  onClose,
  onDone,
}: {
  user: User | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setPassword("");
    setError("");
  }, [user]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (!user) return;
    setError("");
    setSubmitting(true);
    try {
      await userApi.resetPassword(user.id, password);
      onDone();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal
      open={user !== null}
      title={t("users.resetPasswordTitle", user?.username ?? "")}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button
            form="reset-password-form"
            type="submit"
            disabled={submitting}
          >
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form
        id="reset-password-form"
        onSubmit={handleSubmit}
        className="flex flex-col gap-4"
      >
        <Field label={t("users.newPassword")} required>
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
          />
        </Field>
        {error && <Alert tone="danger">{error}</Alert>}
      </form>
    </Modal>
  );
}
