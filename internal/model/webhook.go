package model

// WebhookDeliveryStatus is where one delivery stands.
type WebhookDeliveryStatus string

// The three states a delivery can be in.
const (
	// WebhookPending is queued or awaiting a retry.
	WebhookPending WebhookDeliveryStatus = "PENDING"
	// WebhookDelivered means the receiver answered 2xx.
	WebhookDelivered WebhookDeliveryStatus = "DELIVERED"
	// WebhookFailed means it was given up on: either the receiver refused in
	// a way that will not change, or the attempts ran out. The row stays, so
	// somebody can see what happened and to whom.
	WebhookFailed WebhookDeliveryStatus = "FAILED"
)
