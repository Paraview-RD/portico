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
// A registration decides who may ask this server for tokens about a tenant's
// users, so it is an administrative act of real weight — but it is one a
// tenant administrator is already trusted with. They can reset any password
// in their own tenant, which is strictly more power than registering an
// application, and registration is tenant-scoped, so it grants nothing
// across the boundary. It is therefore available over the API to an
// administrator, as well as from the command line, and every mutation is
// audited.
//
// Dynamic client registration (RFC 7591) is a different question and remains
// deliberately absent: that is registration by an anonymous caller, with no
// administrator in the loop at all.
type OAuthClientService struct {
	store *store.Store
	audit *AuditService
}

// NewOAuthClientService wires an OAuthClientService.
func NewOAuthClientService(st *store.Store, audit *AuditService) *OAuthClientService {
	return &OAuthClientService{store: st, audit: audit}
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
	// LaunchURL is optional: an application without one still signs people
	// in, it just does not appear in the portal as something to open.
	LaunchURL string
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

// Register adds a relying party to the actor's tenant.
func (s *OAuthClientService) Register(ctx context.Context, actor auth.Principal, in RegisterClientInput) (RegisteredClient, error) {
	tenantID := actor.TenantID
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

	launchURL, err := normalizeLaunchURL(in.LaunchURL)
	if err != nil {
		return RegisteredClient{}, err
	}

	err = s.store.ForTenant(tenantID).CreateOAuthClient(ctx, sqlcgen.CreateOAuthClientParams{
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
		LaunchUrl:              launchURL,
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

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionClientCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetOAuthClient, TargetID: in.ClientID, TargetName: in.Name,
		Detail: clientDetail(client),
	})

	return RegisteredClient{Client: client, Secret: secret}, nil
}

// UpdateClientInput is the editable part of a registration.
//
// The client id is absent because it is not editable: it is the name the
// application presents at the token endpoint, and changing it would break
// every deployment of that application rather than reconfigure it. Whether
// the client is confidential is absent for the same reason — flipping a
// public client to confidential would leave it unable to authenticate until
// somebody noticed, so that is a re-registration.
type UpdateClientInput struct {
	Name                   string
	ApplicationType        string
	RedirectURIs           []string
	PostLogoutRedirectURIs []string
	Scopes                 []string
	LaunchURL              string
}

// Update changes a relying party's settings.
func (s *OAuthClientService) Update(ctx context.Context, actor auth.Principal, clientID string, in UpdateClientInput) (model.OAuthClient, error) {
	tenantID := actor.TenantID

	current, err := s.Get(ctx, tenantID, clientID)
	if err != nil {
		return model.OAuthClient{}, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = current.ClientID
	}
	if err := validateRedirectURIs(in.RedirectURIs); err != nil {
		return model.OAuthClient{}, err
	}
	if len(in.PostLogoutRedirectURIs) > 0 {
		if err := validateRedirectURIs(in.PostLogoutRedirectURIs); err != nil {
			return model.OAuthClient{}, err
		}
	}

	applicationType := strings.ToUpper(strings.TrimSpace(in.ApplicationType))
	if applicationType == "" {
		applicationType = current.ApplicationType
	}
	switch applicationType {
	case model.AppTypeWeb, model.AppTypeNative, model.AppTypeUserAgent:
	default:
		return model.OAuthClient{}, httpx.BadRequest("INVALID_APPLICATION_TYPE",
			"Application type must be WEB, NATIVE, or USER_AGENT.")
	}

	scopes := in.Scopes
	if len(scopes) == 0 {
		scopes = current.Scopes
	}
	if !slices.Contains(scopes, "openid") {
		scopes = append([]string{"openid"}, scopes...)
	}

	launchURL, err := normalizeLaunchURL(in.LaunchURL)
	if err != nil {
		return model.OAuthClient{}, err
	}

	err = s.store.ForTenant(tenantID).UpdateOAuthClient(ctx, sqlcgen.UpdateOAuthClientParams{
		ClientID:               clientID,
		Name:                   name,
		ApplicationType:        applicationType,
		RedirectUris:           in.RedirectURIs,
		PostLogoutRedirectUris: emptyIfNil(in.PostLogoutRedirectURIs),
		Scopes:                 scopes,
		LaunchUrl:              launchURL,
		UpdatedAt:              store.Now(),
	})
	if err != nil {
		return model.OAuthClient{}, fmt.Errorf("update client: %w", err)
	}

	updated, err := s.Get(ctx, tenantID, clientID)
	if err != nil {
		return model.OAuthClient{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionClientUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetOAuthClient, TargetID: clientID, TargetName: updated.Name,
		Detail: clientDetail(updated),
	})

	return updated, nil
}

// RotateSecret issues a confidential client a new secret and invalidates the
// old one immediately.
//
// There is no overlap period in which both work. That would be kinder to a
// running deployment, but the reason to rotate is usually that the old
// secret leaked, and a rotation that leaves the leaked value working is not
// a rotation. An operator who is rotating on a schedule instead can register
// a second client and retire the first.
func (s *OAuthClientService) RotateSecret(ctx context.Context, actor auth.Principal, clientID string) (RegisteredClient, error) {
	tenantID := actor.TenantID

	client, err := s.Get(ctx, tenantID, clientID)
	if err != nil {
		return RegisteredClient{}, err
	}
	if !client.Confidential {
		// A public client authenticates with PKCE and has no secret to
		// rotate. Generating one here would make it confidential by
		// accident, and it would then fail to authenticate.
		return RegisteredClient{}, httpx.BadRequest("CLIENT_IS_PUBLIC",
			"This is a public client. It authenticates with PKCE and has no secret.")
	}

	secret, err := newClientSecret()
	if err != nil {
		return RegisteredClient{}, err
	}
	hashed, err := auth.HashPassword(secret)
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("hash client secret: %w", err)
	}

	err = s.store.ForTenant(tenantID).UpdateOAuthClientSecret(ctx, clientID, &hashed, store.Now())
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("rotate client secret: %w", err)
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionClientSecretRotate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetOAuthClient, TargetID: clientID, TargetName: client.Name,
	})

	updated, err := s.Get(ctx, tenantID, clientID)
	if err != nil {
		return RegisteredClient{}, err
	}
	return RegisteredClient{Client: updated, Secret: secret}, nil
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
func (s *OAuthClientService) SetStatus(ctx context.Context, actor auth.Principal, clientID string, status model.Status) (model.OAuthClient, error) {
	tenantID := actor.TenantID

	if !status.Valid() {
		return model.OAuthClient{}, httpx.BadRequest("INVALID_STATUS", "Status must be ACTIVE or DISABLED.")
	}
	current, err := s.Get(ctx, tenantID, clientID)
	if err != nil {
		return model.OAuthClient{}, err
	}

	err = s.store.ForTenant(tenantID).UpdateOAuthClientStatus(ctx, clientID, string(status), store.Now())
	if err != nil {
		return model.OAuthClient{}, fmt.Errorf("update client status: %w", err)
	}

	action := model.ActionClientEnable
	if status == model.StatusDisabled {
		action = model.ActionClientDisable
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetOAuthClient, TargetID: clientID, TargetName: current.Name,
	})

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

	// Checked after the comparison rather than before it, so that a disabled
	// client and a wrong secret take the same time and neither answer says
	// which one it was.
	//
	// This check has to be here and not only where a client is looked up for
	// an authorization request. Client authentication is a separate path —
	// it is what the introspection and revocation endpoints use, and they
	// never fetch the client any other way. Without this, an operator who
	// disabled a compromised client would have stopped it signing anybody
	// in while leaving it able to introspect tokens, which is not what the
	// word "disabled" promises.
	if model.Status(row.Status) != model.StatusActive {
		return httpx.Unauthorized("CLIENT_DISABLED", "This client is disabled.")
	}
	return nil
}

// clientDetail summarizes what a registration permits, for the audit trail.
//
// The redirect URIs are the whole of the security question — an authorization
// code is delivered to one of them — so an entry that omitted them would
// record that something changed without recording the thing that matters.
func clientDetail(c model.OAuthClient) string {
	kind := "confidential"
	if !c.Confidential {
		kind = "public"
	}
	return fmt.Sprintf("%s %s client; redirect URIs: %s; scopes: %s",
		kind, strings.ToLower(c.ApplicationType),
		strings.Join(c.RedirectURIs, ", "),
		strings.Join(c.Scopes, " "))
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
		LaunchURL:              row.LaunchUrl,
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
