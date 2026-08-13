/** Typed wrappers around the API. One function per endpoint. */

import { download, request, upload } from "./client";
import type {
  AdministeredOrganization,
  AdminScope,
  AuditLog,
  Authorization,
  CASService,
  ImportResult,
  IntegrationEndpoints,
  BulkResult,
  IssuedSCIMCredential,
  LDAPSource,
  LDAPSourceInput,
  LDAPSyncRun,
  LogKind,
  OAuthClient,
  Organization,
  OrganizationAdministrator,
  PageResult,
  PortalApplication,
  RecoveryChannel,
  RegisteredClient,
  RegistrationStatus,
  UserProfile,
  UserAttributeDefinition,
  UserAttributeInput,
  Role,
  CreatedWebhookSubscription,
  Group,
  GroupMember,
  GroupRef,
  SAMLServiceProvider,
  SCIMCredential,
  Session,
  Settings,
  Status,
  User,
  UserSession,
  WebhookDelivery,
  WebhookSnapshot,
  WebhookSubscription,
  CatalogueField,
  FieldMapping,
  RecipientKind,
  ExternalIdentity,
  ExternalIdentityProvider,
  ExternalIdentityProviderInput,
  ExternalSignInOption,
  ExternalSignInResult,
} from "./types";

/** Builds a query string, omitting empty values. */
function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const encoded = search.toString();
  return encoded ? `?${encoded}` : "";
}

export const authApi = {
  /**
   * Signs in. The identifier is a username, email address, or phone number
   * — the server works out which. An empty tenant means the default one,
   * which is what a single-tenant deployment always sends.
   */
  login: (tenant: string, identifier: string, password: string) =>
    request<Session>("/auth/login", {
      method: "POST",
      body: { tenant, identifier, password },
      anonymous: true,
    }),

  /**
   * The buttons a tenant's sign-in screen offers.
   *
   * Asked unconditionally and answered with an empty list where nothing is
   * configured, so the screen has one code path rather than two.
   */
  externalOptions: () =>
    request<ExternalSignInOption[]>("/auth/external/providers", {
      anonymous: true,
    }),

  /**
   * Begins a sign-in through somebody else's provider.
   *
   * Answers with an address rather than a redirect: the caller is a fetch,
   * and a 302 here would be followed inside it while the page stayed put.
   */
  startExternalSignIn: (provider: string, tenant: string) =>
    request<{ authorizationUrl: string }>("/auth/external/start", {
      method: "POST",
      body: { provider, tenant },
      anonymous: true,
    }),

  /**
   * Spends the `state` and `code` a browser came back holding.
   *
   * Single-use on the server: the row is deleted by the statement that
   * reads it, so calling this twice with the same state fails the second
   * time. Callers must make sure there is no second time.
   *
   * The tenant is passed rather than taken from storage. It comes from the
   * address the provider redirected to, and an exchange that fails — which
   * is what a reload or a stale link produces — must not have repointed the
   * browser's remembered tenant on its way through.
   */
  completeExternalSignIn: (state: string, code: string, tenant: string) =>
    request<ExternalSignInResult>(
      `/auth/external/callback${query({ state, code, tenant })}`,
      { anonymous: true },
    ),

  /** Ends this session. Other devices stay signed in. */
  logout: () => request<null>("/auth/logout", { method: "POST" }),

  /** Ends every session the account holds, everywhere. */
  logoutEverywhere: () =>
    request<null>("/auth/logout-everywhere", { method: "POST" }),

  register: (input: {
    username: string;
    displayName: string;
    password: string;
    phone?: string;
    email?: string;
  }) =>
    request<User & { verificationRequired?: boolean }>("/auth/register", {
      method: "POST",
      body: input,
      anonymous: true,
    }),

  /**
   * Redeems a confirmation link. Public, because the account cannot sign in
   * until it succeeds — which is the point of it.
   */
  confirmRegistration: (token: string, tenant: string) =>
    request<{ verified: boolean }>("/auth/register/verify", {
      method: "POST",
      body: { token, tenant },
      anonymous: true,
    }),

  /**
   * Asks for another confirmation link.
   *
   * Always succeeds, whether or not the address belongs to anybody — the
   * endpoint is public, so telling the difference would make it a way to
   * find out who has an account here. Callers must not report anything
   * other than "if that address has an account, a message is on its way".
   */
  resendVerification: (destination: string, tenant: string) =>
    request<{ sent: boolean }>("/auth/register/verify/resend", {
      method: "POST",
      body: { destination, tenant },
      anonymous: true,
    }),

  /**
   * Whether sign-up is open, and what the tenant calls itself. The signal
   * lets the sign-in screen drop a lookup for a tenant the user has since
   * changed, so a slow earlier response cannot overwrite a newer one.
   */
  registrationStatus: (signal?: AbortSignal) =>
    request<RegistrationStatus>("/auth/registration-status", {
      anonymous: true,
      signal,
    }),

  /**
   * Which recovery channels this deployment can actually use, and how long
   * a link lasts. The lifetime comes from the server so the copy on screen
   * cannot drift from the constant that enforces it.
   */
  recoveryChannels: (signal?: AbortSignal) =>
    request<{ channels: RecoveryChannel[]; tokenTtlMinutes: number }>(
      "/auth/recovery-channels",
      { anonymous: true, signal },
    ),

  /**
   * Asks for a reset link. The response is the same whether or not an
   * account matched — the server will not say, and neither should the UI.
   */
  requestPasswordRecovery: (channel: RecoveryChannel, destination: string) =>
    request<{ message: string }>("/auth/password-recovery", {
      method: "POST",
      body: { channel, destination },
      anonymous: true,
    }),

  /**
   * Replaces a password that has aged out, and signs in.
   *
   * Public because the caller cannot sign in: the server refuses to issue a
   * token for an expired password rather than handing one out with a flag
   * and trusting the client to act on it.
   */
  changeExpiredPassword: (input: {
    tenant: string;
    identifier: string;
    currentPassword: string;
    newPassword: string;
  }) =>
    request<Session>("/auth/password/expired", {
      method: "POST",
      body: input,
      anonymous: true,
    }),

  /** Redeems a reset link. The tenant travels in the link, not here. */
  confirmPasswordRecovery: (token: string, newPassword: string) =>
    request<{ reauthenticationRequired: boolean }>(
      "/auth/password-recovery/confirm",
      { method: "POST", body: { token, newPassword }, anonymous: true },
    ),
};

