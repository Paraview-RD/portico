package model

import "time"

// SAMLAuthRequestLifetime bounds how long a SAML sign-in may take.
//
// The protocol library independently refuses an authentication request more
// than ninety seconds older than its issue instant, which is far shorter
// than a person takes to find a password. Freshness is therefore judged
// against the moment Portico accepted the request, and this is what bounds
// how long it may then sit waiting — see docs/federation.md.
const SAMLAuthRequestLifetime = 15 * time.Minute

// SAMLAssertionLifetime is how long an issued assertion is valid for.
//
// Short, because a service provider consumes an assertion immediately and
// then keeps its own session; the window only has to cover the browser's
// round trip from here to the assertion consumer service.
const SAMLAssertionLifetime = 5 * time.Minute

// SAMLServiceProvider is a registered SAML service provider.
type SAMLServiceProvider struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	// EntityID is what the service provider calls itself in its requests,
	// and what a request is matched against.
	EntityID string `json:"entityId"`
	Name     string `json:"name"`
	// MetadataXML is the service provider's own metadata document, kept
	// whole. The protocol library reads the assertion consumer service
	// endpoints and signing certificates out of it.
	MetadataXML string `json:"-"`
	// LaunchURL is where a person opens this application, for the portal.
	// Not an assertion consumer service, which is where an assertion is
	// posted mid-flow.
	LaunchURL string `json:"launchUrl"`
	// ACSURLs are the endpoints assertions may be delivered to, extracted
	// from the metadata for display. They are not what the protocol matches
	// against — that is the document itself.
	ACSURLs   []string  `json:"acsUrls"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
