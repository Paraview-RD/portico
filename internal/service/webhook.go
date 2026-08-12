package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/secrets"
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
	// vault may be nil, which is the default: a deployment with no
	// PORTICO_ENCRYPTION_KEY runs, and refuses to store a custom header
	// rather than writing somebody's bearer token into the table in the
	// clear. See internal/secrets.
	vault *secrets.Vault

	// catalogue and mappings may both be nil, which means every payload is
	// delivered exactly as it always was. See WithFieldMappings.
	catalogue *FieldCatalogue
	mappings  *FieldMappingService
}

// WithVault attaches the key custom headers are sealed under. Separate from
// the constructor because the vault is built later in server.New, on the
// same terms as WithEvents elsewhere.
func (s *WebhookService) WithVault(v *secrets.Vault) *WebhookService {
	s.vault = v
	return s
}

// sealHeaders validates a set and returns it ready for the column.
//
// Refused outright where there is no key. A custom header is a credential
// this server stores and later presents — there is nothing to compare a
// digest against — so the choice is sealed or plaintext, and plaintext
// bearer tokens in a table somebody dumps for support is worse than not
// offering the feature at all.
func (s *WebhookService) sealHeaders(headers map[string]string) (string, error) {
	if len(headers) == 0 {
		return "", nil
	}
	if err := webhook.ValidateHeaders(headers); err != nil {
		return "", httpx.BadRequest("INVALID_WEBHOOK_HEADER", err.Error())
	}

	encoded, err := webhook.EncodeHeaders(headers)
	if err != nil {
		return "", err
	}

	sealed, err := s.vault.Seal(encoded)
	if errors.Is(err, secrets.ErrNotConfigured) {
		return "", ErrNoEncryptionKey
	}
	if err != nil {
		return "", fmt.Errorf("seal webhook headers: %w", err)
	}
	return sealed, nil
}

// openHeaders is the reverse, at delivery time.
func (s *WebhookService) openHeaders(sealed string) (map[string]string, error) {
	if sealed == "" {
		return nil, nil
	}
	encoded, err := s.vault.Open(sealed)
	if err != nil {
		return nil, fmt.Errorf("open webhook headers: %w", err)
	}
	return webhook.DecodeHeaders(encoded)
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
	// HeaderNames, without their values. Serving the values back would make
	// this endpoint a way to read every bearer token the tenant has stored,
	// which is the thing sealing them was for.
	HeaderNames []string `json:"headerNames,omitempty"`
}

// CreatedSubscription is what creation returns, once.
type CreatedSubscription struct {
	Subscription
	// Secret is returned on creation and never again — not because it is
	// hashed (it cannot be, it signs) but because there is no reason to
	// serve it a second time and every reason not to have an endpoint that
	// does.
	Secret string `json:"secret"`
	// PreviousExpiry is when the key this replaced stops being sent, and is
	// absent on a first issue because there is nothing it replaced. It is
	// the one number the receiver has to act on: it is their deadline, not
	// ours.
	PreviousExpiry *time.Time `json:"previousSecretExpiresAt,omitempty"`
}

// SubscriptionInput is what an administrator supplies.
type SubscriptionInput struct {
	Name   string
	URL    string
	Events []string
	// Headers are sent with every delivery. Values are credentials and are
	// never served back — only the names are, which is enough to answer
	// "what is this subscription sending".
	Headers map[string]string
}

// SecretOverlap is how long the replaced key keeps being sent alongside the
// new one.
//
// Twenty-four hours because the work it is buying time for is somebody
// deploying a configuration change to another system, which is measured in
// working hours rather than minutes. Not configurable: the number that
// matters to a receiver is when the old key stops, and that is reported to
// them; a knob here would be a second thing to get wrong for no gain.
const SecretOverlap = 24 * time.Hour

