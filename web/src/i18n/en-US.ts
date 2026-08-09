/**
 * English is the source language: every key is defined here, and other
 * bundles are typed against this one so a missing translation is a compile
 * error rather than a raw key rendered in the UI.
 */
export const enUS = {
  "common.docs": "Documentation",
  "common.save": "Save",
  "common.cancel": "Cancel",
  "common.confirm": "Confirm",
  "common.close": "Close",
  "common.create": "Create",
  "common.edit": "Edit",
  "common.enable": "Enable",
  "common.disable": "Disable",
  "common.actions": "Actions",
  "common.loading": "Loading…",
  "common.empty": "Nothing to show",
  "common.all": "All",
  "common.optional": "optional",
  "common.previous": "Previous",
  "common.next": "Next",
  "common.pageOf": "Page {0} of {1}",
  "common.totalItems": "{0} items",
  "common.unexpectedError": "Something went wrong. Please try again.",

  "common.copy": "Copy",
  "common.copied": "Copied",

  "status.ACTIVE": "Active",
  "status.DISABLED": "Disabled",
  "role.SUPER_ADMIN": "Administrator",
  "role.USER": "User",
  // The same word groups use for the same situation, and for the same
  // reason: what matters is not that a directory created it but that a
  // directory still maintains it.
  "source.SCIM": "Directory",
  // Both mean a directory owns the account; they differ in which way it
  // travelled, and an operator chasing a stale account needs to know which
  // — one has a run record here to read, the other does not.
  "source.LDAP": "LDAP",
  "users.directoryManaged":
    "This account is maintained by a directory. Changes made here may be " +
    "overwritten the next time it synchronizes.",

  "nav.home": "Home",
  "nav.users": "Users",
  "nav.organizations": "Organizations",
  "nav.auditLogs": "Audit logs",
  "nav.applications": "Applications",
  // Shorter than the page's own heading, which says "Directory provisioning
  // (SCIM)". A menu entry has to fit beside an icon; the heading has room to
  // name the protocol, and the person looking for it knows one of the two.
  "nav.provisioning": "Directory integration",
  "nav.webhooks": "Webhooks",
  "nav.settings": "Settings",
  "nav.profile": "My profile",
  "nav.signOut": "Sign out",

  "portal.greeting": "Hello, {0}",
  "portal.subtitle": "Your applications and your account.",
  "portal.applications": "Applications",
  "portal.applicationsHint":
    "Everything registered in this tenant. This version has no per-person assignment, so this list is the same for everyone.",
  "portal.noneOpenable":
    "No application has a launch address yet. An administrator can add one when registering it.",
  "portal.account": "Your account",
  "portal.recentSignIns": "Recent sign-ins",
  "portal.passwordExpires": "Password valid until",
  "portal.passwordExpiring":
    "Your password expires in {0} days. Change it now rather than at a sign-in screen on a morning you are busy.",
  "portal.passwordExpired":
    "Your password has expired. You will be asked to change it at your next sign-in.",
  "portal.contactMissingEmail":
    "No email address on your account. Without one you cannot recover it yourself if you forget your password.",
  "portal.contactMissingPhone":
    "No phone number on your account. Without one you cannot recover it yourself if you forget your password.",
  "portal.contactMissingBoth":
    "No email address or phone number on your account. Without one you cannot recover it yourself if you forget your password.",
  "portal.goToProfile": "Go to my account",
  "portal.manageDevices": "Signed-in devices",
  "applications.logoUri": "Icon",
  "applications.logoUriHelp":
    "The picture on this application's tile. Either an address on this server, such as /icons/wiki.svg, or an absolute https URL. Without one the tile carries the first character of the name.",
  "applications.launchUrl": "Launch address",
  "applications.launchUrlHelp":
    "Where a person opens this application from the home screen. Optional, and not a redirect address — opening one of those directly produces an error.",

  "login.title": "Sign in",
  "login.identifier": "Username, email, or phone",
  "login.identifierHint": "Any of the three reaches the same account.",
  "login.tenant": "Tenant",
  "login.tenantHint": "Leave blank unless you were given a tenant code.",
  "login.username": "Username",
  "login.password": "Password",
  "login.currentPassword": "Current password",
  "login.newPassword": "New password",
  "login.passwordExpired":
    "This password has expired. Enter it once more and choose a new one to continue.",
  "login.submit": "Sign in",
  "login.signingIn": "Signing in…",
  "login.noAccount": "Don't have an account?",
  "login.register": "Create one",
  "login.sessionExpired": "Your session ended. Please sign in again.",

  // Signing in on behalf of an application, rather than to Portico itself.
  "authorize.title": "Signing you in",
  "authorize.redirecting": "Taking you back to the application…",
  "authorize.signOutAndRetry": "Sign out and sign in again",
  "authorize.wrongTenant":
    "This sign-in request is for a different tenant. Sign out and sign in to the tenant the application asked for.",
  "authorize.expired":
    "This sign-in request has expired or was already used. Start again from the application.",
  "authorize.clientGone":
    "The application this sign-in was for is no longer registered.",
  "authorize.clientDisabled":
    "The application this sign-in was for has been disabled.",
  "authorize.serviceNotRegistered":
    "That application is not registered with this server.",
  "authorize.signedOut": "You have been signed out.",

  "register.title": "Create an account",
  "register.displayName": "Display name",
  "register.confirmPassword": "Confirm password",
  "register.phone": "Phone",
  "register.email": "Email",
  "register.submit": "Create account",
  "register.haveAccount": "Already have an account?",
  "register.signIn": "Sign in",
  "register.passwordMismatch": "The two passwords do not match.",
  "register.success": "Account created. You can sign in now.",

  "users.title": "Users",
  "users.subtitle": "Accounts, roles, and organization membership.",
  "users.searchPlaceholder": "Search by username or name",
  "users.filterRole": "Role",
  "users.filterStatus": "Status",
  "users.filterOrganization": "Organization",
  "users.create": "New user",
  "users.profileSection": "Further details",
  "users.profileHint":
    "Optional, and named after the SCIM attributes a directory sends — so what your directory has for somebody lands in the right field.",
  "users.attr.givenName": "Given name",
  "users.attr.familyName": "Family name",
  "users.attr.title": "Job title",
  "users.attr.department": "Department (as your directory names it)",
  "users.attr.employeeNumber": "Employee number",
  "users.attr.costCenter": "Cost centre",
  "users.attr.userType": "User type",
  "users.attr.nickName": "Nickname",
  "users.attr.preferredLanguage": "Preferred language",
  "users.attr.timezone": "Time zone",
  "users.attr.locality": "City",
  "users.attr.country": "Country",
  "users.export": "Export",
  "users.exporting": "Exporting…",
  "users.selectAll": "Select every account on this page",
  "users.selectedCount": "{0} selected",
  "users.bulkMoveTo": "Move to…",
  "users.bulkNoOrganization": "No organization",
  "users.bulkSummary": "{0} changed, {1} refused.",
  "users.import": "Import",
  "users.colUsername": "Username",
  "users.colDisplayName": "Name",
  "users.colRole": "Role",
  "users.colOrganization": "Organization",
  "users.colStatus": "Status",
  "users.resetPassword": "Reset password",
  "users.unlock": "Unlock",
  "users.lockedUntil": "Locked until {0}",
  "users.noOrganization": "—",
  "users.createTitle": "New user",
  "users.editTitle": "Edit user",
  "users.confirmDisable":
    "Disable {0}? They will be signed out immediately and cannot sign in again.",
  "users.confirmEnable": "Enable {0}?",
  "users.resetPasswordTitle": "Reset password for {0}",
  "users.newPassword": "New password",
  "users.passwordResetDone":
    "Password reset. The user has been signed out of all sessions.",

  "users.importTitle": "Import users from a spreadsheet",
  "users.importHelp":
    "Upload an .xlsx file. Rows are independent — valid rows are imported even if others fail.",
  "users.importDownloadTemplate": "Download template",
  "users.importChooseFile": "Choose file",
  "users.importSubmit": "Import",
  "users.importing": "Importing…",
  "users.importSummary": "Imported {0} of {1} rows. {2} failed.",
  "users.importColRow": "Row",
  "users.importColUsername": "Username",
  "users.importColProblem": "Problem",

  "organizations.title": "Organizations",
  "organizations.subtitle": "A single flat tier of groupings for users.",
  "organizations.create": "New organization",
  "organizations.colName": "Name",
  "organizations.colCode": "Code",
  "organizations.colMembers": "Members",
  "organizations.colRemark": "Note",
  "organizations.colStatus": "Status",
  "organizations.createTitle": "New organization",
  "organizations.editTitle": "Edit organization",
  "organizations.name": "Name",
  "organizations.code": "Code",
  "organizations.codeHelp":
    "Used by imports and downstream systems. Cannot be changed later.",
  "organizations.remark": "Note",
  "organizations.searchPlaceholder": "Search name or code",
  "organizations.parent": "Parent organization",
  "organizations.parentHelp":
    "Leave as none for a top-level organization. An organization cannot be moved inside its own branch.",
  "organizations.noParent": "None (top level)",
  "organizations.sortOrder": "Sort order",
  "organizations.confirmDisable":
    "Disable {0}? Existing members stay, but no new members can be assigned.",
  "organizations.confirmEnable": "Enable {0}?",

  "auditLogs.title": "Audit logs",
  "auditLogs.subtitle": "Sign-ins, changes, and registrations.",
  "auditLogs.filterKind": "Type",
  "auditLogs.filterFrom": "From",
  "auditLogs.filterTo": "To",
  "auditLogs.searchPlaceholder": "Search by actor or target",
  "auditLogs.colTime": "Time",
  "auditLogs.colKind": "Type",
  "auditLogs.colAction": "Action",
  "auditLogs.colActor": "Actor",
  "auditLogs.colTarget": "Target",
  "auditLogs.colResult": "Result",
  "auditLogs.colIp": "IP",
  "auditLogs.colDetail": "Detail",
  "auditLogs.showDetail": "Show",
  "auditLogs.hideDetail": "Hide",
  "auditLogs.detail": "What changed",
  "auditLogs.targetType": "Target type",
  "auditLogs.targetId": "Target id",
  "auditLogs.actorId": "Actor id",
  "auditLogs.result.SUCCESS": "Success",
  "auditLogs.result.FAILURE": "Failure",
  "auditLogs.kind.LOGIN": "Sign-in",
  "auditLogs.kind.OPERATION": "Operation",
  "auditLogs.kind.AUTH": "Authorization",
  "auditLogs.kind.REGISTRATION": "Registration",
  "auditLogs.kind.ORGANIZATION": "Organization",

  "applications.title": "Applications",
  "applications.subtitle": "The systems that sign in through Portico.",
  "applications.protocol": "Protocol",
  "applications.tab.oauth": "OAuth 2.1 / OIDC",
  "applications.tab.saml": "SAML 2.0",
  "applications.tab.cas": "CAS",
  "applications.hint.oauth":
    "Relying parties that obtain tokens through the authorization code flow with PKCE.",
  "applications.hint.saml":
    "Service providers that receive signed assertions. A registration is the service provider's own metadata document.",
  "applications.hint.cas":
    "Services identified by a URL prefix. There are no wildcards: anything beginning with the registered prefix matches.",
  "applications.create.oauth": "Register a client",
  "applications.create.saml": "Register a service provider",
  "applications.create.cas": "Register a service",
  "applications.editOauth": "Edit client",
  "applications.editSaml": "Edit service provider",
  "applications.editCas": "Edit service",
  "applications.colName": "Name",
  "applications.colClientId": "Client ID",
  "applications.colType": "Kind",
  "applications.colRedirects": "Redirect URIs",
  "applications.colEntityId": "Entity ID",
  "applications.colAcs": "Assertion consumer service",
  "applications.colPrefix": "URL prefix",
  "applications.colStatus": "Status",
  "applications.confidential": "Confidential",
  "applications.public": "Public",
  "applications.name": "Display name",
  "applications.clientId": "Client ID",
  "applications.clientIdFixed":
    "Fixed once registered: it is the name the application presents at the token endpoint.",
  "applications.clientKind": "Client kind",
  "applications.clientKindHelp":
    "A public client is a browser or mobile application, which cannot keep a secret and authenticates with PKCE alone.",
  "applications.applicationType": "Application type",
  "applications.typeWeb": "Web",
  "applications.typeNative": "Native",
  "applications.typeUserAgent": "Browser",
  "applications.redirectUris": "Redirect URIs",
  "applications.redirectUrisHelp":
    "One per line. This is where an authorization code is delivered, so plain http is refused except on loopback.",
  "applications.postLogoutUris": "Post-logout redirect URIs",
  "applications.postLogoutUrisHelp": "One per line. Optional.",
  "applications.scopes": "Scopes",
  "applications.scopesHelp": "Space separated. openid is always included.",
  "applications.entityId": "Entity ID",
  "applications.entityIdFixed":
    "Fixed once registered. Metadata declaring a different entity describes a different service provider.",
  "applications.metadata": "Metadata document",
  "applications.metadataHelp":
    "Upload or paste the service provider's own metadata XML. Portico never fetches it from a URL.",
  "applications.metadataReplaceHelp":
    "Replacing this is how a service provider's certificate is rotated. Leave it alone to keep the current document.",
  "applications.metadataReadFailed": "That file could not be read.",
  "applications.urlPrefix": "Service URL prefix",
  "applications.urlPrefixHelp":
    "A prefix, not a pattern. It is normalized to end at a path boundary, so it can never match a lookalike host.",
  "applications.rotateSecret": "Rotate secret",
  "applications.confirmTitle": "Confirm",
  "applications.confirmEnable": "Put {0} back into service?",
  "applications.confirmDisable":
    "Stop {0} from signing anybody in? Nothing is deleted and it can be enabled again.",
  "applications.confirmRotate":
    "Issue {0} a new secret? The current one stops working immediately.",
  "applications.secretTitle": "Client secret",
  "applications.secretWarning":
    "This is the only time this secret is shown. Only a hash is stored, so it cannot be retrieved later — copy it now, or rotate it to get a new one.",
  "applications.clientSecret": "Client secret",
  "applications.endpoints": "Integration endpoints",
  "applications.endpointsTitle": "What to configure at the other end",
  "applications.endpointsHelp":
    "These addresses come from this deployment, so they always match what the server actually serves.",
  "applications.issuer": "Issuer",
  "applications.discovery": "Discovery document",
  "applications.authorizeEndpoint": "Authorization endpoint",
  "applications.tokenEndpoint": "Token endpoint",
  "applications.userinfoEndpoint": "UserInfo endpoint",
  "applications.jwks": "JWKS",
  "applications.samlEntityId": "IdP entity ID",
  "applications.samlMetadata": "IdP metadata",
  "applications.samlSso": "Single sign-on service",
  "applications.samlCertificate": "Signing certificate",
  "applications.casBaseUrl": "CAS server URL",
  "applications.casLogin": "Login",
  "applications.casValidate": "Service validate",

  "settings.title": "Settings",
  "settings.subtitle": "System-wide behavior.",
  "settings.systemName": "System name",
  "settings.tokenTtl": "Session lifetime (minutes)",
  "settings.tokenTtlHelp":
    "How long a sign-in lasts before it expires. Between 5 and 43200.",
  "settings.registrationEnabled": "Allow self-service registration",
  "settings.registrationHelp":
    "When off, only administrators can create accounts. New accounts always get the User role.",
  "settings.saved": "Settings saved.",
  "settings.auditRetention": "Audit log retention (days)",
  "settings.auditRetentionHelp":
    "0 keeps everything, which is the default. Any other value must be at least 7, and entries older than it are deleted by the hourly cleanup — permanently, with no copy kept.",
  "settings.defaultLocale": "Language of messages sent",
  "settings.defaultLocaleHelp":
    "The language of mail and text messages sent to somebody who has stated no preference of their own. It does not affect the console, which each reader chooses for themselves and which is remembered in their browser.",
  "settings.defaultLocaleFollow": "Follow the deployment default",
  "settings.basicsLegend": "Basics",
  "settings.auditLegend": "Audit log",
  "settings.passwordLegend": "Password policy",
  "settings.passwordHelp":
    "Length is the rule that helps. Composition rules and expiry make passwords more guessable rather than less — NIST 800-63B recommends against both — and are provided for deployments audited against regimes that require them. If you have the choice, leave them off and raise the minimum length.",
  "settings.passwordMinLength": "Minimum length",
  "settings.passwordMinLengthHelp":
    "The built-in floor of 8 applies whatever this is set to.",
  "settings.requireUppercase": "Require an uppercase letter",
  "settings.requireLowercase": "Require a lowercase letter",
  "settings.requireDigit": "Require a digit",
  "settings.requireSymbol": "Require a symbol",
  "settings.passwordHistory": "Passwords remembered",
  "settings.passwordHistoryHelp":
    "How many previous passwords may not be reused. 0 does not check. Each one costs a hash comparison on every password change.",
  "settings.passwordMaxAge": "Maximum age (days)",
  "settings.passwordMaxAgeHelp":
    "0 never expires. An expired password cannot sign in until it is replaced.",
  "settings.registrationVerification": "Require a confirmed email address",
  "settings.registrationVerificationHelp":
    "A self-registered account cannot sign in until it opens a link sent to the address it gave. Without this, somebody can open an account under a colleague's address — and that address is where a password-reset link would be sent. Needs a mail relay; saving is refused without one.",
  "settings.lockoutLegend": "Failed sign-in lockout",
  "settings.lockoutHelp":
    "Locks an account after repeated wrong passwords. This is not a rate limit — it stops one account's password being guessed, and does nothing about the load a flood of attempts puts on the server. Keep the reverse proxy throttle as well.",
  "settings.lockoutThreshold": "Attempts before locking",
  "settings.lockoutThresholdHelp":
    "Consecutive failures within the window below. 0 switches lockout off.",
  "settings.lockoutDuration": "Lock duration (minutes)",
  "settings.lockoutDurationHelp":
    "Also the window failures are counted over. Further attempts while locked do not extend it.",

  "common.done": "Done",
  "common.delete": "Delete",

  "groups.title": "Groups",
  "groups.subtitle":
    "Sets of people. Separate from organizations, which are the org chart — somebody sits in one organization and belongs to any number of groups. Membership grants no permissions.",
  "groups.new": "New group",
  "groups.edit": "Edit group",
  "groups.name": "Name",
  "groups.description": "Description",
  "groups.colName": "Name",
  "groups.colDescription": "Description",
  "groups.colMembers": "Members",
  "groups.colSource": "Maintained by",
  "groups.source.ADMIN": "Console",
  "groups.source.SCIM": "Directory",
  "groups.members": "Members",
  "groups.membersOf": "Members — {0}",
  "groups.addMember": "Add someone",
  "groups.selectUser": "Choose a person…",
  "groups.add": "Add",
  "groups.remove": "Remove",
  "groups.colMemberName": "Name",
  "groups.colUsername": "Username",
  "groups.confirmDeleteTitle": "Delete this group?",
  "groups.confirmDelete":
    "“{0}” and its membership will be removed. The accounts themselves are not affected.",
  "groups.ofUser": "Groups",
  "groups.none": "None",
  "nav.groups": "Groups",

  "webhooks.title": "Event subscriptions (webhooks)",
  "webhooks.subtitle":
    "Where to send a signed notification when something changes here. Https only, and never an address inside your network.",
  "webhooks.new": "New subscription",
  "webhooks.name": "Name",
  "webhooks.url": "Endpoint URL",
  "webhooks.urlHint":
    "Must be https and publicly resolvable. Internal, loopback, and cloud-metadata addresses are refused — otherwise this server would make requests inside its own network on your behalf.",
  "webhooks.events": "Events",
  "webhooks.allEvents": "All events",
  "webhooks.allEventsHint":
    "Everything, including event types added in future versions",
  "webhooks.colName": "Name",
  "webhooks.colUrl": "Endpoint",
  "webhooks.colEvents": "Events",
  "webhooks.colStatus": "Status",
  "webhooks.deliveries": "Deliveries",
  "webhooks.deliveriesFor": "Deliveries — {0}",
  "webhooks.colEvent": "Event",
  "webhooks.colDeliveryStatus": "Result",
  "webhooks.colAttempts": "Attempts",
  "webhooks.colResponse": "Response",
  "webhooks.status.PENDING": "Pending",
  "webhooks.status.DELIVERED": "Delivered",
  "webhooks.status.FAILED": "Failed",
  "webhooks.created": "Subscription created",
  "webhooks.secret": "Signing secret",
  "webhooks.secretWarning":
    "Copy this secret now — it is shown once. Your endpoint uses it to verify that a delivery came from here: recompute HMAC-SHA256 over the timestamp header, a dot, and the raw body, then compare it to the signature header.",
  "webhooks.confirmDeleteTitle": "Delete this subscription?",
  "webhooks.confirmDelete":
    "“{0}” will stop receiving events immediately and its delivery history will be removed. To pause it instead, disable it.",

  // --- Provisioning: two directions ---
  "provisioning.title": "Directory integration",
  "provisioning.subtitle":
    "Accounts arriving from somewhere else, in either direction.",
  // Named for the direction rather than the protocol, because that is the
  // thing people get wrong: one has Portico reach out, the other has a
  // directory reach in, and the difference decides where you look when
  // accounts stop arriving.
  "provisioning.tab.directories": "Portico reads (LDAP / AD)",
  "provisioning.tab.scim": "The directory pushes (SCIM)",

  "directories.hint":
    "Portico connects to your directory and reads accounts out of it. An account that stops appearing is deactivated here; one that reappears comes back.",
  "directories.new": "Add a directory",
  "directories.edit": "Edit directory",
  "directories.colName": "Name",
  "directories.colAddress": "Address",
  "directories.colLastSync": "Last synchronized",
  "directories.colStatus": "Status",
  "directories.neverSynced": "Never",
  "directories.sync": "Synchronize",
  "directories.syncing": "Synchronizing…",
  "directories.history": "History",
  "directories.historyTitle": "Synchronizations — {0}",
  "directories.byScheduler": "scheduled",
  "directories.runSummary":
    "{0} created, {1} updated, {2} deactivated, {3} skipped.",
  "directories.emptyResult":
    "The directory returned no entries while accounts here belong to it. Nothing was changed — an empty result is far more often a wrong base DN or user filter than a directory everybody has left, and acting on it would deactivate every one of those accounts.",
  "directories.runFailed": "The synchronization failed.",
  "directories.outcome.SUCCEEDED": "Succeeded",
  "directories.outcome.FAILED": "Failed",
  "directories.outcome.RUNNING": "Running",

  "directories.name": "Name",
  "directories.namePlaceholder": "Head office AD",
  "directories.host": "Host",
  "directories.port": "Port",
  "directories.encryption": "Encryption",
  "directories.encryptionNone": "None (plain LDAP)",
  "directories.bindDn": "Bind DN",
  "directories.bindDnHelp":
    "The service account Portico reads as. Leave empty for an anonymous bind.",
  "directories.bindPassword": "Bind password",
  "directories.bindPasswordHelp":
    "Stored encrypted. Requires the deployment to have an encryption key configured.",
  "directories.bindPasswordStored":
    "A password is stored. Leave this empty to keep it, or type a new one to replace it.",
  "directories.bindPasswordUnchanged": "Unchanged",
  "directories.baseDn": "Base DN",
  "directories.attributes": "Which attribute carries which fact",
  "directories.attributesHint":
    "There are no defaults, because Active Directory and OpenLDAP disagree on every one of these. The presets fill them in and leave them editable — check them against your own directory rather than trusting them.",
  "directories.presetAD": "Active Directory",
  "directories.presetOpenLDAP": "OpenLDAP",
  "directories.userFilter": "User filter",
  "directories.attrUsername": "Username",
  "directories.attrDisplayName": "Display name",
  "directories.attrEmail": "Email",
  "directories.attrPhone": "Phone",
  "directories.attrExternalId": "External id",
  "directories.attrExternalIdHelp":
    "The directory's own stable identifier — objectGUID on Active Directory, entryUUID on OpenLDAP. This is what makes a rename a rename instead of a second account, so it is the field to get right.",
  "directories.organization": "Put accounts in",
  "directories.organizationHelp": "Where synchronized accounts land. Optional.",
  "directories.organizationNone": "No organization",

  "scim.title": "Directory provisioning (SCIM)",
  "scim.subtitle":
    "Credentials a directory uses to create, update, and deactivate accounts here, and to maintain group membership.",
  "scim.new": "New credential",
  "scim.name": "Name",
  "scim.nameHint":
    "How you will recognize it later, such as “Okta production”.",
  "scim.colName": "Name",
  "scim.colToken": "Token",
  "scim.colLastUsed": "Last used",
  "scim.colStatus": "Status",
  "scim.neverUsed": "Never used",
  "scim.issued": "Credential created",
  "scim.issuedWarning":
    "Copy this token now. It is shown once and stored only as a digest, so it cannot be shown again — if it is lost, issue another and delete this one.",
  "scim.token": "Token",
  "scim.activityTitle": "Recent synchronization",
  "scim.activityHint":
    "What a directory has changed here. The full record is in the audit log.",
  "scim.activityViewAll": "Audit log",
  "scim.colTime": "Time",
  "scim.colAction": "Action",
  "scim.colTarget": "Target",
  "scim.colDetail": "Detail",
  "scim.confirmDeleteTitle": "Delete this credential?",
  "scim.confirmDelete":
    "The directory using “{0}” will stop syncing immediately, and the token cannot be restored. To pause it instead, disable it.",

  "profile.title": "My profile",
  "profile.subtitle": "Your account details and password.",
  "profile.username": "Username",
  "profile.displayName": "Name",
  "profile.role": "Role",
  "profile.organization": "Organization",
  "profile.changePassword": "Change password",
  "profile.currentPassword": "Current password",
  "profile.newPassword": "New password",
  "profile.confirmNewPassword": "Confirm new password",
  "profile.passwordChanged": "Password changed. Please sign in again.",
  "login.forgotPassword": "Forgot your password?",
  "profile.details": "Your details",
  "profile.detailsSaved": "Saved.",
  "profile.closeAccount": "Close this account",
  "profile.closeAccountHelp":
    "Closing your account stops it signing in and ends everything currently signed in as you, immediately. Nothing is deleted — an administrator can reinstate it, and the audit trail keeps pointing at it.",
  "profile.closeAccountConfirm":
    "This takes effect at once. You will be signed out here and everywhere else, and you will not be able to sign in again without an administrator.",
  "profile.closeAccountPassword": "Your password, to confirm",
  "profile.closeAccountAction": "Close my account",
  "profile.sessionsTitle": "Signed-in devices",
  "profile.sessionsHelp":
    "Everything currently signed in as you. The address and browser are shown exactly as they arrived — if you do not recognize one, end it.",
  "profile.sessionCurrent": "This device",
  "profile.sessionNoAddress": "address not recorded",
  "profile.sessionNoAgent": "browser not recorded",
  "profile.sessionLastSeen": "Last used {0}",
  "profile.sessionEnd": "End",
  "profile.sessionEndMine": "Sign out here",
  "profile.sessionEndAll": "Sign out everywhere",
  "profile.email": "Email",
  "profile.phone": "Phone",
  "profile.contactHint":
    "Also works as a sign-in identifier, and is where a reset link would go.",
  "verify.title": "Confirm your account",
  "verify.subtitle": "Finishing the sign-up you started.",
  "verify.working": "Confirming…",
  "verify.done": "Your address is confirmed. You can sign in now.",
  "verify.noToken":
    "This address is missing its confirmation code. Open the link from the message rather than typing the address by hand.",
  "verify.resendAddress": "Send another confirmation to",
  "verify.resendAddressHelp":
    "The address you registered with. It has to be the address rather than the username, because that is where the message goes.",
  "verify.resend": "Send the confirmation message again",
  "verify.resent":
    "If that address has an account waiting to be confirmed, a message is on its way.",
  "verify.checkYourEmail":
    "Almost done. Open the link we sent to {0} to confirm the address, then sign in.",
  "recovery.title": "Reset your password",
  "recovery.subtitle": "We will send you a link to choose a new one.",
  "recovery.channel": "Send it by",
  "recovery.channel.EMAIL": "Email",
  "recovery.channel.SMS": "SMS",
  "recovery.email": "Email address",
  "recovery.phone": "Phone number",
  "recovery.submit": "Send the link",
  "recovery.sending": "Sending…",
  "recovery.sent":
    "If that matches an account, a reset link is on its way. It expires in {0} minutes.",
  "recovery.unavailable":
    "This deployment has no way to send a reset link. Ask an administrator to reset your password.",
  "recovery.backToSignIn": "Back to sign in",
  "reset.title": "Choose a new password",
  "reset.submit": "Set the password",
  "reset.saving": "Saving…",
  "reset.done": "Your password has been changed. Sign in with it.",
  "reset.missingToken":
    "This link is incomplete. Open the one from your message, or ask for a new link.",
  "reset.requestAnother": "Request a new link",
  "brand.descriptor": "Identity Platform",
  "nav.language": "Language",
  // Each names the question its group answers. "Operations" used to be the
  // second of two, which is how application registration ended up beside
  // the password rules — a label that means "the rest" collects whatever
  // the first label will not take.
  "nav.group.directory": "Directory",
  "nav.group.integration": "Integration",
  "nav.group.audit": "Audit",
  "nav.group.system": "System",
  "nav.group.account": "Account",
} as const;

export type TranslationKey = keyof typeof enUS;
