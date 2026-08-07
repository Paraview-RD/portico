// Package oidcp adapts Portico to the OpenID Provider interface that
// zitadel/oidc drives.
//
// Everything here is glue: the protocol library owns the endpoints, the
// grant rules, and the wire formats, and this package answers the questions
// it asks about accounts, clients, keys, and tokens. Nothing in it decides
// policy — where it looked like it might, the decision lives in the service
// layer or the schema instead.
//
// One instance is bound to one tenant. That is the same discipline as
// store.Scoped, for the same reason: each tenant is its own issuer, and an
// adapter that could reach two of them would make cross-tenant token
// confusion a matter of getting a parameter right.
package oidcp

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// Storage implements op.Storage for one tenant.
type Storage struct {
	tenant  model.Tenant
	store   *store.Store
	users   *service.UserService
	clients *service.OAuthClientService
	keys    *service.SigningKeyService

	// loginURL builds where a browser is sent to sign in, given an
	// authorization request id. The provider hands this to the client, which
	// returns it from LoginURL.
	loginURL func(authRequestID string) string
}

// NewStorage binds an adapter to a tenant.
func NewStorage(
	tenant model.Tenant,
	st *store.Store,
	users *service.UserService,
	clients *service.OAuthClientService,
	keys *service.SigningKeyService,
	loginURL func(string) string,
) *Storage {
	return &Storage{
		tenant: tenant, store: st,
		users: users, clients: clients, keys: keys,
		loginURL: loginURL,
	}
}

func (s *Storage) scoped() *store.Scoped { return s.store.ForTenant(s.tenant.ID) }

// --- authorization requests ----------------------------------------------

// CreateAuthRequest records a request arriving at /authorize.
//
// PKCE is required here rather than left to the client's discretion. OAuth
// 2.1 mandates it for every client type, and a code issued without a
// challenge is one that anybody who intercepts it can redeem.
func (s *Storage) CreateAuthRequest(ctx context.Context, req *oidc.AuthRequest, _ string) (op.AuthRequest, error) {
	if req.CodeChallenge == "" {
		return nil, oidc.ErrInvalidRequest().WithDescription(
			"code_challenge is required; this server implements OAuth 2.1, which requires PKCE of every client")
	}
	if req.CodeChallengeMethod != oidc.CodeChallengeMethodS256 {
		// "plain" offers no protection against an attacker who can see the
		// authorization request, which is the attacker PKCE exists for.
		return nil, oidc.ErrInvalidRequest().WithDescription(
			"code_challenge_method must be S256")
	}

	client, err := s.clients.Get(ctx, s.tenant.ID, string(req.ClientID))
	if err != nil {
		return nil, oidc.ErrInvalidClient().WithDescription("unknown client")
	}
	if client.Status != model.StatusActive {
		return nil, oidc.ErrInvalidClient().WithDescription("this client is disabled")
	}
	if !slices.Contains(client.RedirectURIs, req.RedirectURI) {
		// Exact match, and the failure is deliberately not redirected —
		// sending an error to an unregistered URI would be the very thing
		// the check prevents.
		return nil, oidc.ErrInvalidRequest().WithDescription("redirect_uri is not registered for this client")
	}

	now := store.Now()
	id := uuid.NewString()

	err = s.scoped().CreateAuthRequest(ctx, sqlcgen.CreateAuthRequestParams{
		ID:                  id,
		ClientID:            string(req.ClientID),
		RedirectUri:         req.RedirectURI,
		ResponseType:        string(req.ResponseType),
		ResponseMode:        string(req.ResponseMode),
		Scopes:              req.Scopes,
		Audience:            []string{},
		State:               req.State,
		Nonce:               req.Nonce,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: string(req.CodeChallengeMethod),
		CreatedAt:           now,
		ExpiresAt:           now.Add(model.AuthRequestLifetime),
	})
	if err != nil {
		return nil, fmt.Errorf("record authorization request: %w", err)
	}

	return s.AuthRequestByID(ctx, id)
}

// AuthRequestByID returns a request in flight.
func (s *Storage) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	row, err := s.scoped().GetAuthRequest(ctx, id, store.Now())
	if err != nil {
		if store.IsNoRows(err) {
			return nil, oidc.ErrInvalidRequest().WithDescription("unknown or expired authorization request")
		}
		return nil, fmt.Errorf("get authorization request: %w", err)
	}
	return &authRequest{row: row}, nil
}

