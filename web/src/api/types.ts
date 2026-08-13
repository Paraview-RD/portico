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
export type UserSource = "ADMIN" | "IMPORT" | "REGISTRATION" | "SCIM" | "LDAP";

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

  /**
   * When the holder closed the account themselves. Absent for every other
   * reason it might be disabled — the two look the same in `status` and mean
   * different things.
   */
  closedAt?: string;

  /**
   * The descriptive half: who somebody is, as opposed to what they may do.
   * The field names are SCIM's (RFC 7643), which is why a directory's
   * attributes land in them.
   */
  profile?: UserProfile;

  /**
   * Additional organizations this person is involved with, beside the one
   * they belong to. Advisory: they grant nothing. Present on a single
   * account, absent from a page of them.
   */
  attachments?: OrganizationRef[];

  /** Set while the account is locked out after repeated failed sign-ins. */
  lockedUntil?: string;

  /**
   * When this password stops working. Present only on `/users/me`, and only
   * when the tenant expires passwords at all — most do not, and absence
   * means "never" rather than "unknown".
   */
  passwordExpiresAt?: string;
}

/** An organization named without carrying the whole of it. */
/**
 * Somebody recorded as administering an organization.
 *
 * It grants them nothing in this version: no authorization decision reads
 * it. The records are collected now because delegated administration is
 * planned, and a chart entered by people over months cannot be reconstructed
 * on the day the feature ships.
 */
export interface OrganizationAdministrator {
  userId: string;
  username: string;
  displayName: string;
  /** The account's status, so a disabled one can be shown as such. */
  status: Status;
  /** SELF is this organization; SUBTREE is it and every descendant. */
  scope: AdminScope;
  grantedBy: string;
  grantedByName: string;
  grantedAt: string;
}

/** The other direction: what an account is recorded as administering. */
export interface AdministeredOrganization extends OrganizationRef {
  scope: AdminScope;
  grantedAt: string;
}

export type AdminScope = "SELF" | "SUBTREE";

export interface OrganizationRef {
  id: string;
  name: string;
  code: string;
}

/** What a bulk request did, per account. */
export interface BulkResult {
  total: number;
  succeeded: number;
  failed: number;
  outcomes: {
    userId: string;
    /** Empty on success; the error code otherwise. */
    code?: string;
    message?: string;
  }[];
}

/**
 * The descriptive attributes of an account, named after SCIM 2.0's core User
 * schema and its enterprise extension. Every one is optional.
 */
export interface UserProfile {
  nameFormatted: string;
  familyName: string;
  givenName: string;
  middleName: string;
  honorificPrefix: string;
  honorificSuffix: string;
  nickName: string;
  profileUrl: string;
  photoUrl: string;
  title: string;
  userType: string;
  preferredLanguage: string;
  locale: string;
  timezone: string;
  addressFormatted: string;
  streetAddress: string;
  locality: string;
  region: string;
  postalCode: string;
  country: string;
  employeeNumber: string;
  costCenter: string;
  /** Free text as a directory sends it; not the organization tree. */
  department: string;
  managerId: string;
  /** Resolved for display; ignored on write. */
  managerName: string;
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

  /**
   * Whoever is responsible for this organization. Grants nothing — this
   * version has two fixed roles and being named here confers neither.
   */
  managerId: string;
  /** Resolved for display; ignored on write. */
  managerName: string;
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
  /** How long a sign-in to this console lasts. Not the OIDC tokens below. */
  tokenTtlMinutes: number;

  /**
   * The lifetimes of the tokens Portico issues to registered applications.
   *
   * The access token's ceiling is an hour and that is the load-bearing limit:
   * it is verified without calling back here, so it cannot be revoked, and how
   * soon it expires is the only thing bounding a permission that has been
   * withdrawn. The ID token follows this same value.
   */
  oidcAccessTokenTtlMinutes: number;
  /** How long a refresh token may go unused before it stops working. */
  oidcRefreshTokenTtlDays: number;
  /**
   * The absolute age a session may reach, measured from the sign-in rather
   * than from the last refresh. Zero switches it off, which is the default:
   * this is the one setting here that ends sessions which are working.
   */
  oidcSessionMaxAgeDays: number;

  registrationEnabled: boolean;
  /**
   * Offers the explanatory panel at the top of each administrative screen.
   * Tenant-wide; each panel is separately collapsible per browser.
   */
  showGuides: boolean;
  /**
   * Requires a self-registered account to confirm its address before it can
   * sign in. Turning it on is refused where the deployment cannot send one.
   */
  registrationVerification: boolean;
  systemName: string;

  /**
   * The language of messages this tenant sends — a reset link, a
   * confirmation — to somebody who has stated no preference of their own.
   * Empty means "follow the deployment", and is a real value rather than an
   * absence: a tenant that has said nothing follows a deployment that
   * changes its mind later.
   *
   * It does not affect the console. That is each reader's own choice and is
   * remembered in their browser.
   */
  defaultLocale: string;

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
  /**
   * Empty when none was registered, which is normal. The tile falls back to
   * the first character of the name rather than to a broken image.
   */
  logoUri: string;
}

