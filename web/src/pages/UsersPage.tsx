import { useCallback, useEffect, useState } from "react";

import { groupsApi, organizationApi, userApi } from "../api/endpoints";
import type { GroupRef, Organization, Role, Status, User } from "../api/types";
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

const PAGE_SIZE = 20;

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

  const [editing, setEditing] = useState<User | null>(null);
  const [creating, setCreating] = useState(false);
  const [importing, setImporting] = useState(false);
  const [resettingFor, setResettingFor] = useState<User | null>(null);
  const [confirming, setConfirming] = useState<{
    user: User;
    enable: boolean;
  } | null>(null);

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
            <Button variant="secondary" onClick={() => setImporting(true)}>
              {t("users.import")}
            </Button>
            <Button onClick={() => setCreating(true)}>
              {t("users.create")}
            </Button>
          </>
        }
      />

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
        <div className="w-56">
          {/* The list endpoint has taken an organizationId since it was
              written, and the label for this control has been in the
              translations just as long. Only the control was missing, so
              the one filter an administrator most often wants — everyone in
              this department — was the one they could not apply. */}
          <Select
            value={organizationFilter}
            onChange={(e) => {
              setOrganizationFilter(e.target.value);
              setPage(1);
            }}
          >
            <option value="">
              {t("users.filterOrganization")}: {t("common.all")}
            </option>
            {organizations.map((organization) => (
              <option key={organization.id} value={organization.id}>
                {organization.name}
              </option>
            ))}
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

      <Table>
        <thead>
          <tr>
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
            <LoadingRow colSpan={6} />
          ) : users.length === 0 ? (
            <EmptyRow colSpan={6} />
          ) : (
            users.map((user) => (
              <tr key={user.id}>
                <Td>
                  <div className="flex flex-wrap items-center gap-1">
                    {user.username}
                    {/* Only the directory-managed case gets a mark. The
                        other three sources say how the account was born,
                        which is history; this one says who owns it now,
                        which changes what an edit here will do. A column
                        showing all four would give them equal weight. */}
                    {user.source === "SCIM" && (
                      <Badge tone="warning">{t("source.SCIM")}</Badge>
                    )}
                  </div>
                </Td>
                <Td>{user.displayName}</Td>
                <Td>
                  <Badge
                    tone={user.role === "SUPER_ADMIN" ? "warning" : "neutral"}
                  >
                    {t(`role.${user.role}`)}
                  </Badge>
                </Td>
                <Td>{user.organizationName || t("users.noOrganization")}</Td>
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
      } else {
        await userApi.create({
          username: form.username,
          displayName: form.displayName,
          password: form.password,
          phone: form.phone,
          email: form.email,
          role: form.role,
          organizationId: form.organizationId,
        });
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