// AuthRequestByCode returns the request an authorization code names.
func (s *Storage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	row, err := s.scoped().GetAuthRequestByCode(ctx, hashToken(code), store.Now())
	if err != nil {
		if store.IsNoRows(err) {
			return nil, oidc.ErrInvalidGrant().WithDescription("invalid or expired authorization code")
		}
		return nil, fmt.Errorf("get authorization request by code: %w", err)
	}
	return &authRequest{row: row}, nil
}

// SaveAuthCode attaches an authorization code to a completed request.
func (s *Storage) SaveAuthCode(ctx context.Context, id, code string) error {
	return s.scoped().SaveAuthCode(ctx, id, hashToken(code))
}

// DeleteAuthRequest removes a request once its code has been spent.
func (s *Storage) DeleteAuthRequest(ctx context.Context, id string) error {
	return s.scoped().DeleteAuthRequest(ctx, id)
}

// CompleteAuthRequest records who signed in. Called by Portico's own login
// flow, not by the protocol library.
func (s *Storage) CompleteAuthRequest(ctx context.Context, id, subject string) error {
	return s.scoped().CompleteAuthRequest(ctx, id, subject, store.Now(), []string{"pwd"})
}

// --- tokens ---------------------------------------------------------------

// CreateAccessToken issues an access token without a refresh token.
//
// Nothing is stored: an access token is a signed JWT that a resource server
// verifies offline, so there is no row anybody would ever read. What bounds
// its usefulness after a permission is withdrawn is its lifetime, which is
// why that lifetime is short.
func (s *Storage) CreateAccessToken(_ context.Context, _ op.TokenRequest) (string, time.Time, error) {
	return uuid.NewString(), store.Now().Add(model.AccessTokenLifetime), nil
}

// CreateAccessAndRefreshTokens issues both, rotating the refresh token when
// one was presented.
func (s *Storage) CreateAccessAndRefreshTokens(ctx context.Context, request op.TokenRequest, currentRefreshToken string) (string, string, time.Time, error) {
	accessTokenID := uuid.NewString()
	expiry := store.Now().Add(model.AccessTokenLifetime)

	refresh, err := s.issueRefreshToken(ctx, request, currentRefreshToken)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return accessTokenID, refresh, expiry, nil
}

func (s *Storage) issueRefreshToken(ctx context.Context, request op.TokenRequest, currentRefreshToken string) (string, error) {
	q := s.scoped()
	now := store.Now()

	var (
		clientID string
		audience []string
		amr      []string
		authTime time.Time
	)
	switch req := request.(type) {
	case *authRequest:
		clientID = req.GetClientID()
		audience = req.GetAudience()
		amr = req.GetAMR()
		authTime = req.GetAuthTime()
	case *refreshRequest:
		clientID = req.clientID
		audience = req.GetAudience()
		amr = req.GetAMR()
		authTime = req.GetAuthTime()
	default:
		return "", fmt.Errorf("oidcp: cannot issue a refresh token for %T", request)
	}

	token, err := newOpaqueToken()
	if err != nil {
		return "", err
	}
	id := uuid.NewString()

	err = q.CreateRefreshToken(ctx, sqlcgen.CreateRefreshTokenParams{
		ID:        id,
		ClientID:  clientID,
		Subject:   request.GetSubject(),
		TokenHash: hashToken(token),
		Scopes:    request.GetScopes(),
		Audience:  nonNil(audience),
		Amr:       nonNil(amr),
		AuthTime:  authTime,
		CreatedAt: now,
		ExpiresAt: now.Add(model.RefreshTokenLifetime),
	})
	if err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}

	// Rotation: the token just used is spent and points at its replacement,
	// so a later presentation of it can be recognized as a leak and take the
	// whole chain down.
	if currentRefreshToken != "" {
		previous, err := q.GetRefreshToken(ctx, hashToken(currentRefreshToken))
		if err == nil {
			if err := q.SpendRefreshToken(ctx, previous.ID, id, now); err != nil {
				return "", fmt.Errorf("spend refresh token: %w", err)
			}
		}
	}

	return token, nil
}

