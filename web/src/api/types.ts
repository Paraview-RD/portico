/** Wire types, mirroring the Go models in internal/model. */

export type Role = "SUPER_ADMIN" | "USER";
export type Status = "ACTIVE" | "DISABLED";
/**
 * How an account came to exist.
 *
 * Three of these are past tense — they say how it was born and nothing about
 * it now. `SCIM` is present tense: a directory still owns the record and the
 * next sync will overwrite what an administrator changes here. That is why
 * the list marks only this one.
 */
export type UserSource = "ADMIN" | "IMPORT" | "REGISTRATION" | "SCIM";

export interface User {
  id: string;
  /** The tenant the account belongs to. Reported, never sent. */
  tenantId: string;
  username: string;
  displayName: string;
  phone: string;
  email: string;
  role: Role;
  status: Status;
  source: UserSource;
  organizationId: string;
  organizationName: string;
  createdAt: string;
  updatedAt: string;

  /** Set while the account is locked out after repeated failed sign-ins. */
  lockedUntil?: string;
}

export interface Organization {
  id: string;
  name: string;
  code: string;
  remark: string;
  status: Status;
  sortOrder: number;
  userCount: number;
  createdAt: string;
  updatedAt: string;

  /**
   * Empty for a root. The list arrives flat with each row naming its
   * parent; the screen assembles the tree, because a nested payload cannot
   * be sorted or filtered without being taken apart again.
   */
  parentId: string;
}

export type LogKind =
  "LOGIN" | "OPERATION" | "AUTH" | "REGISTRATION" | "ORGANIZATION";

export interface AuditLog {
  id: string;
  kind: LogKind;
  action: string;
  result: "SUCCESS" | "FAILURE";
  actorId: string;
  actorName: string;
  targetType: string;
  targetId: string;
  targetName: string;
  detail: string;
  ip: string;
  createdAt: string;
}

export interface Settings {
  tokenTtlMinutes: number;
  registrationEnabled: boolean;
  systemName: string;

  /**
   * Consecutive failed sign-ins that lock an account. Zero switches lockout
   * off — a deployment that trusts its reverse proxy's throttling may want
   * that, though the two controls cover different attacks.
   */
  lockoutThreshold: number;
  /** How long a lock lasts, and the window failures are counted over. */
  lockoutDurationMinutes: number;

  /**
   * Password policy. Composition rules and expiry are off by default —
   * they make passwords more guessable rather than less, and exist for
   * deployments audited against regimes that require them.
   */
  passwordMinLength: number;
  passwordRequireUppercase: boolean;
  passwordRequireLowercase: boolean;
  passwordRequireDigit: boolean;
  passwordRequireSymbol: boolean;
  /** Previous passwords that may not be reused. 0 does not check. */
  passwordHistoryDepth: number;
  /** How long a password stays usable. 0 never expires. */
  passwordMaxAgeDays: number;

  /**
   * How long audit entries are kept. 0 — the default — keeps them forever,
   * which is the only safe default: the trail is the record of what
   * happened, not an operational buffer.
   */
  auditRetentionDays: number;
}

export interface Session {
  token: string;
  expiresAt: string;
  user: User;
}

export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ImportRowError {
  row: number;
  username: string;
  code: string;
  message: string;
}

export interface ImportResult {
  total: number;
  imported: number;
  failed: number;
  errors: ImportRowError[];
}

/** How a password-reset link reaches its owner. */
export type RecoveryChannel = "EMAIL" | "SMS";

export interface RegistrationStatus {
  registrationEnabled: boolean;
  systemName: string;
  /** The tenant the answer is about, resolved from the request. */
  tenant: string;
  tenantName: string;
}

/**
 * Where a browser goes once a person has signed in for an OAuth
 * authorization request. The destination is the provider's own callback,
 * not the application's: the code is issued there and redirected onward.
 */
export interface Authorization {
  redirectTo: string;
  /** Set on the OAuth path. */
  clientName?: string;
  /** Set on the SAML path. */
  serviceProviderName?: string;
  /** Set on the CAS path. */
  serviceName?: string;
}

/** Which protocol an application signs in with. */
export type Protocol = "oauth" | "saml" | "cas";

/**
 * One thing a person can open from the portal.
 *
 * Deliberately not the administrative shape. A reader does not need the
 * redirect URIs or the metadata document — those are how the protocol works,
 * not what the application is.
 */
export interface PortalApplication {
  name: string;
  protocol: Protocol;
  launchUrl: string;
}

