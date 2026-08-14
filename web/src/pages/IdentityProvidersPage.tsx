import { useCallback, useEffect, useState } from "react";

import { externalIdpApi } from "../api/endpoints";
import type {
  ExternalIdentityProvider,
  ExternalIdentityProviderInput,
  ExternalIdentityProviderKind,
} from "../api/types";
import {
  Alert,
  Badge,
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
  Select,
  StatusBadge,
  Table,
  Td,
  Th,
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
  kind: "OIDC",
  issuer: "",
  clientId: "",
  clientSecret: "",
  scopes: "openid profile email",
  trustVerifiedEmail: false,
};

/**
 * What each kind calls the two credentials, and which fields it has at all.
 *
 * A form that showed every field for every kind would ask for an issuer that
 * is ignored and scopes that are not sent, which reads as configuration
 * somebody got wrong rather than as fields that do not apply. And the names
 * matter more than they look: an administrator is copying these out of
 * WeChat's own console, where they are called AppID and AppSecret, and a
 * field labelled "Client ID" is one they have to translate in their head
 * while holding two browser tabs open.
 */
const KINDS = {
  OIDC: { issuer: true, scopes: true, trustEmail: true },
  // No issuer: it is a constant in the server, because it is the namespace
  // every subject lives in. No scopes: there is exactly one that does
  // anything and the adapter sends it. No trust-email: WeChat returns no
  // address at all, so the switch could only mislead.
  WECHAT: { issuer: false, scopes: false, trustEmail: false },
  DINGTALK: { issuer: false, scopes: false, trustEmail: true },
} as const;

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
      // Carried so the form keeps its shape, and not editable: the server
      // takes the stored kind whatever this sends, because changing it would
      // leave every bound identity pointing at a protocol that did not
      // issue it.
      kind: provider.kind,
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
                {/* The kind above the issuer, because it decides what the
                    issuer beside it even means: for two of the three it is a
                    constant this server chose rather than a value anybody
                    configured. */}
                <div className="flex flex-col gap-0.5">
                  <span>{t(`identityProviders.kind.${provider.kind}`)}</span>
                  <Code>{provider.issuer}</Code>
                </div>
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
                <StatusBadge status={provider.status} />
              </Td>
              <Td>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => startEdit(provider)}
                  >
                    {t("common.edit")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void toggle(provider)}
                  >
                    {provider.status === "ACTIVE"
                      ? t("common.disable")
                      : t("common.enable")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost-danger"
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

          {/* First on a create, because everything below changes with it.
              Absent on an edit: the kind is fixed once identities can be
              bound to a provider, and a disabled control saying so is a
              control somebody tries to use. */}
          {!editing && (
            <Field
              label={t("identityProviders.kind")}
              hint={t("identityProviders.kindHint")}
            >
              <Select
                value={form.kind}
                onChange={(e) =>
                  setForm({
                    ...blankForm,
                    // Keep what has been typed that still means something.
                    name: form.name,
                    buttonLabel: form.buttonLabel,
                    kind: e.target.value as ExternalIdentityProviderKind,
                  })
                }
              >
                <option value="OIDC">{t("identityProviders.kind.OIDC")}</option>
                <option value="WECHAT">
                  {t("identityProviders.kind.WECHAT")}
                </option>
                <option value="DINGTALK">
                  {t("identityProviders.kind.DINGTALK")}
                </option>
              </Select>
            </Field>
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

          {/* Only where it is a field. For WeChat and DingTalk the issuer is
              a constant in the server — it is the namespace every subject
              lives in, and a typed one would let two tenants disagree about
              what WeChat is. */}
          {KINDS[form.kind].issuer && (
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
          )}

          {/* Named as the vendor names it. An administrator is copying this
              out of WeChat's own console, where it is AppID; a field called
              "Client ID" is one they have to translate in their head while
              holding two tabs open. */}
          <Field label={t(`identityProviders.clientId.${form.kind}`)} required>
            <Input
              value={form.clientId}
              onChange={(e) => setForm({ ...form, clientId: e.target.value })}
              required
            />
          </Field>

          <Field
            label={t(`identityProviders.clientSecret.${form.kind}`)}
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

          {/* Not a field for the two whose adapter sends the one scope that
              does anything. Offering it would be asking for a value that is
              discarded. */}
          {KINDS[form.kind].scopes && (
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
          )}

          {/* A checkbox with a paragraph, not a checkbox with a label.
              Turning it on says that an address this provider vouches for is
              enough to be let into an account that already exists here — so
              whoever runs that provider decides who gets in. That is a
              sentence, and a four-word label would be a decision made
              without it.

              Absent for WeChat, which returns no address at all. A control
              that cannot do anything is worse than a missing one: it reads
              as a decision somebody made. */}
          {KINDS[form.kind].trustEmail && (
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
          )}

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
