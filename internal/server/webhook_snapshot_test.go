package server_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Paraview-RD/portico/internal/webhook"
)

// A snapshot is what a new subscription gets instead of a history it missed.
//
// Every other event says what happened, and a subscription created today
// missed everything that happened before it. The delivery table cannot fill
// the gap — finished rows are deleted after thirty days, and the ones that
// survive describe changes rather than state. So without this, a receiver
// building a mirror had no way to make the first copy.

func createSnapshotSubscription(t *testing.T, api *apiTest, admin string, events []string) string {
	t.Helper()

	resp := api.do(http.MethodPost, "/api/v1/webhooks", admin, map[string]any{
		"name": "mirror", "url": "https://203.0.113.10/portico", "events": events,
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("create subscription: %d %s %s", resp.Status, resp.Code, resp.Message)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return created.ID
}

type queuedDelivery struct {
	EventType string `json:"eventType"`
	Status    string `json:"status"`
}

// snapshotPayloads reads the bodies straight from the table.
//
// The delivery listing deliberately does not return them — a payload holds
// somebody's name and address, and that screen is an operational view of
// what was attempted, not a copy of what was said. So a test that needs to
// know what a receiver would actually get has to read the row.
func snapshotPayloads(t *testing.T, api *apiTest, eventType string) []string {
	t.Helper()

	db, err := sql.Open("pgx", api.dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(
		`SELECT payload FROM webhook_deliveries WHERE event_type = $1 ORDER BY created_at`,
		eventType)
	if err != nil {
		t.Fatalf("read payloads: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan payload: %v", err)
		}
		out = append(out, payload)
	}
	return out
}

func snapshotDeliveries(t *testing.T, api *apiTest, admin, id string) []queuedDelivery {
	t.Helper()

	// filter=all: the default hides the sync pages, which are the whole
	// subject of this file.
	resp := api.do(http.MethodGet,
		"/api/v1/webhooks/"+id+"/deliveries?limit=200&filter=all", admin, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("deliveries: %d %s", resp.Status, resp.Code)
	}
	var page struct {
		Items []queuedDelivery `json:"items"`
	}
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		t.Fatalf("decode deliveries: %v", err)
	}
	return page.Items
}

func TestASnapshotOpensAndClosesAroundItsPages(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	// Somebody who existed before the subscription did. This is the whole
	// case: an event-only subscriber never hears about this account.
	api.createUser(admin, "already-here", "already-here-password-1", "USER")

	id := createSnapshotSubscription(t, api, admin, []string{"*"})

	resp := api.do(http.MethodPost, "/api/v1/webhooks/"+id+"/snapshot", admin, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("snapshot: %d %s %s", resp.Status, resp.Code, resp.Message)
	}
	var summary struct {
		SyncID string         `json:"syncId"`
		Scope  []string       `json:"scope"`
		Counts map[string]int `json:"counts"`
		Pages  int            `json:"pages"`
	}
	if err := json.Unmarshal(resp.Data, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.SyncID == "" {
		t.Fatal("the snapshot reported no id; a receiver has nothing to scope its run to")
	}
	if summary.Counts["user"] < 2 {
		t.Errorf("the snapshot counted %d accounts; the bootstrap administrator "+
			"and the account created above are both already there",
			summary.Counts["user"])
	}

	queued := snapshotDeliveries(t, api, admin, id)

	var started, completed, pages int
	var sawUserPage bool
	for _, d := range queued {
		switch d.EventType {
		case webhook.EventSyncStarted:
			started++
		case webhook.EventSyncCompleted:
			completed++
		case webhook.EventSyncUsers:
			pages++
			sawUserPage = true
		case webhook.EventSyncOrganizations, webhook.EventSyncGroups:
			pages++
		}
	}

	// Exactly one of each bracket. Two sync.completed would tell a receiver
	// twice that it holds everything, and the second time it would be wrong
	// about a run still in flight.
	if started != 1 || completed != 1 {
		t.Errorf("queued %d sync.started and %d sync.completed, want one of each",
			started, completed)
	}
	if !sawUserPage {
		t.Error("no page of accounts was queued, so the snapshot delivered nothing " +
			"a mirror could be built from")
	}
	if pages != summary.Pages {
		t.Errorf("the summary claims %d pages and %d were queued", summary.Pages, pages)
	}

	// The account that existed before the subscription is in a page. This is
	// the assertion the feature exists for; the counts above would pass on a
	// snapshot that queued empty pages.
	var found bool
	for _, payload := range snapshotPayloads(t, api, webhook.EventSyncUsers) {
		var envelope struct {
			Type string `json:"type"`
			Data struct {
				SyncID string `json:"syncId"`
				Kind   string `json:"kind"`
				Page   int    `json:"page"`
				Total  int    `json:"total"`
				Items  []struct {
					Username string `json:"username"`
				} `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			t.Fatalf("decode page: %v", err)
		}
		if envelope.Data.SyncID != summary.SyncID {
			t.Errorf("a page carries sync id %q, the run reported %q",
				envelope.Data.SyncID, summary.SyncID)
		}
		if envelope.Data.Page < 1 || envelope.Data.Page > envelope.Data.Total {
			t.Errorf("page %d of %d is not a page number a receiver can act on",
				envelope.Data.Page, envelope.Data.Total)
		}
		for _, item := range envelope.Data.Items {
			if item.Username == "already-here" {
				found = true
			}
		}
	}
	if !found {
		t.Error("the account that existed before the subscription is in no page; " +
			"a receiver would build a mirror missing exactly the people it " +
			"asked for a snapshot to learn about")
	}
}

// A second snapshot while the first is still queued is refused.
//
// Two interleaved runs deliver two sets of pages a receiver cannot tell
// apart mid-stream, and the second sync.completed arrives while the first
// run's pages are still in flight — telling the receiver it holds everything
// when it does not.
func TestASecondSnapshotIsRefusedWhileOneIsStillQueued(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	id := createSnapshotSubscription(t, api, admin, []string{"*"})

	if resp := api.do(http.MethodPost, "/api/v1/webhooks/"+id+"/snapshot", admin, nil); resp.Status != http.StatusOK {
		t.Fatalf("first snapshot: %d %s", resp.Status, resp.Code)
	}
	resp := api.do(http.MethodPost, "/api/v1/webhooks/"+id+"/snapshot", admin, nil)
	if resp.Status != http.StatusConflict || resp.Code != "SNAPSHOT_IN_PROGRESS" {
		t.Fatalf("second snapshot = %d %s, want 409 SNAPSHOT_IN_PROGRESS",
			resp.Status, resp.Code)
	}
}

// The snapshot covers what the subscription asked to hear about, and nothing
// else. Sending the account list to a subscriber that only selected group
// events would answer a question it did not ask — and on a large tenant that
// is most of the data in the system.
func TestASnapshotCoversOnlyWhatTheSubscriptionSelected(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	id := createSnapshotSubscription(t, api, admin,
		[]string{webhook.EventGroupCreated, webhook.EventGroupUpdated})

	resp := api.do(http.MethodPost, "/api/v1/webhooks/"+id+"/snapshot", admin, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("snapshot: %d %s %s", resp.Status, resp.Code, resp.Message)
	}
	var summary struct {
		Scope  []string       `json:"scope"`
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(resp.Data, &summary); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(summary.Scope) != 1 || summary.Scope[0] != "group" {
		t.Errorf("scope is %v; a subscription to group events alone should be "+
			"sent groups alone", summary.Scope)
	}
	if _, ok := summary.Counts["user"]; ok {
		t.Error("the snapshot counted accounts for a subscription that never " +
			"asked for account events")
	}

	for _, d := range snapshotDeliveries(t, api, admin, id) {
		if d.EventType == webhook.EventSyncUsers {
			t.Error("a page of accounts was queued for a group-only subscription")
		}
	}
}

// A disabled subscription is refused rather than queued for.
//
// Disabling is how somebody stops a receiver being called at all; queueing a
// snapshot against one would be the largest delivery this product makes,
// waiting to fire the moment it is re-enabled.
func TestADisabledSubscriptionGetsNoSnapshot(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	id := createSnapshotSubscription(t, api, admin, []string{"*"})
	if resp := api.do(http.MethodPost, "/api/v1/webhooks/"+id+"/disable", admin, nil); resp.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", resp.Status, resp.Code)
	}

	resp := api.do(http.MethodPost, "/api/v1/webhooks/"+id+"/snapshot", admin, nil)
	if resp.Code != "SUBSCRIPTION_DISABLED" {
		t.Fatalf("snapshot of a disabled subscription = %d %s, want SUBSCRIPTION_DISABLED",
			resp.Status, resp.Code)
	}
}
