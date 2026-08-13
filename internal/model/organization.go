package model

import "time"

// Organization is a single flat grouping of users. The MVP has no
// hierarchy, no org-scoped permissions, and no owner — see §3.4.3 for the
// full list of what is deliberately excluded.
type Organization struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code"`
	Remark string `json:"remark"`
	// ParentID is empty for a root. The list returns the tree flat, with
	// each row naming its parent, because a nested shape would have to be
	// unflattened by every consumer and cannot be sorted or filtered
	// without rebuilding it.
	ParentID  string    `json:"parentId"`
	Status    Status    `json:"status"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// ManagerID is whoever is responsible for this organization, and
	// ManagerName is resolved for display. Empty for nobody nominated.
	//
	// It grants nothing. Being named here does not confer administration of
	// the organization and is consulted by no authorization decision — this
	// version has two fixed roles, and a field that quietly became a third
	// would be the worst way to acquire one.
	ManagerID   string `json:"managerId"`
	ManagerName string `json:"managerName"`

	// UserCount is populated by list endpoints so the UI can show how many
	// accounts a disable would affect.
	UserCount int64 `json:"userCount"`
}

// Administrator scopes. SELF is one organization; SUBTREE is it and every
// descendant.
//
// Two values and no more. A third dimension — may manage people but not the
// structure — would be a permission model designed one column at a time,
// which is what the planned feature is for.
const (
	OrgScopeSelf    = "SELF"
	OrgScopeSubtree = "SUBTREE"
)

// ValidOrgScope reports whether s is a scope this version records.
func ValidOrgScope(s string) bool {
	return s == OrgScopeSelf || s == OrgScopeSubtree
}

// OrganizationAdministrator is somebody recorded as administering an
// organization, for a feature that does not exist yet.
//
// It grants nothing. No authorization decision reads it, and a test in
// internal/server requires that to stay true — see migration 00020 for why
// the rows are collected before anything can act on them.
type OrganizationAdministrator struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	// Status is the account's, so a screen can show that somebody named here
	// is currently disabled rather than quietly listing them as usable.
	Status Status `json:"status"`
	// Scope is SELF or SUBTREE.
	Scope string `json:"scope"`
	// GrantedBy is the account that made the assignment, and GrantedByName
	// is resolved for display.
	GrantedBy     string    `json:"grantedBy"`
	GrantedByName string    `json:"grantedByName"`
	GrantedAt     time.Time `json:"grantedAt"`
}

// AdministeredOrganization is the other direction: an organization somebody
// is recorded as administering. Same nothing-is-granted caveat.
type AdministeredOrganization struct {
	OrganizationRef
	Scope     string    `json:"scope"`
	GrantedAt time.Time `json:"grantedAt"`
}
