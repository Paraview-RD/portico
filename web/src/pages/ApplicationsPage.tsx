/**
 * Application management: registering the systems that sign in through
 * Portico, across all three protocols.
 *
 * The screen is in two halves, because registering something is only half
 * the job. The tabs register applications; the "integration endpoints" panel
 * hands back the addresses to paste into the other side. Every value in that
 * panel comes from the server rather than being retyped here, so it cannot
 * drift from what is actually served.
 *
 * There is no delete. Disabling stops an application immediately and leaves
 * the audit trail pointing at something that still exists.
 */

import { useCallback, useEffect, useState } from "react";

import { applicationApi } from "../api/endpoints";
import type {
  CASService,
  IntegrationEndpoints,
  OAuthClient,
  Protocol,
  RegisteredClient,
  SAMLServiceProvider,
} from "../api/types";
import {
  Alert,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  CopyField,
  EmptyRow,
  Field,
  Input,
  Modal,
  PageHeader,
  Select,
  Table,
  Td,
  Textarea,
  Th,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

export function ApplicationsPage() {
  const t = useT();

  const describeError = useErrorMessage();
  const [protocol, setProtocol] = useState<Protocol>("oauth");
  const [error, setError] = useState("");
  const [endpoints, setEndpoints] = useState<IntegrationEndpoints | null>(null);
  const [showEndpoints, setShowEndpoints] = useState(false);

  const [clients, setClients] = useState<OAuthClient[]>([]);
  const [providers, setProviders] = useState<SAMLServiceProvider[]>([]);
  const [casServices, setCASServices] = useState<CASService[]>([]);
  const [loading, setLoading] = useState(true);

  // The secret shown exactly once, after registering or rotating. It is held
  // in state rather than fetched, because it does not exist anywhere to
  // fetch from: only its hash is stored.
  const [issuedSecret, setIssuedSecret] = useState<RegisteredClient | null>(
    null,
  );

  const [editingClient, setEditingClient] = useState<OAuthClient | null>(null);
  const [editingProvider, setEditingProvider] =
    useState<SAMLServiceProvider | null>(null);
  const [editingCAS, setEditingCAS] = useState<CASService | null>(null);
  const [creating, setCreating] = useState(false);

  const [confirming, setConfirming] = useState<{
    label: string;
    enable: boolean;
    run: () => Promise<unknown>;
  } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [oauth, saml, cas] = await Promise.all([
        applicationApi.oauth.list(),
        applicationApi.saml.list(),
        applicationApi.cas.list(),
      ]);
      setClients(oauth);
      setProviders(saml);
      setCASServices(cas);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setLoading(false);
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  // Loaded once and kept: these addresses only change when the deployment's
  // public URL does, which cannot happen while somebody is looking at them.
  useEffect(() => {
    applicationApi
      .integrationEndpoints()
      .then(setEndpoints)
      .catch(() => {
        // A panel of reference addresses is not worth an error banner over
        // the whole screen; the button that opens it simply stays disabled.
      });
  }, []);

  async function runAction(action: () => Promise<unknown>) {
    setConfirming(null);
    setError("");
    try {
      await action();
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  const counts: Record<Protocol, number> = {
    oauth: clients.length,
    saml: providers.length,
    cas: casServices.length,
  };

  return (
    <>
      <PageHeader
        title={t("applications.title")}
        subtitle={t("applications.subtitle")}
        actions={
          <>
            <Button
              variant="secondary"
              disabled={endpoints === null}
              onClick={() => setShowEndpoints(true)}
            >
              {t("applications.endpoints")}
            </Button>
            <Button onClick={() => setCreating(true)}>
              {t(`applications.create.${protocol}`)}
            </Button>
          </>
        }
      />

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      {/* The tab pattern is only complete with the panel: a role="tab" with
          nothing associated announces itself as a tab and then leads
          nowhere, which is worse for a screen reader than plain buttons. */}
      <div
        role="tablist"
        aria-label={t("applications.protocol")}
        className="mb-4 flex gap-1 border-b border-[var(--color-border)]"
      >
        {(["oauth", "saml", "cas"] as const).map((value) => (
          <button
            key={value}
            id={`protocol-tab-${value}`}
            role="tab"
            type="button"
            aria-selected={protocol === value}
            aria-controls="protocol-panel"
            onClick={() => setProtocol(value)}
            className={
              protocol === value
                ? "-mb-px border-b-2 border-[var(--color-primary)] px-4 py-2 text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]"
                : "-mb-px border-b-2 border-transparent px-4 py-2 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)] hover:text-[var(--color-fg)]"
            }
          >
            {t(`applications.tab.${value}`)}
            {/* Hidden from the accessible name, which would otherwise read
                "OAuth 2.1 / OIDC2" — the count running into the label. The
                same number is in the table below it. */}
            <span
              aria-hidden="true"
              className="ml-1.5 text-[length:var(--font-size-xs)] text-[var(--color-fg-muted)]"
            >
              {counts[value]}
            </span>
          </button>
        ))}
      </div>

      <div
        id="protocol-panel"
        role="tabpanel"
        aria-labelledby={`protocol-tab-${protocol}`}
      >
        <p className="mb-4 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
          {t(`applications.hint.${protocol}`)}
        </p>

        {protocol === "oauth" && (
          <Table>
            <thead>
              <tr>
                <Th>{t("applications.colName")}</Th>
                <Th>{t("applications.colClientId")}</Th>
                <Th>{t("applications.colType")}</Th>
                <Th>{t("applications.colRedirects")}</Th>
                <Th>{t("applications.colStatus")}</Th>
                <Th>{t("common.actions")}</Th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <Td className="py-10 text-center">{t("common.loading")}</Td>
                </tr>
              ) : clients.length === 0 ? (
                <EmptyRow colSpan={6} />
              ) : (
                clients.map((client) => (
                  <tr key={client.id}>
                    <Td>{client.name}</Td>
                    <Td>
                      <code className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                        {client.clientId}
                      </code>
                    </Td>
                    <Td>
                      <Badge tone={client.confidential ? "neutral" : "warning"}>
                        {client.confidential
                          ? t("applications.confidential")
                          : t("applications.public")}
                      </Badge>
                    </Td>
                    <Td>
                      <UriList values={client.redirectUris} />
                    </Td>
                    <Td>
                      <StatusBadge status={client.status} />
                    </Td>
                    <Td>
                      <div className="flex gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setEditingClient(client)}
                        >
                          {t("common.edit")}
                        </Button>
                        {client.confidential && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() =>
                              setConfirming({
                                label: t(
                                  "applications.confirmRotate",
                                  client.name,
                                ),
                                enable: false,
                                run: async () => {
                                  const rotated =
                                    await applicationApi.oauth.rotateSecret(
                                      client.clientId,
                                    );
                                  setIssuedSecret(rotated);
                                },
                              })
                            }
                          >
                            {t("applications.rotateSecret")}
                          </Button>
                        )}
                        <StatusButton
                          active={client.status === "ACTIVE"}
                          onToggle={(enable) =>
                            setConfirming({
                              label: enable
                                ? t("applications.confirmEnable", client.name)
                                : t("applications.confirmDisable", client.name),
                              enable,
                              run: () =>
                                enable
                                  ? applicationApi.oauth.enable(client.clientId)
                                  : applicationApi.oauth.disable(
                                      client.clientId,
                                    ),
                            })
                          }
                        />
                      </div>
                    </Td>
                  </tr>
                ))
              )}
            </tbody>
          </Table>
        )}

        {protocol === "saml" && (
          <Table>
            <thead>
              <tr>
                <Th>{t("applications.colName")}</Th>
                <Th>{t("applications.colEntityId")}</Th>
                <Th>{t("applications.colAcs")}</Th>
                <Th>{t("applications.colStatus")}</Th>
                <Th>{t("common.actions")}</Th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <Td className="py-10 text-center">{t("common.loading")}</Td>
                </tr>
              ) : providers.length === 0 ? (
                <EmptyRow colSpan={5} />
              ) : (
                providers.map((provider) => (
                  <tr key={provider.id}>
                    <Td>{provider.name}</Td>
                    <Td>
                      <code className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                        {provider.entityId}
                      </code>
                    </Td>
                    <Td>
                      <UriList values={provider.acsUrls} />
                    </Td>
                    <Td>
                      <StatusBadge status={provider.status} />
                    </Td>
                    <Td>
                      <div className="flex gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setEditingProvider(provider)}
                        >
                          {t("common.edit")}
                        </Button>
                        <StatusButton
                          active={provider.status === "ACTIVE"}
                          onToggle={(enable) =>
                            setConfirming({
                              label: enable
                                ? t("applications.confirmEnable", provider.name)
                                : t(
                                    "applications.confirmDisable",
                                    provider.name,
                                  ),
                              enable,
                              run: () =>
                                enable
                                  ? applicationApi.saml.enable(provider.id)
                                  : applicationApi.saml.disable(provider.id),
                            })
                          }
                        />
                      </div>
                    </Td>
                  </tr>
                ))
              )}
            </tbody>
          </Table>
        )}

        {protocol === "cas" && (
          <Table>
            <thead>
              <tr>
                <Th>{t("applications.colName")}</Th>
                <Th>{t("applications.colPrefix")}</Th>
                <Th>{t("applications.colStatus")}</Th>
                <Th>{t("common.actions")}</Th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <Td className="py-10 text-center">{t("common.loading")}</Td>
                </tr>
              ) : casServices.length === 0 ? (
                <EmptyRow colSpan={4} />
              ) : (
                casServices.map((svc) => (
                  <tr key={svc.id}>
                    <Td>{svc.name}</Td>
                    <Td>
                      <code className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                        {svc.urlPrefix}
                      </code>
                    </Td>
                    <Td>
                      <StatusBadge status={svc.status} />
                    </Td>
                    <Td>
                      <div className="flex gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setEditingCAS(svc)}
                        >
                          {t("common.edit")}
                        </Button>
                        <StatusButton
                          active={svc.status === "ACTIVE"}
                          onToggle={(enable) =>
                            setConfirming({
                              label: enable
                                ? t("applications.confirmEnable", svc.name)
                                : t("applications.confirmDisable", svc.name),
                              enable,
                              run: () =>
                                enable
                                  ? applicationApi.cas.enable(svc.id)
                                  : applicationApi.cas.disable(svc.id),
                            })
                          }
                        />
                      </div>
                    </Td>
                  </tr>
                ))
              )}
            </tbody>
          </Table>
        )}
      </div>

      <ClientFormDialog
        open={(creating && protocol === "oauth") || editingClient !== null}
        client={editingClient}
        onClose={() => {
          setCreating(false);
          setEditingClient(null);
        }}
        onSaved={(registered) => {
          setCreating(false);
          setEditingClient(null);
          if (registered?.secret) setIssuedSecret(registered);
          void load();
        }}
      />

      <ServiceProviderFormDialog
        open={(creating && protocol === "saml") || editingProvider !== null}
        provider={editingProvider}
        onClose={() => {
          setCreating(false);
          setEditingProvider(null);
        }}
        onSaved={() => {
          setCreating(false);
          setEditingProvider(null);
          void load();
        }}
      />

      <CASFormDialog
        open={(creating && protocol === "cas") || editingCAS !== null}
        service={editingCAS}
        onClose={() => {
          setCreating(false);
          setEditingCAS(null);
        }}
        onSaved={() => {
          setCreating(false);
          setEditingCAS(null);
          void load();
        }}
      />

      <SecretDialog
        issued={issuedSecret}
        onClose={() => setIssuedSecret(null)}
      />

      <EndpointsDialog
        open={showEndpoints}
        endpoints={endpoints}
        onClose={() => setShowEndpoints(false)}
      />

      <ConfirmDialog
        open={confirming !== null}
        title={t("applications.confirmTitle")}
        message={confirming?.label ?? ""}
        destructive={confirming !== null && !confirming.enable}
        onConfirm={() => confirming && void runAction(confirming.run)}
        onCancel={() => setConfirming(null)}
      />
    </>
  );
}

