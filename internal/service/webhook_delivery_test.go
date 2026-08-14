package service

// Webhook delivery against a server that actually receives it.
//
// This is the one hop in the chain that nothing covered. The rules for
// registering a destination have their own tests in internal/webhook
// (destination_test.go: six kinds of address refused, the address re-checked
// at connection time, redirects not followed), and everything up to the
// queue is covered in internal/server. What was missing is what happens
// after the queue: does the request actually arrive, is it signed the way
// the documentation says, does a 500 come back for another try, does a 400
// stop.
//
// The subscription is seeded through the store rather than through
// WebhookService.Create, because Create calls ValidateDestination and an
// httptest server listens on 127.0.0.1, which is exactly the address that
// rule exists to refuse. That is not this test smuggling past a security
// control: the control is the registration rule, it is tested where it
// lives, and what is under test here is the hop after it. Delivery itself
// takes the *http.Client as a parameter — production passes the one whose
// dialer refuses local addresses, and nothing here changes that.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// received is one request as the subscriber saw it.
type received struct {
	event      string
	deliveryID string
	timestamp  string
	signature  string
	body       []byte
}

// receiver is a subscriber: it records what arrived and answers with
// whatever the test told it to.
type receiver struct {
	mu     sync.Mutex
	status int
	// replyBody is what it writes back, so a test can assert that what a
	// receiver says about a refusal is what the console will show.
	replyBody string
	got       []received
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)

	r.mu.Lock()
	r.got = append(r.got, received{
		event:      req.Header.Get(webhook.HeaderEvent),
		deliveryID: req.Header.Get(webhook.HeaderDelivery),
		timestamp:  req.Header.Get(webhook.HeaderTimestamp),
		signature:  req.Header.Get(webhook.HeaderSignature),
		body:       body,
	})
	status := r.status
	reply := r.replyBody
	r.mu.Unlock()

	w.WriteHeader(status)
	if reply != "" {
		_, _ = io.WriteString(w, reply)
	}
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *receiver) last() received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.got[len(r.got)-1]
}

func (r *receiver) setReply(body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replyBody = body
}

func (r *receiver) answer(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

// verifyAsDocumented recomputes the signature the way docs/webhooks.md tells
// a subscriber to, rather than by calling webhook.Sign.
//
// Calling Sign would compare the implementation with itself and pass however
// wrong both were. This is the recipe a reader follows, written out: HMAC
// SHA-256 over the timestamp, a dot, and the raw body.
func verifyAsDocumented(secret string, r received) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(r.timestamp))
	mac.Write([]byte("."))
	mac.Write(r.body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(r.signature))
}

type deliveryFixture struct {
	t        *testing.T
	svc      *WebhookService
	store    *store.Store
	tenantID string
	subID    string
	secret   string
	receiver *receiver
	server   *httptest.Server
}

func newDeliveryFixture(t *testing.T) *deliveryFixture {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := store.Now()
	tenantID := "tenant-webhook-delivery"
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "hookdel", Name: "Hooks", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	rec := &receiver{status: http.StatusOK}
	server := httptest.NewServer(rec)
	t.Cleanup(server.Close)

	const (
		subID  = "sub-delivery"
		secret = "whsec_0123456789abcdef"
	)
	if err := st.ForTenant(tenantID).CreateWebhookSubscription(ctx,
		sqlcgen.CreateWebhookSubscriptionParams{
			ID:        subID,
			Name:      "test receiver",
			Url:       server.URL,
			Secret:    secret,
			Events:    webhook.EventUserCreated + "," + webhook.EventUserDisabled,
			CreatedAt: now,
		}); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	return &deliveryFixture{
		t:        t,
		svc:      NewWebhookService(st, NewAuditService(st)),
		store:    st,
		tenantID: tenantID,
		subID:    subID,
		secret:   secret,
		receiver: rec,
		server:   server,
	}
}

// queue publishes one event, failing the test if it could not be queued.
// The exported Publish swallows errors on purpose — a subscriber's problem
// is not the caller's — which is the wrong behaviour to build assertions on.
func (f *deliveryFixture) queue(eventType string) {
	f.t.Helper()
	if err := f.svc.publish(context.Background(), f.tenantID, eventType, map[string]string{
		"id":       "user-1",
		"username": "sam",
	}); err != nil {
		f.t.Fatalf("publish: %v", err)
	}
}

// dispatch runs one pass with a plain client. Production passes the one from
// webhook.NewClient, whose dialer refuses this address; the parameter is why
// no production code has to change for this test to exist.
func (f *deliveryFixture) dispatch() int {
	f.t.Helper()
	n, err := f.svc.DispatchDue(context.Background(), f.tenantID, http.DefaultClient)
	if err != nil {
		f.t.Fatalf("dispatch: %v", err)
	}
	return n
}

