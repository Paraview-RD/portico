// Package handler contains the HTTP handlers. Handlers parse and validate
// the wire format, delegate to a service, and render the result; business
// rules live in the service layer.
package handler

import (
	"github.com/paraview/portico/internal/service"
)

// Handler holds the services the HTTP layer delegates to.
type Handler struct {
	users    *service.UserService
	orgs     *service.OrganizationService
	audit    *service.AuditService
	settings *service.SettingsService
}

// New returns a Handler backed by the given services.
func New(
	users *service.UserService,
	orgs *service.OrganizationService,
	audit *service.AuditService,
	settings *service.SettingsService,
) *Handler {
	return &Handler{users: users, orgs: orgs, audit: audit, settings: settings}
}