/**
 * The value the organization filter takes to mean "the accounts in no
 * organization at all".
 *
 * An empty string cannot say it, because an empty string already means every
 * organization. Reserved on the server for the same reason it is safe here:
 * a real organization's id is a UUID and can never be this.
 */
export const UNASSIGNED_ORGANIZATION = "none";

export const userApi = {
  me: () => request<User>("/users/me"),

  /**
   * Updates the caller's own details. Role, status, organization, and
   * username are absent on purpose — the server rejects them outright.
   */
  updateOwnProfile: (input: {
    displayName: string;
    phone: string;
    email: string;
  }) => request<User>("/users/me", { method: "PUT", body: input }),

  /** The caller's own live sessions, most recently used first. */
  ownSessions: () => request<UserSession[]>("/users/me/sessions"),

  revokeOwnSession: (sessionId: string) =>
    request<null>(`/users/me/sessions/${sessionId}`, { method: "DELETE" }),

  /** What is signed in as somebody, for an administrator. */
  sessionsFor: (id: string) => request<UserSession[]>(`/users/${id}/sessions`),

  revokeSessionFor: (id: string, sessionId: string) =>
    request<null>(`/users/${id}/sessions/${sessionId}`, { method: "DELETE" }),

  /**
   * Closes the caller's own account. The token this is sent with is dead by
   * the time the response arrives, so callers sign out immediately after
   * rather than making another request with it.
   */
  closeOwnAccount: (password: string) =>
    request<{ closed: boolean }>("/users/me/close", {
      method: "POST",
      body: { password },
    }),

  changeOwnPassword: (currentPassword: string, newPassword: string) =>
    request<{ reauthenticationRequired: boolean }>("/users/me/password", {
      method: "POST",
      body: { currentPassword, newPassword },
    }),

  /**
   * `organizationId` selects an organization **and everything under it**,
   * not that organization on its own — the server walks the chart. Omit it
   * for all of them, or pass {@link UNASSIGNED_ORGANIZATION} for the
   * accounts in none.
   */
  list: (params: {
    page?: number;
    pageSize?: number;
    keyword?: string;
    status?: Status | "";
    role?: Role | "";
    organizationId?: string;
  }) => request<PageResult<User>>(`/users${query(params)}`),

  get: (id: string) => request<User>(`/users/${id}`),

  create: (input: {
    username: string;
    displayName: string;
    password: string;
    phone?: string;
    email?: string;
    role: Role;
    organizationId?: string;
  }) => request<User>("/users", { method: "POST", body: input }),

  update: (
    id: string,
    input: {
      displayName: string;
      phone?: string;
      email?: string;
      role: Role;
      organizationId?: string;
    },
  ) => request<User>(`/users/${id}`, { method: "PUT", body: input }),

  enable: (id: string) =>
    request<User>(`/users/${id}/enable`, { method: "POST" }),
  disable: (id: string) =>
    request<User>(`/users/${id}/disable`, { method: "POST" }),

  /** Clears a lockout, leaving the password alone. */
  unlock: (id: string) =>
    request<User>(`/users/${id}/unlock`, { method: "POST" }),

  resetPassword: (id: string, newPassword: string) =>
    request<null>(`/users/${id}/password`, {
      method: "POST",
      body: { newPassword },
    }),

  importUsers: (file: File) => upload<ImportResult>("/users/import", file),

  downloadTemplate: () =>
    download("/users/import/template", "portico-user-import-template.xlsx"),

  /**
   * The tenant's accounts as a spreadsheet, in the same columns the import
   * template uses — so a file taken out can be edited and fed back in.
   *
   * Takes the same filters the list does, so "export what I am looking at"
   * is one call rather than a second, subtly different notion of filtering.
   */
  exportUsers: (query: Record<string, string> = {}) => {
    const params = new URLSearchParams(
      Object.entries(query).filter(([, value]) => value !== ""),
    );
    const suffix = params.toString() === "" ? "" : `?${params}`;
    return download(`/users/export${suffix}`, "portico-users.xlsx");
  },

  /** The descriptive attributes, which cannot reach role or status. */
  setProfile: (id: string, profile: UserProfile) =>
    request<User>(`/users/${segment(id)}/profile`, {
      method: "PUT",
      body: profile,
    }),

  setOwnProfile: (profile: UserProfile) =>
    request<User>("/users/me/profile", { method: "PUT", body: profile }),

  /**
   * This account's answers to the tenant's own attributes, keyed by
   * attribute key — the same key a mapping stores, not the row id, so a
   * caller reading both never has to join them.
   *
   * Retired attributes are left out, which is why the values editor does not
   * filter them itself.
   */
  attributes: (id: string) =>
    request<Record<string, string>>(`/users/${segment(id)}/attributes`),

  /**
   * Writes the answers. A key left out of the map is left alone; a key sent
   * empty is cleared, because "never filled in" and "deliberately blank" are
   * the same answer here, and keeping a row for the second would leave
   * something nobody can tell apart from a typo.
   */
  setAttributes: (id: string, values: Record<string, string>) =>
    request<Record<string, string>>(`/users/${segment(id)}/attributes`, {
      method: "PUT",
      body: { values },
    }),

  /**
   * Enables or disables several accounts.
   *
   * Answers 200 with a per-account result even when some failed: an operator
   * who selected forty people and hit one they may not disable needs to know
   * which one, and wants the other thirty-nine done.
   */
  bulkSetStatus: (userIds: string[], status: Status) =>
    request<BulkResult>("/users/bulk/status", {
      method: "POST",
      body: { userIds, status },
    }),

  bulkSetOrganization: (userIds: string[], organizationId: string) =>
    request<BulkResult>("/users/bulk/organization", {
      method: "POST",
      body: { userIds, organizationId },
    }),
};

