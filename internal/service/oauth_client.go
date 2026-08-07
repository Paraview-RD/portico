package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// Errors from client registration and lookup.
var (
	ErrClientNotFound = httpx.NotFound("CLIENT_NOT_FOUND",
		"No such client.")
	ErrClientIDTaken = httpx.Conflict("CLIENT_ID_TAKEN",
		"That client id is already registered in this tenant.")
)

// OAuthClientService owns the registered relying parties.
//
// Like tenants, clients are registered from the command line. A client
// registration decides who may ask this server for tokens about its users,
// which is an administrative act of the same weight as creating a tenant —
// and V0.1 has no role that could be authorized to perform it over HTTP.
// Dynamic client registration is deliberately not implemented.
type OAuthClientService struct {
	store *store.Store
}

// NewOAuthClientService wires an OAuthClientService.
func NewOAuthClientService(st *store.Store) *OAuthClientService {
	return &OAuthClientService{store: st}
}

// RegisterClientInput describes a relying party to register.
type RegisterClientInput struct {
	ClientID string
	Name     string
	// Public marks a client that cannot keep a secret — a browser or mobile
	// application. It gets no secret and authenticates with PKCE alone.
	Public                 bool
	ApplicationType        string
	RedirectURIs           []string
	PostLogoutRedirectURIs []string
	Scopes                 []string
}

// RegisteredClient is a client together with the secret generated for it,
// which is available exactly once.
type RegisteredClient struct {
	Client model.OAuthClient
	// Secret is empty for a public client, and for a confidential one is the
	// only time the value exists outside the caller's terminal: what is
	// stored is a hash.
	Secret string
}

// Register adds a relying party to a tenant.
func (s *OAuthClientService) Register(ctx context.Context, tenantID string, in RegisterClientInput) (RegisteredClient, error) {
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.Name = strings.TrimSpace(in.Name)

	if err := validateClientID(in.ClientID); err != nil {
		return RegisteredClient{}, err
	}
	if in.Name == "" {
		in.Name = in.ClientID
	}
	if err := validateRedirectURIs(in.RedirectURIs); err != nil {
		return RegisteredClient{}, err
	}
	if len(in.PostLogoutRedirectURIs) > 0 {
		if err := validateRedirectURIs(in.PostLogoutRedirectURIs); err != nil {
			return RegisteredClient{}, err
		}
	}

	applicationType := strings.ToUpper(strings.TrimSpace(in.ApplicationType))
	if applicationType == "" {
		applicationType = model.AppTypeWeb
	}
	switch applicationType {
	case model.AppTypeWeb, model.AppTypeNative, model.AppTypeUserAgent:
	default:
		return RegisteredClient{}, httpx.BadRequest("INVALID_APPLICATION_TYPE",
			"Application type must be WEB, NATIVE, or USER_AGENT.")
	}

	scopes := in.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	if !slices.Contains(scopes, "openid") {
		// Without it this is a plain OAuth client and no ID token is issued.
		// Portico is an OpenID Provider; a client that cannot ask for
		// identity is a client that has nothing to ask for.
		scopes = append([]string{"openid"}, scopes...)
	}

	authMethod := model.AuthMethodBasic
	var secret string
	var secretHash *string
	if in.Public {
		authMethod = model.AuthMethodNone
	} else {
		generated, err := newClientSecret()
		if err != nil {
			return RegisteredClient{}, err
		}
		hashed, err := auth.HashPassword(generated)
		if err != nil {
			return RegisteredClient{}, fmt.Errorf("hash client secret: %w", err)
		}
		secret = generated
		secretHash = &hashed
	}

	now := store.Now()
	id := uuid.NewString()

	// Refresh tokens are always offered. A relying party that does not ask
	// for offline_access simply never receives one, so declaring the grant
	// costs nothing and leaving it out would make the common case need a
	// re-registration.
	grantTypes := []string{"authorization_code", "refresh_token"}

	err := s.store.ForTenant(tenantID).CreateOAuthClient(ctx, sqlcgen.CreateOAuthClientParams{
		ID:                     id,
		ClientID:               in.ClientID,
		Name:                   in.Name,
		SecretHash:             secretHash,
		ApplicationType:        applicationType,
		AuthMethod:             authMethod,
		RedirectUris:           in.RedirectURIs,
		PostLogoutRedirectUris: emptyIfNil(in.PostLogoutRedirectURIs),
		GrantTypes:             grantTypes,
		Scopes:                 scopes,
		Status:                 string(model.StatusActive),
		CreatedAt:              now,
		UpdatedAt:              now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return RegisteredClient{}, ErrClientIDTaken
		}
		return RegisteredClient{}, fmt.Errorf("register client: %w", err)
	}

	client, err := s.Get(ctx, tenantID, in.ClientID)
	if err != nil {
		return RegisteredClient{}, err
	}
	return RegisteredClient{Client: client, Secret: secret}, nil
}

// Get returns one relying party.
func (s *OAuthClientService) Get(ctx context.Context, tenantID, clientID string) (model.OAuthClient, error) {
	row, err := s.store.ForTenant(tenantID).GetOAuthClient(ctx, clientID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.OAuthClient{}, ErrClientNotFound
		}
		return model.OAuthClient{}, fmt.Errorf("get client: %w", err)
	}
	return toOAuthClient(row), nil
}

