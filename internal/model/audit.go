package model

import "time"

// LogKind groups audit entries. The five kinds correspond to the log types
// required by §3.9.
type LogKind string

const (
	// LogLogin records sign-in attempts, successful or not.
	LogLogin LogKind = "LOGIN"
	// LogOperation records administrative changes.
	LogOperation LogKind = "OPERATION"
	// LogAuth records token verification and permission rejections.
	LogAuth LogKind = "AUTH"
	// LogRegistration records account creation, whether self-service,
	// by an administrator, or by bulk import.
	LogRegistration LogKind = "REGISTRATION"
	// LogOrganization records organization changes and membership moves.
	LogOrganization LogKind = "ORGANIZATION"
)

// Valid reports whether k is a known log kind.
func (k LogKind) Valid() bool {
	switch k {
	case LogLogin, LogOperation, LogAuth, LogRegistration, LogOrganization:
		return true
	}
	return false
}

// LogResult is whether the recorded action succeeded.
type LogResult string

// Whether the recorded action succeeded.
const (
	LogSuccess LogResult = "SUCCESS"
	LogFailure LogResult = "FAILURE"
)

// Audit action verbs. Keeping these as constants rather than free-form
// strings means the log stays queryable and a typo cannot create a silent
// second category.
const (
	ActionLoginSuccess = "LOGIN_SUCCESS"
	ActionLoginFailure = "LOGIN_FAILURE"
	ActionLogout       = "LOGOUT"
	// Ending one session rather than all of them. Separate verbs for
	// your own and somebody else's: an administrator ending a session
	ActionSessionRevokeSelf = "SESSION_REVOKE_SELF"
	ActionSessionRevoke     = "SESSION_REVOKE"
	ActionLogoutEverywhere  = "LOGOUT_EVERYWHERE"
	ActionPasswordReset     = "PASSWORD_RESET"
	ActionPasswordSelf      = "PASSWORD_CHANGE_SELF"
	// A change forced by expiry, done without a session because Login
	// refuses to issue one for an expired password.
	ActionPasswordExpiredChange = "PASSWORD_CHANGE_EXPIRED"

	// The password-recovery pair. They are separate verbs because the
	// interesting question after the fact is whether a request was ever
	// completed: a burst of requests with no completions is someone probing,
	// and one completion nobody remembers asking for is the incident.
	ActionPasswordRecoveryRequest  = "PASSWORD_RECOVERY_REQUEST"
	ActionPasswordRecoveryComplete = "PASSWORD_RECOVERY_COMPLETE"

	// ActionAuthorize is a person completing an OAuth authorization request:
	// user X signed in to application Y at time T. It is separate from
	// LOGIN_SUCCESS because they answer different questions — one is "who
	// signed in to Portico", the other "what did that let them into" — and a
	// single sign-in can authorize several applications over its lifetime.
	ActionAuthorize = "OAUTH_AUTHORIZE"

	// ActionSAMLAuthenticate is a person completing a SAML authentication
	// request: an assertion about them was issued to a service provider.
	// Separate from OAUTH_AUTHORIZE because the protocol is part of the
	// answer to "how did this system come to trust that identity".
	ActionSAMLAuthenticate = "SAML_AUTHENTICATE"

	// ActionCASAuthenticate is a person obtaining a CAS service ticket.
	ActionCASAuthenticate = "CAS_AUTHENTICATE"

	// ActionProfileSelf is a user editing their own details, as distinct
	// from USER_UPDATE, which is an administrator editing someone else's.
	ActionProfileSelf = "PROFILE_UPDATE_SELF"

	ActionUserCreate  = "USER_CREATE"
	ActionUserUpdate  = "USER_UPDATE"
	ActionUserEnable  = "USER_ENABLE"
	ActionUserDisable = "USER_DISABLE"
	ActionUserImport  = "USER_IMPORT"
	ActionUserUnlock  = "USER_UNLOCK"
	ActionUserSelfReg = "USER_SELF_REGISTER"

	ActionOrgCreate  = "ORG_CREATE"
	ActionOrgUpdate  = "ORG_UPDATE"
	ActionOrgEnable  = "ORG_ENABLE"
	ActionOrgDisable = "ORG_DISABLE"
	ActionOrgAssign  = "ORG_ASSIGN"

	// Application registration, one set of verbs per protocol.
	//
	// These are the most privileged administrative acts in the system, and
	// the reason they are audited at this weight: registering a relying
	// party, a service provider, or a CAS service decides who may be handed
	// credentials about this tenant's people. A registration nobody
	// remembers making is the shape a compromise takes here, so each of
	// them has to be answerable after the fact.
	ActionClientCreate  = "OAUTH_CLIENT_CREATE"
	ActionClientUpdate  = "OAUTH_CLIENT_UPDATE"
	ActionClientEnable  = "OAUTH_CLIENT_ENABLE"
	ActionClientDisable = "OAUTH_CLIENT_DISABLE"
	// An audit verb, not a credential — gosec matches on the word "SECRET"
	// in the identifier. Renaming it to appease the scanner would make the
	// trail say something other than what happened.
	ActionClientSecretRotate = "OAUTH_CLIENT_SECRET_ROTATE" //nolint:gosec // an audit action verb

	ActionSPCreate  = "SAML_SP_CREATE"
	ActionSPUpdate  = "SAML_SP_UPDATE"
	ActionSPEnable  = "SAML_SP_ENABLE"
	ActionSPDisable = "SAML_SP_DISABLE"

	// A directory Portico reads accounts out of. Registering one hands a
	// service account's credential to this server and makes an external
	// system the source of truth for who exists here, so it is audited at
	// the same weight as an application registration. The synchronizations
	// themselves are recorded separately, per run, because their question is
	// "when did this start" rather than "who allowed it".
	ActionLDAPSourceCreate  = "LDAP_SOURCE_CREATE"
	ActionLDAPSourceUpdate  = "LDAP_SOURCE_UPDATE"
	ActionLDAPSourceEnable  = "LDAP_SOURCE_ENABLE"
	ActionLDAPSourceDisable = "LDAP_SOURCE_DISABLE"
	ActionLDAPSync          = "LDAP_SYNC"

	ActionCASServiceCreate  = "CAS_SERVICE_CREATE"
	ActionCASServiceUpdate  = "CAS_SERVICE_UPDATE"
	ActionCASServiceEnable  = "CAS_SERVICE_ENABLE"
	ActionCASServiceDisable = "CAS_SERVICE_DISABLE"

	// Provisioning credentials, on the same footing as the application
	// registrations above and for the same reason: one of these can create,
	// change, and deactivate accounts across the whole tenant without a
	// person being present. A credential nobody remembers issuing is the
	// shape a compromise takes here.
	//
	// Audit verbs, not credentials — gosec matches on the word "CREDENTIAL"
	// in the identifier, the same false positive as
	// ActionClientSecretRotate above. Renaming them to satisfy the scanner
	// would make the trail say something other than what happened.
	ActionSCIMCredentialCreate  = "SCIM_CREDENTIAL_CREATE"  //nolint:gosec // an audit action verb
	ActionSCIMCredentialEnable  = "SCIM_CREDENTIAL_ENABLE"  //nolint:gosec // an audit action verb
	ActionSCIMCredentialDisable = "SCIM_CREDENTIAL_DISABLE" //nolint:gosec // an audit action verb
	ActionSCIMCredentialDelete  = "SCIM_CREDENTIAL_DELETE"  //nolint:gosec // an audit action verb

	// What a provisioning system did, as distinct from what an administrator
	// did. USER_CREATE by a person and a directory sync creating the same
	// account are different events to whoever is reading the trail later,
	// and collapsing them would make an automated deprovisioning
	// indistinguishable from somebody deciding to disable a colleague.
	ActionSCIMUserCreate  = "SCIM_USER_CREATE"
	ActionSCIMUserUpdate  = "SCIM_USER_UPDATE"
	ActionSCIMUserDisable = "SCIM_USER_DISABLE"
	ActionSCIMUserEnable  = "SCIM_USER_ENABLE"

	// Outbound event subscriptions. Audited because a subscription is an
	// instruction to send this tenant's directory activity to an address
	// somebody chose — who exists, when they were disabled — and one nobody
	// remembers creating is an exfiltration channel with a signature on it.
	ActionWebhookCreate  = "WEBHOOK_CREATE"
	ActionWebhookUpdate  = "WEBHOOK_UPDATE"
	ActionWebhookEnable  = "WEBHOOK_ENABLE"
	ActionWebhookDisable = "WEBHOOK_DISABLE"
	ActionWebhookDelete  = "WEBHOOK_DELETE"

	// Groups and their membership. Membership is audited as its own verb
	// rather than as a group update because the questions differ: "who
	// renamed this group" and "when was this person added to it" are asked
	// by different people for different reasons.
	ActionGroupCreate        = "GROUP_CREATE"
	ActionGroupUpdate        = "GROUP_UPDATE"
	ActionGroupDelete        = "GROUP_DELETE"
	ActionGroupMemberAdd     = "GROUP_MEMBER_ADD"
	ActionGroupMemberRemove  = "GROUP_MEMBER_REMOVE"
	ActionGroupMemberReplace = "GROUP_MEMBER_REPLACE"

	ActionSettingsUpdate = "SETTINGS_UPDATE"
)