function StatusBadge({ status }: { status: "ACTIVE" | "DISABLED" }) {
  const t = useT();
  return (
    <Badge tone={status === "ACTIVE" ? "success" : "danger"}>
      {t(`status.${status}`)}
    </Badge>
  );
}

function StatusButton({
  active,
  onToggle,
}: {
  active: boolean;
  onToggle: (enable: boolean) => void;
}) {
  const t = useT();
  return (
    <Button size="sm" variant="ghost" onClick={() => onToggle(!active)}>
      {active ? t("common.disable") : t("common.enable")}
    </Button>
  );
}

/**
 * A list of addresses in a table cell.
 *
 * They are shown in full rather than truncated to the first one: a redirect
 * URI or an assertion consumer service is the security-relevant part of a
 * registration, and hiding the rest behind a count is how a URI nobody meant
 * to add goes unnoticed.
 */
function UriList({ values }: { values: string[] }) {
  if (values.length === 0) return <span>—</span>;
  return (
    <div className="flex flex-col gap-0.5">
      {values.map((value) => (
        <code
          key={value}
          className="text-[length:var(--font-size-xs)] break-all text-[var(--color-fg-muted)]"
        >
          {value}
        </code>
      ))}
    </div>
  );
}

/** Splits a textarea of one-per-line values, dropping blanks. */
function lines(value: string): string[] {
  return value
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");
}

