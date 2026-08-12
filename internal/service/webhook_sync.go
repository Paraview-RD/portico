package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// A snapshot: everything that already existed, sent to one subscription.
//
// Events describe changes, and a subscription created today has missed every
// change that came before it. The delivery table is no help — a finished
// delivery is deleted after thirty days, and the rows that survive hold what
// happened rather than what is. So a receiver that wants a mirror has had no
// way to build the first copy of it.
//
// This is the answer that keeps one shape and one credential: the same
// signed POST, the same field mappings, the same retry machinery, carrying
// pages of what exists rather than one fabricated creation per account.

// SnapshotSource is what a snapshot reads.
//
// An interface rather than the three services, and attached after
// construction rather than taken by the constructor, because the webhook
// service is built before them — the same arrangement as WithEvents and
// WithFieldMappings. It also keeps the dependency one-way: accounts publish
// events, and nothing in the account service needs to know a snapshot
// exists.
type SnapshotSource interface {
	ListUsers(ctx context.Context, tenantID string, q UserQuery, page Page) ([]model.User, int64, error)
	ListOrganizations(ctx context.Context, tenantID string, activeOnly bool) ([]model.Organization, error)
	ListGroups(ctx context.Context, tenantID string) ([]model.Group, error)
}

// WithSnapshotSource attaches the readers a snapshot needs. Without it,
// StartSnapshot refuses rather than sending an empty one — a receiver that
// was told a snapshot ran and got nothing would conclude the tenant is
// empty.
func (s *WebhookService) WithSnapshotSource(src SnapshotSource) *WebhookService {
	s.snapshot = src
	return s
}

// SnapshotPageSize is how many objects ride in one delivery.
//
// The number is a guess at what a receiver can write in one transaction, and
// it is deliberately not configurable yet: a wrong guess here shows up as
// timeouts at the receiver, which is a conversation to have with real
// deployments before it becomes a setting somebody has to understand.
const SnapshotPageSize = 500

// SnapshotSummary is what the caller is told, so the console can report the
// size of what it just queued rather than only that it queued something.
type SnapshotSummary struct {
	SyncID string         `json:"syncId"`
	Scope  []string       `json:"scope"`
	Counts map[string]int `json:"counts"`
	Pages  int            `json:"pages"`
}

// ErrSnapshotUnavailable is returned when nothing was attached to read from.
var ErrSnapshotUnavailable = httpx.UnprocessableEntity("SNAPSHOT_UNAVAILABLE",
	"This deployment cannot produce a snapshot.")