export const organizationApi = {
  /**
   * Nominates whoever is responsible for an organization. Grants nothing —
   * this version has two fixed roles and being named here confers neither.
   * An empty id clears the nomination.
   */
  setManager: (id: string, managerId: string) =>
    request<Organization>(`/organizations/${segment(id)}/manager`, {
      method: "PUT",
      body: { managerId },
    }),

  /**
   * Who is recorded as administering an organization.
   *
   * These grant nothing today — see the API description. They exist so that
   * delegated administration, when it arrives, reads a chart somebody has
   * already entered rather than an empty table.
   */
  administrators: (id: string) =>
    request<OrganizationAdministrator[]>(
      `/organizations/${segment(id)}/administrators`,
    ),

  assignAdministrator: (id: string, userId: string, scope: AdminScope) =>
    request<null>(`/organizations/${segment(id)}/administrators`, {
      method: "POST",
      body: { userId, scope },
    }),

  revokeAdministrator: (id: string, userId: string) =>
    request<null>(
      `/organizations/${segment(id)}/administrators/${segment(userId)}`,
      { method: "DELETE" },
    ),

  /** What an account is recorded as administering. */
  administeredBy: (userId: string) =>
    request<AdministeredOrganization[]>(
      `/users/${segment(userId)}/administered-organizations`,
    ),

  /** Records that somebody is involved with an organization they do not
   * primarily belong to. Does not move their primary membership. */
  attachUser: (id: string, userId: string) =>
    request<null>(`/organizations/${segment(id)}/attachments`, {
      method: "POST",
      body: { userId },
    }),

  detachUser: (id: string, userId: string) =>
    request<null>(
      `/organizations/${segment(id)}/attachments/${segment(userId)}`,
      { method: "DELETE" },
    ),

  list: (activeOnly = false) =>
    request<Organization[]>(
      `/organizations${activeOnly ? "?activeOnly=true" : ""}`,
    ),

  create: (input: {
    name: string;
    code: string;
    remark?: string;
    parentId?: string;
    sortOrder?: number;
  }) =>
    request<Organization>("/organizations", { method: "POST", body: input }),

  update: (
    id: string,
    input: {
      name: string;
      remark?: string;
      parentId?: string;
      sortOrder?: number;
    },
  ) =>
    request<Organization>(`/organizations/${id}`, {
      method: "PUT",
      body: input,
    }),

  enable: (id: string) =>
    request<Organization>(`/organizations/${id}/enable`, { method: "POST" }),
  disable: (id: string) =>
    request<Organization>(`/organizations/${id}/disable`, { method: "POST" }),
};