function ClientFormDialog({
  open,
  client,
  onClose,
  onSaved,
}: {
  open: boolean;
  client: OAuthClient | null;
  onClose: () => void;
  onSaved: (registered: RegisteredClient | null) => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const isEdit = client !== null;

  const [form, setForm] = useState({
    clientId: "",
    name: "",
    public: false,
    applicationType: "WEB",
    redirectUris: "",
    postLogoutRedirectUris: "",
    scopes: "openid profile email",
    launchUrl: "",
  });
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setError("");
    setForm({
      clientId: client?.clientId ?? "",
      name: client?.name ?? "",
      public: client ? !client.confidential : false,
      applicationType: client?.applicationType ?? "WEB",
      redirectUris: (client?.redirectUris ?? []).join("\n"),
      postLogoutRedirectUris: (client?.postLogoutRedirectUris ?? []).join("\n"),
      scopes: (client?.scopes ?? ["openid", "profile", "email"]).join(" "),
      launchUrl: client?.launchUrl ?? "",
    });
  }, [open, client]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      const shared = {
        name: form.name,
        applicationType: form.applicationType,
        redirectUris: lines(form.redirectUris),
        postLogoutRedirectUris: lines(form.postLogoutRedirectUris),
        scopes: form.scopes.split(/\s+/).filter((s) => s !== ""),
        launchUrl: form.launchUrl,
      };
      if (isEdit && client) {
        await applicationApi.oauth.update(client.clientId, shared);
        onSaved(null);
      } else {
        const registered = await applicationApi.oauth.create({
          ...shared,
          clientId: form.clientId,
          public: form.public,
        });
        onSaved(registered);
      }
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
        isEdit ? t("applications.editOauth") : t("applications.create.oauth")
      }
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button form="client-form" type="submit" disabled={submitting}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form
        id="client-form"
        onSubmit={handleSubmit}
        className="flex flex-col gap-4"
      >
        {/* The client id is what the application presents at the token
            endpoint. Changing it would break every deployment of that
            application rather than reconfigure it, so it is set once. */}
        <Field
          label={t("applications.clientId")}
          hint={isEdit ? t("applications.clientIdFixed") : undefined}
          required={!isEdit}
        >
          <Input
            value={form.clientId}
            onChange={(e) =>
              setForm((f) => ({ ...f, clientId: e.target.value }))
            }
            disabled={isEdit}
            required={!isEdit}
          />
        </Field>

        <Field label={t("applications.name")}>
          <Input
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          />
        </Field>

        {!isEdit && (
          <Field
            label={t("applications.clientKind")}
            hint={t("applications.clientKindHelp")}
          >
            <Select
              value={form.public ? "public" : "confidential"}
              onChange={(e) =>
                setForm((f) => ({ ...f, public: e.target.value === "public" }))
              }
            >
              <option value="confidential">
                {t("applications.confidential")}
              </option>
              <option value="public">{t("applications.public")}</option>
            </Select>
          </Field>
        )}

        <Field label={t("applications.applicationType")}>
          <Select
            value={form.applicationType}
            onChange={(e) =>
              setForm((f) => ({ ...f, applicationType: e.target.value }))
            }
          >
            <option value="WEB">{t("applications.typeWeb")}</option>
            <option value="NATIVE">{t("applications.typeNative")}</option>
            <option value="USER_AGENT">
              {t("applications.typeUserAgent")}
            </option>
          </Select>
        </Field>

        <Field
          label={t("applications.redirectUris")}
          hint={t("applications.redirectUrisHelp")}
          required
        >
          <Textarea
            rows={3}
            value={form.redirectUris}
            onChange={(e) =>
              setForm((f) => ({ ...f, redirectUris: e.target.value }))
            }
            required
            placeholder="https://app.example.com/callback"
          />
        </Field>

        <Field
          label={t("applications.postLogoutUris")}
          hint={t("applications.postLogoutUrisHelp")}
        >
          <Textarea
            rows={2}
            value={form.postLogoutRedirectUris}
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                postLogoutRedirectUris: e.target.value,
              }))
            }
          />
        </Field>

        <Field
          label={t("applications.scopes")}
          hint={t("applications.scopesHelp")}
        >
          <Input
            value={form.scopes}
            onChange={(e) => setForm((f) => ({ ...f, scopes: e.target.value }))}
          />
        </Field>

        <Field
          label={`${t("applications.launchUrl")} (${t("common.optional")})`}
          hint={t("applications.launchUrlHelp")}
        >
          <Input
            value={form.launchUrl}
            onChange={(e) =>
              setForm((f) => ({ ...f, launchUrl: e.target.value }))
            }
            placeholder="https://app.example.com/"
          />
        </Field>

        {error && <Alert tone="danger">{error}</Alert>}
      </form>
    </Modal>
  );
}

