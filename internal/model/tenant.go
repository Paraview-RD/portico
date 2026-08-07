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

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DefaultTenantCode is the tenant a deployment gets on first start, and the
// one a sign-in resolves to when it names none.
//
// It is what keeps a single-tenant deployment — which is most of them —
// from having to know that tenants exist at all, while the same build still
// serves a multi-tenant one (§3.1, "支持单租户/多租户部署模式兼容").
const DefaultTenantCode = "default"