export const auditApi = {
  list: (params: {
    page?: number;
    pageSize?: number;
    kind?: LogKind | "";
    action?: string;
    keyword?: string;
    /** One actor, matched exactly — see PROVISIONING_ACTOR below. */
    actor?: string;
    from?: string;
    to?: string;
  }) => request<PageResult<AuditLog>>(`/audit-logs${query(params)}`),
};

/**
 * The actor a directory's changes are recorded under.
 *
 * Not an account, deliberately: attributing a change no person made to a
 * user id that exists would be a lie about who acted. It is
 * `provisioningActor` in internal/service/scim_provision.go, and a test
 * fails if the two stop agreeing.
 */
export const PROVISIONING_ACTOR = "scim";

export const settingsApi = {
  get: () => request<Settings>("/settings"),
  update: (settings: Settings) =>
    request<Settings>("/settings", { method: "PUT", body: settings }),
};

export const oauthApi = {
  /**
   * Completes an authorization request for the signed-in user, and returns
   * where the browser must go next.
   *
   * The subject is never sent: the server takes it from the token, because
   * an endpoint that accepted one would issue tokens for other people.
   */
  authorize: (authRequestId: string) =>
    request<Authorization>("/oauth/authorize", {
      method: "POST",
      body: { authRequestId },
    }),

  /**
   * The same, for SAML. A separate endpoint because the two protocols park
   * their in-flight requests separately, and an id from one is meaningless
   * to the other.
   */
  authenticate: (samlRequestId: string) =>
    request<Authorization>("/saml/authenticate", {
      method: "POST",
      body: { samlRequestId },
    }),

  /**
   * And for CAS, where the request is the service URL itself. The server
   * checks it against the tenant's registrations rather than trusting it.
   */
  casAuthorize: (service: string) =>
    request<Authorization>("/cas/authorize", {
      method: "POST",
      body: { service },
    }),
};