function ServiceProviderFormDialog({
  open,
  provider,
  onClose,
  onSaved,
}: {
  open: boolean;
  provider: SAMLServiceProvider | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const isEdit = provider !== null;

  const [form, setForm] = useState({
    name: "",
    metadataXml: "",
    launchUrl: "",
  });
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setError("");
    setForm({
      name: provider?.name ?? "",
      metadataXml: provider?.metadataXml ?? "",
      launchUrl: provider?.launchUrl ?? "",
    });
  }, [open, provider]);

  // Reading the file in the browser rather than sending a URL for the server
  // to fetch. A "fetch metadata from this address" field would make the
  // server issue requests to addresses an administrator names, which is a
  // server-side request forgery against whatever else the server can reach.
  function handleFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;
    file
      .text()
      .then((text) => setForm((f) => ({ ...f, metadataXml: text })))
      .catch(() => setError(t("applications.metadataReadFailed")));
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      if (isEdit && provider) {
        await applicationApi.saml.update(provider.id, form);
      } else {
        await applicationApi.saml.create(form);
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
        isEdit ? t("applications.editSaml") : t("applications.create.saml")
      }
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button form="sp-form" type="submit" disabled={submitting}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form
        id="sp-form"
        onSubmit={handleSubmit}
        className="flex flex-col gap-4"
      >
        <Field label={t("applications.name")}>
          <Input
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          />
        </Field>

        {isEdit && provider && (
          <Field
            label={t("applications.entityId")}
            hint={t("applications.entityIdFixed")}
          >
            <Input value={provider.entityId} disabled />
          </Field>
        )}

        <Field
          label={t("applications.metadata")}
          hint={
            isEdit
              ? t("applications.metadataReplaceHelp")
              : t("applications.metadataHelp")
          }
          required={!isEdit}
        >
          <div className="flex flex-col gap-2">
            <input
              type="file"
              accept=".xml,application/xml,text/xml"
              onChange={handleFile}
              className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]"
            />
            <Textarea
              rows={8}
              value={form.metadataXml}
              onChange={(e) =>
                setForm((f) => ({ ...f, metadataXml: e.target.value }))
              }
              required={!isEdit}
              placeholder="<EntityDescriptor …>"
              className="font-mono"
            />
          </div>
        </Field>

        <Field
          label={`${t("applications.launchUrl")} (${t("common.optional")})`}
          hint={t("applications.launchUrlHelp")}
        >
          <Input
            value={form.launchUrl}
            onChange={(e) =>
              setForm((f) => ({ ...f, launchUrl: e.target.value }))
            }
            placeholder="https://app.example.com/"
          />
        </Field>

        {error && <Alert tone="danger">{error}</Alert>}
      </form>
    </Modal>
  );
}

