package model

import "time"

// LDAPSource is a directory Portico reads accounts out of.
//
// The bind password is deliberately absent from this type. It exists in the
// database, sealed, and is opened only inside the synchronization path — so
// there is no shape in which it can reach a response by somebody forgetting
// a `json:"-"`.
type LDAPSource struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	Name     string `json:"name"`

	Host       string `json:"host"`
	Port       int    `json:"port"`
	Encryption string `json:"encryption"`

	// BindDN empty means an anonymous bind.
	BindDN string `json:"bindDn"`
	// HasBindPassword reports whether a credential is stored, which is what
	// a form needs to know: it can show "set" and offer to replace, without
	// the value ever being sent.
	HasBindPassword bool `json:"hasBindPassword"`

	BaseDN     string `json:"baseDn"`
	UserFilter string `json:"userFilter"`

	AttrUsername    string `json:"attrUsername"`
	AttrDisplayName string `json:"attrDisplayName"`
	AttrEmail       string `json:"attrEmail"`
	AttrPhone       string `json:"attrPhone"`
	AttrExternalID  string `json:"attrExternalId"`

	// OrganizationID is where synchronized accounts land, empty for none.
	OrganizationID   string `json:"organizationId"`
	OrganizationName string `json:"organizationName"`

	Status Status `json:"status"`

	// LastSyncedAt is nil until the first run finishes, which is how the
	// console tells "never ran" from "ran and found nothing".
	LastSyncedAt *time.Time `json:"lastSyncedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Sync outcomes.
const (
	SyncRunning   = "RUNNING"
	SyncSucceeded = "SUCCEEDED"
	SyncFailed    = "FAILED"
)

// LDAPSyncRun is what one synchronization did.
//
// Kept per run rather than as a last-result column on the source: the
// question asked when a directory integration misbehaves is never "what is
// the state now" but "when did this start", and a single overwritten result
// cannot answer it.
type LDAPSyncRun struct {
	ID       string `json:"id"`
	SourceID string `json:"sourceId"`
	// ActorName is empty for the scheduler, which is not a person and must
	// not be recorded as one.
	ActorName string `json:"actorName"`

	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Outcome    string     `json:"outcome"`

	CreatedCount     int `json:"createdCount"`
	UpdatedCount     int `json:"updatedCount"`
	DeactivatedCount int `json:"deactivatedCount"`
	// SkippedCount is entries the directory returned that could not become
	// an account. Counted rather than fatal: one malformed entry in ten
	// thousand must not stop the rest.
	SkippedCount int `json:"skippedCount"`
	// SkippedDetail is why, grouped by reason with an example of each.
	//
	// A count alone sends an operator to the documentation, which says a
	// skip is most often a username collision — and when it is not, that is
	// a wrong lead rather than no lead. Empty when nothing was skipped.
	SkippedDetail string `json:"skippedDetail"`

	// ErrorCode is set when Portico itself refused, and empty when the
	// directory reported the failure. The console renders a known code in
	// the reader's language and shows the text below verbatim otherwise —
	// an LDAP server's own wording is what somebody will search for.
	ErrorCode string `json:"errorCode,omitempty"`
	Error     string `json:"error,omitempty"`
}
