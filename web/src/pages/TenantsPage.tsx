import { useCallback, useEffect, useState } from "react";

import { tenantsApi } from "../api/endpoints";
import { DEFAULT_TENANT_CODE } from "../api/types";
import type { TenantOverview } from "../api/types";
import {
  Alert,
  Button,
  Code,
  EmptyRow,
  Field,
  Input,
  LoadingRow,
  Modal,
  PageHeader,
  StatusBadge,
  Table,
  Td,
  Th,
} from "../components/ui";
import { daysUntil, formatInstant } from "../i18n/format";
import { useErrorMessage, useT } from "../i18n";

/**
 * The tenants on this deployment, and how much is in each.
 *
 * The only screen in this console that shows anything belonging to a tenant
 * other than the reader's own — which is why it shows so little. Sizes, never
 * contents: a count of accounts and no account, a count of organizations and
 * no organization chart. There is no way from here into another tenant's
 * data, and there is deliberately no link that looks like one.
 *
 * It exists at all only where the deployment set PORTICO_TENANT_CONSOLE, and
 * then only for an administrator of the default tenant. Both of those are
 * decided by the server: this page is reached from a menu entry drawn from
 * `mayManageTenants`, and asks for nothing it was not offered.
 *
 * Creating and deleting tenants stay on the command line. Disabling is here
 * because it is reversible and sometimes urgent; deletion is neither, and a
 * tenant deleted by a mis-click is a hundred rows across thirty tables that
 * no screen can put back.
 */
