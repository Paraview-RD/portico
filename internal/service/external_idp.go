package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/oidcrp"
	"github.com/Paraview-RD/portico/internal/secrets"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Configuring the providers this deployment will believe.
//
// This is administrative rather than protocol work — the protocol is in
// internal/oidcrp — but it decides something larger than a setting usually
// does: who may hold an account here on somebody else's word. That is why it
// audits like an application registration rather than like a preference.

// ExternalIDPService owns the provider configuration.
type ExternalIDPService struct {
	store *store.Store
	audit *AuditService
	// users issues the session an accepted identity earns. The dependency
	// is one-way: nothing in the account service knows this exists.
	users *UserService
	// vault may be nil, in which case saving a client secret is refused
	// rather than written in the clear — the same position directory bind
	// passwords and webhook headers take, and for the same reason: this is
	// a credential Portico must present, so a digest is useless.
	vault *secrets.Vault
	// publicURL is what the redirect URI is built from. It has to match
	// what was registered at the other end exactly, character for
	// character, so it is derived from one configured value rather than
	// from the request — a redirect URI that varied with the Host header
	// would work until somebody put a second name on the deployment.
	publicURL string
}

// NewExternalIDPService builds it.
func NewExternalIDPService(st *store.Store, users *UserService, audit *AuditService, vault *secrets.Vault, publicURL string) *ExternalIDPService {
	return &ExternalIDPService{store: st, users: users, audit: audit, vault: vault, publicURL: publicURL}
}

// Errors this service returns.
var (
	ErrExternalIDPNotFound = httpx.NotFound("EXTERNAL_IDP_NOT_FOUND",
		"No such identity provider.")
	ErrExternalIDPIssuerTaken = httpx.Conflict("EXTERNAL_IDP_ISSUER_TAKEN",
		"This tenant already has a provider for that issuer.")
)

// ExternalIDP is one configured provider, as the console sees it.
//
// There is no secret on it, by construction rather than by omission. What is
// stored is sealed and is only ever unsealed on the way to the provider; a
// field here would mean every list of providers carried every secret to a
// browser.
type ExternalIDP struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	ButtonLabel        string `json:"buttonLabel"`
	Issuer             string `json:"issuer"`
	ClientID           string `json:"clientId"`
	Scopes             string `json:"scopes"`
	TrustVerifiedEmail bool   `json:"trustVerifiedEmail"`
	Status             string `json:"status"`
	// HasSecret says whether one is stored, which is what an edit form needs
	// in order to explain that leaving the field blank keeps it.
	HasSecret bool `json:"hasSecret"`
	// RedirectURI is what has to be registered at the other end. Returned
	// rather than described, because it is the value somebody copies and a
	// sentence about how it is composed is a sentence they have to compose
	// correctly.
	RedirectURI string `json:"redirectUri"`
}

// ExternalIDPInput is what an administrator supplies.
type ExternalIDPInput struct {
	Name        string
	ButtonLabel string
	Issuer      string
	ClientID    string
	// ClientSecret empty on an edit means "keep the stored one". On a
	// create it means a public client, which is why it is not required.
	ClientSecret       string
	Scopes             string
	TrustVerifiedEmail bool
}

// RedirectURI is where a provider sends somebody back to.
//
// Per tenant, because the client registration is per tenant: two tenants
// signing in through the same issuer are two different applications to it,
// with their own client ids and their own registered addresses.
//
// A console address rather than the API endpoint that does the work. What
// arrives here is a top-level navigation — the browser leaves for the
// provider and comes back by following a redirect, so whatever answers has
// to be something a person can look at. The API endpoint answers JSON, and
// JSON is what a person would have been shown. So the console takes the
// landing, reads the `state` and `code` out of its own address, and spends
// them on the API call itself; the session that comes back is stored the
// same way a password sign-in's is, rather than travelling in a URL that
// browser history and every proxy in between would keep.
//
// The tenant is in the path for the same reason it is in the sign-in
// screen's: the page has to know which tenant it is completing for, and it
// arrives without a header, without a cookie, and without anything else this
// deployment gave it.
func (s *ExternalIDPService) RedirectURI(tenantCode string) string {
	base := strings.TrimRight(s.publicURL, "/")
	if tenantCode == "" || tenantCode == model.DefaultTenantCode {
		return base + ExternalCallbackPath
	}
	return base + "/t/" + tenantCode + ExternalCallbackPath
}

// ExternalCallbackPath is the console route that completes a sign-in.
//
// Named here rather than written twice, because it is the one string in this
// feature that two systems have to agree on character for character: the
// console serves it, and somebody registers it at a provider that will
// refuse anything else.
const ExternalCallbackPath = "/external/callback"