func (f *deliveryFixture) delivery() sqlcgen.WebhookDelivery {
	f.t.Helper()
	rows, err := f.store.ForTenant(f.tenantID).ListWebhookDeliveries(
		context.Background(), f.subID, store.AfterEverything, "", "all", 10)
	if err != nil {
		f.t.Fatalf("list deliveries: %v", err)
	}
	if len(rows) != 1 {
		f.t.Fatalf("expected one delivery, got %d", len(rows))
	}
	return rows[0]
}

func TestAnEventArrivesSignedAndTheDeliveryIsRecorded(t *testing.T) {
	f := newDeliveryFixture(t)
	f.queue(webhook.EventUserCreated)

	if n := f.dispatch(); n != 1 {
		t.Fatalf("dispatched %d deliveries, want 1", n)
	}
	if got := f.receiver.count(); got != 1 {
		t.Fatalf("receiver saw %d requests, want 1", got)
	}

	req := f.receiver.last()

	if !verifyAsDocumented(f.secret, req) {
		t.Error("the signature does not verify by the recipe in docs/webhooks.md")
	}

	// The timestamp is inside the signature, so a receiver can reject a
	// replay. Worth asserting it is a sane clock value and not, say, zero,
	// which would verify perfectly and be useless for that purpose.
	seconds, err := strconv.ParseInt(req.timestamp, 10, 64)
	if err != nil {
		t.Fatalf("timestamp header %q is not unix seconds: %v", req.timestamp, err)
	}
	if age := time.Since(time.Unix(seconds, 0)); age < 0 || age > time.Minute {
		t.Errorf("timestamp is %v away from now", age)
	}

	if req.event != webhook.EventUserCreated {
		t.Errorf("event header = %q, want %q", req.event, webhook.EventUserCreated)
	}

	var envelope struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Tenant string `json:"tenant"`
		Data   struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := json.Unmarshal(req.body, &envelope); err != nil {
		t.Fatalf("body is not the documented envelope: %v", err)
	}
	if envelope.ID != req.deliveryID {
		t.Errorf("envelope id %q and delivery header %q disagree, so a "+
			"receiver deduplicating on either one would be deduplicating on "+
			"a different thing than the other", envelope.ID, req.deliveryID)
	}
	if envelope.Type != webhook.EventUserCreated || envelope.Tenant != f.tenantID {
		t.Errorf("envelope = %+v, want the event and tenant published", envelope)
	}
	if envelope.Data.Username != "sam" {
		t.Errorf("payload lost its data: %+v", envelope.Data)
	}

	row := f.delivery()
	if row.Status != string(model.WebhookDelivered) {
		t.Errorf("status = %q, want DELIVERED", row.Status)
	}
	if row.DeliveredAt == nil {
		t.Error("delivered_at is null on a delivery that arrived")
	}
	if !row.LastStatus.Valid || row.LastStatus.Int32 != http.StatusOK {
		t.Errorf("last_status = %+v, want 200", row.LastStatus)
	}
}

func TestAServerErrorIsTriedAgainLaterRatherThanImmediately(t *testing.T) {
	f := newDeliveryFixture(t)
	f.receiver.answer(http.StatusInternalServerError)
	f.queue(webhook.EventUserDisabled)

	f.dispatch()
	if got := f.receiver.count(); got != 1 {
		t.Fatalf("receiver saw %d requests, want 1", got)
	}

	row := f.delivery()
	if row.Status != string(model.WebhookPending) {
		t.Errorf("status = %q, want PENDING: a 500 is the receiver having a "+
			"bad moment, not a refusal", row.Status)
	}
	if row.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", row.Attempts)
	}
	if !row.LastStatus.Valid || row.LastStatus.Int32 != http.StatusInternalServerError {
		t.Errorf("last_status = %+v, want 500", row.LastStatus)
	}
	if row.NextAttemptAt == nil {
		t.Fatal("next_attempt_at is null on a delivery that will be retried")
	}

	// The backoff has to be real rather than recorded. A second pass right
	// now must send nothing: if it does, a receiver that is down gets
	// hammered at the tick rate for as many attempts as it has left, which
	// is the opposite of what backing off is for.
	if n := f.dispatch(); n != 0 {
		t.Errorf("a second immediate pass dispatched %d, want 0", n)
	}
	if got := f.receiver.count(); got != 1 {
		t.Errorf("receiver saw %d requests after a second pass, want still 1", got)
	}
}

