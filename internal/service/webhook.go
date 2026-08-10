package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// WebhookService owns subscriptions and the queue of things to send them.
//
// Publishing is a database write and nothing more: the event is queued and
// the request that caused it returns. An outbound HTTP call on the request
// path would make creating a user as slow as the slowest subscriber and
// would fail the creation when a subscriber is down — which is the wrong
// way round, because the account was created either way.
type WebhookService struct {
	store *store.Store
	audit *AuditService
}

// NewWebhookService wires a WebhookService.
func NewWebhookService(st *store.Store, audit *AuditService) *WebhookService {
	return &WebhookService{store: st, audit: audit}
}

// Errors this service returns.
var (
	ErrWebhookNotFound  = httpx.NotFound("WEBHOOK_NOT_FOUND", "No such subscription.")
	ErrWebhookNameTaken = httpx.Conflict("WEBHOOK_NAME_TAKEN",
		"A subscription with that name already exists.")
)

// Subscription is a subscription as the console sees it.
type Subscription struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreatedSubscription is what creation returns, once.
type CreatedSubscription struct {
	Subscription
	// Secret is returned on creation and never again — not because it is
	// hashed (it cannot be, it signs) but because there is no reason to
	// serve it a second time and every reason not to have an endpoint that
	// does.
	Secret string `json:"secret"`
}

// SubscriptionInput is what an administrator supplies.
type SubscriptionInput struct {
	Name   string
	URL    string
	Events []string
}

// Create registers a subscription and returns its signing secret.
func (s *WebhookService) Create(ctx context.Context, actor auth.Principal, in SubscriptionInput) (CreatedSubscription, error) {
	if err := in.validate(); err != nil {
		return CreatedSubscription{}, err
	}

	secret, err := newWebhookSecret()
	if err != nil {
		return CreatedSubscription{}, err
	}

	now := store.Now()
	id := uuid.NewString()

	err = s.store.ForTenant(actor.TenantID).CreateWebhookSubscription(ctx,
		sqlcgen.CreateWebhookSubscriptionParams{
			ID:        id,
			Name:      strings.TrimSpace(in.Name),
			Url:       strings.TrimSpace(in.URL),
			Secret:    secret,
			Events:    strings.Join(in.Events, ","),
			CreatedAt: now,
		})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return CreatedSubscription{}, ErrWebhookNameTaken
		}
		return CreatedSubscription{}, fmt.Errorf("create subscription: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionWebhookCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "WEBHOOK", TargetID: id, TargetName: in.Name,
		Detail: in.URL,
	})

	return CreatedSubscription{
		Subscription: Subscription{
			ID: id, Name: in.Name, URL: in.URL, Events: in.Events,
			Status: string(model.StatusActive), CreatedAt: now,
		},
		Secret: secret,
	}, nil
}

// List returns a tenant's subscriptions, without secrets.
func (s *WebhookService) List(ctx context.Context, tenantID string) ([]Subscription, error) {
	rows, err := s.store.ForTenant(tenantID).ListWebhookSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}

	out := make([]Subscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, Subscription{
			ID: row.ID, Name: row.Name, URL: row.Url,
			Events: strings.Split(row.Events, ","),
			Status: row.Status, CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

// SetStatus pauses or resumes a subscription.
func (s *WebhookService) SetStatus(ctx context.Context, actor auth.Principal, id string, status model.Status) error {
	err := s.store.ForTenant(actor.TenantID).SetWebhookSubscriptionStatus(ctx,
		sqlcgen.SetWebhookSubscriptionStatusParams{
			ID: id, Status: string(status), UpdatedAt: store.Now(),
		})
	if err != nil {
		return fmt.Errorf("set subscription status: %w", err)
	}

	action := model.ActionWebhookEnable
	if status != model.StatusActive {
		action = model.ActionWebhookDisable
	}
	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "WEBHOOK", TargetID: id,
	})
	return nil
}

// Delete removes a subscription and its delivery history.
func (s *WebhookService) Delete(ctx context.Context, actor auth.Principal, id string) error {
	if err := s.store.ForTenant(actor.TenantID).DeleteWebhookSubscription(ctx, id); err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionWebhookDelete,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "WEBHOOK", TargetID: id,
	})
	return nil
}

// Delivery is one attempt's record, for the console.
type Delivery struct {
	ID          string     `json:"id"`
	EventType   string     `json:"eventType"`
	Status      string     `json:"status"`
	Attempts    int32      `json:"attempts"`
	LastStatus  *int32     `json:"lastStatus"`
	LastError   string     `json:"lastError"`
	CreatedAt   time.Time  `json:"createdAt"`
	DeliveredAt *time.Time `json:"deliveredAt"`
}