/**
 * Application management.
 *
 * SAML service providers and CAS services are addressed by the
 * registration's own id, not by its entity id or URL prefix. Those are a URI
 * and a URL: percent-encoding one into a path segment works until a reverse
 * proxy normalizes the path, decodes the %2F, and splits the identifier —
 * at which point every request 404s in production and nowhere else. An
 * opaque id has no slashes.
 *
 * OAuth client ids are addressed directly, which is safe because the server
 * restricts them to letters, digits, and . _ -
 */
const segment = encodeURIComponent;

/**
 * The home screen's own view of the applications.
 *
 * Separate from applicationApi because the caller is different: this one is
 * readable by anybody signed in, and returns a name and a link rather than a
 * registration.
 */
export const portalApi = {
  applications: () => request<PortalApplication[]>("/portal/applications"),
};

export const applicationApi = {
  /** What to configure at the other end of an integration. */
  integrationEndpoints: () =>
    request<IntegrationEndpoints>("/applications/integration-endpoints"),

  /**
   * Stores a picture for a tile and returns the path to reference it by.
   *
   * Not nested under a protocol: one picture is one picture whichever of the
   * three an application speaks, and the upload happens before the form is
   * saved — so it cannot belong to a client that does not exist yet.
   *
   * The response is a path rather than an id because the path is what goes into
   * the logoUri field. Which means the console never has to know how the
   * address is spelled; the server decides and says.
   */
  uploadLogo: (file: File) =>
    upload<{ path: string }>("/applications/logos", file),

  oauth: {
    list: () => request<OAuthClient[]>("/applications/oauth-clients"),

    create: (input: {
      clientId: string;
      name: string;
      public: boolean;
      applicationType: string;
      redirectUris: string[];
      postLogoutRedirectUris: string[];
      scopes: string[];
      launchUrl?: string;
    }) =>
      request<RegisteredClient>("/applications/oauth-clients", {
        method: "POST",
        body: input,
      }),

    update: (
      clientId: string,
      input: {
        name: string;
        applicationType: string;
        redirectUris: string[];
        postLogoutRedirectUris: string[];
        scopes: string[];
      },
    ) =>
      request<OAuthClient>(`/applications/oauth-clients/${segment(clientId)}`, {
        method: "PUT",
        body: input,
      }),

    enable: (clientId: string) =>
      request<OAuthClient>(
        `/applications/oauth-clients/${segment(clientId)}/enable`,
        { method: "POST" },
      ),

    disable: (clientId: string) =>
      request<OAuthClient>(
        `/applications/oauth-clients/${segment(clientId)}/disable`,
        { method: "POST" },
      ),

    /** Issues a new secret and invalidates the old one immediately. */
    rotateSecret: (clientId: string) =>
      request<RegisteredClient>(
        `/applications/oauth-clients/${segment(clientId)}/rotate-secret`,
        { method: "POST" },
      ),
  },

  saml: {
    list: () =>
      request<SAMLServiceProvider[]>("/applications/saml-service-providers"),

    create: (input: {
      name: string;
      metadataXml: string;
      launchUrl?: string;
    }) =>
      request<SAMLServiceProvider>("/applications/saml-service-providers", {
        method: "POST",
        body: input,
      }),

    /** Replacing the metadata is how a certificate is rotated. */
    update: (
      id: string,
      input: { name: string; metadataXml: string; launchUrl?: string },
    ) =>
      request<SAMLServiceProvider>(
        `/applications/saml-service-providers/${segment(id)}`,
        { method: "PUT", body: input },
      ),

    enable: (id: string) =>
      request<SAMLServiceProvider>(
        `/applications/saml-service-providers/${segment(id)}/enable`,
        { method: "POST" },
      ),

    disable: (id: string) =>
      request<SAMLServiceProvider>(
        `/applications/saml-service-providers/${segment(id)}/disable`,
        { method: "POST" },
      ),
  },

  cas: {
    list: () => request<CASService[]>("/applications/cas-services"),

    create: (input: { name: string; urlPrefix: string; launchUrl?: string }) =>
      request<CASService>("/applications/cas-services", {
        method: "POST",
        body: input,
      }),

    update: (
      id: string,
      input: { name: string; urlPrefix: string; launchUrl?: string },
    ) =>
      request<CASService>(`/applications/cas-services/${segment(id)}`, {
        method: "PUT",
        body: input,
      }),

    enable: (id: string) =>
      request<CASService>(`/applications/cas-services/${segment(id)}/enable`, {
        method: "POST",
      }),

    disable: (id: string) =>
      request<CASService>(`/applications/cas-services/${segment(id)}/disable`, {
        method: "POST",
      }),
  },
};