// List returns every relying party in a tenant.
func (s *OAuthClientService) List(ctx context.Context, tenantID string) ([]model.OAuthClient, error) {
	rows, err := s.store.ForTenant(tenantID).ListOAuthClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}

	clients := make([]model.OAuthClient, 0, len(rows))
	for _, row := range rows {
		clients = append(clients, toOAuthClient(row))
	}
	return clients, nil
}

// SetStatus enables or disables a relying party. A disabled client's
// authorization requests are refused and its tokens stop refreshing, without
// anything being deleted.
func (s *OAuthClientService) SetStatus(ctx context.Context, tenantID, clientID string, status model.Status) (model.OAuthClient, error) {
	if !status.Valid() {
		return model.OAuthClient{}, httpx.BadRequest("INVALID_STATUS", "Status must be ACTIVE or DISABLED.")
	}
	if _, err := s.Get(ctx, tenantID, clientID); err != nil {
		return model.OAuthClient{}, err
	}

	err := s.store.ForTenant(tenantID).UpdateOAuthClientStatus(ctx, clientID, string(status), store.Now())
	if err != nil {
		return model.OAuthClient{}, fmt.Errorf("update client status: %w", err)
	}
	return s.Get(ctx, tenantID, clientID)
}

// VerifySecret checks a confidential client's credentials.
func (s *OAuthClientService) VerifySecret(ctx context.Context, tenantID, clientID, secret string) error {
	row, err := s.store.ForTenant(tenantID).GetOAuthClient(ctx, clientID)
	if err != nil {
		if store.IsNoRows(err) {
			// Spend the same time as a real comparison, so response timing
			// does not say which client ids are registered.
			auth.BurnPasswordComparison()
			return ErrClientNotFound
		}
		return fmt.Errorf("get client: %w", err)
	}
	if row.SecretHash == nil {
		return httpx.Unauthorized("INVALID_CLIENT", "This client has no secret.")
	}
	if !auth.CheckPassword(*row.SecretHash, secret) {
		return httpx.Unauthorized("INVALID_CLIENT", "Client authentication failed.")
	}
	return nil
}

// emptyIfNil turns a nil slice into an empty one.
//
// The driver encodes a nil slice as SQL NULL, not as an empty array, so a
// column declared NOT NULL DEFAULT '{}' rejects it — and the failure is a
// constraint violation naming a column the caller never mentioned, which
// reads as a schema bug rather than an unset optional field.
func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func toOAuthClient(row sqlcgen.OauthClient) model.OAuthClient {
	return model.OAuthClient{
		ID:                     row.ID,
		TenantID:               row.TenantID,
		ClientID:               row.ClientID,
		Name:                   row.Name,
		Confidential:           row.SecretHash != nil,
		ApplicationType:        row.ApplicationType,
		AuthMethod:             row.AuthMethod,
		RedirectURIs:           row.RedirectUris,
		PostLogoutRedirectURIs: row.PostLogoutRedirectUris,
		GrantTypes:             row.GrantTypes,
		Scopes:                 row.Scopes,
		Status:                 model.Status(row.Status),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
}

// newClientSecret returns 32 random bytes, base64url encoded.
func newClientSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate client secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validateClientID(clientID string) error {
	if clientID == "" {
		return httpx.BadRequest("CLIENT_ID_REQUIRED", "A client id is required.")
	}
	if len(clientID) < 3 || len(clientID) > 128 {
		return httpx.BadRequest("INVALID_CLIENT_ID",
			"Client id must be between 3 and 128 characters.")
	}
	for _, r := range clientID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return httpx.BadRequest("INVALID_CLIENT_ID",
				"Client id may contain only letters, digits, and the characters . _ -")
		}
	}
	return nil
}

// validateRedirectURIs rejects anything that cannot be matched exactly and
// safely.
//
// The checks are strict on purpose. A redirect URI is where an
// authorization code is delivered, so every relaxation here is a way for one
// to be delivered somewhere else: a fragment can be rewritten by the page
// that receives it, and a wildcard turns exact matching — the only defence
// this flow has — into a pattern match.
func validateRedirectURIs(uris []string) error {
	if len(uris) == 0 {
		return httpx.BadRequest("REDIRECT_URI_REQUIRED",
			"At least one redirect URI is required.")
	}

	for _, raw := range uris {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || !parsed.IsAbs() {
			return httpx.BadRequest("INVALID_REDIRECT_URI",
				fmt.Sprintf("%q is not an absolute URI.", raw))
		}
		if parsed.Fragment != "" || strings.Contains(raw, "#") {
			return httpx.BadRequest("INVALID_REDIRECT_URI",
				fmt.Sprintf("%q has a fragment, which a redirect URI may not.", raw))
		}
		if strings.Contains(raw, "*") {
			return httpx.BadRequest("INVALID_REDIRECT_URI",
				fmt.Sprintf("%q contains a wildcard. Redirect URIs are matched exactly; "+
					"register each one.", raw))
		}
		// http is allowed only for loopback, which is how a native
		// application receives a code and the one case where there is no
		// network to intercept.
		if parsed.Scheme == "http" && !isLoopback(parsed.Hostname()) {
			return httpx.BadRequest("INVALID_REDIRECT_URI",
				fmt.Sprintf("%q uses http. Use https, or http on localhost for a native app.", raw))
		}
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}
