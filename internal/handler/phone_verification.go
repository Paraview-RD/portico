package handler

import (
	"net/http"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
)

type requestPhoneVerificationRequest struct {
	Phone string `json:"phone"`
}

// RequestPhoneVerification asks for a code proving the caller controls
// phone, the first half of binding it to their own profile (§3.5).
//
// Unlike RequestSMSLoginCode, this answers directly rather than always with
// {"sent": true} -- the caller is already signed in, so there is no
// enumeration concern in telling them a send failed or that the number is
// already somebody else's.
func (h *Handler) RequestPhoneVerification(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req requestPhoneVerificationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	if err := h.phoneVerification.RequestPhoneChange(r.Context(), principal, req.Phone, httpx.ClientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"sent": true})
}

type confirmPhoneVerificationRequest struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

// ConfirmPhoneVerification checks a code and, if it is right, writes phone
// into the caller's own profile.
func (h *Handler) ConfirmPhoneVerification(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req confirmPhoneVerificationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	user, err := h.phoneVerification.ConfirmPhoneChange(r.Context(), principal, req.Phone, req.Code, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, user)
}