// RotateSecret issues a new signing key and keeps the old one alive briefly.
//
// The overlap is the whole point. Portico produces the signature and the
// receiver verifies it, so the receiver is the side that has to deploy
// something, and a rotation that took effect instantly would reject every
// delivery until they had. During the overlap each delivery carries both
// signatures, comma-separated, and the receiver accepts either.
//
// That has a consequence worth stating plainly rather than discovering: a
// receiver comparing the whole X-Portico-Signature header as one string
// verifies nothing from the moment this is called until the overlap ends.
// Splitting on "," is a requirement of the protocol, not a nicety, and the
// console says so before starting one.
//
// The subscription id does not change. Deleting and re-registering was the
// only previous remedy for a leaked key, and it discarded the delivery
// history and broke deduplication at the far end — the cure destroyed the
// evidence.
func (s *WebhookService) RotateSecret(ctx context.Context, actor auth.Principal, id string) (CreatedSubscription, error) {
	q := s.store.ForTenant(actor.TenantID)

	existing, err := q.GetWebhookSubscription(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return CreatedSubscription{}, ErrWebhookNotFound
		}
		return CreatedSubscription{}, fmt.Errorf("get subscription: %w", err)
	}

	secret, err := newWebhookSecret()
	if err != nil {
		return CreatedSubscription{}, err
	}

	now := store.Now()
	expires := now.Add(SecretOverlap)
	if err := q.RotateWebhookSubscriptionSecret(ctx,
		sqlcgen.RotateWebhookSubscriptionSecretParams{
			Secret:                  secret,
			PreviousSecretExpiresAt: &expires,
			UpdatedAt:               now,
			ID:                      id,
		}); err != nil {
		return CreatedSubscription{}, fmt.Errorf("rotate secret: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionWebhookRotate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "WEBHOOK", TargetID: id, TargetName: existing.Name,
		Detail: "previous secret accepted until " + expires.UTC().Format(time.RFC3339),
	})

	return CreatedSubscription{
		Subscription:   toSubscription(existing),
		Secret:         secret,
		PreviousExpiry: &expires,
	}, nil
}

// Create registers a subscription and returns its signing secret.
func (s *WebhookService) Create(ctx context.Context, actor auth.Principal, in SubscriptionInput) (CreatedSubscription, error) {
	if err := in.validate(); err != nil {
		return CreatedSubscription{}, err
	}

	sealedHeaders, err := s.sealHeaders(in.Headers)
	if err != nil {
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
			Headers:   sealedHeaders,
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
			HeaderNames: webhook.HeaderNames(in.Headers),
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
		sub := toSubscription(row)
		// The names live inside the sealed blob, so listing them means
		// opening it. Kept there rather than in a second plaintext column
		// because two places holding the same names is two places to drift,
		// and the cost is one decryption per subscription on a screen that
		// shows a handful.
		//
		// A failure here leaves the names empty rather than failing the
		// listing. The key can be taken out of the environment after a
		// subscription was created with headers, and a screen that will not
		// load is a worse way to learn that than a row whose headers are not
		// described — the delivery failing is what says it properly.
		if headers, err := s.openHeaders(row.Headers); err == nil {
			sub.HeaderNames = webhook.HeaderNames(headers)
		}
		out = append(out, sub)
	}
	return out, nil
}

// toSubscription is the one place a row becomes the wire shape, so a field
// added to one listing cannot be missing from another.
func toSubscription(row sqlcgen.WebhookSubscription) Subscription {
	sub := Subscription{
		ID: row.ID, Name: row.Name, URL: row.Url,
		Events: strings.Split(row.Events, ","),
		Status: row.Status, CreatedAt: row.CreatedAt,
	}
	return sub
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
	// Prepared here and resolved on first use, so an event nobody has
	// configured mappings for costs nothing beyond this allocation.
	overlay := s.overlayFor(tenantID, eventType, data)

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
			OccurredAt: now, Data: overlay.dataFor(ctx, subscription.ID),
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