// Deliveries returns a subscription's recent attempts, newest first.
func (s *WebhookService) Deliveries(ctx context.Context, tenantID, subscriptionID string, limit int32) ([]Delivery, error) {
	rows, err := s.store.ForTenant(tenantID).ListWebhookDeliveries(ctx, subscriptionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}

	out := make([]Delivery, 0, len(rows))
	for _, row := range rows {
		out = append(out, Delivery{
			ID: row.ID, EventType: row.EventType, Status: row.Status,
			Attempts: row.Attempts, LastStatus: nullableInt32(row.LastStatus),
			LastError: row.LastError, CreatedAt: row.CreatedAt,
			DeliveredAt: row.DeliveredAt,
		})
	}
	return out, nil
}

// Publish queues an event for every subscription that selected it.
//
// Errors are logged and swallowed, deliberately. This is called from inside
// operations that have already succeeded — the account exists, the
// organization was renamed — and failing them because a notification could
// not be queued would undo work that was correct, to report a problem with
// telling somebody about it.
func (s *WebhookService) Publish(ctx context.Context, tenantID, eventType string, data any) {
	if err := s.publish(ctx, tenantID, eventType, data); err != nil {
		logPublishFailure(ctx, tenantID, eventType, err)
	}
}

func (s *WebhookService) publish(ctx context.Context, tenantID, eventType string, data any) error {
	q := s.store.ForTenant(tenantID)

	subscriptions, err := q.ListActiveWebhookSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}
	if len(subscriptions) == 0 {
		return nil
	}

	now := store.Now()
	for _, subscription := range subscriptions {
		if !webhook.Selects(subscription.Events, eventType) {
			continue
		}

		deliveryID := uuid.NewString()
		// Rendered now and stored, not rendered at delivery time. An event
		// describes what happened; re-rendering from current state at the
		// moment of sending would deliver a "disabled" event describing an
		// account somebody has since re-enabled.
		body, err := json.Marshal(webhook.Envelope{
			ID: deliveryID, Type: eventType, Tenant: tenantID,
			OccurredAt: now, Data: data,
		})
		if err != nil {
			return fmt.Errorf("render event: %w", err)
		}

		err = q.EnqueueWebhookDelivery(ctx, sqlcgen.EnqueueWebhookDeliveryParams{
			ID:             deliveryID,
			SubscriptionID: subscription.ID,
			EventType:      eventType,
			Payload:        string(body),
			// Due immediately; the dispatcher picks it up on its next pass.
			NextAttemptAt: &now,
		})
		if err != nil {
			return fmt.Errorf("enqueue delivery: %w", err)
		}
	}
	return nil
}

// nullableInt32 renders a nullable column as a pointer, so a delivery that
// never reached a server reports null rather than a status code of zero —
// which would read as a real answer.
func nullableInt32(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

// logPublishFailure records that an event could not be queued.
//
// An error rather than a warning: the operation it belongs to succeeded, so
// nothing else will report this, and a subscriber silently missing events is
// exactly the failure a webhook integration cannot detect from its own side.
func logPublishFailure(ctx context.Context, tenantID, eventType string, err error) {
	slog.ErrorContext(ctx, "could not queue webhook event",
		"tenant", tenantID, "event", eventType, "error", err)
}

func (in SubscriptionInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return httpx.BadRequest("INVALID_NAME", "A name is required.")
	}
	if err := webhook.ValidateDestination(in.URL); err != nil {
		// The reason is passed through: an administrator who typed an
		// internal address needs to know which rule refused it, or they will
		// try three more variations of the same address.
		return httpx.BadRequest("INVALID_WEBHOOK_URL", err.Error())
	}
	if len(in.Events) == 0 {
		return httpx.BadRequest("NO_EVENTS_SELECTED",
			"Select at least one event, or * for all of them.")
	}
	for _, event := range in.Events {
		if event == webhook.Wildcard {
			continue
		}
		if !knownEvent(event) {
			return httpx.BadRequest("UNKNOWN_EVENT",
				"Unknown event type: "+event)
		}
	}
	return nil
}

func knownEvent(event string) bool {
	for _, known := range webhook.AllEvents {
		if known == event {
			return true
		}
	}
	return false
}

// newWebhookSecret returns a signing key.
//
// 32 bytes, and stored in the clear because it signs rather than
// authenticates: there is nothing to compare a digest against. That puts it
// in the same category as the OIDC and SAML private keys, which is stated in
// the schema and in docs/backup-and-restore.md.
func newWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(buf), nil
}
