/**
 * Messages for the API's error codes.
 *
 * The server answers in English — its messages go to logs, to API clients,
 * and to whoever is reading a failed request in a terminal, and translating
 * them there would mean an Accept-Language round trip and a second place for
 * the wording to live. The console has the code, so it can say it in the
 * reader's own language without the server knowing anything about it.
 *
 * These are separate from the main bundle because they are a different kind
 * of text: the main bundle is what the UI calls things, this is what went
 * wrong. Keeping them apart means adding an error code does not enlarge the
 * screen vocabulary, and the completeness check still applies — zh-CN is
 * typed against this file, so a missing translation fails the build rather
 * than rendering English into a Chinese screen.
 *
 * A code with no entry here falls back to the server's own message, which is
 * English but correct. That is the right failure: a new code added on the
 * server should not blank out the reason.
 */
export const errorsEnUS = {
  // --- Authentication and session ---
  INVALID_CREDENTIALS: "Wrong username or password.",
  MISSING_CREDENTIALS: "Enter your username and password.",
  ACCOUNT_DISABLED: "This account has been disabled.",
  ACCOUNT_UNVERIFIED:
    "This account has not confirmed its email address yet. Check for the message, or ask for another below.",
  INVALID_VERIFICATION_TOKEN:
    "That confirmation link is not valid or has already been used. Ask for another.",
  VERIFICATION_UNAVAILABLE:
    "New accounts here must confirm an address, and this deployment cannot send one. An administrator has to configure a mail relay or switch the requirement off.",
  NO_DELIVERY_CHANNEL:
    "Requiring confirmation needs a way to send it. Configure a mail relay first, or leave it off.",
  ACCOUNT_CLOSED:
    "This account was closed by its owner. An administrator can reinstate it.",
  ACCOUNT_LOCKED:
    "Too many failed sign-in attempts. Try again later, or ask an administrator to unlock the account.",
  MISSING_TOKEN: "You are not signed in.",
  INVALID_TOKEN: "Your session is no longer valid. Sign in again.",
  TOKEN_EXPIRED: "Your session has expired. Sign in again.",
  TOKEN_REVOKED: "Your session was ended. Sign in again.",
  SESSION_NOT_FOUND: "No such session.",
  MALFORMED_AUTHORIZATION: "The credentials sent were not readable.",
  UNAUTHENTICATED: "You are not signed in.",
  ADMIN_REQUIRED: "This needs an administrator.",
  CURRENT_PASSWORD_MISMATCH: "The current password is wrong.",
  WEAK_PASSWORD: "That password does not meet the requirements.",
  PASSWORD_EXPIRED:
    "This password has expired and must be changed before signing in.",
  PASSWORD_CHANGE_REQUIRED:
    "This account is still on its default password, which must be replaced before signing in.",
  PASSWORD_NOT_EXPIRED:
    "This password has not expired. Sign in and change it from your profile.",
  // --- Webhook snapshots ---
  SNAPSHOT_IN_PROGRESS:
    "A snapshot for this subscription is still being delivered. Wait for it to finish, or disable the subscription to abandon it.",
  SNAPSHOT_EMPTY_SCOPE:
    "This subscription selects no events a snapshot can fill.",
  SNAPSHOT_UNAVAILABLE: "This deployment cannot produce a snapshot.",
  SUBSCRIPTION_DISABLED:
    "This subscription is disabled. Enable it before asking for a snapshot.",

  // --- Signing in through another provider ---
  EXTERNAL_IDP_NOT_FOUND: "No such identity provider.",
  EXTERNAL_IDP_DISABLED: "That identity provider is switched off.",
  EXTERNAL_IDP_ISSUER_TAKEN:
    "This tenant already has a provider for that issuer.",
  EXTERNAL_IDP_ISSUER_REQUIRED: "An issuer URL is required.",
  EXTERNAL_IDP_KIND_UNKNOWN:
    "That is not a kind of identity provider this version speaks.",
  EXTERNAL_IDP_CLIENT_ID_REQUIRED: "A client id is required.",
  EXTERNAL_IDP_UNREACHABLE:
    "That issuer could not be read as an OpenID Provider. Check the URL, and that this server can reach it.",
  EXTERNAL_IDP_SECRET_UNREADABLE:
    "This provider's client secret cannot be read. It was sealed with a different encryption key; re-enter it.",
  EXTERNAL_STATE_UNKNOWN:
    "That sign-in could not be matched to one this server started. Begin again.",
  EXTERNAL_EXCHANGE_FAILED:
    "The identity provider's answer could not be accepted.",
  EXTERNAL_PROVIDER_REFUSED: "The identity provider refused the sign-in.",
  EXTERNAL_IDENTITY_UNKNOWN:
    "That account is not linked here. Sign in with your password first, then link it from your profile.",
  EXTERNAL_IDENTITY_TAKEN: "That identity is already linked to an account.",
  EXTERNAL_IDENTITY_NOT_FOUND: "No such linked identity.",

  PASSWORD_REUSED:
    "That password has been used recently. Choose one you have not used before.",

  // --- Password recovery ---
  INVALID_RESET_TOKEN: "That reset link is invalid or has already been used.",
  INVALID_CHANNEL: "That recovery method is not available.",
  MISSING_DESTINATION: "Enter the email address or phone number to send to.",
  RECOVERY_UNAVAILABLE:
    "Password recovery is not configured on this deployment.",

  // --- Tenants ---
  TENANT_NOT_FOUND: "No such tenant.",
  TENANT_DISABLED: "That tenant has been disabled.",
  TENANT_CODE_TAKEN: "That tenant code is already in use.",

  // --- Users ---
  USER_NOT_FOUND: "No such account.",
  USERNAME_TAKEN: "That username is already in use.",
  USERNAME_REQUIRED: "A username is required.",
  INVALID_USERNAME: "That username is not allowed.",
  DISPLAY_NAME_REQUIRED: "A display name is required.",
  EMAIL_TAKEN: "That email address is already in use.",
  PHONE_TAKEN: "That phone number is already in use.",
  INVALID_EMAIL: "That is not a valid email address.",
  INVALID_PHONE: "That is not a valid phone number.",
  INVALID_ROLE: "That is not a valid role.",
  INVALID_STATUS: "Status must be active or disabled.",
  CANNOT_DISABLE_SELF: "You cannot disable your own account.",
  EMPLOYEE_NUMBER_TAKEN: "Another account already has that employee number.",
  MANAGER_NOT_FOUND: "No such account to report to.",
  MANAGER_IS_SELF: "An account cannot report to itself.",
  LAST_ADMIN:
    "This is the tenant's last active administrator. Promote another account first.",
  REGISTRATION_DISABLED: "Sign-up is closed on this deployment.",

  // --- Organizations ---
  ORGANIZATION_NOT_FOUND: "No such organization.",
  ORGANIZATION_DISABLED: "That organization has been disabled.",
  ORGANIZATION_CODE_TAKEN: "That organization code is already in use.",
  ORGANIZATION_MANAGER_NOT_FOUND:
    "No such account to put in charge of this organization.",
  ALREADY_PRIMARY_ORGANIZATION:
    "That account already belongs to this organization. An attachment is for the ones it does not.",
  ALREADY_ORGANIZATION_ADMIN:
    "That account is already recorded as an administrator of this organization. Remove it first to change its scope.",
  INVALID_ADMIN_SCOPE:
    "Choose how far this reaches: this organization only, or it and everything under it.",
  ORGANIZATION_CYCLE:
    "That would put the organization inside itself or one of its own descendants.",
  ORGANIZATION_TOO_DEEP: "Organizations may not be nested that deeply.",
  NAME_REQUIRED: "A name is required.",
  CODE_REQUIRED: "A code is required.",

  // --- Bulk import ---
  MISSING_FILE: "Choose a file to upload.",
  INVALID_UPLOAD: "That upload could not be read.",
  INVALID_SPREADSHEET: "That file is not a readable .xlsx workbook.",
  EMPTY_SPREADSHEET: "That workbook has no rows.",
  TOO_MANY_USERS:
    "Too many accounts in one request. Select fewer and try again.",
  TOO_MANY_ROWS: "That workbook has too many rows for one import.",

  // --- Application logos ---
  //
  // One code for every rejection about the file itself. Saying which format it
  // turned out to be would be telling somebody what they already know about the
  // file they just chose; what they need is what to choose instead.
  UNSUPPORTED_IMAGE:
    "That file is not a PNG or JPEG image. An SVG cannot be uploaded — it is a document that can carry script, and it would be served from this server's own address.",
  LOGO_TOO_LARGE:
    "That image is too large. A tile needs at most 512 KiB and 1024 pixels on a side.",
  LOGO_NOT_FOUND: "That image is no longer stored here.",
  IMPORT_FAILED: "The import could not be completed.",

  // --- OAuth clients ---
  CLIENT_NOT_FOUND: "No such client.",
  CLIENT_ID_TAKEN: "That client id is already registered in this tenant.",
  CLIENT_ID_REQUIRED: "A client id is required.",
  INVALID_CLIENT_ID:
    "A client id may contain only letters, digits, and . _ - and must be 3–128 characters.",
  CLIENT_IS_PUBLIC:
    "This is a public client. It authenticates with PKCE and has no secret to rotate.",
  CLIENT_DISABLED: "This client has been disabled.",
  INVALID_CLIENT: "Client authentication failed.",
  OAUTH_CLIENT_NOT_FOUND: "No such client.",
  OAUTH_CLIENT_DISABLED: "That client has been disabled.",
  INVALID_APPLICATION_TYPE: "Application type must be web, native, or browser.",
  REDIRECT_URI_REQUIRED: "At least one redirect URI is required.",
  INVALID_REDIRECT_URI: "A redirect URI is not acceptable.",

  // --- Authorization requests ---
  AUTH_REQUEST_NOT_FOUND:
    "That sign-in request has expired. Start again from the application.",
  AUTH_REQUEST_REQUIRED: "No sign-in request was supplied.",
  AUTH_REQUEST_TAKEN:
    "Someone else completed this sign-in request. Start again from the application.",
  AUTH_REQUEST_WRONG_TENANT: "That sign-in request belongs to another tenant.",
  INVALID_CODE: "That authorization code is invalid or has been used.",

  // --- SAML ---
  SERVICE_PROVIDER_NOT_FOUND: "No such service provider.",
  SERVICE_PROVIDER_TAKEN:
    "That entity id is already registered in this tenant.",
  SERVICE_PROVIDER_DISABLED: "That service provider has been disabled.",
  METADATA_REQUIRED: "A metadata document is required.",
  METADATA_INVALID: "That does not parse as SAML metadata.",
  METADATA_AMBIGUOUS:
    "That metadata describes more than one entity. Register them one at a time.",
  METADATA_NO_ENTITY_ID:
    "That metadata has no entityID, so requests from it could not be matched to this registration.",
  METADATA_NO_ACS:
    "That metadata declares no AssertionConsumerService, so there would be nowhere to deliver an assertion.",
  METADATA_ACS_INVALID: "An AssertionConsumerService location is not usable.",
  METADATA_ACS_INSECURE:
    "An AssertionConsumerService location uses plain http over a network.",
  METADATA_ENTITY_ID_MISMATCH:
    "That metadata declares a different entity id, so it describes a different service provider. Register it separately.",

  // --- CAS ---
  CAS_SERVICE_NOT_FOUND: "No such CAS service.",
  CAS_SERVICE_TAKEN: "That URL prefix is already registered in this tenant.",
  CAS_SERVICE_REQUIRED: "A service URL prefix is required.",
  CAS_SERVICE_INVALID:
    "A service URL prefix must be an absolute http or https URL with a host, and no query string or fragment.",
  CAS_SERVICE_INSECURE:
    "A service URL prefix must not use plain http over a network: a ticket delivered there is readable in transit.",
  CAS_SERVICE_WILDCARD:
    "Wildcards are not accepted. Register the URL prefix itself; anything beginning with it matches.",
  CAS_SERVICE_NOT_REGISTERED:
    "That service is not registered with this server.",

  // --- Groups ---
  GROUP_NOT_FOUND: "No such group.",
  GROUP_NAME_TAKEN: "A group with that name already exists.",
  GROUP_EXTERNAL_ID_TAKEN: "That externalId is already bound to another group.",
  MEMBER_NOT_FOUND: "One of those accounts does not exist in this tenant.",

  // --- Provisioning ---
  SCIM_CREDENTIAL_NOT_FOUND: "No such provisioning credential.",
  SCIM_CREDENTIAL_NAME_TAKEN:
    "A provisioning credential with that name already exists.",
  SCIM_UNAUTHORIZED: "That token is not valid for provisioning.",
  EXTERNAL_ID_TAKEN: "That externalId is already bound to another account.",

  // --- Directory synchronization ---
  LDAP_SOURCE_NOT_FOUND: "No such directory.",
  LDAP_SOURCE_NAME_TAKEN: "A directory with that name already exists.",
  LDAP_SOURCE_DISABLED: "That directory is disabled.",
  INVALID_LDAP_ENCRYPTION: "Encryption must be none, STARTTLS, or TLS.",
  INVALID_LDAP_PORT: "The port must be between 1 and 65535.",
  INVALID_SYNC_INTERVAL:
    "The automatic synchronization interval must be 15 minutes to 7 days, or off.",
  INVALID_LDAP_HOST:
    "Give the host name on its own, without a scheme, path, or port — those are separate fields.",
  LDAP_FIELD_REQUIRED:
    "The host, base DN, user filter, and the username, display name, and external id attributes are all required.",
  // The name or value of a custom header the subscription cannot send. The
  // server's message names which and why; this is the fallback.
  INVALID_WEBHOOK_HEADER:
    "That header cannot be sent. Portico sets some itself, and a value cannot contain a line break.",
  NO_ENCRYPTION_KEY:
    "This deployment has no encryption key, so a bind password cannot be stored. Ask an operator to set PORTICO_ENCRYPTION_KEY, or use an anonymous bind.",

  // --- Webhooks ---
  WEBHOOK_NOT_FOUND: "No such subscription.",
  WEBHOOK_NAME_TAKEN: "A subscription with that name already exists.",
  INVALID_WEBHOOK_URL: "That destination cannot be used.",
  NO_EVENTS_SELECTED: "Choose at least one event, or * for all of them.",
  UNKNOWN_EVENT: "That is not an event this version sends.",

  // --- Settings and filters ---
  INVALID_LAUNCH_URL: "A launch address must be an http or https URL.",
  INVALID_LOGO_URI:
    "A logo address must be an http or https URL, or a path on this server such as /icons/wiki.svg.",
  INVALID_NAME: "A name is required.",
  INVALID_SETTINGS: "Those settings are not valid.",
  INVALID_LOG_KIND: "That is not a valid log type.",
  INVALID_TIMESTAMP: "That is not a valid date and time.",

  // --- Request shape ---
  MALFORMED_BODY: "The request could not be read.",
  EMPTY_BODY: "The request had no content.",
  BODY_TOO_LARGE: "That request is too large.",
  WEBHOOK_DELIVERY_NOT_FOUND:
    "No such delivery. Finished deliveries are removed after 30 days.",
  INVALID_CURSOR:
    "That page marker is no longer valid. Go back to the first page.",
  INVALID_DELIVERY_FILTER: "That is not a filter this list offers.",
  UNSUPPORTED_MEDIA_TYPE:
    "The request was sent in a format this endpoint does not read.",
  TOO_MANY_ATTEMPTS:
    "Too many attempts from this address. Please wait a moment and try again.",
  // Self-service trials, on a demonstration deployment. Every one of these is
  // something the visitor can act on, which is why none of them is a bare
  // refusal: a stranger who cannot get in has no support channel to ask.
  TRIAL_SIGNUP_CLOSED: "This deployment does not offer self-service trials.",
  TRIAL_MAIL_UNAVAILABLE:
    "Trials need email, and this deployment has no relay configured. Tell whoever runs it.",
  // A different thing from UNAVAILABLE: that one is "not configured", this
  // one is "configured and refusing right now" — a quota, a credential, a
  // network. The first needs whoever runs it; the second needs a minute.
  TRIAL_MAIL_FAILED:
    "The confirmation email could not be sent just now. Try again in a minute.",
  TRIAL_QUOTA_REACHED:
    "The demonstration is full. Try again later, or run Portico yourself.",
  TRIAL_CODE_TAKEN: "That tenant code is taken. Choose another.",
  TRIAL_EMAIL_USED:
    "That address already has a trial. Sign in with it instead.",
  TRIAL_TOO_MANY: "Too many trials requested from this address today.",
  TRIAL_LINK_INVALID: "That link is not valid. Request a new trial.",
  TRIAL_LINK_EXPIRED: "That link has expired. Request a new trial.",
  TRIAL_LINK_SPENT:
    "That link has already been used. Sign in with the credentials it sent you.",
  COMPANY_REQUIRED: "Enter an organization name.",
  INVALID_INDUSTRY: "Pick one of the offered industries.",
  INVALID_PATH_PARAMETER: "That address is not valid.",
  ROUTE_NOT_FOUND: "No such endpoint.",
  METHOD_NOT_ALLOWED: "That endpoint does not support this method.",
  ALREADY_EXISTS: "That already exists.",
  INTERNAL_ERROR: "Something went wrong on the server.",
  // --- Tenant-defined user attributes and the field catalogue ---
  USER_ATTRIBUTE_NOT_FOUND: "No such attribute.",
  USER_ATTRIBUTE_KEY_TAKEN:
    "That key is already in use — keys are unique across both the built-in fields and your own.",
  INVALID_USER_ATTRIBUTE_KEY:
    "A key is 3 to 40 characters of lower-case letters, digits, and underscores, starting with a letter.",
  INVALID_USER_ATTRIBUTE_KIND:
    "The kind must be text, number, yes/no, date, or single-select.",
  USER_ATTRIBUTE_LABEL_REQUIRED:
    "A label is required: it is what appears on the form.",
  USER_ATTRIBUTE_NEEDS_VALUES:
    "A single-select attribute needs at least one permitted value.",
  TOO_MANY_USER_ATTRIBUTES:
    "You have reached the limit on defined attributes. Each one is a candidate for outbound mapping, and a mapped attribute is bytes in every token.",
  INVALID_USER_ATTRIBUTE_VALUE:
    "That value does not match the attribute's kind.",
  UNKNOWN_FIELD: "No such field. Only fields from the catalogue can be mapped.",
  MAPPING_TARGET_REQUIRED:
    "Give the name this recipient expects, or switch the field off.",
  DUPLICATE_MAPPING_SOURCE:
    "This field is already mapped. One rule per field — two would be settled by whichever was read first.",
  DUPLICATE_MAPPING_TARGET:
    "Two fields are being sent under the same name. Only one would arrive, and not the one you pick.",
  RESERVED_CLAIM_NAME:
    "OpenID Connect reserves that name and relies on what it means. Choose another.",
  PAYLOAD_NAME_TAKEN:
    "The event already uses that name for something else. Choose another.",
  RECIPIENT_NOT_FOUND: "No such application or subscription.",
  CLAIM_NAME_TAKEN:
    "This application already receives another field under that claim name.",
};

/** Every code this table knows. */
export type ErrorCode = keyof typeof errorsEnUS;

/**
 * Codes whose server message carries specifics the translation cannot — a
 * URL that was rejected, an entity id that did not match.
 *
 * For these the server's own message is shown after the translation, so the
 * reader gets both the explanation in their language and the value the
 * server actually objected to. Without this, translating would be a downgrade
 * — a tidier sentence that no longer says which URI was the problem.
 */
export const codesCarryingDetail: ReadonlySet<string> = new Set<ErrorCode>([
  "INVALID_REDIRECT_URI",
  "METADATA_ACS_INVALID",
  "METADATA_ACS_INSECURE",
  "METADATA_ENTITY_ID_MISMATCH",
  "INVALID_SETTINGS",
  "IMPORT_FAILED",
]);
