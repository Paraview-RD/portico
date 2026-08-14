import { useCallback, useEffect, useState } from "react";

import {
  auditApi,
  PROVISIONING_ACTOR,
  scimCredentialsApi,
} from "../api/endpoints";
import type {
  AuditLog,
  IssuedSCIMCredential,
  SCIMCredential,
} from "../api/types";
import { DirectoriesPanel } from "./DirectoriesPanel";
import { useRouter } from "../router";
import {
  Alert,
  Button,
  Code,
  ConfirmDialog,
  CopyField,
  DocsLink,
  EmptyRow,
  Field,
  GuidePanel,
  Input,
  LoadingRow,
  Modal,
  PageHeader,
  StatusBadge,
  Table,
  Td,
  Th,
  Timestamp,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

/**
 * The credentials a directory provisions with.
 *
 * The screen is shaped around one fact: the token exists exactly once, in
 * the response to creating it. There is no "reveal" and no "copy again",
 * because the server keeps a digest and has nothing to give back — so the
 * dialog that shows it says so, and does not close on a stray click.
 *
 * Its own screen rather than a section of the settings page. Issuing one of
 * these is not configuration — it is handing a directory the ability to
 * create, update, and disable every account in the tenant — and it belongs
 * beside the other things that connect to Portico rather than below the
 * password rules.
 */
/**
 * Two directions, one screen.
 *
 * A SCIM credential lets a directory push accounts into Portico. An LDAP
 * source has Portico reach out and pull them. They land in the same place,
 * and the tabs exist so that nobody has to work out which of the two a given
 * deployment is doing from the shape of the form.
 */
export function ProvisioningPage() {
  const t = useT();
  const [tab, setTab] = useState<"directories" | "scim">("directories");

  return (
    <>
      <PageHeader
        title={t("provisioning.title")}
        subtitle={t("provisioning.subtitle")}
        actions={<DocsLink page="scim/" />}
      />

      {/* No docs link of its own, unlike the other screens' guides. This
          page is two directions, documented in two places — ldap.md and
          scim.md — and a single link here would send half the readers to
          the wrong one while sitting inches from the header's link to the
          other. The tab they pick is the better disambiguator. */}
      <GuidePanel
        id="provisioning"
        docsPage="scim/"
        title={t("provisioning.guideTitle")}
      >
        {t("provisioning.guideBody")}
      </GuidePanel>

      <div
        role="tablist"
        aria-label={t("provisioning.title")}
        className="mb-4 flex gap-1 border-b border-[var(--color-border)]"
      >
        {(["directories", "scim"] as const).map((value) => (
          <button
            key={value}
            id={`provisioning-tab-${value}`}
            role="tab"
            type="button"
            aria-selected={tab === value}
            aria-controls="provisioning-panel"
            onClick={() => setTab(value)}
            className={
              tab === value
                ? "-mb-px border-b-2 border-[var(--color-primary)] px-4 py-2 text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]"
                : "-mb-px border-b-2 border-transparent px-4 py-2 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
            }
          >
            {t(`provisioning.tab.${value}`)}
          </button>
        ))}
      </div>

      <div
        id="provisioning-panel"
        role="tabpanel"
        aria-labelledby={`provisioning-tab-${tab}`}
      >
        {tab === "directories" ? (
          <DirectoriesPanel />
        ) : (
          <SCIMCredentialsPanel />
        )}
      </div>
    </>
  );
}

function SCIMCredentialsPanel() {
  const t = useT();
  const describeError = useErrorMessage();

  const [credentials, setCredentials] = useState<SCIMCredential[] | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [issued, setIssued] = useState<IssuedSCIMCredential | null>(null);
  const [deleting, setDeleting] = useState<SCIMCredential | null>(null);

  const load = useCallback(async () => {
    try {
      setCredentials(await scimCredentialsApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  async function create(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      const credential = await scimCredentialsApi.create(name);
      setCreating(false);
      setName("");
      // Shown before the list refreshes: this is the only moment the token
      // is available anywhere.
      setIssued(credential);
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function toggle(credential: SCIMCredential) {
    setError("");
    try {
      if (credential.status === "ACTIVE") {
        await scimCredentialsApi.disable(credential.id);
      } else {
        await scimCredentialsApi.enable(credential.id);
      }
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function remove() {
    if (!deleting) return;
    setError("");
    try {
      await scimCredentialsApi.remove(deleting.id);
      setDeleting(null);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  return (
    <>
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-[var(--prose-form-width)] text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
          {t("scim.subtitle")}
        </p>
        <Button onClick={() => setCreating(true)}>{t("scim.new")}</Button>
      </div>

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      <Table>
        <thead>
          <tr>
            <Th>{t("scim.colName")}</Th>
            <Th>{t("scim.colToken")}</Th>
            <Th>{t("scim.colLastUsed")}</Th>
            <Th>{t("scim.colStatus")}</Th>
            <Th>{t("common.actions")}</Th>
          </tr>
        </thead>
        <tbody>
          {credentials === null && <LoadingRow colSpan={5} />}
          {credentials?.length === 0 && <EmptyRow colSpan={5} />}
          {credentials?.map((credential) => (
            <tr key={credential.id}>
              <Td>{credential.name}</Td>
              <Td>
                <Code>
                  {credential.tokenPrefix}…
                </Code>
              </Td>
              <Td>
                {/* Never used reads as a problem in its own right: a
                    directory that was configured and has never connected. */}
                {credential.lastUsedAt
                  ? <Timestamp value={credential.lastUsedAt} />
                  : t("scim.neverUsed")}
              </Td>
              <Td>
                <StatusBadge status={credential.status} />
              </Td>
              <Td>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void toggle(credential)}
                  >
                    {credential.status === "ACTIVE"
                      ? t("common.disable")
                      : t("common.enable")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost-danger"
                    onClick={() => setDeleting(credential)}
                  >
                    {t("common.delete")}
                  </Button>
                </div>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>

      <Modal
        open={creating}
        title={t("scim.new")}
        onClose={() => setCreating(false)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setCreating(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" form="scim-create" disabled={submitting}>
              {t("common.create")}
            </Button>
          </>
        }
      >
        <form
          id="scim-create"
          onSubmit={create}
          className="flex flex-col gap-4"
        >
          <Field label={t("scim.name")} hint={t("scim.nameHint")} required>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              autoFocus
            />
          </Field>
        </form>
      </Modal>

      <Modal
        open={issued !== null}
        title={t("scim.issued")}
        onClose={() => setIssued(null)}
        footer={
          <Button onClick={() => setIssued(null)}>{t("common.close")}</Button>
        }
      >
        <div className="flex flex-col gap-4">
          <Alert tone="warning">{t("scim.issuedWarning")}</Alert>
          <CopyField label={t("scim.token")} value={issued?.token ?? ""} />
        </div>
      </Modal>

      <ConfirmDialog
        open={deleting !== null}
        title={t("scim.confirmDeleteTitle")}
        message={t("scim.confirmDelete", deleting?.name ?? "")}
        destructive
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      />

      <SyncActivity />
    </>
  );
}

/**
 * What the directory has actually done.
 *
 * The table above answers "is a credential configured" and "has anything
 * ever used it". Neither is the question an operator arrives with, which is
 * whether the sync is working — and the records that answer it existed all
 * along, written to the audit trail under the `scim` actor, on a screen
 * nobody visits to ask about provisioning.
 *
 * Actions are shown as the verbs the server records rather than translated
 * prose, the same as in the audit log: they are identifiers, and an operator
 * comparing them with what their directory reported needs the identifier.
 */
function SyncActivity() {
  const t = useT();
  const describeError = useErrorMessage();
  const { navigate } = useRouter();

  const [entries, setEntries] = useState<AuditLog[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    auditApi
      .list({ actor: PROVISIONING_ACTOR, page: 1, pageSize: 10 })
      .then((result) => setEntries(result.items))
      .catch((err) => setError(describeError(err)));
  }, [describeError]);

  return (
    <section className="mt-8">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-[length:var(--font-size-base)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
            {t("scim.activityTitle")}
          </h2>
          <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("scim.activityHint")}
          </p>
        </div>
        <Button variant="secondary" onClick={() => navigate("/audit-logs")}>
          {t("scim.activityViewAll")}
        </Button>
      </div>

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      <Table>
        <thead>
          <tr>
            <Th>{t("scim.colTime")}</Th>
            <Th>{t("scim.colAction")}</Th>
            <Th>{t("scim.colTarget")}</Th>
            <Th>{t("scim.colDetail")}</Th>
          </tr>
        </thead>
        <tbody>
          {entries === null && <LoadingRow colSpan={4} />}
          {entries?.length === 0 && <EmptyRow colSpan={4} />}
          {entries?.map((entry) => (
            <tr key={entry.id}>
              <Td><Timestamp value={entry.createdAt} /></Td>
              <Td>
                <Code>
                  {entry.action}
                </Code>
              </Td>
              <Td>{entry.targetName || "—"}</Td>
              <Td className="text-[var(--color-fg-muted)]">
                {entry.detail || "—"}
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </section>
  );
}
