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
)

// User is an account. It never carries the password hash outside the
// service layer.
type User struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Phone       string     `json:"phone"`
	Email       string     `json:"email"`
	Role        Role       `json:"role"`
	Status      Status     `json:"status"`
	Source      UserSource `json:"source"`

	// OrganizationID is empty when the user belongs to no organization,
	// which is the default for self-registered accounts (§3.4.2).
	OrganizationID string `json:"organizationId"`
	// OrganizationName is resolved for display so clients never have to
	// show a bare id.
	OrganizationName string `json:"organizationName"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
