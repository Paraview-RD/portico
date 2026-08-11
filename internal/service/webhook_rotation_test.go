package service

// Rotating a signing secret without dropping deliveries.
//
// The property under test is not "a new secret is issued" — that is one
// line. It is that a receiver which has not yet deployed the new secret goes
// on verifying deliveries, because Portico produces the signature and the
// receiver checks it, so the receiver is the side that needs a window.
//
// The window is bought by signing with both keys at once. That is the whole
// mechanism, and it is invisible from anywhere except a receiver, which is
// why these check the header a receiver would actually see.

import (
	"strings"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/webhook"
)

func verifiesWith(secret, header string, timestamp time.Time, body []byte) bool {
	// What the documentation tells a receiver to do: split, then compare.
	// Comparing the header whole is exactly the mistake this arrangement
	// makes fatal, so the test must not make it either.
	expected := webhook.Sign(secret, timestamp, body)
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimSpace(candidate) == expected {
			return true
		}
	}
	return false
}

func TestDuringTheOverlapBothSecretsVerify(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Hour)
	subscription := sqlcgen.WebhookSubscription{
		Secret:                  "whsec_new",
		PreviousSecret:          "whsec_old",
		PreviousSecretExpiresAt: &expires,
	}

	body := []byte(`{"type":"user.disabled"}`)
	header := webhook.SignWith(signingSecrets(subscription, now), now, body)

	if !verifiesWith("whsec_new", header, now, body) {
		t.Error("a receiver that has deployed the new secret cannot verify")
	}
	if !verifiesWith("whsec_old", header, now, body) {
		t.Error("a receiver that has not deployed the new secret yet cannot " +
			"verify, which is the entire reason the overlap exists")
	}
	if !strings.Contains(header, ",") {
		t.Error("only one signature was sent during an overlap")
	}
}

func TestOnceTheOverlapEndsTheOldSecretStops(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Minute)
	subscription := sqlcgen.WebhookSubscription{
		Secret:                  "whsec_new",
		PreviousSecret:          "whsec_old",
		PreviousSecretExpiresAt: &expired,
	}

	body := []byte(`{"type":"user.disabled"}`)
	header := webhook.SignWith(signingSecrets(subscription, now), now, body)

	if verifiesWith("whsec_old", header, now, body) {
		t.Error("the replaced secret still verifies after its expiry, so " +
			"rotating never actually retires anything")
	}
	if !verifiesWith("whsec_new", header, now, body) {
		t.Error("the current secret does not verify")
	}
	if strings.Contains(header, ",") {
		t.Error("a second signature is still being sent after the overlap ended")
	}
}

// The ordinary case, which is every subscription that has never been
// rotated. It has to be byte-identical to what was sent before this existed,
// or introducing rotation would have broken every receiver that never uses
// it.
func TestWithoutARotationNothingChanges(t *testing.T) {
	now := time.Now()
	subscription := sqlcgen.WebhookSubscription{Secret: "whsec_only"}

	body := []byte(`{"type":"user.created"}`)
	header := webhook.SignWith(signingSecrets(subscription, now), now, body)

	if header != webhook.Sign("whsec_only", now, body) {
		t.Errorf("a subscription with no rotation signs differently now: %q", header)
	}
}

// A row where the expiry is set but the secret is empty, or the reverse.
// Neither is a state the service writes, and both would produce a header
// with a stray comma or an empty element — which a receiver splitting on ","
// would then compare against, and one of those comparisons is against the
// empty string.
func TestAHalfSetRotationDoesNotProduceAnEmptySignature(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Hour)

	for _, subscription := range []sqlcgen.WebhookSubscription{
		{Secret: "whsec_a", PreviousSecretExpiresAt: &expires},
		{Secret: "whsec_a", PreviousSecret: "whsec_b"},
	} {
		header := webhook.SignWith(signingSecrets(subscription, now), now, []byte("{}"))
		for _, part := range strings.Split(header, ",") {
			if strings.TrimSpace(part) == "" {
				t.Errorf("empty signature element in %q", header)
			}
		}
	}
}
