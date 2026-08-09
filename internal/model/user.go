// Package model holds the domain types shared by the service and HTTP
// layers. These are distinct from the sqlc row structs: rows carry storage
// concerns (nullable columns, string enums), while these carry meaning.
package model

import "time"

// Role is one of the two fixed roles. The MVP deliberately has no custom
// roles and no permission assignment — see requirements §3.3.
type Role string

const (
	// RoleSuperAdmin can operate every administrative function.
	RoleSuperAdmin Role = "SUPER_ADMIN"
	// RoleUser can only manage their own profile and password.
	RoleUser Role = "USER"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	return r == RoleSuperAdmin || r == RoleUser
}

// IsAdmin reports whether r grants administrative access.
func (r Role) IsAdmin() bool { return r == RoleSuperAdmin }

// Status is the enabled/disabled state shared by users and organizations.
type Status string

// The enabled and disabled states. Records are disabled, never deleted.
const (
	StatusActive   Status = "ACTIVE"
	StatusDisabled Status = "DISABLED"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	return s == StatusActive || s == StatusDisabled
}

// UserSource records how an account came to exist, which the registration
// log reports on (§3.9).
type UserSource string

const (
	// SourceAdmin is an account created by an administrator.
	SourceAdmin UserSource = "ADMIN"
	// SourceImport is an account created by an Excel bulk import.
	SourceImport UserSource = "IMPORT"
	// SourceRegistration is a self-registered account.
	SourceRegistration UserSource = "REGISTRATION"
	// SourceLDAP is an account a directory synchronization pulled in. It is
	// distinguished from SourceSCIM even though both mean "a directory owns
	// this", because the direction differs and so does what an operator does
	// about it: a SCIM account stopped arriving because the directory stopped
	// pushing, and an LDAP account stopped arriving because a run here
	// failed — and there is a run record to read.
	SourceLDAP UserSource = "LDAP"
	// SourceSCIM is an account a directory created through provisioning. It
	// is worth distinguishing from the rest: an administrator editing one is
	// editing something the next sync may overwrite, and the source is what
	// says so.
	SourceSCIM UserSource = "SCIM"
)

// UserProfile is the optional half of an account: everything that describes
// a person rather than authenticating one.
//
// Nested rather than flattened onto User, for two reasons. It keeps the
// fields that decide access — username, role, status — visibly separate from
// the ones that merely describe, so a reviewer reading a handler can see at
// a glance which is being touched. And it gives the console one object to
// send back, instead of thirty fields that each have to be remembered
// individually in an update.
type UserProfile struct {
	// Name, in the parts a directory sends. DisplayName stays on User: it is
	// the one thing every screen shows, and it exists whether or not any of
	// these do.
	NameFormatted   string `json:"nameFormatted"`
	FamilyName      string `json:"familyName"`
	GivenName       string `json:"givenName"`
	MiddleName      string `json:"middleName"`
	HonorificPrefix string `json:"honorificPrefix"`
	HonorificSuffix string `json:"honorificSuffix"`

	NickName   string `json:"nickName"`
	ProfileURL string `json:"profileUrl"`
	PhotoURL   string `json:"photoUrl"`

	Title             string `json:"title"`
	UserType          string `json:"userType"`
	PreferredLanguage string `json:"preferredLanguage"`
	Locale            string `json:"locale"`
	Timezone          string `json:"timezone"`

	AddressFormatted string `json:"addressFormatted"`
	StreetAddress    string `json:"streetAddress"`
	Locality         string `json:"locality"`
	Region           string `json:"region"`
	PostalCode       string `json:"postalCode"`
	Country          string `json:"country"`

	EmployeeNumber string `json:"employeeNumber"`
	CostCenter     string `json:"costCenter"`
	// Department is free text as a directory sends it, and is deliberately
	// not the organization tree. A directory often sends a name that
	// corresponds to nothing in this tenant; dropping it would lose
	// information an operator can use to place the person later.
	Department string `json:"department"`

	// ManagerID is who this person reports to, by id so it survives a
	// rename. ManagerName is resolved for display so a client never has to
	// show a bare identifier.
	ManagerID   string `json:"managerId"`
	ManagerName string `json:"managerName"`
}

// OrganizationRef names an organization without carrying the whole of it.
type OrganizationRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

// User is an account. It never carries the password hash outside the
// service layer.
type User struct {
	ID string `json:"id"`
	// TenantID is the isolation boundary the account lives in. It is
	// reported so a downstream system syncing users from here can keep them
	// apart (§3.5); it is never accepted as input.
	TenantID    string     `json:"tenantId"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Phone       string     `json:"phone"`
	Email       string     `json:"email"`
	Role        Role       `json:"role"`
	Status      Status     `json:"status"`
	Source      UserSource `json:"source"`

	// The attributes below come from SCIM 2.0's core User schema
	// (RFC 7643 §4.1) and its enterprise extension (§4.3), rather than from
	// this project's imagination. Portico is already a SCIM server, so using
	// those names means a directory's fields land where they belong and the
	// meaning of each is settled by a specification.
	//
	// Every one is optional. An account with only a username and a display
	// name is a complete account.
	Profile UserProfile `json:"profile"`

	// Attachments are additional organizations this person is involved with,
	// beside the one they primarily belong to.
	//
	// Advisory. They grant nothing, synchronize nowhere, and do not change
	// OrganizationID — which remains the one authoritative membership, the
	// one SCIM and the directory sync write and the one an export names.
	// Populated only where a caller asked for a single account; a page of
	// users does not pay for them.
	Attachments []OrganizationRef `json:"attachments,omitempty"`

	// OrganizationID is empty when the user belongs to no organization,
	// which is the default for self-registered accounts (§3.4.2).
	OrganizationID string `json:"organizationId"`
	// OrganizationName is resolved for display so clients never have to
	// show a bare id.
	OrganizationName string `json:"organizationName"`

	// ClosedAt is when the account holder closed it themselves, and nil for
	// every other reason an account might be disabled. The distinction is
	// the reason the field exists: "they left" and "we suspended them" look
	// identical in the status column and call for different responses.
	ClosedAt *time.Time `json:"closedAt,omitempty"`

	// LockedUntil is set while the account is locked out after repeated
	// failed sign-ins, and nil otherwise. It is reported so an administrator
	// looking at a user who "cannot log in" can see why without reading the
	// audit trail.
	LockedUntil *time.Time `json:"lockedUntil,omitempty"`

	// ExternalID is the identifier a provisioning system knows this account
	// by — SCIM's externalId — and is empty for accounts created any other
	// way. It is reported so an administrator can see that an account is
	// managed by a directory before wondering why their edit was overwritten
	// by the next sync.
	ExternalID string `json:"externalId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RecoveryChannel is how a password-reset link reaches its owner (§3.5).
type RecoveryChannel string

const (
	// RecoveryEmail sends the link to the account's bound email address.
	RecoveryEmail RecoveryChannel = "EMAIL"
	// RecoverySMS sends it to the bound phone number.
	RecoverySMS RecoveryChannel = "SMS"
)

// Valid reports whether c is a known channel.
func (c RecoveryChannel) Valid() bool {
	return c == RecoveryEmail || c == RecoverySMS
}