function CASFormDialog({
  open,
  service,
  onClose,
  onSaved,
}: {
  open: boolean;
  service: CASService | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const isEdit = service !== null;

  const [form, setForm] = useState({
    name: "",
    urlPrefix: "",
    launchUrl: "",
  });
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setError("");
    setForm({
      name: service?.name ?? "",
      urlPrefix: service?.urlPrefix ?? "",
      launchUrl: service?.launchUrl ?? "",
    });
  }, [open, service]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      if (isEdit && service) {
        await applicationApi.cas.update(service.id, form);
      } else {
        await applicationApi.cas.create(form);
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
      title={isEdit ? t("applications.editCas") : t("applications.create.cas")}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button form="cas-form" type="submit" disabled={submitting}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form
        id="cas-form"
        onSubmit={handleSubmit}
        className="flex flex-col gap-4"
      >
        <Field label={t("applications.name")}>
          <Input
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          />
        </Field>

        <Field
          label={t("applications.urlPrefix")}
          hint={t("applications.urlPrefixHelp")}
          required
        >
          <Input
            value={form.urlPrefix}
            onChange={(e) =>
              setForm((f) => ({ ...f, urlPrefix: e.target.value }))
            }
            required
            placeholder="https://wiki.example.com/"
          />
        </Field>

        <Field
          label={`${t("applications.launchUrl")} (${t("common.optional")})`}
          hint={t("applications.launchUrlHelp")}
        >
          <Input
            value={form.launchUrl}
            onChange={(e) =>
              setForm((f) => ({ ...f, launchUrl: e.target.value }))
            }
            placeholder="https://app.example.com/"
          />
        </Field>

        {error && <Alert tone="danger">{error}</Alert>}
      </form>
    </Modal>
  );
}

