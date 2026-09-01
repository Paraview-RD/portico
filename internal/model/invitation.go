package model

import "time"

// Invitation is an administrator-issued, quota-limited code that gates
// self-registration. See
// docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md for the
// reasoning behind its status model.
type Invitation struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	Code     string `json:"code"`

	// OrganizationID and GroupIDs are applied to the new account at
	// redemption time, in the same transaction as the account itself. Empty
	// means no organization; a nil or empty GroupIDs means no groups.
	OrganizationID string   `json:"organizationId,omitempty"`
	GroupIDs       []string `json:"groupIds"`

	Quota     int `json:"quota"`
	UsedCount int `json:"usedCount"`

	// ExpiresAt is nil for a code that never expires.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`

	// Status is ACTIVE or DISABLED only. "Exhausted" (UsedCount >= Quota)
	// and "expired" (past ExpiresAt) are not stored here — a caller derives
	// them from the fields above, the same way the server does at
	// redemption time. A client that reads only Status will show a code as
	// ACTIVE after its quota is spent, which is correct: ACTIVE describes
	// whether an administrator has disabled it, not whether it currently
	// has anything left to give.
	Status string `json:"status"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
