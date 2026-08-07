package handler

import (
	"net/http"

	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
)

type recoveryRequest struct {
	Tenant string `json:"tenant"`
	// Channel is EMAIL or SMS. It is explicit rather than inferred from the
	// destination's shape because the two lookups must not be
	// interchangeable: resolving across both and then sending a token is how
	// a colliding identifier routes someone else's reset.
	Channel string `json:"channel"`
	// Destination is the email address or phone number the caller believes
	// is on the account. It is only ever used to find the account; the
	// message goes to what the account actually has bound.
	Destination string `json:"destination"`
}

// RequestPasswordRecovery starts password recovery (§3.5).
//
// It answers the same way whether or not an account was found, so it cannot
// be used to ask whether someone has an account here.
func (h *Handler) RequestPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	var req recoveryRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	channel := model.RecoveryChannel(req.Channel)
	if !channel.Valid() {
		httpx.Fail(w, r, httpx.BadRequest("INVALID_CHANNEL", "Channel must be EMAIL or SMS."))
		return
	}

	err = h.recovery.Request(r.Context(), tenant, channel, req.Destination, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Deliberately says "if" rather than "we sent". The wording is part of
	// the neutrality: a response that announced a message would tell the
	// caller the account exists just as plainly as a 404 would.
	httpx.OK(w, map[string]any{
		"message": "If that matches an account, a reset link is on its way.",
	})
}

type recoveryConfirmRequest struct {
	Tenant      string `json:"tenant"`
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

// ConfirmPasswordRecovery redeems a reset token and sets a new password.
func (h *Handler) ConfirmPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	var req recoveryConfirmRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.resolvePublicTenant(r, req.Tenant)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	err = h.recovery.Confirm(r.Context(), tenant.ID, req.Token, req.NewPassword, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Completing recovery bumped token_version, so any session the account
	// had is gone. Signing in is the next step either way, but saying so
	// keeps a client from assuming it now holds a session.
	httpx.OK(w, map[string]any{"reauthenticationRequired": true})
}

// RecoveryChannels reports which channels this deployment can actually use,
// so the sign-in screen offers the ones that will work rather than a form
// that fails on submit.
func (h *Handler) RecoveryChannels(w http.ResponseWriter, r *http.Request) {
	if _, err := h.resolvePublicTenant(r, ""); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"channels": h.recovery.AvailableChannels()})
}
