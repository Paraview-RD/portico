/** Typed wrappers around the API. One function per endpoint. */

import { download, request, upload } from "./client";
import type {
  AuditLog,
  Authorization,
  CASService,
  ImportResult,
  IntegrationEndpoints,
  IssuedSCIMCredential,
  LogKind,
  OAuthClient,
  Organization,
  PageResult,
  RecoveryChannel,
  RegisteredClient,
  RegistrationStatus,
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
  WebhookSubscription,
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
    request<User>("/auth/register", {
      method: "POST",
      body: input,
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

  changeOwnPassword: (currentPassword: string, newPassword: string) =>
    request<{ reauthenticationRequired: boolean }>("/users/me/password", {
      method: "POST",
      body: { currentPassword, newPassword },
    }),

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
};

export const organizationApi = {
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
    from?: string;
    to?: string;
  }) => request<PageResult<AuditLog>>(`/audit-logs${query(params)}`),
};

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

export const applicationApi = {
  /** What to configure at the other end of an integration. */
  integrationEndpoints: () =>
    request<IntegrationEndpoints>("/applications/integration-endpoints"),

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

    create: (input: { name: string; metadataXml: string }) =>
      request<SAMLServiceProvider>("/applications/saml-service-providers", {
        method: "POST",
        body: input,
      }),

    /** Replacing the metadata is how a certificate is rotated. */
    update: (id: string, input: { name: string; metadataXml: string }) =>
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

    create: (input: { name: string; urlPrefix: string }) =>
      request<CASService>("/applications/cas-services", {
        method: "POST",
        body: input,
      }),

    update: (id: string, input: { name: string; urlPrefix: string }) =>
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

  create: (input: { name: string; url: string; events: string[] }) =>
    request<CreatedWebhookSubscription>("/webhooks", {
      method: "POST",
      body: input,
    }),

  deliveries: (id: string) =>
    request<WebhookDelivery[]>(`/webhooks/${segment(id)}/deliveries`),

  enable: (id: string) =>
    request<void>(`/webhooks/${segment(id)}/enable`, { method: "POST" }),

  disable: (id: string) =>
    request<void>(`/webhooks/${segment(id)}/disable`, { method: "POST" }),

  remove: (id: string) =>
    request<void>(`/webhooks/${segment(id)}`, { method: "DELETE" }),
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
};
