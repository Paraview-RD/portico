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

	// EventUserPasswordChanged fires whether the account changed its own
	// password or an administrator reset it. Both end every session, and a
	// system holding one has to be told; which of the two it was is an audit
	// question, and the audit trail is where it is answered.
	//
	// The payload is the account, which carries no credential — model.User
	// has no password field at all, by construction rather than by omission
	// here. An event named after a password must never be the thing that
	// carries one out of the building.
	EventUserPasswordChanged = "user.password_changed"
	// EventUserLocked fires where the lock is applied, not where a locked
	// account is turned away. A lock happens once and is refused many times;
	// a subscriber watching for attacks wants the first.
	EventUserLocked   = "user.locked"
	EventUserUnlocked = "user.unlocked"

	EventOrgCreated = "organization.created"
	EventOrgUpdated = "organization.updated"
	// EventOrgEnabled and EventOrgDisabled are separate from
	// organization.updated for the same reason user.enabled is separate from
	// user.updated: a status change is the one a downstream mirror must act
	// on, and it should not have to diff a payload to find it.
	EventOrgEnabled  = "organization.enabled"
	EventOrgDisabled = "organization.disabled"

	EventGroupCreated = "group.created"
	EventGroupUpdated = "group.updated"
	EventGroupDeleted = "group.deleted"
	// EventGroupMembersChanged carries the group as it now stands rather
	// than the delta. A subscriber wanting to know who is in a group reads
	// the group; an event per member would turn a bulk replacement into a
	// burst nobody asked for.
	EventGroupMembersChanged = "group.members_changed"

	// The events of a snapshot: what already existed when a subscription
	// was created.
	//
	// Every other event here says what happened. These say what is, which
	// is a different thing and is why they are not dressed up as
	// `user.created` — a subscriber backfilled that way would have an
	// account "created" today that has been on the payroll for six years,
	// and the audit trail and the delivery history would both record it.
	//
	// They are paged rather than one event per account because the unit
	// that matters to a receiver is a batch it can write in one
	// transaction. Fifty thousand accounts is a hundred deliveries here and
	// fifty thousand under any scheme that sends one each.
	EventSyncStarted = "sync.started"
	// A page. The kind is in the type rather than the body so that a
	// subscriber can select `sync.users` alone, and so the mapping subject
	// can be read from the type the way every other event's is.
	EventSyncUsers         = "sync.users"
	EventSyncOrganizations = "sync.organizations"
	EventSyncGroups        = "sync.groups"
	// EventSyncCompleted is the signal a receiver needs and no per-object
	// scheme can give: that it now holds everything, and may switch from
	// building its mirror to trusting it.
	EventSyncCompleted = "sync.completed"
)

// SyncStarted opens a snapshot.
type SyncStarted struct {
	SyncID string `json:"syncId"`
	// Scope is the kinds that will follow, decided by what the
	// subscription selected: a subscriber that only asked for group events
	// is not sent the account list.
	Scope    []string  `json:"scope"`
	PageSize int       `json:"pageSize"`
	AsOf     time.Time `json:"asOf"`
}

// SyncPage is one batch of what exists.
type SyncPage struct {
	SyncID string `json:"syncId"`
	Kind   string `json:"kind"`
	// Page counts from one. Total is the number of pages of this kind, so a
	// receiver can show progress and can tell a truncated run from a
	// finished one without waiting for sync.completed.
	Page  int   `json:"page"`
	Total int   `json:"total"`
	Items []any `json:"items"`
}

// SyncCompleted closes a snapshot.
type SyncCompleted struct {
	SyncID string `json:"syncId"`
	// Counts is per kind, and is what a receiver compares against its own
	// row count to discover it dropped a page it answered 200 to.
	Counts map[string]int `json:"counts"`
}

// AllEvents is what a subscription gets when it asks for everything.
var AllEvents = []string{
	EventUserCreated, EventUserUpdated, EventUserEnabled, EventUserDisabled,
	EventUserPasswordChanged, EventUserLocked, EventUserUnlocked,
	EventOrgCreated, EventOrgUpdated, EventOrgEnabled, EventOrgDisabled,
	EventGroupCreated, EventGroupUpdated, EventGroupDeleted,
	EventGroupMembersChanged,
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

// SignWith returns the header value for one or more keys.
//
// During a rotation there are two, newest first, comma-separated — the shape
// Stripe uses, and for the same reason. The receiver is the side that has to
// deploy a new secret, so the only way to give it a window is to sign each
// delivery with both keys until the window closes.
//
// A receiver must therefore split on "," and accept any element, rather than
// comparing the header as one string. That is a real requirement and not a
// formality: a receiver doing an exact match on the whole header verifies
// nothing from the moment a rotation starts until it finishes. It is why the
// console will not start a rotation without saying so, and why the
// documentation's example splits.
//
// Outside a rotation this returns exactly what it always did — one
// signature, no comma — so nothing changes for anybody not rotating.
func SignWith(secrets []string, timestamp time.Time, body []byte) string {
	parts := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		parts = append(parts, Sign(secret, timestamp, body))
	}
	return strings.Join(parts, ",")
}

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