// TokenRequestByRefreshToken validates a presented refresh token.
//
// A token that has already been spent is the interesting case: it means a
// copy leaked, because the legitimate holder would have the replacement. The
// response is to revoke the entire chain rather than fail this one call,
// since which link leaked is unknowable.
func (s *Storage) TokenRequestByRefreshToken(ctx context.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	q := s.scoped()
	now := store.Now()

	row, err := q.GetRefreshToken(ctx, hashToken(refreshToken))
	if err != nil {
		if store.IsNoRows(err) {
			return nil, op.ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("get refresh token: %w", err)
	}

	if row.UsedAt != nil {
		if err := q.RevokeRefreshTokenChain(ctx, row.ID, now); err != nil {
			return nil, fmt.Errorf("revoke reused refresh token chain: %w", err)
		}
		return nil, op.ErrInvalidRefreshToken
	}
	if row.RevokedAt != nil || row.ExpiresAt.Before(now) {
		return nil, op.ErrInvalidRefreshToken
	}

	// The account must still be usable. Without this a refresh would keep
	// working after the person was disabled, which is exactly the gap
	// federation is accused of opening.
	account, err := q.GetUserByID(ctx, row.Subject)
	if err != nil || model.Status(account.Status) != model.StatusActive {
		return nil, op.ErrInvalidRefreshToken
	}

	return &refreshRequest{row: row, clientID: row.ClientID, token: refreshToken}, nil
}

// TerminateSession ends a person's session with one relying party.
func (s *Storage) TerminateSession(ctx context.Context, userID, clientID string) error {
	return s.scoped().RevokeRefreshTokensForSession(ctx, userID, clientID, store.Now())
}

// RevokeToken revokes a refresh token. Access tokens are not stored and
// cannot be revoked; the endpoint answers successfully anyway, which is what
// RFC 7009 requires.
func (s *Storage) RevokeToken(ctx context.Context, tokenOrTokenID, _, clientID string) *oidc.Error {
	q := s.scoped()

	row, err := q.GetRefreshToken(ctx, hashToken(tokenOrTokenID))
	if err != nil {
		// Either an access token id or something that was never a token.
		// Revocation is idempotent and must not report which.
		return nil
	}
	if row.ClientID != clientID {
		return oidc.ErrInvalidClient().WithDescription("token was not issued to this client")
	}
	if err := q.RevokeRefreshToken(ctx, row.ID, store.Now()); err != nil {
		return oidc.ErrServerError().WithDescription("could not revoke the token")
	}
	return nil
}

// GetRefreshTokenInfo identifies a refresh token for the revocation endpoint.
func (s *Storage) GetRefreshTokenInfo(ctx context.Context, clientID, token string) (userID, tokenID string, err error) {
	row, err := s.scoped().GetRefreshToken(ctx, hashToken(token))
	if err != nil {
		return "", "", op.ErrInvalidRefreshToken
	}
	if row.ClientID != clientID {
		return "", "", op.ErrInvalidRefreshToken
	}
	return row.Subject, row.ID, nil
}

// --- keys -----------------------------------------------------------------

// SigningKey returns the key new tokens are signed with.
func (s *Storage) SigningKey(ctx context.Context) (op.SigningKey, error) {
	key, err := s.keys.Active(ctx, s.tenant.ID)
	if err != nil {
		return nil, err
	}
	return signingKey{key}, nil
}

// SignatureAlgorithms reports what this provider signs with.
func (s *Storage) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}

// KeySet is what the JWKS endpoint publishes: the active key and any retired
// key whose tokens may still be in flight.
func (s *Storage) KeySet(ctx context.Context) ([]op.Key, error) {
	keys, err := s.keys.Published(ctx, s.tenant.ID)
	if err != nil {
		return nil, err
	}

	published := make([]op.Key, 0, len(keys))
	for _, key := range keys {
		published = append(published, publicKey{key})
	}
	return published, nil
}

// --- clients and claims ---------------------------------------------------

// GetClientByClientID returns a relying party.
func (s *Storage) GetClientByClientID(ctx context.Context, clientID string) (op.Client, error) {
	found, err := s.clients.Get(ctx, s.tenant.ID, clientID)
	if err != nil {
		return nil, oidc.ErrInvalidClient().WithDescription("unknown client")
	}
	if found.Status != model.StatusActive {
		return nil, oidc.ErrInvalidClient().WithDescription("this client is disabled")
	}
	return &client{model: found, loginURL: s.loginURL}, nil
}

// AuthorizeClientIDSecret authenticates a confidential client.
func (s *Storage) AuthorizeClientIDSecret(ctx context.Context, clientID, clientSecret string) error {
	if err := s.clients.VerifySecret(ctx, s.tenant.ID, clientID, clientSecret); err != nil {
		return oidc.ErrInvalidClient().WithDescription("client authentication failed")
	}
	return nil
}

// SetUserinfoFromScopes is deprecated in the interface and must stay empty.
func (s *Storage) SetUserinfoFromScopes(context.Context, *oidc.UserInfo, string, string, []string) error {
	return nil
}

// SetUserinfoFromRequest fills claims for a token being issued.
func (s *Storage) SetUserinfoFromRequest(ctx context.Context, userinfo *oidc.UserInfo, request op.IDTokenRequest, scopes []string) error {
	return s.setUserinfo(ctx, userinfo, request.GetSubject(), scopes)
}

