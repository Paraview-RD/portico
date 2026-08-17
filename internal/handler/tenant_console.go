package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// The operator console: the one place in this API where a request learns that
// another tenant exists.
//
// Every other handler in this package is scoped to the caller's own tenant,
// and the file this one sits beside says so in its package comment. So the
// boundaries here are written out rather than inherited, and there are three
// of them:
//
//  1. The routes are not registered unless PORTICO_TENANT_CONSOLE is on. On
//     a deployment that did not ask for this, they do not exist — which is
//     what every deployment before this feature had.
//  2. The caller must administer the default tenant. Not a role that can be
//     granted from inside the product: on a deployment where `default` is one
//     customer among several, granting it would let that customer's
//     administrator enumerate the others, which is why (1) is a decision made
//     by whoever runs the deployment rather than by anybody signed in to it.
//  3. What crosses is counts and never contents. See model.TenantOverview.
//
// Somebody who fails (2) is answered 404 rather than 403. A 403 would confirm
// that the console exists and that they are merely the wrong person for it,
// which is a fact about the deployment they have no business learning from a
// tenant they administer.

// operatorOnly reports whether this caller may use the operator console, and
// writes the refusal if not.
//
// Resolved by reading the default tenant rather than by comparing a code
// carried on the principal, because the principal carries a tenant ID and
// nothing else — and a comparison against a code would need something to have
// mapped one to the other, which is this read.
func (h *Handler) operatorOnly(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		httpx.Fail(w, r, httpx.Unauthorized("UNAUTHENTICATED", "Authentication is required."))
		return auth.Principal{}, false
	}

	base, err := h.tenants.Resolve(r.Context(), model.DefaultTenantCode)
	if err != nil {
		httpx.Fail(w, r, err)
		return auth.Principal{}, false
	}
	if principal.TenantID != base.ID {
		httpx.Fail(w, r, httpx.NotFound("NOT_FOUND", "Not found."))
		return auth.Principal{}, false
	}
	return principal, true
}

// ListTenants answers with every tenant and how much is inside it.
func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.operatorOnly(w, r); !ok {
		return
	}

	overview, err := h.tenants.Overview(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, overview)
}

type tenantStatusRequest struct {
	Status string `json:"status"`
	// Confirm is the tenant's own code, typed by the person doing this.
	//
	// A second field rather than a dialog, because a dialog is a thing the
	// browser draws and the API knows nothing about — and this endpoint is
	// reachable without one. What it buys is that disabling the wrong tenant
	// takes a mistake in two places at once: switching off a tenant signs
	// every account in it out immediately, and the people it happens to have
	// no way to undo it and nobody obvious to ask.
	Confirm string `json:"confirm"`
}

// SetTenantStatus disables a tenant, or turns one back on.
func (h *Handler) SetTenantStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.operatorOnly(w, r)
	if !ok {
		return
	}

	code := chi.URLParam(r, "code")
	var req tenantStatusRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}

	status := model.Status(req.Status)
	if status != model.StatusActive && status != model.StatusDisabled {
		httpx.Fail(w, r, httpx.BadRequest("INVALID_STATUS",
			"Status must be ACTIVE or DISABLED."))
		return
	}
	if req.Confirm != code {
		httpx.Fail(w, r, httpx.BadRequest("TENANT_CONFIRM_MISMATCH",
			"Type the tenant code to confirm."))
		return
	}
	// The default tenant is where this console lives and where its operator
	// signs in. Disabling it would refuse the next sign-in of the person who
	// just did it, and there is no screen anywhere that could undo it — the
	// way back is the command line, on the machine, which is not where
	// somebody who clicked a button in a browser is.
	if code == model.DefaultTenantCode && status == model.StatusDisabled {
		httpx.Fail(w, r, httpx.UnprocessableEntity("TENANT_CANNOT_DISABLE_DEFAULT",
			"The default tenant cannot be disabled from here: it is where this console is served from. Use the command line."))
		return
	}

	tenant, err := h.tenants.SetStatus(r.Context(), code, status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	action := model.ActionTenantEnable
	if status == model.StatusDisabled {
		action = model.ActionTenantDisable
	}
	// Recorded in the operator's own tenant. The affected tenant's log would
	// be a record its administrators could not act on, and — if they are the
	// ones who were just disabled — could not reach.
	h.audit.Log(r.Context(), principal.TenantID, service.AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: principal.UserID, ActorName: principal.Username,
		TargetType: "TENANT", TargetID: tenant.ID, TargetName: tenant.Code,
		IP: httpx.ClientIP(r),
	})

	httpx.OK(w, tenant)
}

// ExtendTenant moves a tenant's deadline out by one trial period.
//
// One fixed step rather than a date the caller sends. An operator looking at a
// tenant that lapses on Thursday wants it to not lapse on Thursday; asking for
// a date invites a typo that either does nothing or hands out a decade, and
// neither is visible afterwards. Pressing it twice is how you get four weeks.
//
// No confirmation field, unlike disabling: this takes nothing away. Somebody
// who presses it by mistake has given a demonstration tenant another
// fortnight, and the other button undoes it.
func (h *Handler) ExtendTenant(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.operatorOnly(w, r)
	if !ok {
		return
	}

	code := chi.URLParam(r, "code")
	tenant, err := h.tenants.Extend(r.Context(), code, service.TrialTenantTTL)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// In the operator's own tenant, for the same reason enable and disable are:
	// the affected tenant's log is one its administrators may not be able to
	// reach, and this is a record of what an operator did.
	h.audit.Log(r.Context(), principal.TenantID, service.AuditEntry{
		Kind: model.LogOperation, Action: model.ActionTenantExtend,
		ActorID: principal.UserID, ActorName: principal.Username,
		TargetType: "TENANT", TargetID: tenant.ID, TargetName: tenant.Code,
		IP: httpx.ClientIP(r),
	})

	httpx.OK(w, tenant)
}

// mayManageTenants reports whether this caller would be admitted to the
// operator console, which is what the console asks in order to decide whether
// to draw the menu entry at all.
//
// Three conditions, and the cheap ones first: an ordinary user and an
// ordinary deployment both answer false without touching the database, so
// the great majority of sign-ins pay nothing for a feature they do not have.
func (h *Handler) mayManageTenants(r *http.Request, principal auth.Principal) bool {
	if !h.tenants.OperatorConsole() || !principal.IsAdmin() {
		return false
	}
	base, err := h.tenants.Resolve(r.Context(), model.DefaultTenantCode)
	if err != nil {
		// Including the case where the default tenant is disabled, which
		// Resolve reports as an error. Fail closed: a menu entry leading to
		// a screen that will refuse is worse than no entry.
		return false
	}
	return principal.TenantID == base.ID
}
