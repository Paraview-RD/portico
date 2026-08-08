/** Wire types, mirroring the Go models in internal/model. */

export type Role = "SUPER_ADMIN" | "USER";
export type Status = "ACTIVE" | "DISABLED";
export type UserSource = "ADMIN" | "IMPORT" | "REGISTRATION";

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

/** A registered OAuth 2.1 / OpenID Connect relying party. */
export interface OAuthClient {
  id: string;
  tenantId: string;
  clientId: string;
  name: string;
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
