package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// Event types. Namespaced by subject so a subscriber can select what it
// cares about without a schema.
const (
	EventUserCreated  = "user.created"
	EventUserUpdated  = "user.updated"
	EventUserEnabled  = "user.enabled"
	EventUserDisabled = "user.disabled"
	EventOrgCreated   = "organization.created"
	EventOrgUpdated   = "organization.updated"
)

// AllEvents is what a subscription gets when it asks for everything.
var AllEvents = []string{
	EventUserCreated, EventUserUpdated, EventUserEnabled, EventUserDisabled,
	EventOrgCreated, EventOrgUpdated,
}

// Wildcard subscribes to every event, including ones added later.
//
// Offered because the alternative is that every new event type silently
// misses the subscribers who wanted everything — they would have to know a
// release added one. A subscriber that cannot tolerate unknown types picks
// them explicitly instead.
const Wildcard = "*"

// Envelope is the body delivered.
//
// It carries an id so a receiver can recognise a redelivery. Retries mean at
// least once, never exactly once — a response lost on the way back is
// indistinguishable from one that never arrived — so the receiver has to be
// able to tell, and this is what it uses.
type Envelope struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Tenant     string    `json:"tenant"`
	OccurredAt time.Time `json:"occurredAt"`
	Data       any       `json:"data"`
}

// Selects reports whether a subscription's event list covers eventType.
func Selects(events, eventType string) bool {
	for _, selected := range strings.Split(events, ",") {
		selected = strings.TrimSpace(selected)
		if selected == Wildcard || selected == eventType {
			return true
		}
	}
	return false
}

// Signature headers, in the shape most receivers already have code for.
const (
	HeaderSignature = "X-Portico-Signature"
	HeaderTimestamp = "X-Portico-Timestamp"
	HeaderEvent     = "X-Portico-Event"
	HeaderDelivery  = "X-Portico-Delivery"
)

// Sign returns the signature for a body at a moment in time.
//
// The timestamp is inside the signed string, not merely sent beside it. A
// signature over the body alone is replayable forever by anyone who ever saw
// one — including the receiver's own logs and any proxy in between — and a
// replayed "user.disabled" is a denial of service against one person's
// account in whatever system consumes these.
//
// The receiver checks it by recomputing this and comparing in constant time,
// then rejecting a timestamp too far from now. Both halves are needed: the
// signature says the body is ours, the timestamp says it is current.
func Sign(secret string, timestamp time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	// Length-prefixed by construction: the timestamp is fixed-width digits
	// followed by a separator that cannot appear in it, so no two different
	// (timestamp, body) pairs produce the same signed string. Concatenating
	// them directly would let a body beginning with digits impersonate a
	// different timestamp.
	mac.Write([]byte(formatTimestamp(timestamp)))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// formatTimestamp renders the value that goes in the header and the
// signature. Unix seconds: unambiguous, and a receiver in any language can
// compare it to their own clock without parsing a date format.
func formatTimestamp(t time.Time) string {
	return strconv.FormatInt(t.UTC().Unix(), 10)
}
