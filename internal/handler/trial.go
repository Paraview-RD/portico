package handler

import (
	"net/http"

	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/service"
)

// The three endpoints a stranger can reach, and the two writes in this API that
// are not authorized inside a tenant.
//
// The two writes are registered only where PORTICO_TRIAL_SIGNUP is on, and
// answer 404 from the router otherwise. TrialStatus is always routed: the
// sign-in screen reads it on every load, so a missing route would leave a
// console error on every ordinary deployment — see routes.go, where that
// distinction is made.

// Named rather than anonymous structs, because a test compares these field
// names against the OpenAPI document — an inline struct has no type for it to
// look at, so the two would be free to drift.
type trialRequest struct {
	Email       string `json:"email"`
	CompanyName string `json:"companyName"`
	TenantCode  string `json:"tenantCode"`
	Industry    string `json:"industry"`
}

type trialConfirmRequest struct {
	Token string `json:"token"`
}

// TrialStatus tells the sign-in screen whether to offer a trial at all, and
// which seeded worlds are on offer.
//
// Readable without signing in because the screen asking is the signed-out one.
// It says nothing a visitor could not learn by pressing the button.
func (h *Handler) TrialStatus(w http.ResponseWriter, _ *http.Request) {
	httpx.OK(w, map[string]any{
		"enabled":    h.trials.Enabled(),
		"industries": []string{service.TrialIndustryGeneric},
	})
}

// RequestTrial takes the form and emails a confirmation link.
func (h *Handler) RequestTrial(w http.ResponseWriter, r *http.Request) {
	var req trialRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}

	err := h.trials.Request(r.Context(), service.TrialRequestInput{
		Email:       req.Email,
		CompanyName: req.CompanyName,
		TenantCode:  req.TenantCode,
		Industry:    req.Industry,
	}, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// No echo of what was submitted. The next thing that happens is an email,
	// and a response repeating the address would let this endpoint be used to
	// check whether a link was actually sent.
	httpx.OK(w, map[string]any{"sent": true})
}

// ConfirmTrial spends the link and answers with the credentials.
//
// The credentials are in the response as well as in the email, deliberately:
// the visitor is standing in front of the page that just created the tenant,
// and making them go back to their inbox for a password that was generated
// one request ago is a step with nothing behind it. The email is the copy
// they keep.
func (h *Handler) ConfirmTrial(w http.ResponseWriter, r *http.Request) {
	var req trialConfirmRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return
	}

	tenant, err := h.trials.Confirm(r.Context(), req.Token)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	httpx.OK(w, map[string]any{
		"tenantCode":    tenant.TenantCode,
		"tenantName":    tenant.TenantName,
		"adminUsername": tenant.AdminUsername,
		"adminPassword": tenant.AdminPassword,
		"signInUrl":     tenant.SignInURL,
	})
}
