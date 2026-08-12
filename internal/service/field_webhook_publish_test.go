package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// The wiring, as opposed to the logic.
//
// field_webhook_test.go proves the overlay does the right thing to a payload.
// Nothing there would notice if publish looked up the rules under the wrong
// subscription, or handed the overlay an event type it could not read a
// subject from, or applied one subscriber's rules to another's delivery.
// Those are the failures that survive a green unit suite, so this goes through
// publish and reads what actually landed in the queue.

type publishFixture struct {
	t        *testing.T
	svc      *WebhookService
	store    *store.Store
	mappings *FieldMappingService
	tenantID string
}

func newPublishFixture(t *testing.T) *publishFixture {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := store.Now()
	tenantID := "tenant-hook-mapping"
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "hookmap", Name: "Hook mapping", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	audit := NewAuditService(st)
	catalogue := NewFieldCatalogue(st)
	mappings := NewFieldMappingService(st, audit, catalogue)

	return &publishFixture{
		t:        t,
		svc:      NewWebhookService(st, audit).WithFieldMappings(catalogue, mappings),
		store:    st,
		mappings: mappings,
		tenantID: tenantID,
	}
}

// subscribe registers a receiver through the store, for the reason
// newDeliveryFixture gives: Create refuses a local address, and what is under
// test here is not that rule.
func (f *publishFixture) subscribe(id, name string) string {
	f.t.Helper()
	err := f.store.ForTenant(f.tenantID).CreateWebhookSubscription(context.Background(),
		sqlcgen.CreateWebhookSubscriptionParams{
			ID: id, Name: name, Url: "https://203.0.113.10/hook",
			Secret: "whsec_0123456789abcdef", Events: webhook.Wildcard,
			CreatedAt: store.Now(),
		})
	if err != nil {
		f.t.Fatalf("seed subscription %s: %v", id, err)
	}
	return id
}

// mapFields saves one subscription's rules the way the console would.
func (f *publishFixture) mapFields(subscriptionID string, inputs ...FieldMappingInput) {
	f.t.Helper()
	actor := auth.Principal{TenantID: f.tenantID, UserID: "admin-1", Username: "admin"}
	ref := store.RecipientRef{WebhookSubscriptionID: subscriptionID}
	if _, err := f.mappings.Replace(context.Background(), actor, ref, inputs); err != nil {
		f.t.Fatalf("save mappings: %v", err)
	}
}

// account is a payload with something in every place a rule can reach.
func (f *publishFixture) account() model.User {
	return model.User{
		ID: "user-1", TenantID: f.tenantID, Username: "ada",
		DisplayName: "Ada Lovelace", Email: "ada@example.org",
		Role: model.RoleUser, Status: model.StatusActive,
		Profile: model.UserProfile{Department: "Analytical Engines", Title: "Engineer"},
	}
}

// delivered is the `data` object of the body queued for one subscription.
func (f *publishFixture) delivered(subscriptionID string) map[string]any {
	f.t.Helper()
	rows, err := f.store.ForTenant(f.tenantID).ListWebhookDeliveries(context.Background(), subscriptionID, 10)
	if err != nil {
		f.t.Fatalf("list deliveries: %v", err)
	}
	if len(rows) != 1 {
		f.t.Fatalf("subscription %s has %d queued deliveries, want exactly 1", subscriptionID, len(rows))
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(rows[0].Payload), &envelope); err != nil {
		f.t.Fatalf("read queued payload: %v", err)
	}
	return envelope.Data
}

// A subscription's rules reach the body that is queued for it, and only for it.
//
// Two subscribers to the same event, one of which configured a rename. The
// second is the assertion that matters: it has changed nothing, so it must
// receive what it received before this feature existed — and if publish looked
// the rules up by anything other than the subscription being written for, this
// is where that shows.
func TestOnlyTheSubscriptionThatConfiguredARenameSeesIt(t *testing.T) {
	f := newPublishFixture(t)
	renamer := f.subscribe("sub-renamer", "renames department")
	plain := f.subscribe("sub-plain", "configures nothing")

	f.mapFields(renamer, FieldMappingInput{SourceKey: "department", TargetName: "dept"})

	if err := f.svc.publish(context.Background(), f.tenantID, webhook.EventUserUpdated, f.account()); err != nil {
		t.Fatalf("publish: %v", err)
	}

	mapped := f.delivered(renamer)
	if mapped["dept"] != "Analytical Engines" {
		t.Errorf("the configured subscription received dept=%#v, want the department", mapped["dept"])
	}
	if profile, ok := mapped["profile"].(map[string]any); ok {
		if _, still := profile["department"]; still {
			t.Error("the department is also still inside profile, so it arrived twice")
		}
	}

	untouched := f.delivered(plain)
	profile, ok := untouched["profile"].(map[string]any)
	if !ok {
		t.Fatalf("the unconfigured subscription received no profile object: %#v", untouched)
	}
	if profile["department"] != "Analytical Engines" {
		t.Errorf("the unconfigured subscription's profile is %#v, want the untouched one", profile)
	}
	if _, leaked := untouched["dept"]; leaked {
		t.Error("another subscription's rename reached a subscriber that configured nothing")
	}
}

// Suppression reaches the queue too, and takes nothing else with it.
func TestASuppressedFieldIsNotQueued(t *testing.T) {
	f := newPublishFixture(t)
	sub := f.subscribe("sub-suppress", "does not want email")

	f.mapFields(sub, FieldMappingInput{SourceKey: "email", Suppressed: true})

	if err := f.svc.publish(context.Background(), f.tenantID, webhook.EventUserCreated, f.account()); err != nil {
		t.Fatalf("publish: %v", err)
	}

	body := f.delivered(sub)
	if _, still := body["email"]; still {
		t.Errorf("a suppressed field was queued: %#v", body["email"])
	}
	if body["username"] != "ada" {
		t.Errorf("suppressing the email disturbed the rest: username is %#v", body["username"])
	}
}

// Group events are delivered as they always were, even to a subscription that
// has configured mappings.
//
// The subscription below renames a field that a group payload does not have,
// which is the realistic case: somebody configured rules for accounts and
// subscribed to everything. What must not happen is the group body arriving
// changed — or the publish failing because nothing could be resolved for it.
func TestAGroupEventIsUnchangedForASubscriptionWithRules(t *testing.T) {
	f := newPublishFixture(t)
	sub := f.subscribe("sub-groups", "has account rules and takes everything")
	f.mapFields(sub, FieldMappingInput{SourceKey: "department", TargetName: "dept"})

	group := model.Group{ID: "group-1", DisplayName: "Engineering", Description: "everyone"}
	if err := f.svc.publish(context.Background(), f.tenantID, webhook.EventGroupCreated, group); err != nil {
		t.Fatalf("publish: %v", err)
	}

	body := f.delivered(sub)
	if body["displayName"] != "Engineering" || body["id"] != "group-1" {
		t.Errorf("the group payload arrived as %#v, want it unchanged", body)
	}
	if _, added := body["dept"]; added {
		t.Error("an account rule reached a group payload")
	}
}