// dueNow drags a pending delivery's next attempt into the past, so the next
// pass picks it up. Nothing in a test waits for real time.
func (f *deliveryFixture) dueNow() {
	f.t.Helper()
	if _, err := f.store.DB().ExecContext(context.Background(),
		`UPDATE webhook_deliveries SET next_attempt_at = now() - interval '1 hour' WHERE id = $1`,
		f.delivery().ID); err != nil {
		f.t.Fatalf("bring the next attempt forward: %v", err)
	}
}

// The gap between attempts grows the way the documentation says it does.
//
// Backoff is indexed by the attempt that just failed — its own comment says
// so — and the delay is chosen one line after the count is incremented. Using
// the count from before the increment gives every failure the delay belonging
// to the previous one: the second waits the first's thirty seconds rather
// than two minutes, and the twenty-minute arm is unreachable, because the
// largest value that can arrive is three.
//
// The visible consequence is the whole schedule. Five attempts over roughly
// half an hour is what docs/webhooks.md promises a subscriber; off by one it
// is eight minutes, and a receiver that was restarting has lost its window.
func TestTheDelayBetweenAttemptsGrowsAsDocumented(t *testing.T) {
	f := newDeliveryFixture(t)
	f.receiver.answer(http.StatusInternalServerError)
	f.queue(webhook.EventUserDisabled)

	for attempt, want := range []time.Duration{
		30 * time.Second,
		2 * time.Minute,
		5 * time.Minute,
		20 * time.Minute,
	} {
		before := store.Now()
		f.dispatch()

		row := f.delivery()
		if row.NextAttemptAt == nil {
			t.Fatalf("attempt %d: next_attempt_at is null, so it will never "+
				"be retried", attempt+1)
		}
		got := row.NextAttemptAt.Sub(before)
		// Generous either way: what is being asserted is which arm of the
		// switch was chosen, and the arms are far enough apart that no
		// plausible delay confuses two of them.
		if got < want-5*time.Second || got > want+30*time.Second {
			t.Errorf("after failure %d the next attempt is %s away, want about %s",
				attempt+1, got.Round(time.Second), want)
		}
		f.dueNow()
	}

	// The fifth is the last. Spent, not scheduled.
	f.dispatch()
	row := f.delivery()
	if row.Status != string(model.WebhookFailed) {
		t.Errorf("status after the fifth attempt = %q, want FAILED", row.Status)
	}
	if row.NextAttemptAt != nil {
		t.Error("a delivery that has spent its attempts is still scheduled")
	}
}

// Nothing outlives the retention window, including a delivery that never
// finished.
//
// A subscription paused while something was queued for it leaves those rows
// pending: the worker skips a paused subscription without recording an
// attempt, so they are never marked failed, and the sweep only removed rows
// that were not pending. A subscription paused and forgotten therefore grew
// this table for as long as the deployment lived, while the documentation
// said deliveries are kept for thirty days.
//
// Thirty days is far past any retry — five attempts span under half an hour
// — so a row still pending at that age is not waiting for anything.
func TestAPausedSubscriptionsBacklogDoesNotOutliveRetention(t *testing.T) {
	f := newDeliveryFixture(t)
	f.receiver.answer(http.StatusInternalServerError)
	f.queue(webhook.EventUserDisabled)

	// Failed once, so it is pending a retry, and then the subscription is
	// paused — which is what stops it ever being attempted again.
	f.dispatch()
	if row := f.delivery(); row.Status != string(model.WebhookPending) {
		t.Fatalf("status = %q, want PENDING before pausing", row.Status)
	}
	if _, err := f.store.DB().ExecContext(context.Background(),
		`UPDATE webhook_subscriptions SET status = 'DISABLED' WHERE id = $1`,
		f.subID); err != nil {
		t.Fatalf("pause the subscription: %v", err)
	}

	// Older than the retention window.
	if _, err := f.store.DB().ExecContext(context.Background(),
		`UPDATE webhook_deliveries SET created_at = now() - interval '31 days'
		 WHERE subscription_id = $1`, f.subID); err != nil {
		t.Fatalf("age the delivery: %v", err)
	}

	if err := f.svc.SweepDeliveries(context.Background(), f.tenantID, store.Now()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	rows, err := f.store.ForTenant(f.tenantID).ListWebhookDeliveries(
		context.Background(), f.subID, store.AfterEverything, "", "all", 10)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("a delivery from 31 days ago survived the sweep (%d left) — "+
			"it is pending against a paused subscription, so nothing will ever "+
			"finish it and nothing was removing it", len(rows))
	}
}