export function TenantsPage() {
  const t = useT();
  const describeError = useErrorMessage();

  const [tenants, setTenants] = useState<TenantOverview[] | null>(null);
  const [error, setError] = useState("");

  // The tenant whose switch was clicked, held while its code is typed back.
  const [switching, setSwitching] = useState<TenantOverview | null>(null);
  const [typed, setTyped] = useState("");
  const [saving, setSaving] = useState(false);
  // Which row's extend button is in flight, so only that one is disabled.
  const [extending, setExtending] = useState("");

  const load = useCallback(async () => {
    try {
      setTenants(await tenantsApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  async function applyStatus() {
    if (!switching) return;
    setSaving(true);
    setError("");
    try {
      const next = switching.status === "ACTIVE" ? "DISABLED" : "ACTIVE";
      await tenantsApi.setStatus(switching.code, next, typed);
      setSwitching(null);
      setTyped("");
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSaving(false);
    }
  }

  // No dialog. Extending takes nothing away — the worst a mis-click does is
  // give a demonstration tenant another fortnight, and the switch beside it
  // undoes that. The confirmation on the other button is there because
  // disabling signs everybody in a tenant out at once.
  async function extend(tenant: TenantOverview) {
    setExtending(tenant.code);
    setError("");
    try {
      await tenantsApi.extend(tenant.code);
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setExtending("");
    }
  }

  return (
    <div>
      <PageHeader title={t("tenants.title")} subtitle={t("tenants.subtitle")} />

      {error && <Alert tone="danger">{error}</Alert>}

      {/* Said once, at the top, rather than left to be inferred from a screen
          that looks like every other list here. A reader has to know what this
          can see, what it cannot, and where the rest is. Not an Alert: nothing
          is wrong, and the tones this console has all mean something is. */}
      <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("tenants.scopeNote")}
      </p>

      <div className="mt-4">
        <Table>
          <thead>
            <tr>
              <Th>{t("tenants.colCode")}</Th>
              <Th>{t("tenants.colName")}</Th>
              <Th>{t("tenants.colStatus")}</Th>
              <Th>{t("tenants.colUsers")}</Th>
              <Th>{t("tenants.colOrganizations")}</Th>
              <Th>{t("tenants.colApplications")}</Th>
              <Th>{t("tenants.colLastActivity")}</Th>
              <Th>{t("tenants.colExpires")}</Th>
              <Th>{t("common.actions")}</Th>
            </tr>
          </thead>
          <tbody>
            {tenants === null && <LoadingRow colSpan={9} />}
            {tenants?.length === 0 && <EmptyRow colSpan={9} />}
            {tenants?.map((tenant) => (
              <tr key={tenant.id}>
                <Td>
                  <Code>{tenant.code}</Code>
                </Td>
                <Td>{tenant.name}</Td>
                <Td>
                  <StatusBadge status={tenant.status} />
                </Td>
                {/* Active out of total, because the difference is the
                    interesting number: a tenant with forty accounts and two
                    that still work is a different situation from one with
                    two accounts. */}
                <Td>
                  {tenant.activeUsers} / {tenant.users}
                </Td>
                <Td>{tenant.organizations}</Td>
                <Td>{tenant.applications}</Td>
                {/* A tenant nothing has ever happened in is the row an
                    operator is usually looking for, so it says so rather than
                    showing an empty cell that reads as a rendering fault. */}
                <Td>
                  {tenant.lastActivity
                    ? formatInstant(tenant.lastActivity)
                    : t("tenants.neverUsed")}
                </Td>
                {/* The date and how long is left, because neither answers the
                    question alone: a date needs arithmetic against today, and
                    "3 days" gives no way to check it against an email somebody
                    was sent. Most tenants have no date at all — only a
                    self-service trial sets one — so the common cell is the
                    quiet one. */}
                <Td>
                  {tenant.expiresAt ? (
                    <span className="flex flex-col">
                      <span>{formatInstant(tenant.expiresAt, "date")}</span>
                      <span
                        className={
                          daysUntil(tenant.expiresAt) < 0
                            ? "text-[length:var(--font-size-sm)] text-[var(--color-danger-text)]"
                            : "text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]"
                        }
                      >
                        {daysUntil(tenant.expiresAt) < 0
                          ? t("tenants.expired")
                          : t(
                              "tenants.expiresInDays",
                              daysUntil(tenant.expiresAt),
                            )}
                      </span>
                    </span>
                  ) : (
                    <span className="text-[var(--color-fg-muted)]">
                      {t("tenants.noExpiry")}
                    </span>
                  )}
                </Td>
                <Td>
                  {/* No button on the tenant this console is served from.
                      The API refuses it — there would be no way back from a
                      browser — and offering an action that always fails is
                      worse than offering none: it reads as a permission
                      problem rather than as a rule. */}
                  {tenant.code === DEFAULT_TENANT_CODE &&
                  tenant.status === "ACTIVE" ? (
                    <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                      {t("tenants.thisConsole")}
                    </span>
                  ) : (
                    <Button
                      variant={
                        tenant.status === "ACTIVE" ? "ghost-danger" : "ghost"
                      }
                      onClick={() => {
                        setSwitching(tenant);
                        setTyped("");
                      }}
                    >
                      {tenant.status === "ACTIVE"
                        ? t("common.disable")
                        : t("common.enable")}
                    </Button>
                  )}
                  {/* Offered only where there is a date to move. A tenant with
                      none is not on a clock, and the server refuses to give it
                      one here — a button that always fails reads as a
                      permission problem rather than as a rule. */}
                  {tenant.expiresAt && (
                    <Button
                      variant="ghost"
                      disabled={extending === tenant.code}
                      onClick={() => void extend(tenant)}
                    >
                      {t("tenants.extend")}
                    </Button>
                  )}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </div>

      {/* Typing the code rather than pressing "yes".

          Disabling a tenant signs everybody in it out at once, and they hear
          it from a sign-in screen rather than from anybody. A dialog with a
          confirm button is one mis-click; a dialog that will not proceed
          until its own name is typed is not. The API enforces the same thing
          independently, so this is the visible half of a rule rather than the
          whole of it. */}
      <Modal
        open={switching !== null}
        title={
          switching?.status === "ACTIVE"
            ? t("tenants.disableTitle", switching?.code ?? "")
            : t("tenants.enableTitle", switching?.code ?? "")
        }
        onClose={() => setSwitching(null)}
      >
        <div className="flex flex-col gap-4">
          {switching?.status === "ACTIVE" ? (
            <Alert tone="warning">
              {t("tenants.disableWarning", String(switching?.activeUsers ?? 0))}
            </Alert>
          ) : (
            <p className="text-[var(--color-fg-muted)]">
              {t("tenants.enableBody")}
            </p>
          )}

          <Field
            label={t("tenants.confirmLabel")}
            hint={t("tenants.confirmHint")}
          >
            <Input
              value={typed}
              autoFocus
              onChange={(e) => setTyped(e.target.value)}
            />
          </Field>

          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setSwitching(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant={switching?.status === "ACTIVE" ? "danger" : "primary"}
              disabled={saving || typed !== switching?.code}
              onClick={() => void applyStatus()}
            >
              {switching?.status === "ACTIVE"
                ? t("common.disable")
                : t("common.enable")}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
