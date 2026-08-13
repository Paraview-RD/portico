package server_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/webhook"
)

// Webhooks, end to end: an administrator subscribes, something happens, and
// the event arrives signed.
//
// The delivery itself cannot run against a real external endpoint here — the
// dialer refuses local addresses, which is the point of it — so these tests
// exercise everything up to and including the queue, and the signing and
// destination rules have their own tests in internal/webhook.
//
// Subscriptions below are registered against a literal address from RFC
// 5737's documentation range rather than a hostname. Registering resolves the
// destination, so a hostname makes every test here depend on whoever is
// answering DNS: these once used hooks.example.com, which does not exist, and
// so they failed in CI with `400 INVALID_WEBHOOK_URL` — a webhook test
// reporting a DNS fact. What they are about is the queue and the secret, and
// a literal address takes the branch that never resolves anything.

func TestSubscribingRefusesDestinationsThatWouldReachInside(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	// The API-level half of internal/webhook's rules. It is checked here as
	// well because this is the boundary an administrator actually touches,
	// and a validation that exists but is not called is the usual way this
	// kind of rule goes missing.
	for _, url := range []string{
		"https://169.254.169.254/latest/meta-data/",
		"https://127.0.0.1:5432/hook",
		"https://10.1.2.3/hook",
		"http://example.com/hook",
	} {
		resp := api.do(http.MethodPost, "/api/v1/webhooks", admin, map[string]any{
			"name": "bad-" + url, "url": url, "events": []string{"*"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("POST %s was accepted (status %d); this server would then "+
				"make requests inside its own network on an administrator's behalf",
				url, resp.Status)
		}
	}
}

func TestTheSecretIsReturnedOnceAndNotListed(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	resp := api.do(http.MethodPost, "/api/v1/webhooks", admin, map[string]any{
		"name": "billing", "url": "https://203.0.113.10/portico",
		"events": []string{"*"},
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("create: %d %s", resp.Status, resp.Code)
	}

	var created struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Secret == "" {
		t.Fatal("no secret returned; the subscriber cannot verify anything")
	}

	listing := api.do(http.MethodGet, "/api/v1/webhooks", admin, nil)
	if strings.Contains(string(listing.Data), created.Secret) {
		t.Error("the listing returned the signing secret. It has to be stored " +
			"in the clear because it signs, which is exactly why it must not " +
			"be served a second time.")
	}
}

func TestAnEventIsQueuedForASubscriberThatSelectedIt(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	resp := api.do(http.MethodPost, "/api/v1/webhooks", admin, map[string]any{
		"name": "directory-sync", "url": "https://203.0.113.10/portico",
		"events": []string{webhook.EventUserCreated},
	})
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	api.createUser(admin, "watched-user", "watched-password-123", "USER")

	deliveries := api.do(http.MethodGet,
		"/api/v1/webhooks/"+created.ID+"/deliveries", admin, nil)
	var page struct {
		Items []struct {
			EventType string `json:"eventType"`
			Status    string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(deliveries.Data, &page); err != nil {
		t.Fatalf("decode deliveries: %v", err)
	}
	queued := page.Items

	if len(queued) == 0 {
		t.Fatal("creating a user queued nothing for a subscription to user.created")
	}
	if queued[0].EventType != webhook.EventUserCreated {
		t.Errorf("queued %q, want %q", queued[0].EventType, webhook.EventUserCreated)
	}
}

func TestASubscriberOnlyGetsWhatItSelected(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	// Subscribed to organizations, and a user is created. A subscription
	// that receives everything regardless of its selection is worse than no
	// filter at all: the subscriber has said what it can handle.
	resp := api.do(http.MethodPost, "/api/v1/webhooks", admin, map[string]any{
		"name": "org-only", "url": "https://203.0.113.10/orgs",
		"events": []string{webhook.EventOrgCreated},
	})
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	api.createUser(admin, "unwatched-user", "unwatched-password-123", "USER")

	deliveries := api.do(http.MethodGet,
		"/api/v1/webhooks/"+created.ID+"/deliveries", admin, nil)
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(deliveries.Data, &page); err != nil {
		t.Fatalf("decode deliveries: %v", err)
	}
	queued := page.Items
	if len(queued) != 0 {
		t.Errorf("a subscription to %s received %d user events",
			webhook.EventOrgCreated, len(queued))
	}
}

func TestAPausedSubscriptionIsNotQueuedFor(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	resp := api.do(http.MethodPost, "/api/v1/webhooks", admin, map[string]any{
		"name": "paused", "url": "https://203.0.113.10/paused",
		"events": []string{"*"},
	})
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	api.do(http.MethodPost, "/api/v1/webhooks/"+created.ID+"/disable", admin, nil)
	api.createUser(admin, "after-pause", "after-pause-password-123", "USER")

	deliveries := api.do(http.MethodGet,
		"/api/v1/webhooks/"+created.ID+"/deliveries", admin, nil)
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(deliveries.Data, &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	queued := page.Items
	// Pausing means stop sending, not hold everything and deliver it in a
	// burst when somebody resumes.
	if len(queued) != 0 {
		t.Errorf("a paused subscription queued %d events", len(queued))
	}
}

// The signature is what a receiver checks, so the test verifies it the way a
// receiver would rather than by calling Sign and comparing to Sign.
func TestASubscriberCanVerifyTheSignatureItReceives(t *testing.T) {
	t.Parallel()

	const secret = "whsec_example"
	body := []byte(`{"id":"abc","type":"user.created"}`)
	sent := time.Now()

	signature := webhook.Sign(secret, sent, body)

	// What a receiver's own code does: recompute over timestamp.body and
	// compare in constant time.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(sent.UTC().Unix(), 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expected)) {
		t.Fatalf("a receiver following the documented scheme computes a "+
			"different signature:\n got %s\nwant %s", signature, expected)
	}

	// And the timestamp is inside it, so a captured delivery cannot be
	// replayed later with its own header rewritten.
	replayed := webhook.Sign(secret, sent.Add(time.Hour), body)
	if replayed == signature {
		t.Error("the signature does not cover the timestamp; anyone who saw " +
			"one delivery could replay it forever")
	}
}

// A receiver that verifies, spelled out, because this is the code every
// subscriber has to write and getting it wrong is silent.
func TestTheDocumentedVerificationRejectsATamperedBody(t *testing.T) {
	t.Parallel()

	const secret = "whsec_example"
	original := []byte(`{"type":"user.disabled","data":{"id":"alice"}}`)
	tampered := []byte(`{"type":"user.disabled","data":{"id":"bob"}}`)
	sent := time.Now()

	signature := webhook.Sign(secret, sent, original)
	if webhook.Sign(secret, sent, tampered) == signature {
		t.Error("a modified body produces the same signature")
	}
}
