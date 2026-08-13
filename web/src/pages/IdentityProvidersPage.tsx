import { useCallback, useEffect, useState } from "react";

import { externalIdpApi } from "../api/endpoints";
import type {
  ExternalIdentityProvider,
  ExternalIdentityProviderInput,
} from "../api/types";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  CopyField,
  EmptyRow,
  Field,
  GuidePanel,
  Input,
  LoadingRow,
  Modal,
  PageHeader,
  Table,
  Td,
  Th,
  DocsLink,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

/**
 * The OpenID Providers this deployment will believe.
 *
 * The opposite direction from the applications screen. There, other systems
 * ask Portico who somebody is. Here, Portico asks somebody else — which is
 * a different job with a different threat model, because the assertions
 * arrive from outside and what is done with them is the whole security
 * question.
 *
 * Its own screen rather than a card on the settings page, for the same
 * reason as provisioning and webhooks: registering one of these is handing
 * an outside system the ability to say who anybody here is, and that is a
 * connection to another system rather than a preference.
 *
 * Two things the screen exists to make plain. The redirect URI, which has to
 * be registered at the other end character for character and is therefore
 * shown rather than described. And the trust-verified-email switch, which is
 * the one control here that can lose an account.
 */

/** An empty form, and what an edit resets to. */
const blankForm: ExternalIdentityProviderInput = {
  name: "",
  buttonLabel: "",
  issuer: "",
  clientId: "",
  clientSecret: "",
  scopes: "openid profile email",
  trustVerifiedEmail: false,
};

