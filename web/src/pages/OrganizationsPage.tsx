import { useCallback, useEffect, useState } from "react";

import { organizationApi } from "../api/endpoints";
import type { Organization } from "../api/types";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  EmptyRow,
  Field,
  Input,
  Modal,
  PageHeader,
  Table,
  Td,
  Th,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

export function OrganizationsPage() {
  const t = useT();

  const describeError = useErrorMessage();
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Organization | null>(null);
  const [creating, setCreating] = useState(false);
  const [confirming, setConfirming] = useState<{
    org: Organization;
    enable: boolean;
  } | null>(null);

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
          <Button onClick={() => setCreating(true)}>
            {t("organizations.create")}
          </Button>
        }
      />

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
            <tr>
              <Td className="py-10 text-center">{t("common.loading")}</Td>
            </tr>
          ) : organizations.length === 0 ? (
            <EmptyRow colSpan={6} />
          ) : (
            organizations.map((org) => (
              <tr key={org.id}>
                <Td>{org.name}</Td>
                <Td>
                  <code className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {org.code}
                  </code>
                </Td>
                <Td>{org.userCount}</Td>
                <Td>{org.remark || "—"}</Td>
                <Td>
                  <Badge tone={org.status === "ACTIVE" ? "success" : "danger"}>
                    {t(`status.${org.status}`)}
                  </Badge>
                </Td>
                <Td>
                  <div className="flex gap-1">
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

function OrganizationFormDialog({
  open,
  organization,
  onClose,
  onSaved,
}: {
  open: boolean;
  organization: Organization | null;
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