// There is deliberately no verb for a downstream synchronisation.
//
// A DOWNSTREAM_SYNC constant existed here and was never written, and the
// reason is worth recording rather than fixing: the event it named is not
// observable from this side. A downstream system pulls a profile with the
// user's own token — a read — and whether it went on to create an account,
// update one, or discard the response is something only that system knows.
// Recording "somebody read a profile" under a name that claims a
// synchronisation happened would put a confident sentence in the trail
// about something this server did not witness.
//
// The questions a reader actually asks are already answered: LOGIN_SUCCESS
// says who signed in, and OAUTH_AUTHORIZE, SAML_AUTHENTICATE, and
// CAS_AUTHENTICATE say which applications that let them into. A downstream
// that needs its own trail has to keep it, because it is the only party
// that can.

// AuditLog is one recorded event.
type AuditLog struct {
	ID     string    `json:"id"`
	Kind   LogKind   `json:"kind"`
	Action string    `json:"action"`
	Result LogResult `json:"result"`

	// ActorID is empty when the actor could not be identified, such as a
	// failed login against a username that does not exist.
	ActorID   string `json:"actorId"`
	ActorName string `json:"actorName"`

	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	TargetName string `json:"targetName"`

	Detail string `json:"detail"`
	IP     string `json:"ip"`

	CreatedAt time.Time `json:"createdAt"`
}