export function IdentityProvidersPage() {
  const t = useT();
  const describeError = useErrorMessage();

  const [providers, setProviders] = useState<ExternalIdentityProvider[] | null>(
    null,
  );
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  // The provider being edited, or null while creating. Both use one form:
  // the fields are the same, and the only difference is what a blank secret
  // means — which the form says out loud rather than encoding in its shape.
  const [editing, setEditing] = useState<ExternalIdentityProvider | null>(null);
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<ExternalIdentityProviderInput>(blankForm);
  const [deleting, setDeleting] = useState<ExternalIdentityProvider | null>(
    null,
  );

  const load = useCallback(async () => {
    try {
      setProviders(await externalIdpApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  function startCreate() {
    setEditing(null);
    setForm(blankForm);
    setError("");
    setOpen(true);
  }

  function startEdit(provider: ExternalIdentityProvider) {
    setEditing(provider);
    setForm({
      name: provider.name,
      buttonLabel: provider.buttonLabel,
      issuer: provider.issuer,
      clientId: provider.clientId,
      // Never pre-filled, because it is never served. Blank means keep.
      clientSecret: "",
      scopes: provider.scopes,
      trustVerifiedEmail: provider.trustVerifiedEmail,
    });
    setError("");
    setOpen(true);
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      // The issuer is contacted before the row is written, so this can take
      // a moment and can fail on a configuration that looks right. That is
      // the point of it: the person able to fix a bad issuer is the one
      // filling in this form, not the user who meets it at a sign-in screen
      // three days later.
      if (editing) {
        await externalIdpApi.update(editing.id, form);
      } else {
        await externalIdpApi.create(form);
      }
      setOpen(false);
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function toggle(provider: ExternalIdentityProvider) {
    setError("");
    try {
      if (provider.status === "ACTIVE") {
        await externalIdpApi.disable(provider.id);
      } else {
        await externalIdpApi.enable(provider.id);
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
      await externalIdpApi.remove(deleting.id);
      setDeleting(null);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  return (
    <>
      <PageHeader
        title={t("identityProviders.title")}
        subtitle={t("identityProviders.subtitle")}
        actions={
          <>
            <DocsLink page="federation/" />
            <Button onClick={startCreate}>{t("identityProviders.new")}</Button>
          </>
        }
      />

      <GuidePanel
        id="identity-providers"
        title={t("identityProviders.guideTitle")}
        docsPage="federation/"
      >
        {t("identityProviders.guideBody")}
      </GuidePanel>

      {error && !open && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      <Table>
        <thead>
          <tr>
            <Th>{t("identityProviders.colName")}</Th>
            <Th>{t("identityProviders.colIssuer")}</Th>
            <Th>{t("identityProviders.colTrustEmail")}</Th>
            <Th>{t("identityProviders.colStatus")}</Th>
            <Th>{t("common.actions")}</Th>
          </tr>
        </thead>
        <tbody>
          {providers === null && <LoadingRow colSpan={5} />}
          {providers?.length === 0 && <EmptyRow colSpan={5} />}
          {providers?.map((provider) => (
            <tr key={provider.id}>
              <Td>{provider.name}</Td>
              <Td>
                <code className="text-[length:var(--font-size-sm)]">
                  {provider.issuer}
                </code>
              </Td>
              <Td>
                {/* Called out rather than shown as a tick, because it is the
                    one setting here that decides whether an address is
                    enough to get into an existing account. */}
                <Badge
                  tone={provider.trustVerifiedEmail ? "warning" : "neutral"}
                >
                  {provider.trustVerifiedEmail
                    ? t("identityProviders.trusted")
                    : t("identityProviders.notTrusted")}
                </Badge>
              </Td>
              <Td>
                <Badge
                  tone={provider.status === "ACTIVE" ? "success" : "neutral"}
                >
                  {t(`status.${provider.status}`)}
                </Badge>
              </Td>
              <Td>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => startEdit(provider)}
                  >
                    {t("common.edit")}
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void toggle(provider)}
                  >
                    {provider.status === "ACTIVE"
                      ? t("common.disable")
                      : t("common.enable")}
                  </Button>
                  <Button
                    size="sm"
                    variant="danger"
                    onClick={() => setDeleting(provider)}
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
        open={open}
        title={
          editing ? t("identityProviders.edit") : t("identityProviders.new")
        }
        onClose={() => setOpen(false)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              form="identity-provider"
              disabled={submitting}
            >
              {submitting
                ? t("identityProviders.checking")
                : editing
                  ? t("common.save")
                  : t("common.create")}
            </Button>
          </>
        }
      >
        <form
          id="identity-provider"
          onSubmit={save}
          className="flex flex-col gap-4"
        >
          {/* First, and above the fields, because it is the value that has to
              exist at the other end before any of this works. Shown only
              when editing: a provider that does not exist yet has no tenant
              address to copy, and inventing one here would be a second place
              for the composition rule to live. */}
          {editing && (
            <CopyField
              label={t("identityProviders.redirectUri")}
              value={editing.redirectUri}
            />
          )}

          <Field label={t("identityProviders.name")} required>
            <Input
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              required
              autoFocus
            />
          </Field>

          <Field
            label={t("identityProviders.buttonLabel")}
            hint={t("identityProviders.buttonLabelHint")}
          >
            <Input
              value={form.buttonLabel}
              onChange={(e) =>
                setForm({ ...form, buttonLabel: e.target.value })
              }
            />
          </Field>

          <Field
            label={t("identityProviders.issuer")}
            hint={t("identityProviders.issuerHint")}
            required
          >
            <Input
              type="url"
              value={form.issuer}
              onChange={(e) => setForm({ ...form, issuer: e.target.value })}
              placeholder="https://accounts.google.com"
              required
            />
          </Field>

          <Field label={t("identityProviders.clientId")} required>
            <Input
              value={form.clientId}
              onChange={(e) => setForm({ ...form, clientId: e.target.value })}
              required
            />
          </Field>

          <Field
            label={t("identityProviders.clientSecret")}
            hint={
              editing
                ? editing.hasSecret
                  ? t("identityProviders.secretKept")
                  : t("identityProviders.secretNone")
                : t("identityProviders.secretHint")
            }
          >
            <Input
              type="password"
              value={form.clientSecret}
              onChange={(e) =>
                setForm({ ...form, clientSecret: e.target.value })
              }
              autoComplete="new-password"
            />
          </Field>

          <Field
            label={t("identityProviders.scopes")}
            hint={t("identityProviders.scopesHint")}
          >
            <Input
              value={form.scopes}
              onChange={(e) => setForm({ ...form, scopes: e.target.value })}
              placeholder="openid profile email"
            />
          </Field>

          {/* A checkbox with a paragraph, not a checkbox with a label.
              Turning it on says that an address this provider vouches for is
              enough to be let into an account that already exists here — so
              whoever runs that provider decides who gets in. That is a
              sentence, and a four-word label would be a decision made
              without it. */}
          <label className="flex items-start gap-2 text-[length:var(--font-size-sm)]">
            <input
              type="checkbox"
              className="mt-1"
              checked={form.trustVerifiedEmail}
              onChange={(e) =>
                setForm({ ...form, trustVerifiedEmail: e.target.checked })
              }
            />
            <span>
              <span className="font-[weight:var(--font-weight-medium)]">
                {t("identityProviders.trustVerifiedEmail")}
              </span>
              <br />
              <span className="text-[var(--color-fg-muted)]">
                {t("identityProviders.trustVerifiedEmailHint")}
              </span>
            </span>
          </label>

          {error && <Alert tone="danger">{error}</Alert>}
        </form>
      </Modal>

      <ConfirmDialog
        open={deleting !== null}
        title={t("identityProviders.delete")}
        message={t("identityProviders.deleteConfirm", deleting?.name ?? "")}
        destructive
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      />
    </>
  );
}
