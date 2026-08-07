package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"github.com/crewjam/saml"
	"github.com/google/uuid"

	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// Errors from service-provider registration and lookup.
var (
	ErrServiceProviderNotFound = httpx.NotFound("SERVICE_PROVIDER_NOT_FOUND",
		"No such service provider.")
	ErrServiceProviderTaken = httpx.Conflict("SERVICE_PROVIDER_TAKEN",
		"That entity id is already registered in this tenant.")
)

// SAMLServiceProviderService owns the registered SAML service providers.
//
// Registered from the command line, like OAuth clients and tenants, and for
// the same reason: a registration decides who may receive assertions about
// this tenant's people, and there is no role in this version that could be
// authorized to grant that over HTTP.
type SAMLServiceProviderService struct {
	store *store.Store
}

// NewSAMLServiceProviderService wires the service.
func NewSAMLServiceProviderService(st *store.Store) *SAMLServiceProviderService {
	return &SAMLServiceProviderService{store: st}
}

// RegisterSPInput describes a service provider to register.
//
// The metadata document is the registration. Everything the protocol needs —
// the entity id, the assertion consumer service endpoints, the NameID
// formats a service provider will accept, its signing certificate — is in
// there, published by the service provider itself, and asking an operator to
// retype any of it is asking them to get it subtly wrong.
type RegisterSPInput struct {
	// MetadataXML is the service provider's metadata document.
	MetadataXML string
	// Name is what an operator calls it. Defaults to the entity id.
	Name string
}

// Register adds a service provider to a tenant.
func (s *SAMLServiceProviderService) Register(ctx context.Context, tenantID string, in RegisterSPInput) (model.SAMLServiceProvider, error) {
	descriptor, err := parseSPMetadata(in.MetadataXML)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = descriptor.EntityID
	}

	now := store.Now()
	err = s.store.ForTenant(tenantID).CreateSAMLServiceProvider(ctx, sqlcgen.CreateSAMLServiceProviderParams{
		ID:          uuid.NewString(),
		EntityID:    descriptor.EntityID,
		Name:        name,
		MetadataXml: in.MetadataXML,
		Status:      string(model.StatusActive),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.SAMLServiceProvider{}, ErrServiceProviderTaken
		}
		return model.SAMLServiceProvider{}, fmt.Errorf("register service provider: %w", err)
	}

	return s.Get(ctx, tenantID, descriptor.EntityID)
}

// Get returns one service provider.
func (s *SAMLServiceProviderService) Get(ctx context.Context, tenantID, entityID string) (model.SAMLServiceProvider, error) {
	row, err := s.store.ForTenant(tenantID).GetSAMLServiceProvider(ctx, entityID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.SAMLServiceProvider{}, ErrServiceProviderNotFound
		}
		return model.SAMLServiceProvider{}, fmt.Errorf("get service provider: %w", err)
	}
	return toServiceProvider(row), nil
}

// Descriptor returns the parsed metadata the protocol library works from.
func (s *SAMLServiceProviderService) Descriptor(ctx context.Context, tenantID, entityID string) (*saml.EntityDescriptor, error) {
	provider, err := s.Get(ctx, tenantID, entityID)
	if err != nil {
		return nil, err
	}
	if provider.Status != model.StatusActive {
		return nil, httpx.Forbidden("SERVICE_PROVIDER_DISABLED",
			"That service provider has been disabled.")
	}
	return parseSPMetadata(provider.MetadataXML)
}

// List returns every service provider in a tenant.
func (s *SAMLServiceProviderService) List(ctx context.Context, tenantID string) ([]model.SAMLServiceProvider, error) {
	rows, err := s.store.ForTenant(tenantID).ListSAMLServiceProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list service providers: %w", err)
	}

	providers := make([]model.SAMLServiceProvider, 0, len(rows))
	for _, row := range rows {
		providers = append(providers, toServiceProvider(row))
	}
	return providers, nil
}

// SetStatus enables or disables a service provider.
func (s *SAMLServiceProviderService) SetStatus(ctx context.Context, tenantID, entityID string, status model.Status) (model.SAMLServiceProvider, error) {
	if !status.Valid() {
		return model.SAMLServiceProvider{}, httpx.BadRequest("INVALID_STATUS",
			"Status must be ACTIVE or DISABLED.")
	}
	if _, err := s.Get(ctx, tenantID, entityID); err != nil {
		return model.SAMLServiceProvider{}, err
	}

	err := s.store.ForTenant(tenantID).UpdateSAMLServiceProviderStatus(
		ctx, entityID, string(status), store.Now())
	if err != nil {
		return model.SAMLServiceProvider{}, fmt.Errorf("update service provider status: %w", err)
	}
	return s.Get(ctx, tenantID, entityID)
}

