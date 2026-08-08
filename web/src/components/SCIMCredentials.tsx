import { useCallback, useEffect, useState } from "react";

import { scimCredentialsApi } from "../api/endpoints";
import type { IssuedSCIMCredential, SCIMCredential } from "../api/types";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  CopyField,
  EmptyRow,
  Field,
  Input,
  Modal,
  Table,
  Td,
  Th,
} from "./ui";
import { useErrorMessage, useT } from "../i18n";

/**
 * The credentials a directory provisions with.
 *
 * The screen is shaped around one fact: the token exists exactly once, in
 * the response to creating it. There is no "reveal" and no "copy again",
 * because the server keeps a digest and has nothing to give back — so the
 * dialog that shows it says so, and does not close on a stray click.
 */
export function SCIMCredentials() {
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
    <section className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-medium)]">
            {t("scim.title")}
          </h2>
          <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("scim.subtitle")}
          </p>
        </div>
        <Button onClick={() => setCreating(true)}>{t("scim.new")}</Button>
      </div>

      {error && <Alert tone="danger">{error}</Alert>}

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
          {credentials?.length === 0 && <EmptyRow colSpan={5} />}
          {credentials?.map((credential) => (
            <tr key={credential.id}>
              <Td>{credential.name}</Td>
              <Td>
                <code className="text-[length:var(--font-size-sm)]">
                  {credential.tokenPrefix}…
                </code>
              </Td>
              <Td>
                {/* Never used reads as a problem in its own right: a
                    directory that was configured and has never connected. */}
                {credential.lastUsedAt
                  ? new Date(credential.lastUsedAt).toLocaleString()
                  : t("scim.neverUsed")}
              </Td>
              <Td>
                <Badge
                  tone={credential.status === "ACTIVE" ? "success" : "neutral"}
                >
                  {t(`status.${credential.status}`)}
                </Badge>
              </Td>
              <Td>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void toggle(credential)}
                  >
                    {credential.status === "ACTIVE"
                      ? t("common.disable")
                      : t("common.enable")}
                  </Button>
                  <Button
                    size="sm"
                    variant="danger"
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
          <Button onClick={() => setIssued(null)}>{t("common.done")}</Button>
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
    </section>
  );
}
