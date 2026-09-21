package handler

import (
	"net/http"

	"github.com/Paraview-RD/portico/internal/httpx"
)

type smsLoginCodeRequest struct {
	Tenant string `json:"tenant"`
	Phone  string `json:"phone"`
}

// RequestSMSLoginCode asks for a login code to be sent to a phone number.
//
// Always answers 200 whether or not the number is bound to an account --
// see SMSLoginService.RequestCode's doc comment for why.
func (h *Handler) RequestSMSLoginCode(w http.ResponseWriter, r *http.Request) {
	var req smsLoginCodeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if err := h.smsLogin.RequestCode(r.Context(), tenant, req.Phone, httpx.ClientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"sent": true})
}

type smsLoginRequest struct {
	Tenant string `json:"tenant"`
	Phone  string `json:"phone"`
	Code   string `json:"code"`
}

// LoginWithSMSCode authenticates a user by phone number and SMS code and
// returns a token.
func (h *Handler) LoginWithSMSCode(w http.ResponseWriter, r *http.Request) {
	var req smsLoginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	session, err := h.smsLogin.LoginWithCode(r.Context(), tenant, req.Phone, req.Code,
		httpx.ClientIP(r), userAgent(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, session)
}