/**
 * The credentials a directory provisions with.
 *
 * Its own export rather than part of applicationApi: a provisioning
 * credential is not an application registration. It authenticates a
 * directory, not a relying party, and the two have nothing in common beyond
 * both being things an administrator issues.
 */
export const directoriesApi = {
  list: () => request<LDAPSource[]>("/directories"),

  create: (input: LDAPSourceInput) =>
    request<LDAPSource>("/directories", { method: "POST", body: input }),

  update: (id: string, input: LDAPSourceInput) =>
    request<LDAPSource>(`/directories/${segment(id)}`, {
      method: "PUT",
      body: input,
    }),

  enable: (id: string) =>
    request<LDAPSource>(`/directories/${segment(id)}/enable`, {
      method: "POST",
    }),

  disable: (id: string) =>
    request<LDAPSource>(`/directories/${segment(id)}/disable`, {
      method: "POST",
    }),

  /**
   * Runs the synchronization and waits for it. A failed run answers 200
   * with the reason in the body — the request succeeded, the sync did not —
   * so callers read `outcome` rather than catching.
   */
  sync: (id: string) =>
    request<LDAPSyncRun>(`/directories/${segment(id)}/sync`, {
      method: "POST",
    }),

  runs: (id: string) =>
    request<LDAPSyncRun[]>(`/directories/${segment(id)}/runs`),
};

export const scimCredentialsApi = {
  list: () => request<SCIMCredential[]>("/scim-credentials"),

  create: (name: string) =>
    request<IssuedSCIMCredential>("/scim-credentials", {
      method: "POST",
      body: { name },
    }),

  enable: (id: string) =>
    request<void>(`/scim-credentials/${segment(id)}/enable`, {
      method: "POST",
    }),

  disable: (id: string) =>
    request<void>(`/scim-credentials/${segment(id)}/disable`, {
      method: "POST",
    }),

  remove: (id: string) =>
    request<void>(`/scim-credentials/${segment(id)}`, { method: "DELETE" }),
};

/**
 * Outbound event subscriptions, and the delivery history that answers "we
 * are not receiving anything" without having to ask the receiver.
 */
