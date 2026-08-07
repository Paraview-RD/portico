package handler

import (
	"net/http"
	"strings"

	"github.com/paraview/portico/internal/model"
)

// TenantHeader names the tenant on requests that have no authenticated
// caller to take it from.
const TenantHeader = "X-Portico-Tenant"

// tenantQueryParam is the same thing in a query string, for the cases where
// setting a header is inconvenient — following a sign-in link, for
// instance.
const tenantQueryParam = "tenant"

// resolvePublicTenant determines which tenant an unauthenticated request is
// for.
//
// It must only be called from a public handler. Authenticated requests take
// their tenant from the principal and nowhere else: if a signed-in caller
// could name their tenant, an administrator of one tenant would reach every
// other tenant by sending a header. TestAuthenticatedRequestsIgnoreTenantHeader
// holds that line — it signs in as one tenant's administrator, sends the
// header naming another, and asserts the first tenant's data comes back.
//
// The order is explicit body field, then header, then query parameter, then
// the default tenant. The default is what lets a single-tenant deployment —
// which is most of them — never mention tenants at all, while the same
// build serves a multi-tenant one (§3.1).
func (h *Handler) resolvePublicTenant(r *http.Request, fromBody string) (model.Tenant, error) {
	code := strings.TrimSpace(fromBody)
	if code == "" {
		code = strings.TrimSpace(r.Header.Get(TenantHeader))
	}
	if code == "" {
		code = strings.TrimSpace(r.URL.Query().Get(tenantQueryParam))
	}

	// An empty code resolves to the default tenant; Resolve owns that rule
	// so every entry point agrees on it.
	return h.tenants.Resolve(r.Context(), code)
}