// SetUserinfoFromToken fills claims for the userinfo endpoint.
func (s *Storage) SetUserinfoFromToken(ctx context.Context, userinfo *oidc.UserInfo, _, subject, _ string) error {
	// The scopes are not available here, so everything the account has is
	// returned. That is what userinfo is for, and the access token presented
	// to reach it was issued for this subject.
	return s.setUserinfo(ctx, userinfo, subject, []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail, oidc.ScopePhone})
}

// SetIntrospectionFromToken answers the introspection endpoint.
func (s *Storage) SetIntrospectionFromToken(ctx context.Context, response *oidc.IntrospectionResponse, _, subject, _ string) error {
	account, err := s.scoped().GetUserByID(ctx, subject)
	if err != nil {
		return err
	}
	// A disabled account's tokens report inactive, which is how a resource
	// server that does introspect finds out promptly.
	if model.Status(account.Status) != model.StatusActive {
		response.Active = false
		return nil
	}
	response.Active = true
	response.Subject = subject

	// IntrospectionResponse embeds the userinfo claim groups rather than a
	// UserInfo, so it is filled through one and copied across.
	var userinfo oidc.UserInfo
	err = s.setUserinfo(ctx, &userinfo, subject,
		[]string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail, oidc.ScopePhone})
	if err != nil {
		return err
	}
	response.SetUserInfo(&userinfo)
	return nil
}

// GetPrivateClaimsFromScopes adds the claims that are Portico's own rather
// than OpenID's: the tenant, the role, and the organization, which is what
// §3.8.2 asks the token to carry.
func (s *Storage) GetPrivateClaimsFromScopes(ctx context.Context, userID, _ string, _ []string) (map[string]any, error) {
	user, err := s.users.Get(ctx, s.tenant.ID, userID)
	if err != nil {
		return nil, err
	}

	claims := map[string]any{
		"tenant_id":   s.tenant.ID,
		"tenant_code": s.tenant.Code,
		"role":        string(user.Role),
	}
	if user.OrganizationID != "" {
		claims["organization_id"] = user.OrganizationID
		claims["organization_name"] = user.OrganizationName
	}
	return claims, nil
}

// GetKeyByIDAndClientID serves private_key_jwt client authentication, which
// this version does not offer.
func (s *Storage) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, errors.New("oidcp: private_key_jwt client authentication is not supported")
}

// ValidateJWTProfileScopes serves the JWT profile grant, which this version
// does not offer.
func (s *Storage) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, errors.New("oidcp: the JWT profile grant is not supported")
}

// Health reports whether the storage is usable, for the provider's probes.
func (s *Storage) Health(ctx context.Context) error {
	return s.store.DB().PingContext(ctx)
}

func (s *Storage) setUserinfo(ctx context.Context, userinfo *oidc.UserInfo, subject string, scopes []string) error {
	user, err := s.users.Get(ctx, s.tenant.ID, subject)
	if err != nil {
		return err
	}

	userinfo.Subject = user.ID
	for _, scope := range scopes {
		switch scope {
		case oidc.ScopeProfile:
			userinfo.Name = user.DisplayName
			userinfo.PreferredUsername = user.Username
			userinfo.UpdatedAt = oidc.FromTime(user.UpdatedAt)
		case oidc.ScopeEmail:
			if user.Email != "" {
				userinfo.Email = user.Email
				// Not verified: this version never asks anyone to prove an
				// address, so claiming otherwise would be a lie a relying
				// party might act on.
				userinfo.EmailVerified = false
			}
		case oidc.ScopePhone:
			if user.Phone != "" {
				userinfo.PhoneNumber = user.Phone
				userinfo.PhoneNumberVerified = false
			}
		}
	}
	return nil
}

// hashToken is the SHA-256 of a bearer credential, which is what gets
// stored. The value is high-entropy and random, so there is nothing for a
// slow hash to protect against.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := cryptoRead(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// signingKey adapts a service key to what the provider signs with.
type signingKey struct{ key service.SigningKey }

func (k signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k signingKey) Key() any                                    { return k.key.Private }
func (k signingKey) ID() string                                  { return k.key.ID }

// publicKey adapts a service key to what the JWKS publishes.
type publicKey struct{ key service.SigningKey }

func (k publicKey) ID() string                         { return k.key.ID }
func (k publicKey) Algorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k publicKey) Use() string                        { return "sig" }
func (k publicKey) Key() any                           { return publicHalf(k.key.Private) }

func publicHalf(private *rsa.PrivateKey) *rsa.PublicKey {
	if private == nil {
		return nil
	}
	return &private.PublicKey
}
