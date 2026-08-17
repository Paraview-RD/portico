package model

import "time"

// Tenant is an isolated slice of the system. Users, organizations, audit
// logs, and settings all belong to exactly one, and nothing crosses the
// boundary: see docs/database-conventions.md, "Tenant isolation".
//
// V0.1 has no cross-tenant role. Tenants are created and disabled from the
// command line by whoever operates the deployment, and each tenant's own
// administrators manage everything inside it and nothing outside it
// (docs/requirements/v0.1-requirements.md §3.1).
type Tenant struct {
	ID string `json:"id"`
	// Code is what a user types at sign-in to say which tenant they mean.
	Code string `json:"code"`
	Name string `json:"name"`
	// Status of DISABLED refuses sign-in without deleting anything.
	Status Status `json:"status"`

	// ExpiresAt is when sign-in stops being allowed, or nil for never —
	// which is every tenant except one a self-service trial created.
	//
	// Reaching it disables the tenant rather than deleting it, so it is
	// reversible: an operator can move the deadline and switch it back on.
	// Deletion is a separate, later step.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TenantOverview is a tenant and how much is inside it.
//
// Counts and nothing else. This is the one shape in the system that crosses
// the tenant boundary, and what makes that acceptable is exactly what is
// absent from it: no username, no address, no organization name, nothing that
// belongs to the people in a tenant other than how many of them there are.
// Adding a field here is a decision about the isolation boundary — the rule
// is that an operator may learn a tenant's size and never its contents.
type TenantOverview struct {
	Tenant
	Users         int64 `json:"users"`
	ActiveUsers   int64 `json:"activeUsers"`
	Organizations int64 `json:"organizations"`
	Applications  int64 `json:"applications"`
	// LastActivity is when anything was last recorded in that tenant's audit
	// trail, and is absent for a tenant where nothing ever has been — which
	// is the row an operator is usually looking for.
	LastActivity *time.Time `json:"lastActivity"`
}

// DefaultTenantCode is the tenant a deployment gets on first start, and the
// one a sign-in resolves to when it names none.
//
// It is what keeps a single-tenant deployment — which is most of them —
// from having to know that tenants exist at all, while the same build still
// serves a multi-tenant one (§3.1, "支持单租户/多租户部署模式兼容").
const DefaultTenantCode = "default"
