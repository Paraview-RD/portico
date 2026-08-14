import { useCallback, useEffect, useState } from "react";

import { directoriesApi, organizationApi } from "../api/endpoints";
import type {
  LDAPSource,
  LDAPSourceInput,
  LDAPSyncRun,
  Organization,
} from "../api/types";
import {
  Alert,
  Badge,
  Button,
  Code,
  DocsLink,
  EmptyRow,
  Field,
  Input,
  LoadingRow,
  Modal,
  Select,
  StatusBadge,
  Table,
  Td,
  Th,
  Timestamp,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

/**
 * Directories Portico reads accounts out of.
 *
 * The screen leads with the direction, because the tab beside it is the
 * opposite one and the two are easy to confuse: a SCIM credential lets a
 * directory push into Portico, and this has Portico connect to an AD and
 * pull. What an operator does when accounts stop arriving differs
 * accordingly, which is why the run history is on this screen and there is
 * nothing equivalent on the other.
 *
 * The attribute map is the part that repays care and the part a form is
 * tempted to hide behind defaults. It has none, because Active Directory and
 * OpenLDAP disagree on every one of them — so the form ships two presets
 * that fill the fields in and leave them visible and editable, rather than a
 * dropdown that decides silently.
 */
export function DirectoriesPanel() {
  const t = useT();
  const describeError = useErrorMessage();

  const [sources, setSources] = useState<LDAPSource[] | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<LDAPSource | null>(null);
  const [creating, setCreating] = useState(false);
  const [syncing, setSyncing] = useState<string | null>(null);
  const [lastRun, setLastRun] = useState<LDAPSyncRun | null>(null);
  const [history, setHistory] = useState<{
    source: LDAPSource;
    runs: LDAPSyncRun[] | null;
  } | null>(null);

  const load = useCallback(async () => {
    setError("");
    try {
      setSources(await directoriesApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  async function toggle(source: LDAPSource) {
    setError("");
    try {
      if (source.status === "ACTIVE") {
        await directoriesApi.disable(source.id);
      } else {
        await directoriesApi.enable(source.id);
      }
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function sync(source: LDAPSource) {
    setError("");
    setLastRun(null);
    setSyncing(source.id);
    try {
      // A failed run arrives here rather than in the catch: the request
      // succeeded and the synchronization did not, and the counts and the
      // reason are what somebody needs to see.
      setLastRun(await directoriesApi.sync(source.id));
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSyncing(null);
    }
  }

  async function openHistory(source: LDAPSource) {
    setHistory({ source, runs: null });
    try {
      setHistory({ source, runs: await directoriesApi.runs(source.id) });
    } catch (err) {
      setError(describeError(err));
      setHistory(null);
    }
  }

  return (
    <>
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-[var(--prose-form-width)] text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
          {t("directories.hint")}
        </p>
        <div className="flex gap-2">
          <DocsLink page="ldap/" />
          <Button onClick={() => setCreating(true)}>
            {t("directories.new")}
          </Button>
        </div>
      </div>

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      {lastRun && (
        <div className="mb-4">
          <Alert tone={lastRun.outcome === "SUCCEEDED" ? "success" : "danger"}>
            {lastRun.outcome === "SUCCEEDED"
              ? t(
                  "directories.runSummary",
                  String(lastRun.createdCount),
                  String(lastRun.updatedCount),
                  String(lastRun.deactivatedCount),
                  String(lastRun.skippedCount),
                )
              : failureText(t, lastRun)}
          </Alert>
        </div>
      )}

      <Table>
        <thead>
          <tr>
            <Th>{t("directories.colName")}</Th>
            <Th>{t("directories.colAddress")}</Th>
            <Th>{t("directories.colLastSync")}</Th>
            <Th>{t("directories.colStatus")}</Th>
            <Th>{t("common.actions")}</Th>
          </tr>
        </thead>
        <tbody>
          {sources === null && <LoadingRow colSpan={5} />}
          {sources?.length === 0 && <EmptyRow colSpan={5} />}
          {sources?.map((source) => (
            <tr key={source.id}>
              <Td>{source.name}</Td>
              <Td>
                <Code>
                  {source.host}:{source.port}
                </Code>
                <div className="text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]">
                  {source.baseDn}
                </div>
              </Td>
              <Td>
                {/* Never synchronized reads as a problem in its own right: a
                    directory that was configured and has never run. */}
                {source.lastSyncedAt ? (
                  <Timestamp value={source.lastSyncedAt} />
                ) : (
                  t("directories.neverSynced")
                )}
                {/* Whether anything will happen without being asked, which is
                    otherwise only visible by opening each form in turn. */}
                {source.syncIntervalMinutes > 0 && (
                  <div className="text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]">
                    {intervalLabel(t, source.syncIntervalMinutes)}
                  </div>
                )}
              </Td>
              <Td>
                <StatusBadge status={source.status} />
              </Td>
              <Td>
                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    disabled={syncing !== null || source.status !== "ACTIVE"}
                    onClick={() => void sync(source)}
                  >
                    {syncing === source.id
                      ? t("directories.syncing")
                      : t("directories.sync")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void openHistory(source)}
                  >
                    {t("directories.history")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setEditing(source)}
                  >
                    {t("common.edit")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void toggle(source)}
                  >
                    {source.status === "ACTIVE"
                      ? t("common.disable")
                      : t("common.enable")}
                  </Button>
                </div>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>

      <DirectoryFormDialog
        open={creating || editing !== null}
        source={editing}
        onClose={() => {
          setCreating(false);
          setEditing(null);
        }}
        onSaved={async () => {
          setCreating(false);
          setEditing(null);
          await load();
        }}
      />

      <Modal
        open={history !== null}
        title={t("directories.historyTitle", history?.source.name ?? "")}
        onClose={() => setHistory(null)}
      >
        {history?.runs === null ? (
          <p className="text-[var(--color-fg-muted)]">{t("common.loading")}</p>
        ) : history?.runs.length === 0 ? (
          <p className="text-[var(--color-fg-muted)]">{t("common.empty")}</p>
        ) : (
          <ul className="flex flex-col gap-3">
            {history?.runs?.map((run) => (
              <li
                key={run.id}
                className="border-b border-[var(--color-border)] pb-3 last:border-0 last:pb-0"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <Badge
                    tone={run.outcome === "SUCCEEDED" ? "success" : "danger"}
                  >
                    {t(`directories.outcome.${run.outcome}`)}
                  </Badge>
                  <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    <Timestamp value={run.startedAt} />
                  </span>
                  {/* Empty means the scheduler, which is not a person. */}
                  <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {run.actorName || t("directories.byScheduler")}
                  </span>
                </div>
                <div className="mt-1 text-[length:var(--font-size-sm)]">
                  {t(
                    "directories.runSummary",
                    String(run.createdCount),
                    String(run.updatedCount),
                    String(run.deactivatedCount),
                    String(run.skippedCount),
                  )}
                </div>
                {/* Shown whatever the outcome. A run that skipped entries
                    succeeded — that is the point of counting rather than
                    failing — so the reason has to appear next to a success
                    or nobody sees it. */}
                {run.skippedDetail && (
                  <p className="mt-1 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {run.skippedDetail}
                  </p>
                )}
                {run.outcome === "FAILED" && (
                  <p className="mt-1 text-[length:var(--font-size-sm)] text-[var(--color-danger-text)]">
                    {failureText(t, run)}
                  </p>
                )}
              </li>
            ))}
          </ul>
        )}
      </Modal>
    </>
  );
}

/**
 * Why a run failed, in the reader's language where that is possible.
 *
 * A refusal Portico decided on carries a code and is translated. An error the
 * directory reported does not, and is shown exactly as it arrived: "No Such
 * Object" is the string an operator will paste into a search engine, and a
 * translated version would be worse than useless.
 */
function failureText(
  t: ReturnType<typeof useT>,
  run: { errorCode?: string; error?: string },
): string {
  if (run.errorCode === "DIRECTORY_RETURNED_NOTHING") {
    return t("directories.emptyResult");
  }
  // Its near neighbour, and a separate message on purpose: both end with
  // nothing to reconcile against, and they send the reader to opposite ends
  // of the configuration.
  if (run.errorCode === "DIRECTORY_ENTRIES_UNREADABLE") {
    return t("directories.entriesUnreadable");
  }
  return run.error || t("directories.runFailed");
}

/**
 * Presets, not defaults.
 *
 * They fill the attribute fields in and leave every one visible and
 * editable. A dropdown that decided silently would be the same trap the
 * server refuses: a wrong guess imports a directory's worth of accounts
 * named after the wrong field, and it looks like it worked.
 */
const presets: Record<string, Partial<LDAPSourceInput>> = {
  ad: {
    userFilter:
      "(&(objectClass=user)(objectCategory=person)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))",
    attrUsername: "sAMAccountName",
    attrDisplayName: "displayName",
    attrEmail: "mail",
    attrPhone: "telephoneNumber",
    attrExternalId: "objectGUID",
  },
  openldap: {
    userFilter: "(objectClass=inetOrgPerson)",
    attrUsername: "uid",
    attrDisplayName: "cn",
    attrEmail: "mail",
    attrPhone: "telephoneNumber",
    attrExternalId: "entryUUID",
  },
};

const emptyForm: LDAPSourceInput = {
  name: "",
  host: "",
  port: 389,
  encryption: "starttls",
  bindDn: "",
  baseDn: "",
  userFilter: "",
  attrUsername: "",
  attrDisplayName: "",
  attrEmail: "",
  attrPhone: "",
  attrExternalId: "",
  organizationId: "",
  syncIntervalMinutes: 0,
};

/**
 * The intervals offered, in minutes, with 0 for "manual only".
 *
 * A list rather than a number box. The server refuses anything under fifteen
 * minutes — a synchronization enumerates the whole directory, so there is no
 * cheap pass to run every minute — and a field that lets somebody type 5 only
 * to be told no is a worse way to say that than one that never offers it.
 */
const syncIntervals = [0, 15, 30, 60, 360, 720, 1440, 10080];

/** "Every 6 hours", in the reader's language and units. */
function intervalLabel(t: ReturnType<typeof useT>, minutes: number): string {
  if (minutes === 0) return t("directories.syncManualOnly");
  if (minutes < 60) return t("directories.syncEveryMinutes", String(minutes));
  if (minutes === 1440) return t("directories.syncDaily");
  if (minutes === 10080) return t("directories.syncWeekly");
  return t("directories.syncEveryHours", String(minutes / 60));
}

function DirectoryFormDialog({
  open,
  source,
  onClose,
  onSaved,
}: {
  open: boolean;
  source: LDAPSource | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const isEdit = source !== null;

  const [form, setForm] = useState<LDAPSourceInput>(emptyForm);
  // Separate from the form, because "" and "not touched" are different
  // requests: the first clears the stored credential and the second leaves
  // it alone. Merging them would blank a service account's password every
  // time somebody opened the form and pressed save.
  const [password, setPassword] = useState<string | null>(null);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setError("");
    setPassword(null);
    setForm(
      source
        ? {
            name: source.name,
            host: source.host,
            port: source.port,
            encryption: source.encryption,
            bindDn: source.bindDn,
            baseDn: source.baseDn,
            userFilter: source.userFilter,
            attrUsername: source.attrUsername,
            attrDisplayName: source.attrDisplayName,
            attrEmail: source.attrEmail,
            attrPhone: source.attrPhone,
            attrExternalId: source.attrExternalId,
            organizationId: source.organizationId,
            syncIntervalMinutes: source.syncIntervalMinutes,
          }
        : emptyForm,
    );
  }, [open, source]);

  useEffect(() => {
    if (!open) return;
    organizationApi
      .list()
      // Optional context; a failure here must not block the form.
      .then(setOrganizations)
      .catch(() => setOrganizations([]));
  }, [open]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      const payload: LDAPSourceInput = { ...form };
      if (password !== null) payload.bindPassword = password;

      if (source) {
        await directoriesApi.update(source.id, payload);
      } else {
        await directoriesApi.create(payload);
      }
      await onSaved();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  function set<K extends keyof LDAPSourceInput>(
    key: K,
    value: LDAPSourceInput[K],
  ) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  return (
    <Modal
      open={open}
      title={isEdit ? t("directories.edit") : t("directories.new")}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            form="directory-form"
            disabled={submitting}
            onClick={() => undefined}
          >
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form
        id="directory-form"
        onSubmit={handleSubmit}
        className="flex flex-col gap-4"
      >
        <Field label={t("directories.name")}>
          <Input
            value={form.name}
            onChange={(e) => set("name", e.target.value)}
            placeholder={t("directories.namePlaceholder")}
          />
        </Field>

        <div className="grid gap-4 sm:grid-cols-[1fr_7rem_9rem]">
          <Field label={t("directories.host")} required>
            <Input
              value={form.host}
              onChange={(e) => set("host", e.target.value)}
              placeholder="ldap.example.com"
              required
            />
          </Field>
          <Field label={t("directories.port")} required>
            <Input
              type="number"
              value={String(form.port)}
              onChange={(e) => set("port", Number(e.target.value))}
              required
            />
          </Field>
          <Field label={t("directories.encryption")}>
            <Select
              value={form.encryption}
              onChange={(e) =>
                set(
                  "encryption",
                  e.target.value as LDAPSourceInput["encryption"],
                )
              }
            >
              <option value="starttls">STARTTLS</option>
              <option value="tls">TLS (LDAPS)</option>
              <option value="none">{t("directories.encryptionNone")}</option>
            </Select>
          </Field>
        </div>

        <Field
          label={t("directories.bindDn")}
          hint={t("directories.bindDnHelp")}
        >
          <Input
            value={form.bindDn}
            onChange={(e) => set("bindDn", e.target.value)}
            placeholder="cn=reader,dc=example,dc=com"
          />
        </Field>

        <Field
          label={t("directories.bindPassword")}
          hint={
            isEdit && source?.hasBindPassword
              ? t("directories.bindPasswordStored")
              : t("directories.bindPasswordHelp")
          }
        >
          <Input
            type="password"
            value={password ?? ""}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            placeholder={
              isEdit && source?.hasBindPassword
                ? t("directories.bindPasswordUnchanged")
                : ""
            }
          />
        </Field>

        <Field label={t("directories.baseDn")} required>
          <Input
            value={form.baseDn}
            onChange={(e) => set("baseDn", e.target.value)}
            placeholder="dc=example,dc=com"
            required
          />
        </Field>

        <div className="rounded-[var(--radius-sm)] border border-[var(--color-border)] p-4">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <span className="font-[weight:var(--font-weight-medium)]">
              {t("directories.attributes")}
            </span>
            <span className="flex gap-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setForm((f) => ({ ...f, ...presets.ad }))}
              >
                {t("directories.presetAD")}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setForm((f) => ({ ...f, ...presets.openldap }))}
              >
                {t("directories.presetOpenLDAP")}
              </Button>
            </span>
          </div>

          <p className="mb-4 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("directories.attributesHint")}
          </p>

          <div className="flex flex-col gap-4">
            <Field label={t("directories.userFilter")} required>
              <Input
                value={form.userFilter}
                onChange={(e) => set("userFilter", e.target.value)}
                required
              />
            </Field>

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label={t("directories.attrUsername")} required>
                <Input
                  value={form.attrUsername}
                  onChange={(e) => set("attrUsername", e.target.value)}
                  required
                />
              </Field>
              <Field label={t("directories.attrDisplayName")} required>
                <Input
                  value={form.attrDisplayName}
                  onChange={(e) => set("attrDisplayName", e.target.value)}
                  required
                />
              </Field>
              <Field label={t("directories.attrEmail")}>
                <Input
                  value={form.attrEmail}
                  onChange={(e) => set("attrEmail", e.target.value)}
                />
              </Field>
              <Field label={t("directories.attrPhone")}>
                <Input
                  value={form.attrPhone}
                  onChange={(e) => set("attrPhone", e.target.value)}
                />
              </Field>
            </div>

            <Field
              label={t("directories.attrExternalId")}
              hint={t("directories.attrExternalIdHelp")}
              required
            >
              <Input
                value={form.attrExternalId}
                onChange={(e) => set("attrExternalId", e.target.value)}
                required
              />
            </Field>
          </div>
        </div>

        <Field
          label={t("directories.syncSchedule")}
          hint={t("directories.syncScheduleHelp")}
        >
          <Select
            value={String(form.syncIntervalMinutes)}
            onChange={(e) => set("syncIntervalMinutes", Number(e.target.value))}
          >
            {syncIntervals.map((minutes) => (
              <option key={minutes} value={minutes}>
                {intervalLabel(t, minutes)}
              </option>
            ))}
          </Select>
        </Field>

        <Field
          label={t("directories.organization")}
          hint={t("directories.organizationHelp")}
        >
          <Select
            value={form.organizationId}
            onChange={(e) => set("organizationId", e.target.value)}
          >
            <option value="">{t("directories.organizationNone")}</option>
            {organizations.map((org) => (
              <option key={org.id} value={org.id}>
                {org.name}
              </option>
            ))}
          </Select>
        </Field>

        {error && <Alert tone="danger">{error}</Alert>}
      </form>
    </Modal>
  );
}