/** A registered OAuth 2.1 / OpenID Connect relying party. */
export interface OAuthClient {
  id: string;
  tenantId: string;
  clientId: string;
  name: string;
  /** Where a person opens it, for the portal. Empty when none was given. */
  launchUrl: string;
  /**
   * False for a browser or mobile application, which cannot keep a secret
   * and authenticates with PKCE alone.
   */
  confidential: boolean;
  applicationType: "WEB" | "NATIVE" | "USER_AGENT";
  authMethod: string;
  redirectUris: string[];
  postLogoutRedirectUris: string[];
  grantTypes: string[];
  scopes: string[];
  status: Status;
  createdAt: string;
  updatedAt: string;
}

/**
 * A client together with a freshly generated secret.
 *
 * The secret is present on exactly two responses — registration and
 * rotation — and is never readable afterwards, because only a hash is
 * stored. The screen that receives one has to say so.
 */
export interface RegisteredClient {
  client: OAuthClient;
  secret?: string;
}

/** A registered SAML 2.0 service provider. */
export interface SAMLServiceProvider {
  id: string;
  tenantId: string;
  entityId: string;
  name: string;
  metadataXml: string;
  launchUrl: string;
  /** Where assertions are delivered, read out of the metadata document. */
  acsUrls: string[];
  status: Status;
  createdAt: string;
  updatedAt: string;
}

/** A registered CAS service. */
export interface CASService {
  id: string;
  tenantId: string;
  name: string;
  /** A prefix, not a pattern: there are no wildcards. */
  urlPrefix: string;
  launchUrl: string;
  status: Status;
  createdAt: string;
  updatedAt: string;
}

/**
 * The addresses to configure at the other end of an integration.
 *
 * Every value is derived by the server from its own public URL and the
 * tenant's code, so what this screen shows cannot drift from what the
 * server actually serves.
 */
export interface IntegrationEndpoints {
  tenantCode: string;
  issuer: string;
  oidc: {
    discovery: string;
    authorize: string;
    token: string;
    userinfo: string;
    jwks: string;
    endSession: string;
    introspect: string;
    revoke: string;
  };
  saml: {
    entityId: string;
    metadata: string;
    sso: string;
    certificatePem: string;
  };
  cas: {
    baseUrl: string;
    login: string;
    logout: string;
    serviceValidate: string;
  };
}

/**
 * One sign-in, as shown to the person it belongs to.
 *
 * There is no token here and never will be: the server does not store one,
 * so there is nothing for this shape to leak even by accident.
 */
export interface UserSession {
  id: string;
  ip: string;
  userAgent: string;
  /** The session making the request, so the screen can say which is yours. */
  current: boolean;
  createdAt: string;
  lastSeenAt: string;
  expiresAt: string;
}

/**
 * A credential a directory authenticates with, as the console sees it.
 *
 * There is no token field, and that is not an omission: the server stores a
 * digest, so it has nothing to return. The token exists once, in the
 * response to creating it.
 */
export interface SCIMCredential {
  id: string;
  name: string;
  /** The first characters, enough to tell two credentials apart. */
  tokenPrefix: string;
  status: "ACTIVE" | "DISABLED";
  /** Null until a directory has used it. The question asked when a sync
   * has quietly stopped. */
  lastUsedAt: string | null;
  createdAt: string;
}

/** What creating one returns. The token is here and nowhere else, ever. */
export interface IssuedSCIMCredential extends SCIMCredential {
  token: string;
}

/** An outbound event subscription, as the console sees it. */
export interface WebhookSubscription {
  id: string;
  name: string;
  url: string;
  events: string[];
  status: "ACTIVE" | "DISABLED";
  createdAt: string;
}

/**
 * What creating one returns. The secret is here and nowhere else — it is
 * stored in the clear because it signs rather than authenticates, which is
 * exactly why no endpoint serves it a second time.
 */
export interface CreatedWebhookSubscription extends WebhookSubscription {
  secret: string;
}

/** One attempt to deliver one event. */
export interface WebhookDelivery {
  id: string;
  eventType: string;
  status: "PENDING" | "DELIVERED" | "FAILED";
  attempts: number;
  /** Null when the request never reached a server at all. */
  lastStatus: number | null;
  lastError: string;
  createdAt: string;
  deliveredAt: string | null;
}

/**
 * A group: a set of people.
 *
 * Not an organization. An organization is where somebody sits — one of them,
 * in a tree, with a code downstream systems store. A group is a set they
 * belong to — any number of them, flat. Membership grants nothing.
 */
export interface Group {
  id: string;
  displayName: string;
  description: string;
  /** Present when a directory maintains it, empty otherwise. */
  externalId?: string;
  source: "ADMIN" | "SCIM";
  memberCount: number;
  createdAt: string;
  updatedAt: string;
}

/** One person in a group. */
export interface GroupMember {
  userId: string;
  username: string;
  displayName: string;
}

/** A group as it appears on a user: enough to name it, no more. */
export interface GroupRef {
  id: string;
  displayName: string;
}