// parseSPMetadata reads a service provider's metadata document and checks it
// says enough to be usable.
//
// The checks are the ones whose absence would otherwise surface as a failure
// mid-sign-in, in front of a person who cannot do anything about it: no
// entity id means requests cannot be matched to this registration, and no
// assertion consumer service means there is nowhere to deliver an assertion.
func parseSPMetadata(document string) (*saml.EntityDescriptor, error) {
	document = strings.TrimSpace(document)
	if document == "" {
		return nil, httpx.BadRequest("METADATA_REQUIRED",
			"A service provider metadata document is required.")
	}

	var descriptor saml.EntityDescriptor
	if err := xml.Unmarshal([]byte(document), &descriptor); err != nil {
		// An EntitiesDescriptor wrapping one entity is what several
		// federations publish, and it is the same document with one more
		// layer. Accepting it costs nothing and refusing it would be a
		// confusing "invalid metadata" for a file that is perfectly valid.
		var entities saml.EntitiesDescriptor
		if err := xml.Unmarshal([]byte(document), &entities); err != nil {
			return nil, httpx.BadRequest("METADATA_INVALID",
				"That does not parse as SAML metadata.")
		}
		if len(entities.EntityDescriptors) != 1 {
			return nil, httpx.BadRequest("METADATA_AMBIGUOUS",
				"That metadata describes more than one entity. Register them one at a time.")
		}
		descriptor = entities.EntityDescriptors[0]
	}

	if strings.TrimSpace(descriptor.EntityID) == "" {
		return nil, httpx.BadRequest("METADATA_NO_ENTITY_ID",
			"That metadata has no entityID, so requests from it could not be matched to this registration.")
	}
	if len(acsURLs(&descriptor)) == 0 {
		return nil, httpx.BadRequest("METADATA_NO_ACS",
			"That metadata declares no AssertionConsumerService, so there would be nowhere to deliver an assertion.")
	}
	if err := validateACSURLs(acsURLs(&descriptor)); err != nil {
		return nil, err
	}
	return &descriptor, nil
}

func acsURLs(descriptor *saml.EntityDescriptor) []string {
	var urls []string
	for _, sp := range descriptor.SPSSODescriptors {
		for _, acs := range sp.AssertionConsumerServices {
			urls = append(urls, acs.Location)
		}
	}
	return urls
}

// validateACSURLs applies the same rules as an OAuth redirect URI, because
// an assertion consumer service is the same thing: the address a credential
// about somebody is delivered to.
func validateACSURLs(locations []string) error {
	for _, location := range locations {
		parsed, err := url.Parse(location)
		if err != nil || !parsed.IsAbs() {
			return httpx.BadRequest("METADATA_ACS_INVALID",
				"An AssertionConsumerService location is not an absolute URL: "+location)
		}
		if parsed.Fragment != "" {
			return httpx.BadRequest("METADATA_ACS_INVALID",
				"An AssertionConsumerService location has a fragment, which is never sent to a server: "+location)
		}
		if parsed.Scheme == "http" && !isLoopback(parsed.Hostname()) {
			return httpx.BadRequest("METADATA_ACS_INSECURE",
				"An AssertionConsumerService location uses plain http over a network: "+location)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return httpx.BadRequest("METADATA_ACS_INVALID",
				"An AssertionConsumerService location must be http or https: "+location)
		}
	}
	return nil
}

func toServiceProvider(row sqlcgen.SamlServiceProvider) model.SAMLServiceProvider {
	provider := model.SAMLServiceProvider{
		ID:          row.ID,
		TenantID:    row.TenantID,
		EntityID:    row.EntityID,
		Name:        row.Name,
		MetadataXML: row.MetadataXml,
		Status:      model.Status(row.Status),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	// Best effort: a stored document parsed once when it was registered, so
	// a failure here is not something a caller can act on and an empty list
	// is more useful than an error on a listing.
	if descriptor, err := parseSPMetadata(row.MetadataXml); err == nil {
		provider.ACSURLs = acsURLs(descriptor)
	}
	return provider
}
