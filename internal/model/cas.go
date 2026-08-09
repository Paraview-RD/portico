package model

import "time"

// CASTicketLifetime is how long a service ticket is worth anything.
//
// The CAS specification says a ticket should be good for a single use and
// no more than a few seconds — it exists only to survive one browser
// redirect from here to the service, which then validates it immediately.
// A minute is generous for a slow network and still short enough that an
// intercepted ticket is rarely worth anything.
const CASTicketLifetime = time.Minute

// CASService is a registered CAS service.
type CASService struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	Name     string `json:"name"`
	// URLPrefix is what a `service` parameter must begin with. It is a
	// prefix rather than an exact URL because CAS clients legitimately
	// append their own return-to parameters, and there are no wildcards in
	// it — see service.MatchCASService for exactly what matches.
	URLPrefix string `json:"urlPrefix"`
	// LaunchURL is where a person opens this application, for the portal.
	// Not the prefix above, which is a matching rule.
	LaunchURL string    `json:"launchUrl"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