// List returns every provider configured for a tenant.
func (s *ExternalIDPService) List(ctx context.Context, tenantID, tenantCode string) ([]ExternalIDP, error) {
	rows, err := s.store.ForTenant(tenantID).ListExternalIdentityProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list identity providers: %w", err)
	}
	out := make([]ExternalIDP, 0, len(rows))
	for _, row := range rows {
		out = append(out, toExternalIDP(row, s.RedirectURI(tenantCode)))
	}
	return out, nil
}

// Create registers one, after proving it exists.
//
// The provider is contacted before the row is written. A configuration that
// cannot be discovered is one every sign-in through it will fail on, and the
// person able to fix it is the one filling in this form — not the user who
// meets the failure three days later at a login screen.
func (s *ExternalIDPService) Create(ctx context.Context, actor auth.Principal, tenantCode string, in ExternalIDPInput) (ExternalIDP, error) {
	in, err := s.normalize(in)
	if err != nil {
		return ExternalIDP{}, err
	}

	if err := s.verifyReachable(ctx, in, tenantCode); err != nil {
		return ExternalIDP{}, err
	}

	sealed := ""
	if in.ClientSecret != "" {
		if sealed, err = s.seal(actor.TenantID, in.ClientSecret); err != nil {
			return ExternalIDP{}, err
		}
	}

	id := uuid.NewString()
	now := store.Now()
	err = s.store.ForTenant(actor.TenantID).CreateExternalIdentityProvider(ctx,
		sqlcgen.CreateExternalIdentityProviderParams{
			ID: id, Name: in.Name, ButtonLabel: in.ButtonLabel,
			Issuer: in.Issuer, ClientID: in.ClientID, ClientSecret: sealed,
			Scopes: in.Scopes, TrustVerifiedEmail: in.TrustVerifiedEmail,
			CreatedAt: now,
		})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return ExternalIDP{}, ErrExternalIDPIssuerTaken
		}
		return ExternalIDP{}, fmt.Errorf("create identity provider: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionExternalIDPCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "EXTERNAL_IDP", TargetID: id, TargetName: in.Name,
		Detail: in.Issuer,
	})

	return s.Get(ctx, actor.TenantID, tenantCode, id)
}

// Get returns one.
func (s *ExternalIDPService) Get(ctx context.Context, tenantID, tenantCode, id string) (ExternalIDP, error) {
	row, err := s.store.ForTenant(tenantID).GetExternalIdentityProvider(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return ExternalIDP{}, ErrExternalIDPNotFound
		}
		return ExternalIDP{}, fmt.Errorf("get identity provider: %w", err)
	}
	return toExternalIDP(row, s.RedirectURI(tenantCode)), nil
}

// Update edits one. An empty secret keeps the stored one.
func (s *ExternalIDPService) Update(ctx context.Context, actor auth.Principal, tenantCode, id string, in ExternalIDPInput) (ExternalIDP, error) {
	in, err := s.normalize(in)
	if err != nil {
		return ExternalIDP{}, err
	}
	if _, err := s.Get(ctx, actor.TenantID, tenantCode, id); err != nil {
		return ExternalIDP{}, err
	}
	if err := s.verifyReachable(ctx, in, tenantCode); err != nil {
		return ExternalIDP{}, err
	}

	sealed := ""
	if in.ClientSecret != "" {
		if sealed, err = s.seal(actor.TenantID, in.ClientSecret); err != nil {
			return ExternalIDP{}, err
		}
	}

	now := store.Now()
	err = s.store.ForTenant(actor.TenantID).UpdateExternalIdentityProvider(ctx,
		sqlcgen.UpdateExternalIdentityProviderParams{
			ID: id, Name: in.Name, ButtonLabel: in.ButtonLabel,
			Issuer: in.Issuer, ClientID: in.ClientID, ClientSecret: sealed,
			Scopes: in.Scopes, TrustVerifiedEmail: in.TrustVerifiedEmail,
			Now: now,
		})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return ExternalIDP{}, ErrExternalIDPIssuerTaken
		}
		return ExternalIDP{}, fmt.Errorf("update identity provider: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionExternalIDPUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "EXTERNAL_IDP", TargetID: id, TargetName: in.Name,
		Detail: in.Issuer,
	})
	return s.Get(ctx, actor.TenantID, tenantCode, id)
}

