package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// Outbound event subscriptions.

// ListWebhooks returns the tenant's subscriptions.
func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	subscriptions, err := h.webhooks.List(r.Context(), actor.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, subscriptions)
}

// WebhookEvents lists what can be subscribed to, so the screen does not have
// to keep its own copy of the list and drift from the server's.
func (h *Handler) WebhookEvents(w http.ResponseWriter, _ *http.Request) {
	httpx.OK(w, webhook.AllEvents)
}

type webhookRequest struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// CreateWebhook registers one and returns its signing secret, once.
func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	var req webhookRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	created, err := h.webhooks.Create(r.Context(), actor, service.SubscriptionInput{
		Name: req.Name, URL: req.URL, Events: req.Events,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, created)
}

// RotateWebhookSecret issues a new signing key, returning it once.
//
// The response also carries when the replaced key stops being sent, which is
// the receiver's deadline rather than this server's — it is the only number
// they can act on, so it travels with the secret they have to install.
func (h *Handler) RotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	rotated, err := h.webhooks.RotateSecret(r.Context(), actor, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, rotated)
}

// EnableWebhook resumes delivery.
func (h *Handler) EnableWebhook(w http.ResponseWriter, r *http.Request) {
	h.setWebhookStatus(w, r, model.StatusActive)
}

// DisableWebhook pauses it. Events that occur while paused are not queued
// for it, so resuming does not produce a flood of things that happened while
// somebody deliberately had it switched off.
func (h *Handler) DisableWebhook(w http.ResponseWriter, r *http.Request) {
	h.setWebhookStatus(w, r, model.StatusDisabled)
}

func (h *Handler) setWebhookStatus(w http.ResponseWriter, r *http.Request, status model.Status) {
	actor := auth.MustPrincipal(r.Context())

	if err := h.webhooks.SetStatus(r.Context(), actor, chi.URLParam(r, "id"), status); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// DeleteWebhook removes a subscription and its delivery history.
func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	if err := h.webhooks.Delete(r.Context(), actor, chi.URLParam(r, "id")); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, nil)
}

// ListWebhookDeliveries returns recent attempts for one subscription.
//
// This is the screen an operator opens when a subscriber says they are not
// receiving anything: it shows what was attempted, what came back, and how
// many times — which is the difference between "we never sent it" and "your
// endpoint answered 500 five times".
func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	actor := auth.MustPrincipal(r.Context())

	// Bounded before narrowing, so the conversion is provably safe and a
	// caller cannot ask for the whole table.
	const defaultLimit, maxLimit = 50, 200
	limit := int32(defaultLimit)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 32); err == nil && n > 0 && n <= maxLimit {
			limit = int32(n)
		}
	}

	deliveries, err := h.webhooks.Deliveries(
		r.Context(), actor.TenantID, chi.URLParam(r, "id"), limit)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, deliveries)
}
