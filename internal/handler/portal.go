package handler

import (
	"net/http"
	"sort"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
)

// portalApplication is one thing a person can open from the portal.
//
// Deliberately not the administrative shape. A reader does not need the
// redirect URIs, the client id, or the metadata document; those are how the
// protocol works, and putting them on the home screen would be showing
// somebody the wiring of a door they are trying to walk through.
type portalApplication struct {
	Name string `json:"name"`
	// Protocol is carried because an administrator looking at their own
	// portal is often asking which registration a tile came from.
	Protocol  string `json:"protocol"`
	LaunchURL string `json:"launchUrl"`
}

// PortalApplications lists the applications a person can open.
//
// Readable by any signed-in caller, which is the whole point: the portal is
// the screen for somebody who is not an administrator, and every other
// application endpoint requires one.
//
// **These are the tenant's applications, not the caller's.** This version has
// no notion of who may use what — two fixed roles, no assignment — so every
// authenticated person can sign in to every registered application, and this
// list says the same thing to everybody. The screen states that rather than
// implying an entitlement that does not exist. Adding one is a real
// authorization feature and a different decision.
//
// Disabled registrations are left out, and so are the ones with no launch
// address: a tile that cannot be opened is not an offer.
func (h *Handler) PortalApplications(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())
	tenantID := principal.TenantID
	ctx := r.Context()

	applications := make([]portalApplication, 0)

	clients, err := h.clients.List(ctx, tenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, client := range clients {
		if openable(client.Status, client.LaunchURL) {
			applications = append(applications, portalApplication{
				Name: client.Name, Protocol: "oauth", LaunchURL: client.LaunchURL,
			})
		}
	}

	providers, err := h.serviceProviders.List(ctx, tenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, provider := range providers {
		if openable(provider.Status, provider.LaunchURL) {
			applications = append(applications, portalApplication{
				Name: provider.Name, Protocol: "saml", LaunchURL: provider.LaunchURL,
			})
		}
	}

	services, err := h.casServices.List(ctx, tenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, svc := range services {
		if openable(svc.Status, svc.LaunchURL) {
			applications = append(applications, portalApplication{
				Name: svc.Name, Protocol: "cas", LaunchURL: svc.LaunchURL,
			})
		}
	}

	// By name, not by protocol. Which protocol an application speaks is an
	// integration detail; a person looking for one knows what it is called.
	sort.Slice(applications, func(i, j int) bool {
		return applications[i].Name < applications[j].Name
	})

	httpx.OK(w, applications)
}

func openable(status model.Status, launchURL string) bool {
	return status == model.StatusActive && launchURL != ""
}