func TestARefusalIsNotTriedAgain(t *testing.T) {
	f := newDeliveryFixture(t)
	f.receiver.answer(http.StatusBadRequest)
	f.queue(webhook.EventUserCreated)

	f.dispatch()

	row := f.delivery()
	if row.Status != string(model.WebhookFailed) {
		t.Errorf("status = %q, want FAILED: a 400 means the receiver "+
			"understood and refused, and sending it again produces another "+
			"refusal", row.Status)
	}
	if row.NextAttemptAt != nil {
		t.Errorf("next_attempt_at = %v, want none", row.NextAttemptAt)
	}
	if row.LastError == "" {
		t.Error("last_error is empty, so the subscriptions screen would show " +
			"a failure with no reason")
	}
}

func TestATamperedBodyDoesNotVerify(t *testing.T) {
	f := newDeliveryFixture(t)
	f.queue(webhook.EventUserCreated)
	f.dispatch()

	req := f.receiver.last()
	req.body = append(req.body, ' ')

	if verifyAsDocumented(f.secret, req) {
		t.Error("a body with one byte added still verified")
	}
}

// What the receiver said is kept, because "500" is not a diagnosis.
//
// A receiver refusing a delivery almost always says why — which field it did
// not like, which id it could not resolve — and until this was stored that
// sentence lived only in their logs, on the other side of an email. The
// status code was all this side kept.
func TestWhatTheReceiverAnsweredIsKept(t *testing.T) {
	f := newDeliveryFixture(t)
	f.receiver.answer(http.StatusBadRequest)
	f.receiver.setReply(`{"error":"unknown organization id org-404"}`)

	f.queue(webhook.EventUserCreated)
	f.dispatch()

	detail, err := f.svc.Delivery(context.Background(), f.tenantID, f.subID, f.delivery().ID)
	if err != nil {
		t.Fatalf("delivery detail: %v", err)
	}
	if !strings.Contains(detail.Response, "org-404") {
		t.Errorf("response = %q, want what the receiver actually said", detail.Response)
	}
	// And the request body, which is what the signature was computed over —
	// the thing a receiver debugging a signature mismatch has to compare
	// against.
	if !strings.Contains(detail.Payload, "sam") {
		t.Errorf("payload = %q, want the body as sent", detail.Payload)
	}
}

// A receiver answering an error with a page of HTML must not put a page of
// HTML in this database, once per attempt, forever.
func TestTheKeptAnswerIsCapped(t *testing.T) {
	f := newDeliveryFixture(t)
	f.receiver.answer(http.StatusInternalServerError)
	f.receiver.setReply(strings.Repeat("x", webhook.ResponseKeep*4))

	f.queue(webhook.EventUserCreated)
	f.dispatch()

	detail, err := f.svc.Delivery(context.Background(), f.tenantID, f.subID, f.delivery().ID)
	if err != nil {
		t.Fatalf("delivery detail: %v", err)
	}
	if len(detail.Response) > webhook.ResponseKeep {
		t.Errorf("kept %d bytes of the answer, cap is %d",
			len(detail.Response), webhook.ResponseKeep)
	}
}

// Paging is by cursor, and the pages do not overlap or skip.
//
// The table is written to while it is read, which is why this is not an
// offset: the assertion that matters is that walking the pages visits every
// delivery exactly once even though more arrive in between.
func TestDeliveriesPageByCursor(t *testing.T) {
	f := newDeliveryFixture(t)
	for i := 0; i < 5; i++ {
		f.queue(webhook.EventUserCreated)
	}

	ctx := context.Background()
	seen := map[string]int{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("paging did not terminate")
		}
		page, err := f.svc.Deliveries(ctx, f.tenantID, f.subID, cursor, DeliveryFilterAll, 2)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, item := range page.Items {
			seen[item.ID]++
		}
		// A delivery queued between pages, which is the case an offset gets
		// wrong: it shifts every later row by one, so the reader sees one
		// twice and misses another entirely.
		f.queue(webhook.EventUserCreated)

		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	if len(seen) < 5 {
		t.Errorf("walked the pages and saw %d deliveries, want at least the 5 that existed first", len(seen))
	}
	for id, times := range seen {
		if times != 1 {
			t.Errorf("delivery %s appeared %d times across the pages", id, times)
		}
	}
}

// A cursor this server did not issue is refused rather than interpreted.
func TestAForeignCursorIsRefused(t *testing.T) {
	f := newDeliveryFixture(t)
	if _, err := f.svc.Deliveries(context.Background(), f.tenantID, f.subID,
		"not-a-cursor", DeliveryFilterAll, 10); err == nil {
		t.Error("a cursor this server never issued was accepted")
	}
}