/** A registered OAuth 2.1 / OpenID Connect relying party. */
export interface OAuthClient {
  id: string;
  tenantId: string;
  clientId: string;
  name: string;
  /** Where a person opens it, for the portal. Empty when none was given. */
  launchUrl: string;
  /** The picture on its portal tile. Empty when none was given. */
  logoUri: string;
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

/**
 * A directory Portico reads accounts out of.
 *
 * The opposite direction from a SCIM credential: that lets a directory push
 * into /scim/v2, this has Portico connect and pull. There is no bind password
 * field, and that is not an omission — the server never sends one back.
 */
export interface LDAPSource {
  id: string;
  tenantId: string;
  name: string;
  host: string;
  port: number;
  encryption: "none" | "starttls" | "tls";
  /** Empty means an anonymous bind. */
  bindDn: string;
  /** Whether a credential is stored, so a form can say "set" without it. */
  hasBindPassword: boolean;
  baseDn: string;
  userFilter: string;
  attrUsername: string;
  attrDisplayName: string;
  attrEmail: string;
  attrPhone: string;
  /**
   * Where the reconciliation key comes from — objectGUID on Active
   * Directory, entryUUID on OpenLDAP. The most consequential field on the
   * form: it is what makes a rename a rename rather than a second account.
   */
  attrExternalId: string;
  organizationId: string;
  organizationName: string;
  status: Status;
  /**
   * How often this directory is read without anybody asking, in minutes. Zero
   * means never, and is the default.
   */
  syncIntervalMinutes: number;
  /**
   * Absent until the first run *succeeds*. A scheduled run that failed
   * advances the schedule without touching this, so it stays the answer to
   * "is this directory still working".
   */
  lastSyncedAt?: string;
  createdAt: string;
  updatedAt: string;
}

/** What one synchronization did. */
export interface LDAPSyncRun {
  id: string;
  sourceId: string;
  /** Empty for the scheduler, which is not a person. */
  actorName: string;
  startedAt: string;
  finishedAt?: string;
  outcome: "RUNNING" | "SUCCEEDED" | "FAILED";
  createdCount: number;
  updatedCount: number;
  deactivatedCount: number;
  /** Entries that could not become an account. Counted, not fatal. */
  skippedCount: number;
  /**
   * Why they were skipped, grouped by reason with an example of each. Empty
   * when nothing was. A count on its own tells an operator that something is
   * wrong and nothing about what.
   */
  skippedDetail: string;
  /**
   * Set when Portico refused, empty when the directory reported the failure.
   * A known code is rendered in the reader's language; anything else is the
   * LDAP server's own wording and is shown verbatim, because that is the
   * string somebody will search for.
   */
  errorCode?: string;
  error?: string;
}

/** What a form sends. bindPassword is write-only and optional. */
export interface LDAPSourceInput {
  name: string;
  host: string;
  port: number;
  encryption: "none" | "starttls" | "tls";
  bindDn: string;
  /** Omit to leave the stored credential alone; empty string clears it. */
  bindPassword?: string;
  baseDn: string;
  userFilter: string;
  attrUsername: string;
  attrDisplayName: string;
  attrEmail: string;
  attrPhone: string;
  attrExternalId: string;
  organizationId: string;
  /** 0 for manual only; otherwise 15 to 10080 minutes. */
  syncIntervalMinutes: number;
}

/** A registered SAML 2.0 service provider. */
export interface SAMLServiceProvider {
  id: string;
  tenantId: string;
  entityId: string;
  name: string;
  metadataXml: string;
  launchUrl: string;
  logoUri: string;
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
  logoUri: string;
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
  /**
   * The names of the custom headers this subscription sends, without their
   * values. The values are credentials, sealed at rest and never served
   * back — a name is enough to answer what a subscription is sending.
   */
  headerNames?: string[];
}

/**
 * What creating one returns. The secret is here and nowhere else — it is
 * stored in the clear because it signs rather than authenticates, which is
 * exactly why no endpoint serves it a second time.
 */
export interface CreatedWebhookSubscription extends WebhookSubscription {
  secret: string;
  /**
   * When the key this replaced stops being sent, on a rotation only. Absent
   * on a first issue, because there is nothing it replaced.
   *
   * This is the receiver's deadline rather than ours: until it passes, every
   * delivery carries both signatures and either verifies.
   */
  previousSecretExpiresAt?: string;
}

/**
 * What a snapshot queued.
 *
 * Counts are objects, pages are deliveries: the two differ by the page size,
 * and an operator watching the delivery list is counting the second.
 */
export interface WebhookSnapshot {
  syncId: string;
  scope: string[];
  counts: Record<string, number>;
  pages: number;
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

/**
 * One entry of the field catalogue: something that may be mapped.
 *
 * A key, never a column name. The catalogue exists so that a configuration
 * cannot name `password_hash` — see docs/field-mappings.md.
 */
export interface CatalogueField {
  key: string;
  group: "identity" | "profile" | "organization" | "tenant" | "custom";
  kind: "TEXT" | "NUMBER" | "BOOLEAN" | "DATE" | "SELECT";
  /**
   * Filled in for a tenant's own attributes, whose name is whatever somebody
   * typed. Empty for a built-in, whose label the console holds under
   * `fields.<key>` — a built-in has to read the same in both languages, and a
   * stored string can only be one of them.
   */
  label?: string;
  custom: boolean;
  inbound: boolean;
  outboundOnlyBecause?: string;
  allowedValues?: string[];
  /** A tenant attribute that has been retired. Its values are kept. */
  disabled?: boolean;
}

/**
 * One attribute a tenant defined for itself.
 *
 * The catalogue entry above is the read-only view of the same thing, joined
 * with the built-ins. This is the editable half: what the definitions screen
 * writes, and what a mapping names by `key`.
 */
export interface UserAttributeDefinition {
  id: string;
  tenantId: string;
  key: string;
  label: string;
  description?: string;
  kind: "TEXT" | "NUMBER" | "BOOLEAN" | "DATE" | "SELECT";
  allowedValues?: string[];
  required: boolean;
  sortOrder: number;
  /** Retired: no longer offered on a form, every recorded value kept. */
  disabled?: boolean;
}

/**
 * What the definitions form sends.
 *
 * `key` is read on creation and ignored afterwards, because a mapping stores
 * it — renaming it would silently stop whichever rule names it, and the
 * screen it was configured on would still look right.
 */
export interface UserAttributeInput {
  key: string;
  label: string;
  description: string;
  kind: UserAttributeDefinition["kind"];
  allowedValues: string[];
  required: boolean;
  sortOrder: number;
}

/**
 * One rule: a fact Portico holds, and the name a recipient expects it under.
 *
 * `suppressed` is a flag rather than an empty `targetName` because "send
 * nothing" and "send under a name I have not chosen yet" are different
 * intentions that one empty string cannot hold.
 */
export interface FieldMapping {
  sourceKey: string;
  targetName?: string;
  /** SAML's second, human-readable name. Ignored by the other three. */
  friendlyName?: string;
  suppressed?: boolean;
}

/** Which kind of recipient a mapping belongs to. */
export type RecipientKind = "oauth" | "saml" | "cas" | "webhook";

/**
 * An OpenID Provider somebody else runs, trusted to say who a person is.
 *
 * The other direction from everything else here: Portico is the relying
 * party, spending assertions rather than issuing them.
 *
 * No client secret, by construction. What is stored is sealed and only ever
 * unsealed on the way to the provider; a field here would carry every
 * tenant's secret to a browser on every list. `hasSecret` is what the edit
 * form needs instead — enough to say that leaving the field blank keeps the
 * stored one, and nothing more.
 */
export interface ExternalIdentityProvider {
  id: string;
  name: string;
  /** What the sign-in button says, when it should not say `name`. */
  buttonLabel: string;
  issuer: string;
  clientId: string;
  scopes: string;
  /**
   * Whether this provider's `email_verified` may link a first-time arrival
   * to an existing account by address. Off unless somebody turned it on:
   * it delegates account security to whoever runs that provider.
   */
  trustVerifiedEmail: boolean;
  status: "ACTIVE" | "DISABLED";
  hasSecret: boolean;
  /**
   * What has to be registered at the other end. Returned by the server
   * rather than composed here, because it is the value somebody copies and
   * the provider matches it character for character.
   */
  redirectUri: string;
}

/** What an administrator supplies. */
export interface ExternalIdentityProviderInput {
  name: string;
  buttonLabel: string;
  issuer: string;
  clientId: string;
  /** Blank on an edit keeps the stored one; blank on a create means none. */
  clientSecret: string;
  scopes: string;
  trustVerifiedEmail: boolean;
}

/**
 * One button on the sign-in screen.
 *
 * Everything else about a provider is configuration, and this is answered
 * before anybody has proved anything.
 */
export interface ExternalSignInOption {
  id: string;
  label: string;
}

/** One binding, as its owner sees it. */
export interface ExternalIdentity {
  id: string;
  providerId: string;
  providerName: string;
  subject: string;
  email: string;
  createdAt: string;
  lastUsedAt: string | null;
}

/**
 * What a completed callback produced: a session, or a binding.
 *
 * Exactly one, and the envelope carries no discriminator — a binding does
 * not issue a session, because the person already had one. Tell them apart
 * by whether a token is present rather than by guessing from context: the
 * same callback address serves both journeys.
 */
export type ExternalSignInResult = Session | ExternalIdentity;

export function isSession(result: ExternalSignInResult): result is Session {
  return "token" in result;
}