export const webhooksApi = {
  list: () => request<WebhookSubscription[]>("/webhooks"),

  events: () => request<string[]>("/webhooks/events"),

  /**
   * `headers` are sent with every delivery, for a receiver behind a gateway
   * that wants an Authorization of its own. Write-only: sealed with
   * PORTICO_ENCRYPTION_KEY, and refused outright where no key is
   * configured rather than stored in the clear.
   */
  create: (input: {
    name: string;
    url: string;
    events: string[];
    headers?: Record<string, string>;
  }) =>
    request<CreatedWebhookSubscription>("/webhooks", {
      method: "POST",
      body: input,
    }),

  deliveries: (id: string) =>
    request<WebhookDelivery[]>(`/webhooks/${segment(id)}/deliveries`),

  /**
   * Issues a new signing key, returned once. The subscription keeps its id,
   * so the delivery history and the receiver's deduplication survive.
   *
   * During the overlap each delivery carries both signatures, comma
   * separated — a receiver comparing the whole header as one string
   * verifies nothing until it ends.
   */
  rotateSecret: (id: string) =>
    request<CreatedWebhookSubscription>(
      `/webhooks/${segment(id)}/rotate-secret`,
      { method: "POST" },
    ),

  enable: (id: string) =>
    request<void>(`/webhooks/${segment(id)}/enable`, { method: "POST" }),

  // Queues a copy of everything that already exists. Answers when the pages
  // are queued, not when the receiver has taken them — the delivery list is
  // where progress is read.
  snapshot: (id: string) =>
    request<WebhookSnapshot>(`/webhooks/${segment(id)}/snapshot`, {
      method: "POST",
    }),

  disable: (id: string) =>
    request<void>(`/webhooks/${segment(id)}/disable`, { method: "POST" }),

  remove: (id: string) =>
    request<void>(`/webhooks/${segment(id)}`, { method: "DELETE" }),
};

/**
 * The OpenID Providers this deployment sends people to.
 *
 * Administrative: who may vouch for accounts here is a tenant
 * administrator's decision, not an account holder's.
 */
export const externalIdpApi = {
  list: () =>
    request<ExternalIdentityProvider[]>("/external-identity-providers"),

  /**
   * Registers one. The issuer is contacted before the row is written, so a
   * configuration that cannot be discovered is refused at the form rather
   * than at somebody's sign-in three days later — which is why this can be
   * slow and can fail with EXTERNAL_IDP_UNREACHABLE.
   */
  create: (input: ExternalIdentityProviderInput) =>
    request<ExternalIdentityProvider>("/external-identity-providers", {
      method: "POST",
      body: input,
    }),

  /** A blank `clientSecret` keeps the stored one. */
  update: (id: string, input: ExternalIdentityProviderInput) =>
    request<ExternalIdentityProvider>(
      `/external-identity-providers/${segment(id)}`,
      { method: "PUT", body: input },
    ),

  enable: (id: string) =>
    request<void>(`/external-identity-providers/${segment(id)}/enable`, {
      method: "POST",
    }),

  /** Takes the button off the sign-in screen, leaving every binding. */
  disable: (id: string) =>
    request<void>(`/external-identity-providers/${segment(id)}/disable`, {
      method: "POST",
    }),

  /** Removes the provider and every binding that named it. */
  remove: (id: string) =>
    request<void>(`/external-identity-providers/${segment(id)}`, {
      method: "DELETE",
    }),
};

/**
 * The caller's own external identities.
 *
 * Separate from the administrative API above because the account is taken
 * from the session rather than from the request. A caller-supplied account
 * here would be the whole vulnerability this journey is arranged to avoid.
 */
export const myExternalIdentitiesApi = {
  list: () => request<ExternalIdentity[]>("/users/me/external-identities"),

  /**
   * Begins linking a provider to the caller's own account. Same round trip
   * as a sign-in and the same callback address; what makes it a binding is
   * remembered server-side when it departs.
   */
  startBinding: (providerId: string) =>
    request<{ authorizationUrl: string }>(
      `/users/me/external-identities/${segment(providerId)}/start`,
      { method: "POST" },
    ),

  unlink: (id: string) =>
    request<void>(`/users/me/external-identities/${segment(id)}`, {
      method: "DELETE",
    }),
};

/**
 * Groups: sets of people, as distinct from the organization chart.
 */
