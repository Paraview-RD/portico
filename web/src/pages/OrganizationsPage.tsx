import { useCallback, useEffect, useState } from "react";

import { organizationApi } from "../api/endpoints";
import type { Organization } from "../api/types";
import {
  Alert,
  Button,
  Code,
  ConfirmDialog,
  DocsLink,
  EmptyRow,
  Field,
  GuidePanel,
  Input,
  LoadingRow,
  Modal,
  PageHeader,
  Select,
  StatusBadge,
  Table,
  Td,
  Th,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { OrganizationAdministratorsDialog } from "./OrganizationAdministratorsDialog";

export function OrganizationsPage() {
  const t = useT();

  const describeError = useErrorMessage();
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Organization | null>(null);
  const [creating, setCreating] = useState(false);
  const [managing, setManaging] = useState<Organization | null>(null);
  const [confirming, setConfirming] = useState<{
    org: Organization;
    enable: boolean;
  } | null>(null);
  const [keyword, setKeyword] = useState("");

  // Filtering flattens the tree on purpose. A match three levels down is
  // easier to act on as a plain row than as a branch the reader has to
  // reassemble, and showing its ancestors as context would put rows on
  // screen that do not match — which reads as the filter being broken.
  const filtered = keyword.trim()
    ? organizations.filter((org) =>
        [org.name, org.code].some((field) =>
          field.toLowerCase().includes(keyword.trim().toLowerCase()),
        ),
      )
    : null;

  // Unfiltered, rows are ordered so a child always follows its parent, with
  // a depth for indentation. Pagination is deliberately absent: a page
  // boundary through a tree separates children from parents, and the
  // resulting list is not a tree or a list.
  const rows = filtered
    ? filtered.map((org) => ({ org, depth: 0 }))
    : arrangeAsTree(organizations);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      setOrganizations(await organizationApi.list());
    } catch (err) {
      setError(describeError(err));
    } finally {
      setLoading(false);
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  async function toggleStatus(org: Organization, enable: boolean) {
    setConfirming(null);
    setError("");
    try {
      if (enable) {
        await organizationApi.enable(org.id);
      } else {
        await organizationApi.disable(org.id);
      }
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  return (
    <>
      <PageHeader
        title={t("organizations.title")}
        subtitle={t("organizations.subtitle")}
        actions={
          <>
            <DocsLink page="organizations/" />
            <Button onClick={() => setCreating(true)}>
              {t("organizations.create")}
            </Button>
          </>
        }
      />

      <GuidePanel
        id="organizations"
        docsPage="organizations/"
        title={t("organizations.guideTitle")}
      >
        {t("organizations.guideBody")}
      </GuidePanel>

      <div className="mb-4 w-72">
        <Input
          aria-label={t("organizations.searchPlaceholder")}
          placeholder={t("organizations.searchPlaceholder")}
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
        />
      </div>

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      <Table>
        <thead>
          <tr>
            <Th>{t("organizations.colName")}</Th>
            <Th>{t("organizations.colCode")}</Th>
            <Th>{t("organizations.colMembers")}</Th>
            <Th>{t("organizations.colRemark")}</Th>
            <Th>{t("organizations.colStatus")}</Th>
            <Th>{t("common.actions")}</Th>
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <LoadingRow colSpan={6} />
          ) : rows.length === 0 ? (
            <EmptyRow colSpan={6} />
          ) : (
            rows.map(({ org, depth }) => (
              <tr key={org.id}>
                <Td>
                  <span
                    style={{ paddingLeft: `${depth * 1.25}rem` }}
                    className="inline-block"
                  >
                    {depth > 0 && (
                      <span
                        aria-hidden="true"
                        className="mr-1.5 text-[var(--color-fg-muted)]"
                      >
                        └
                      </span>
                    )}
                    {org.name}
                  </span>
                </Td>
                <Td>
                  <Code>
                    {org.code}
                  </Code>
                </Td>
                <Td>{org.userCount}</Td>
                <Td>{org.remark || "—"}</Td>
                <Td>
                  <StatusBadge status={org.status} />
                </Td>
                <Td>
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setEditing(org)}
                    >
                      {t("common.edit")}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setManaging(org)}
                    >
                      {t("organizations.administratorsAction")}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() =>
                        setConfirming({ org, enable: org.status !== "ACTIVE" })
                      }
                    >
                      {org.status === "ACTIVE"
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

      <OrganizationFormDialog
        open={creating || editing !== null}
        organization={editing}
        all={organizations}
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

      <OrganizationAdministratorsDialog
        open={managing !== null}
        organization={managing}
        onClose={() => setManaging(null)}
      />

      <ConfirmDialog
        open={confirming !== null}
        title={confirming?.enable ? t("common.enable") : t("common.disable")}
        message={
          confirming
            ? confirming.enable
              ? t("organizations.confirmEnable", confirming.org.name)
              : t("organizations.confirmDisable", confirming.org.name)
            : ""
        }
        destructive={confirming?.enable === false}
        onConfirm={() =>
          confirming && void toggleStatus(confirming.org, confirming.enable)
        }
        onCancel={() => setConfirming(null)}
      />
    </>
  );
}

/**
 * Orders a flat list so every child immediately follows its parent, with the
 * depth to indent by.
 *
 * Rows whose parent is missing — filtered out, or disabled and not returned
 * — are treated as roots rather than dropped. A row that exists and is not
 * shown is the worse failure: an administrator looking for an organization
 * they can see in the database would find the list simply lacking it, with
 * nothing to say why.
 */
export function arrangeAsTree(
  organizations: Organization[],
): { org: Organization; depth: number }[] {
  const byParent = new Map<string, Organization[]>();
  const known = new Set(organizations.map((org) => org.id));

  for (const org of organizations) {
    const parent = org.parentId && known.has(org.parentId) ? org.parentId : "";
    const siblings = byParent.get(parent) ?? [];
    siblings.push(org);
    byParent.set(parent, siblings);
  }

  const rows: { org: Organization; depth: number }[] = [];

  // Iterative rather than recursive. The server bounds the depth at ten, so
  // recursion would be safe — but this runs against whatever the server
  // sent, and a stack overflow in a list component is a blank screen with
  // no explanation.
  const stack = [...(byParent.get("") ?? [])]
    .reverse()
    .map((org) => ({ org, depth: 0 }));

  while (stack.length > 0) {
    const entry = stack.pop() as { org: Organization; depth: number };
    rows.push(entry);

    const children = byParent.get(entry.org.id) ?? [];
    for (let i = children.length - 1; i >= 0; i--) {
      stack.push({ org: children[i], depth: entry.depth + 1 });
    }
  }

  return rows;
}

function OrganizationFormDialog({
  open,
  organization,
  all,
  onClose,
  onSaved,
}: {
  open: boolean;
  organization: Organization | null;
  all: Organization[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const isEdit = organization !== null;

  const [form, setForm] = useState({
    name: "",
    code: "",
    remark: "",
    parentId: "",
    sortOrder: 0,
  });
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setError("");
    setForm({
      name: organization?.name ?? "",
      code: organization?.code ?? "",
      remark: organization?.remark ?? "",
      parentId: organization?.parentId ?? "",
      sortOrder: organization?.sortOrder ?? 0,
    });
  }, [open, organization]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      if (isEdit && organization) {
        await organizationApi.update(organization.id, {
          name: form.name,
          remark: form.remark,
          parentId: form.parentId,
          sortOrder: form.sortOrder,
        });
      } else {
        await organizationApi.create(form);
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
      title={
        isEdit ? t("organizations.editTitle") : t("organizations.createTitle")
      }
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button form="organization-form" type="submit" disabled={submitting}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form
        id="organization-form"
        onSubmit={handleSubmit}
        className="flex flex-col gap-4"
      >
        <Field label={t("organizations.name")} required>
          <Input
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
            required
          />
        </Field>

        {/* The code is immutable once created: downstream systems and import
            files reference it, so changing it would break them silently. */}
        <Field
          label={t("organizations.code")}
          hint={t("organizations.codeHelp")}
          required={!isEdit}
        >
          <Input
            value={form.code}
            onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))}
            disabled={isEdit}
            required={!isEdit}
          />
        </Field>

        {/* The organization being edited is not offered as its own parent.
            Its descendants are still offered, and the server refuses those
            — the check has to be there anyway, since it is the only place
            that can see the whole tree. */}
        <Field
          label={t("organizations.parent")}
          hint={t("organizations.parentHelp")}
        >
          <Select
            value={form.parentId}
            onChange={(e) =>
              setForm((f) => ({ ...f, parentId: e.target.value }))
            }
          >
            <option value="">{t("organizations.noParent")}</option>
            {all
              .filter((candidate) => candidate.id !== organization?.id)
              .map((candidate) => (
                <option key={candidate.id} value={candidate.id}>
                  {candidate.name}
                </option>
              ))}
          </Select>
        </Field>

        <Field label={t("organizations.remark")}>
          <Input
            value={form.remark}
            onChange={(e) => setForm((f) => ({ ...f, remark: e.target.value }))}
          />
        </Field>

        <Field label={t("organizations.sortOrder")}>
          <Input
            type="number"
            value={form.sortOrder}
            onChange={(e) =>
              setForm((f) => ({ ...f, sortOrder: Number(e.target.value) }))
            }
          />
        </Field>

        {error && <Alert tone="danger">{error}</Alert>}
      </form>
    </Modal>
  );
}