// StartSnapshot queues a full copy of what exists for one subscription.
//
// Everything is queued in one pass rather than streamed as the dispatcher
// drains it. The alternative — a producer that keeps state between passes —
// would need somewhere to keep it and a story for a process that restarts
// mid-snapshot. Queued rows already have both: they are in the table, and
// the dispatcher retries them.
func (s *WebhookService) StartSnapshot(ctx context.Context, actor auth.Principal, subscriptionID string) (SnapshotSummary, error) {
	if s.snapshot == nil {
		return SnapshotSummary{}, ErrSnapshotUnavailable
	}

	tenantID := actor.TenantID
	q := s.store.ForTenant(tenantID)

	subscription, err := q.GetWebhookSubscription(ctx, subscriptionID)
	if err != nil {
		if store.IsNoRows(err) {
			return SnapshotSummary{}, ErrWebhookNotFound
		}
		return SnapshotSummary{}, fmt.Errorf("get subscription: %w", err)
	}
	if model.Status(subscription.Status) != model.StatusActive {
		return SnapshotSummary{}, httpx.UnprocessableEntity("SUBSCRIPTION_DISABLED",
			"This subscription is disabled. Enable it before asking for a snapshot.")
	}

	// One snapshot at a time. Two runs interleaved would deliver two sets of
	// pages a receiver has no way to tell apart mid-stream — and the second
	// sync.completed would arrive while the first run's pages were still in
	// flight, telling the receiver it holds everything when it does not.
	pending, err := q.CountPendingSnapshotDeliveries(ctx, subscription.ID)
	if err != nil {
		return SnapshotSummary{}, fmt.Errorf("count queued snapshot deliveries: %w", err)
	}
	if pending > 0 {
		return SnapshotSummary{}, httpx.Conflict("SNAPSHOT_IN_PROGRESS",
			"A snapshot for this subscription is still being delivered. Wait for it to finish, or disable the subscription to abandon it.")
	}

	scope := snapshotScope(subscription.Events)
	if len(scope) == 0 {
		return SnapshotSummary{}, httpx.UnprocessableEntity("SNAPSHOT_EMPTY_SCOPE",
			"This subscription selects no events a snapshot can fill.")
	}

	syncID := uuid.NewString()
	now := store.Now()

	pages, counts, err := s.snapshotPages(ctx, tenantID, subscription.ID, scope)
	if err != nil {
		return SnapshotSummary{}, err
	}

	// The opening event carries the page size and the scope so a receiver
	// can size its work before the first page lands.
	queued := []queuedEvent{{
		eventType: webhook.EventSyncStarted,
		data: webhook.SyncStarted{
			SyncID: syncID, Scope: scope, PageSize: SnapshotPageSize, AsOf: now,
		},
	}}
	for _, page := range pages {
		page.body.SyncID = syncID
		queued = append(queued, queuedEvent{eventType: page.eventType, data: page.body})
	}
	queued = append(queued, queuedEvent{
		eventType: webhook.EventSyncCompleted,
		data:      webhook.SyncCompleted{SyncID: syncID, Counts: counts},
	})

	for _, event := range queued {
		if err := s.enqueueFor(ctx, q, subscription.ID, event, now); err != nil {
			return SnapshotSummary{}, err
		}
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionWebhookSnapshot,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "WEBHOOK_SUBSCRIPTION", TargetID: subscription.ID,
		TargetName: subscription.Name,
		Detail:     fmt.Sprintf("snapshot %s, %d pages", syncID, len(pages)),
	})

	return SnapshotSummary{SyncID: syncID, Scope: scope, Counts: counts, Pages: len(pages)}, nil
}

type queuedEvent struct {
	eventType string
	data      any
}

type snapshotPage struct {
	eventType string
	body      webhook.SyncPage
}

// snapshotScope is the kinds this subscription asked to hear about.
//
// A subscriber that only selected group events gets only groups: sending it
// the account list would be answering a question it did not ask, and on a
// large tenant that is most of the data in the system.
func snapshotScope(events string) []string {
	var scope []string
	for _, kind := range []struct {
		name    string
		witness string
	}{
		{"user", webhook.EventUserUpdated},
		{"organization", webhook.EventOrgUpdated},
		{"group", webhook.EventGroupUpdated},
	} {
		if webhook.Selects(events, kind.witness) {
			scope = append(scope, kind.name)
		}
	}
	return scope
}