// SetStatus enables or disables one.
//
// Disabling takes the button off the sign-in screen and leaves every binding
// in place, so switching it back on does not ask everybody to bind again. It
// is the control for "this provider is having an outage", which is the
// common case; deleting is for "we are not using them".
func (s *ExternalIDPService) SetStatus(ctx context.Context, actor auth.Principal, id string, status model.Status) error {
	provider, err := s.Get(ctx, actor.TenantID, "", id)
	if err != nil {
		return err
	}

	if err := s.store.ForTenant(actor.TenantID).SetExternalIdentityProviderStatus(
		ctx, id, string(status), store.Now()); err != nil {
		return fmt.Errorf("set identity provider status: %w", err)
	}

	action := model.ActionExternalIDPEnable
	if status == model.StatusDisabled {
		action = model.ActionExternalIDPDisable
	}
	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "EXTERNAL_IDP", TargetID: id, TargetName: provider.Name,
	})
	return nil
}

// BoundCount is how many accounts sign in through one.
//
// What a delete confirmation has to say out loud: removing a provider
// unbinds everybody who arrived through it, and an account whose only way in
// was that button is an account that has lost it.
func (s *ExternalIDPService) BoundCount(ctx context.Context, tenantID, id string) (int64, error) {
	return s.store.ForTenant(tenantID).CountExternalIdentitiesForProvider(ctx, id)
}

// Delete removes a provider and the bindings that named it.
func (s *ExternalIDPService) Delete(ctx context.Context, actor auth.Principal, id string) error {
	provider, err := s.Get(ctx, actor.TenantID, "", id)
	if err != nil {
		return err
	}
	bound, err := s.BoundCount(ctx, actor.TenantID, id)
	if err != nil {
		return fmt.Errorf("count bound identities: %w", err)
	}

	if err := s.store.ForTenant(actor.TenantID).DeleteExternalIdentityProvider(ctx, id); err != nil {
		return fmt.Errorf("delete identity provider: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionExternalIDPDelete,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "EXTERNAL_IDP", TargetID: id, TargetName: provider.Name,
		// The count is the part somebody looking at this entry later needs:
		// it says how many people lost a way in.
		Detail: fmt.Sprintf("%s, %d bound identities removed", provider.Issuer, bound),
	})
	return nil
}

// verifyReachable proves the configuration describes a real provider.
func (s *ExternalIDPService) verifyReachable(ctx context.Context, in ExternalIDPInput, tenantCode string) error {
	_, err := oidcrp.Discover(ctx, oidcrp.Config{
		Issuer: in.Issuer, ClientID: in.ClientID,
		RedirectURI: s.RedirectURI(tenantCode),
		Scopes:      strings.Fields(in.Scopes),
	})
	if err != nil {
		return httpx.UnprocessableEntity("EXTERNAL_IDP_UNREACHABLE",
			"That issuer could not be read as an OpenID Provider: "+err.Error())
	}
	return nil
}

// clientSecretBinding ties a stored client secret to the tenant it belongs
// to and to being a client secret, so a ciphertext moved into another row or
// another tenant no longer opens.
func clientSecretBinding(tenantID string) secrets.Binding {
	return secrets.Binding{Purpose: secrets.PurposeExternalIDPSecret, TenantID: tenantID}
}

func (s *ExternalIDPService) seal(tenantID, plaintext string) (string, error) {
	sealed, err := s.vault.Seal(clientSecretBinding(tenantID), plaintext)
	if errors.Is(err, secrets.ErrNotConfigured) {
		return "", ErrNoEncryptionKey
	}
	if err != nil {
		return "", fmt.Errorf("seal client secret: %w", err)
	}
	return sealed, nil
}

func (s *ExternalIDPService) normalize(in ExternalIDPInput) (ExternalIDPInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.ButtonLabel = strings.TrimSpace(in.ButtonLabel)
	in.Issuer = strings.TrimRight(strings.TrimSpace(in.Issuer), "/")
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.Scopes = strings.Join(strings.Fields(in.Scopes), " ")

	if in.Name == "" {
		return in, httpx.BadRequest("NAME_REQUIRED", "A name is required.")
	}
	if in.Issuer == "" {
		return in, httpx.BadRequest("EXTERNAL_IDP_ISSUER_REQUIRED",
			"An issuer URL is required.")
	}
	if in.ClientID == "" {
		return in, httpx.BadRequest("EXTERNAL_IDP_CLIENT_ID_REQUIRED",
			"A client id is required.")
	}
	if in.Scopes == "" {
		in.Scopes = "openid profile email"
	}
	if in.ButtonLabel == "" {
		in.ButtonLabel = in.Name
	}
	return in, nil
}

func toExternalIDP(row sqlcgen.ExternalIdentityProvider, redirectURI string) ExternalIDP {
	return ExternalIDP{
		ID: row.ID, Name: row.Name, ButtonLabel: row.ButtonLabel,
		Issuer: row.Issuer, ClientID: row.ClientID, Scopes: row.Scopes,
		TrustVerifiedEmail: row.TrustVerifiedEmail, Status: row.Status,
		HasSecret:   row.ClientSecret != "",
		RedirectURI: redirectURI,
	}
}