export const groupsApi = {
  list: () => request<Group[]>("/groups"),

  create: (input: { displayName: string; description: string }) =>
    request<Group>("/groups", { method: "POST", body: input }),

  update: (id: string, input: { displayName: string; description: string }) =>
    request<Group>(`/groups/${segment(id)}`, { method: "PUT", body: input }),

  remove: (id: string) =>
    request<void>(`/groups/${segment(id)}`, { method: "DELETE" }),

  members: (id: string) =>
    request<GroupMember[]>(`/groups/${segment(id)}/members`),

  addMembers: (id: string, userIds: string[]) =>
    request<void>(`/groups/${segment(id)}/members`, {
      method: "POST",
      body: { userIds },
    }),

  removeMember: (id: string, userId: string) =>
    request<void>(`/groups/${segment(id)}/members/${segment(userId)}`, {
      method: "DELETE",
    }),

  forUser: (userId: string) =>
    request<GroupRef[]>(`/users/${segment(userId)}/groups`),

  /**
   * The caller's own groups.
   *
   * Not forUser with your own id: that route is administrator-only, because
   * asking about somebody else is an administrative act. This one takes the
   * account from the token, so the home screen works for the people who only
   * have a home screen.
   */
  forMe: () => request<GroupRef[]>("/users/me/groups"),
};

/** The field catalogue: everything that may be mapped, for this tenant. */
export const fieldsApi = {
  list: () => request<CatalogueField[]>("/fields"),
};

/**
 * The tenant's own attributes — the half of the catalogue somebody defines.
 *
 * Retiring and deleting are separate calls rather than one status field,
 * because they answer different questions: retiring takes an attribute off
 * the forms and keeps every value recorded under it, and deleting discards
 * them. A single control for both would make the second look undoable.
 */
export const userAttributesApi = {
  /** Includes the retired ones, which the definitions screen has to show. */
  list: () => request<UserAttributeDefinition[]>("/user-attributes"),

  define: (input: UserAttributeInput) =>
    request<UserAttributeDefinition>("/user-attributes", {
      method: "POST",
      body: input,
    }),

  /** `key` is ignored by the server; everything else is replaced. */
  update: (id: string, input: UserAttributeInput) =>
    request<UserAttributeDefinition>(`/user-attributes/${segment(id)}`, {
      method: "PUT",
      body: input,
    }),

  enable: (id: string) =>
    request<UserAttributeDefinition>(`/user-attributes/${segment(id)}/enable`, {
      method: "POST",
    }),

  disable: (id: string) =>
    request<UserAttributeDefinition>(
      `/user-attributes/${segment(id)}/disable`,
      { method: "POST" },
    ),

  /** Discards every value recorded under it, and cannot be undone. */
  remove: (id: string) =>
    request<{ status: string }>(`/user-attributes/${segment(id)}`, {
      method: "DELETE",
    }),
};

/**
 * Where each recipient's rules live.
 *
 * Four paths rather than one, because the id in each is the one that
 * recipient's own screens use — an OAuth client is addressed by its client
 * id, the other three by their row id.
 */
const mappingPath: Record<RecipientKind, (id: string) => string> = {
  oauth: (id) => `/applications/oauth-clients/${segment(id)}/field-mappings`,
  saml: (id) =>
    `/applications/saml-service-providers/${segment(id)}/field-mappings`,
  cas: (id) => `/applications/cas-services/${segment(id)}/field-mappings`,
  webhook: (id) => `/webhooks/${segment(id)}/field-mappings`,
};

export const fieldMappingsApi = {
  list: (kind: RecipientKind, id: string) =>
    request<FieldMapping[]>(mappingPath[kind](id)),

  /**
   * Replaces the whole set. A save is a table somebody edited, so merging
   * would leave the rows they deleted still in place — the one outcome
   * nobody expects from a save. An empty list restores the defaults.
   */
  replace: (kind: RecipientKind, id: string, mappings: FieldMapping[]) =>
    request<FieldMapping[]>(mappingPath[kind](id), {
      method: "PUT",
      body: { mappings },
    }),
};