// snapshotPages renders everything in scope into pages.
//
// Rendered here rather than at delivery time, the same way every other event
// is: what a receiver gets is what the tenant looked like when the snapshot
// was asked for, not what it looks like when a retry finally succeeds three
// hours later.
func (s *WebhookService) snapshotPages(ctx context.Context, tenantID, subscriptionID string, scope []string) ([]snapshotPage, map[string]int, error) {
	// One lookup rather than one per object. dataFor reads the mappings on
	// every call, which is right for a single event and wrong for fifty
	// thousand — and the common case is a subscription with no mappings at
	// all, where this answers the question once and the rest of the
	// snapshot skips the machinery entirely.
	mapped := false
	if s.mappings != nil {
		out, err := s.mappings.OutboundFor(ctx, tenantID,
			store.RecipientRef{WebhookSubscriptionID: subscriptionID})
		mapped = err == nil && !out.Empty()
	}

	render := func(witness string, item any) any {
		if !mapped {
			return item
		}
		return s.overlayFor(tenantID, witness, item).dataFor(ctx, subscriptionID)
	}

	var pages []snapshotPage
	counts := map[string]int{}

	for _, kind := range scope {
		var items []any
		var eventType, witness string

		switch kind {
		case "user":
			eventType, witness = webhook.EventSyncUsers, webhook.EventUserUpdated
			// Paged out of the database as well as into the deliveries: a
			// tenant with fifty thousand accounts should not be held in
			// memory whole to be cut into pages of five hundred.
			for offset := 0; ; offset += SnapshotPageSize {
				users, _, err := s.snapshot.ListUsers(ctx, tenantID, UserQuery{},
					Page{Limit: SnapshotPageSize, Offset: offset})
				if err != nil {
					return nil, nil, fmt.Errorf("list accounts for snapshot: %w", err)
				}
				if len(users) == 0 {
					break
				}
				for _, user := range users {
					items = append(items, render(witness, user))
				}
				if len(users) < SnapshotPageSize {
					break
				}
			}
		case "organization":
			eventType, witness = webhook.EventSyncOrganizations, webhook.EventOrgUpdated
			// Disabled organizations included, deliberately. A mirror that
			// held only the active ones could not show an account sitting
			// in one that was closed, which is a state this product allows.
			orgs, err := s.snapshot.ListOrganizations(ctx, tenantID, false)
			if err != nil {
				return nil, nil, fmt.Errorf("list organizations for snapshot: %w", err)
			}
			for _, org := range orgs {
				items = append(items, render(witness, org))
			}
		case "group":
			// No witness: group events carry no mappings, and a snapshot
			// that mapped them would be the one place group fields could be
			// renamed. See field-mappings.md.
			eventType = webhook.EventSyncGroups
			groups, err := s.snapshot.ListGroups(ctx, tenantID)
			if err != nil {
				return nil, nil, fmt.Errorf("list groups for snapshot: %w", err)
			}
			for _, group := range groups {
				items = append(items, group)
			}
		}

		counts[kind] = len(items)
		total := (len(items) + SnapshotPageSize - 1) / SnapshotPageSize
		for i := 0; i < len(items); i += SnapshotPageSize {
			end := min(i+SnapshotPageSize, len(items))
			pages = append(pages, snapshotPage{
				eventType: eventType,
				body: webhook.SyncPage{
					Kind: kind, Page: i/SnapshotPageSize + 1, Total: total,
					Items: items[i:end],
				},
			})
		}
	}
	return pages, counts, nil
}

// enqueueFor queues one delivery against one subscription.
//
// Deliberately not publish: that fans out to every subscription selecting an
// event, and a snapshot belongs to the one somebody asked about. It also
// ignores the subscription's event selection, because the selection governs
// what happens on its own and this was asked for explicitly.
func (s *WebhookService) enqueueFor(ctx context.Context, q *store.Scoped, subscriptionID string, event queuedEvent, now time.Time) error {
	deliveryID := uuid.NewString()
	body, err := json.Marshal(webhook.Envelope{
		ID: deliveryID, Type: event.eventType, Tenant: q.TenantID(),
		OccurredAt: now, Data: event.data,
	})
	if err != nil {
		return fmt.Errorf("render %s: %w", event.eventType, err)
	}
	return q.EnqueueWebhookDelivery(ctx, sqlcgen.EnqueueWebhookDeliveryParams{
		ID:             deliveryID,
		SubscriptionID: subscriptionID,
		EventType:      event.eventType,
		Payload:        string(body),
		NextAttemptAt:  &now,
	})
}

// NewSnapshotSource adapts the three services a snapshot reads.
//
// An adapter rather than making the services implement the interface
// directly: their List methods are named for their own package, and
// renaming three public methods so one consumer can name them alike would
// be the tail wagging the dog.
func NewSnapshotSource(users *UserService, orgs *OrganizationService, groups *GroupService) SnapshotSource {
	return snapshotServices{users: users, orgs: orgs, groups: groups}
}

type snapshotServices struct {
	users  *UserService
	orgs   *OrganizationService
	groups *GroupService
}

func (a snapshotServices) ListUsers(ctx context.Context, tenantID string, q UserQuery, page Page) ([]model.User, int64, error) {
	return a.users.List(ctx, tenantID, q, page)
}

func (a snapshotServices) ListOrganizations(ctx context.Context, tenantID string, activeOnly bool) ([]model.Organization, error) {
	return a.orgs.List(ctx, tenantID, activeOnly)
}

func (a snapshotServices) ListGroups(ctx context.Context, tenantID string) ([]model.Group, error) {
	return a.groups.List(ctx, tenantID)
}
