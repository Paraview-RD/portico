package service

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
	"github.com/paraview/portico/internal/webhook"
)

// DispatchDue delivers whatever is due for one tenant, and reports how many
// it attempted.
//
// Called on a timer by the server; see cmd/server. It is a pass rather than
// a long-running loop so that it shares the sweep's schedule and its failure
// mode — a pass that errors is retried on the next tick, and nothing has to
// supervise a goroutine.
func (s *WebhookService) DispatchDue(ctx context.Context, tenantID string, client *http.Client) (int, error) {
	q := s.store.ForTenant(tenantID)
	now := store.Now()

	// Claimed inside a transaction and delivered outside it. Holding a row
	// lock across an HTTP call to somebody else's server would tie a database
	// connection to their response time, and a slow receiver would exhaust
	// the pool — which is the failure that looks like the whole application
	// being slow for no reason.
	var due []sqlcgen.WebhookDelivery
	err := s.store.WithTx(func(tx *sqlcgen.Queries) error {
		claimed, err := q.ClaimDueWebhookDeliveries(ctx, tx, now, webhook.BatchSize)
		if err != nil {
			return err
		}
		due = claimed
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(due) == 0 {
		return 0, nil
	}

	for _, delivery := range due {
		subscription, err := q.GetWebhookSubscription(ctx, delivery.SubscriptionID)
		if err != nil {
			// The subscription went away between queueing and sending. The
			// cascade removes its deliveries, so this is a race rather than a
			// state, and there is nobody left to deliver to.
			continue
		}
		// A subscription paused after an event was queued does not receive
		// it. Pausing means "stop sending me things", and a backlog arriving
		// the moment it is resumed is the opposite of that.
		if model.Status(subscription.Status) != model.StatusActive {
			continue
		}

		result := webhook.Deliver(ctx, client,
			subscription.Url, subscription.Secret,
			delivery.EventType, delivery.ID, []byte(delivery.Payload))
		webhook.LogAttempt(ctx, subscription.ID, delivery.EventType, result)

		if err := s.recordAttempt(ctx, q, delivery, result); err != nil {
			return len(due), err
		}
	}
	return len(due), nil
}

// recordAttempt writes what happened and decides whether to try again.
func (s *WebhookService) recordAttempt(ctx context.Context, q *store.Scoped, delivery sqlcgen.WebhookDelivery, result webhook.Result) error {
	now := store.Now()

	if result.Err == nil {
		return q.MarkWebhookDelivered(ctx, sqlcgen.MarkWebhookDeliveredParams{
			ID:          delivery.ID,
			LastStatus:  statusCode(result.StatusCode),
			DeliveredAt: &now,
		})
	}

	attempts := delivery.Attempts + 1
	// Given up on when the receiver refused rather than failed — a 400 will
	// be a 400 next time — or when the attempts are spent.
	giveUp := !result.Retryable || attempts >= webhook.MaxAttempts

	params := sqlcgen.MarkWebhookAttemptFailedParams{
		ID:         delivery.ID,
		LastStatus: statusCode(result.StatusCode),
		LastError:  truncateError(result.Err.Error()),
		Status:     string(model.WebhookFailed),
	}
	if !giveUp {
		next := now.Add(webhook.Backoff(delivery.Attempts))
		params.Status = string(model.WebhookPending)
		params.NextAttemptAt = &next
	}
	return q.MarkWebhookAttemptFailed(ctx, params)
}

// truncateError bounds what a receiver's error message can put in this
// database. A server answering with a megabyte of HTML should not be able to
// decide the size of a row here.
func truncateError(message string) string {
	const limit = 500
	if len(message) <= limit {
		return message
	}
	return message[:limit] + "…"
}

// statusCode narrows an HTTP status for the column, treating zero as "no
// response at all" — which is what a connection failure produces, and is
// genuinely absent rather than a status of 0.
//
// Clamped rather than converted directly: a status code is three digits by
// definition, but the type it arrives in is an int, and a scanner is right
// to point out that nothing here guarantees the value.
func statusCode(v int) sql.NullInt32 {
	if v <= 0 || v > 999 {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(v), Valid: true}
}

// deliveryRetention is how long finished deliveries are kept.
//
// Long enough to answer "did they receive the deactivation" weeks later,
// which is the question that gets asked, and bounded so the table does not
// grow forever.
const deliveryRetention = 30 * 24 * time.Hour

// SweepDeliveries removes finished deliveries past their retention.
func (s *WebhookService) SweepDeliveries(ctx context.Context, tenantID string, now time.Time) error {
	return s.store.ForTenant(tenantID).
		DeleteOldWebhookDeliveries(ctx, now.Add(-deliveryRetention))
}
