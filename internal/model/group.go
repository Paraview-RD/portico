package model

import "time"

// GroupSource says who maintains a group.
type GroupSource string

const (
	// GroupSourceAdmin is a group somebody created in the console.
	GroupSourceAdmin GroupSource = "ADMIN"
	// GroupSourceSCIM is a group a directory pushed. Worth distinguishing:
	// an administrator editing one is editing something the next sync may
	// overwrite.
	GroupSourceSCIM GroupSource = "SCIM"
)

// Group is a set of people.
//
// Not the organization chart — see the schema comment on the groups table.
// An organization is where somebody sits, one of them, in a tree; a group is
// a set they belong to, any number of them, flat.
//
// Membership grants nothing. A directory says who somebody is, not what they
// may do.
type Group struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	// ExternalID is what a provisioning system knows it by, empty otherwise.
	ExternalID  string      `json:"externalId,omitempty"`
	Source      GroupSource `json:"source"`
	MemberCount int64       `json:"memberCount"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

// GroupMember is one person in a group.
type GroupMember struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// GroupRef is a group as it appears on a user: enough to name it, no more.
type GroupRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}
