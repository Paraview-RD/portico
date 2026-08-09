package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"github.com/crewjam/saml"
	"github.com/google/uuid"
	xrv "github.com/mattermost/xml-roundtrip-validator"

	"github.com/paraview/portico/internal/auth"
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
// A registration decides who may receive assertions about this tenant's
// people, which is the same weight of decision as registering an OAuth
// relying party and is available on the same terms: a tenant administrator,
// over the API or from the command line, with every mutation audited. See
// OAuthClientService for why that is the right boundary.
type SAMLServiceProviderService struct {
	store *store.Store
	audit *AuditService
}

// NewSAMLServiceProviderService wires the service.
func NewSAMLServiceProviderService(st *store.Store, audit *AuditService) *SAMLServiceProviderService {
	return &SAMLServiceProviderService{store: st, audit: audit}
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
	// LaunchURL is optional: an application without one still signs people
	// in, it just does not appear in the portal as something to open.
	LaunchURL string
}

// Register adds a service provider to the actor's tenant.
func (s *SAMLServiceProviderService) Register(ctx context.Context, actor auth.Principal, in RegisterSPInput) (model.SAMLServiceProvider, error) {
	tenantID := actor.TenantID

	descriptor, err := parseSPMetadata(in.MetadataXML)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = descriptor.EntityID
	}

	now := store.Now()
	launchURL, err := normalizeLaunchURL(in.LaunchURL)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	err = s.store.ForTenant(tenantID).CreateSAMLServiceProvider(ctx, sqlcgen.CreateSAMLServiceProviderParams{
		ID:          uuid.NewString(),
		EntityID:    descriptor.EntityID,
		Name:        name,
		MetadataXml: in.MetadataXML,
		LaunchUrl:   launchURL,
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

	provider, err := s.Get(ctx, tenantID, descriptor.EntityID)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionSPCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetSAMLSP, TargetID: provider.EntityID, TargetName: provider.Name,
		Detail: spDetail(provider),
	})

	return provider, nil
}

// UpdateSPInput is the editable part of a service provider registration.
//
// Replacing the metadata document is how a service provider's signing or
// encryption certificate is rotated, so it has to be editable for a
// registration to survive past its first certificate expiry.
type UpdateSPInput struct {
	MetadataXML string
	Name        string
	LaunchURL   string
}

// Update replaces a service provider's name and metadata.
func (s *SAMLServiceProviderService) Update(ctx context.Context, actor auth.Principal, entityID string, in UpdateSPInput) (model.SAMLServiceProvider, error) {
	tenantID := actor.TenantID

	current, err := s.Get(ctx, tenantID, entityID)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	metadata := strings.TrimSpace(in.MetadataXML)
	if metadata == "" {
		metadata = current.MetadataXML
	}
	descriptor, err := parseSPMetadata(metadata)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	// A document declaring a different entity id describes a different
	// service provider. Storing it under this registration would silently
	// repoint it: assertions meant for one system would start being issued
	// to another, and the listing would still show the old name.
	if descriptor.EntityID != entityID {
		return model.SAMLServiceProvider{}, httpx.BadRequest("METADATA_ENTITY_ID_MISMATCH",
			"That metadata declares entity id "+descriptor.EntityID+
				", but this registration is for "+entityID+
				". Register it separately rather than replacing this one.")
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = current.Name
	}

	launchURL, err := normalizeLaunchURL(in.LaunchURL)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	err = s.store.ForTenant(tenantID).UpdateSAMLServiceProvider(
		ctx, entityID, name, metadata, launchURL, store.Now())
	if err != nil {
		return model.SAMLServiceProvider{}, fmt.Errorf("update service provider: %w", err)
	}

	updated, err := s.Get(ctx, tenantID, entityID)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionSPUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetSAMLSP, TargetID: entityID, TargetName: updated.Name,
		Detail: spDetail(updated),
	})

	return updated, nil
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

// GetByID returns one service provider by the registration's own id.
//
// The console addresses registrations this way rather than by entity id. An
// entity id is a URI, so putting one in a URL path means percent-encoding
// its slashes — and a reverse proxy that normalizes paths decodes them
// again, splitting the identifier across segments. That failure depends on
// somebody else's proxy configuration and would never show up in a test
// here, so the identifier is one that has no slashes to begin with.
func (s *SAMLServiceProviderService) GetByID(ctx context.Context, tenantID, id string) (model.SAMLServiceProvider, error) {
	row, err := s.store.ForTenant(tenantID).GetSAMLServiceProviderByID(ctx, id)
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
func (s *SAMLServiceProviderService) SetStatus(ctx context.Context, actor auth.Principal, entityID string, status model.Status) (model.SAMLServiceProvider, error) {
	tenantID := actor.TenantID

	if !status.Valid() {
		return model.SAMLServiceProvider{}, httpx.BadRequest("INVALID_STATUS",
			"Status must be ACTIVE or DISABLED.")
	}
	current, err := s.Get(ctx, tenantID, entityID)
	if err != nil {
		return model.SAMLServiceProvider{}, err
	}

	err = s.store.ForTenant(tenantID).UpdateSAMLServiceProviderStatus(
		ctx, entityID, string(status), store.Now())
	if err != nil {
		return model.SAMLServiceProvider{}, fmt.Errorf("update service provider status: %w", err)
	}

	action := model.ActionSPEnable
	if status == model.StatusDisabled {
		action = model.ActionSPDisable
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetSAMLSP, TargetID: entityID, TargetName: current.Name,
	})

	return s.Get(ctx, tenantID, entityID)
}

// spDetail summarizes a registration for the audit trail. The assertion
// consumer service endpoints are the security question here — an assertion
// about somebody is delivered to one of them — in the same way redirect URIs
// are for an OAuth client.
func spDetail(p model.SAMLServiceProvider) string {
	return "assertion consumer services: " + strings.Join(p.ACSURLs, ", ")
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

	// Go's XML parser silently re-writes some namespace constructs, which is
	// how signature-wrapping bugs get in. The protocol library runs this
	// check before parsing anything it will act on; a metadata document is
	// operator-supplied rather than attacker-supplied, but it is the same
	// parser and the validator is already here.
	if err := xrv.Validate(strings.NewReader(document)); err != nil {
		return nil, httpx.BadRequest("METADATA_INVALID",
			"That document does not survive an XML round trip, so it cannot be parsed safely.")
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
		LaunchURL:   row.LaunchUrl,
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
