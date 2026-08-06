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

const (
	LogSuccess LogResult = "SUCCESS"
	LogFailure LogResult = "FAILURE"
)

// Audit action verbs. Keeping these as constants rather than free-form
// strings means the log stays queryable and a typo cannot create a silent
// second category.
const (
	ActionLoginSuccess  = "LOGIN_SUCCESS"
	ActionLoginFailure  = "LOGIN_FAILURE"
	ActionLogout        = "LOGOUT"
	ActionPasswordReset = "PASSWORD_RESET"
	ActionPasswordSelf  = "PASSWORD_CHANGE_SELF"

	ActionUserCreate  = "USER_CREATE"
	ActionUserUpdate  = "USER_UPDATE"
	ActionUserEnable  = "USER_ENABLE"
	ActionUserDisable = "USER_DISABLE"
	ActionUserImport  = "USER_IMPORT"
	ActionUserSelfReg = "USER_SELF_REGISTER"

	ActionOrgCreate  = "ORG_CREATE"
	ActionOrgUpdate  = "ORG_UPDATE"
	ActionOrgEnable  = "ORG_ENABLE"
	ActionOrgDisable = "ORG_DISABLE"
	ActionOrgAssign  = "ORG_ASSIGN"

	ActionSettingsUpdate = "SETTINGS_UPDATE"

	ActionDownstreamSync = "DOWNSTREAM_SYNC"
)

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