/**
 * The one and only showing of a client secret.
 *
 * It cannot be reopened, because the value is not stored anywhere — only a
 * bcrypt hash is. The copy says so plainly rather than leaving somebody to
 * discover it by closing the dialog.
 */
function SecretDialog({
  issued,
  onClose,
}: {
  issued: RegisteredClient | null;
  onClose: () => void;
}) {
  const t = useT();

  return (
    <Modal
      open={issued !== null}
      title={t("applications.secretTitle")}
      onClose={onClose}
      footer={<Button onClick={onClose}>{t("common.close")}</Button>}
    >
      <div className="flex flex-col gap-4">
        <Alert tone="danger">{t("applications.secretWarning")}</Alert>
        {issued && (
          <>
            <CopyField
              label={t("applications.clientId")}
              value={issued.client.clientId}
            />
            <CopyField
              label={t("applications.clientSecret")}
              value={issued.secret ?? ""}
            />
          </>
        )}
      </div>
    </Modal>
  );
}

/**
 * What to configure at the other end.
 *
 * Grouped by protocol rather than shown as one long list, because an
 * integrator is configuring exactly one of them and the other two are noise.
 */
function EndpointsDialog({
  open,
  endpoints,
  onClose,
}: {
  open: boolean;
  endpoints: IntegrationEndpoints | null;
  onClose: () => void;
}) {
  const t = useT();

  return (
    <Modal
      open={open && endpoints !== null}
      title={t("applications.endpointsTitle")}
      onClose={onClose}
      footer={<Button onClick={onClose}>{t("common.close")}</Button>}
    >
      {endpoints && (
        <div className="flex flex-col gap-5">
          <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("applications.endpointsHelp")}
          </p>

          <Card title={t("applications.tab.oauth")}>
            <div className="flex flex-col gap-3">
              <CopyField
                label={t("applications.issuer")}
                value={endpoints.issuer}
              />
              <CopyField
                label={t("applications.discovery")}
                value={endpoints.oidc.discovery}
              />
              <CopyField
                label={t("applications.authorizeEndpoint")}
                value={endpoints.oidc.authorize}
              />
              <CopyField
                label={t("applications.tokenEndpoint")}
                value={endpoints.oidc.token}
              />
              <CopyField
                label={t("applications.userinfoEndpoint")}
                value={endpoints.oidc.userinfo}
              />
              <CopyField
                label={t("applications.jwks")}
                value={endpoints.oidc.jwks}
              />
            </div>
          </Card>

          <Card title={t("applications.tab.saml")}>
            <div className="flex flex-col gap-3">
              <CopyField
                label={t("applications.samlEntityId")}
                value={endpoints.saml.entityId}
              />
              <CopyField
                label={t("applications.samlMetadata")}
                value={endpoints.saml.metadata}
              />
              <CopyField
                label={t("applications.samlSso")}
                value={endpoints.saml.sso}
              />
              {endpoints.saml.certificatePem && (
                <CopyField
                  label={t("applications.samlCertificate")}
                  value={endpoints.saml.certificatePem}
                  multiline
                />
              )}
            </div>
          </Card>

          <Card title={t("applications.tab.cas")}>
            <div className="flex flex-col gap-3">
              <CopyField
                label={t("applications.casBaseUrl")}
                value={endpoints.cas.baseUrl}
              />
              <CopyField
                label={t("applications.casLogin")}
                value={endpoints.cas.login}
              />
              <CopyField
                label={t("applications.casValidate")}
                value={endpoints.cas.serviceValidate}
              />
            </div>
          </Card>
        </div>
      )}
    </Modal>
  );
}
