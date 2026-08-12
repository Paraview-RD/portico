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
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Storage implements op.Storage for one tenant.
type Storage struct {
	tenant model.Tenant
	// issuer is this adapter's own issuer identifier. It is recorded on
	// every authorization request, because the default tenant is reachable
	// at two mounts and only the request knows which one it arrived at.
	issuer  string
	store   *store.Store
	users   *service.UserService
	clients *service.OAuthClientService
	keys    *service.SigningKeyService
	// settings supplies the token lifetimes, which are per-tenant and
	// changeable while the server runs. Read on each request that needs one
	// rather than captured here: a Storage lives as long as its provider,
	// which is cached for the process's lifetime, so a value read once would
	// mean a settings change appearing to do nothing until a restart. Reads
	// are served from the service's own cache, so this is not a query per
	// token.
	settings *service.SettingsService

	// catalogue and mappings are what a client receives, and under what name.
	// Both may be nil, which means every client receives the documented
	// defaults — the state every deployment is in until somebody configures
	// something, and the one a test that only cares about sign-in wants.
	catalogue *service.FieldCatalogue
	mappings  *service.FieldMappingService

	// loginURL builds where a browser is sent to sign in, given an
	// authorization request id. The provider hands this to the client, which
	// returns it from LoginURL.
	loginURL func(authRequestID string) string
}

// NewStorage binds an adapter to a tenant.
func NewStorage(
	tenant model.Tenant,
	issuer string,
	st *store.Store,
	users *service.UserService,
	clients *service.OAuthClientService,
	keys *service.SigningKeyService,
	settings *service.SettingsService,
	catalogue *service.FieldCatalogue,
	mappings *service.FieldMappingService,
	loginURL func(string) string,
) *Storage {
	return &Storage{
		tenant: tenant, issuer: issuer, store: st,
		users: users, clients: clients, keys: keys,
		settings:  settings,
		catalogue: catalogue, mappings: mappings,
		loginURL: loginURL,
	}
}

func (s *Storage) scoped() *store.Scoped { return s.store.ForTenant(s.tenant.ID) }

// lifetimes reads this tenant's current token lifetimes.
//
// Falling back to the defaults rather than failing the request, because the
// alternative is worse than a slightly wrong expiry: a settings row that
// cannot be read would otherwise take sign-in down for the whole tenant. The
// defaults are the values these settings shipped as, so a deployment that has
// never changed them cannot tell the difference.
func (s *Storage) lifetimes(ctx context.Context) service.Settings {
	settings, err := s.settings.Get(ctx, s.tenant.ID)
	if err != nil {
		return s.settings.Defaults()
	}
	return settings
}

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
		Issuer:              s.issuer,
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
func (s *Storage) CreateAccessToken(ctx context.Context, request op.TokenRequest) (string, time.Time, error) {
	return newAccessTokenID(clientOf(request)),
		store.Now().Add(s.lifetimes(ctx).OIDCAccessTokenLifetime()), nil
}

// newAccessTokenID names the client the token was issued to, in the id.
//
// This exists for one reason: the userinfo endpoint has to know which client
// is asking, and the interface it is called through does not say. Access
// tokens are JWTs and nothing about them is stored, so there is no row to look
// the client up in — the id is the only thing that reaches
// SetUserinfoFromToken, so the answer has to travel in it.
//
// Nothing is disclosed by this. The token is a JWT that already carries the
// client in `aud` and `client_id`, and only that client and the resource
// servers it presents the token to ever see either.
//
// Nothing else consumes this id, which is what makes it available to use. It
// is not a revocation key: access tokens are not stored and cannot be revoked,
// and RevokeToken is passed the client separately. If either of those changes,
// this is the code that has to change with it.
//
// The separator cannot appear in a client id — validateClientID allows only
// letters, digits, and `. _ -` — so splitting on the first one is exact rather
// than a guess.
func newAccessTokenID(clientID string) string {
	return clientID + accessTokenIDSeparator + uuid.NewString()
}

const accessTokenIDSeparator = "|"

// clientOf reads the client id out of whichever request shape arrived.
//
// op.TokenRequest is the narrow interface and does not carry one, but every
// concrete request that reaches these two methods does: an authorization
// request, a refresh request, and a device authorization all declare
// GetClientID. Asserting on the method rather than on the three types means a
// fourth shape added by the library is handled the same way.
//
// An empty answer is safe rather than wrong: the token is then issued with an
// id carrying no client, and the userinfo endpoint falls back to the
// documented defaults for it.
func clientOf(request op.TokenRequest) string {
	if named, ok := request.(interface{ GetClientID() string }); ok {
		return named.GetClientID()
	}
	return ""
}

// clientFromAccessTokenID reads the client back out, and returns empty for an
// id that does not carry one.
//
// An id from before this existed has no separator, and an empty answer sends
// the documented defaults — which is what such a token got when it was issued.
func clientFromAccessTokenID(tokenID string) string {
	clientID, _, found := strings.Cut(tokenID, accessTokenIDSeparator)
	if !found {
		return ""
	}
	return clientID
}

// CreateAccessAndRefreshTokens issues both, rotating the refresh token when
// one was presented.
func (s *Storage) CreateAccessAndRefreshTokens(ctx context.Context, request op.TokenRequest, currentRefreshToken string) (string, string, time.Time, error) {
	accessTokenID := newAccessTokenID(clientOf(request))
	// The same lifetime CreateAccessToken uses. Two call sites, one setting:
	// these are the authorization-code path and the refresh path, and an
	// access token whose validity depended on which of the two minted it
	// would be a difference nobody could see and no document could state.
	expiry := store.Now().Add(s.lifetimes(ctx).OIDCAccessTokenLifetime())

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
		// From now, not from authTime: this bounds how long the holder may go
		// without exchanging it. How long the session itself may last is a
		// separate question, answered by the absolute cap in
		// TokenRequestByRefreshToken — which is why authTime is carried
		// forward across rotations rather than refreshed here.
		ExpiresAt: now.Add(s.lifetimes(ctx).OIDCRefreshTokenLifetime()),
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

	// The absolute age of the session, which is a different question from the
	// age of the token in hand.
	//
	// Rotation gives each replacement a fresh expiry, so a chain that is
	// exchanged diligently never reaches one: "thirty days" bounds how long
	// the holder may go quiet, not how long the access lasts. auth_time is
	// carried forward across every rotation — it is the moment the person
	// actually signed in — so measuring from it is what makes this a cap on
	// the session rather than on the token.
	//
	// Deliberately not RevokeRefreshTokenChain. Revoking the chain is this
	// server's statement that a copy leaked, and it is worth reading that way
	// precisely because it is not said about anything else. A session that
	// reached its age limit is the system working as configured; filing it
	// under the same response would make the one signal that means "somebody
	// has your token" ambiguous. The refusal is enough — the client starts a
	// fresh authorization and the person signs in again, which is the entire
	// point of having a cap.
	if settings := s.lifetimes(ctx); settings.OIDCSessionCapped() {
		if now.Sub(row.AuthTime) > settings.OIDCSessionMaxAge() {
			return nil, op.ErrInvalidRefreshToken
		}
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
//
// The first argument is an id, not a token. The protocol library resolves
// whatever was presented to one — through GetRefreshTokenInfo — before
// calling, so looking it up by hash finds nothing and revokes nothing, and
// because revocation is required to answer successfully either way, the
// endpoint reports having done something it did not.
func (s *Storage) RevokeToken(ctx context.Context, tokenID, _, clientID string) *oidc.Error {
	q := s.scoped()

	row, err := q.GetRefreshTokenByID(ctx, tokenID)
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
	// The ID token's lifetime is read here rather than inside the client,
	// because op.Client's IDTokenLifetime() takes no context and cannot fail.
	// This method is called on every request the protocol library serves, so
	// reading it here is still per-request — a settings change takes effect on
	// the next token, not on the next restart.
	return &client{
		model:           found,
		loginURL:        s.loginURL,
		idTokenLifetime: s.lifetimes(ctx).OIDCAccessTokenLifetime(),
	}, nil
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
//
// One of the two places the client is known, so one of the two where a
// per-application mapping can be applied. See SetUserinfoFromToken for the
// other side of that.
func (s *Storage) SetUserinfoFromRequest(ctx context.Context, userinfo *oidc.UserInfo, request op.IDTokenRequest, scopes []string) error {
	out, err := s.outboundFor(ctx, request.GetClientID())
	if err != nil {
		return err
	}
	return s.setUserinfo(ctx, userinfo, request.GetSubject(), scopes, out)
}

// SetUserinfoFromToken fills claims for the userinfo endpoint.
func (s *Storage) SetUserinfoFromToken(ctx context.Context, userinfo *oidc.UserInfo, tokenID, subject, _ string) error {
	// The scopes are not available here, so everything the account has is
	// returned. That is what userinfo is for, and the access token presented
	// to reach it was issued for this subject.
	//
	// The client is not a parameter here — the interface passes the request
	// origin where the introspection one passes a client id — so it is read
	// out of the token id, which CreateAccessToken puts it in for exactly
	// this. A token issued before that carries none, and gets the defaults it
	// was issued under.
	out, err := s.outboundFor(ctx, clientFromAccessTokenID(tokenID))
	if err != nil {
		return err
	}
	return s.setUserinfo(ctx, userinfo, subject,
		[]string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail, oidc.ScopePhone},
		out)
}

// errInactiveToken reports a token whose subject may no longer use it.
//
// It has to be an error rather than a field, because the endpoint's handler
// sets Active itself once this returns without one — so writing false here
// and returning nil reports the token as live, which is the opposite of the
// answer. Returning an error leaves the response in its zero state, which
// is `{"active": false}`, exactly what RFC 7662 asks for.
var errInactiveToken = errors.New("oidcp: the token's subject is not active")

// SetIntrospectionFromToken answers the introspection endpoint.
func (s *Storage) SetIntrospectionFromToken(ctx context.Context, response *oidc.IntrospectionResponse, _, subject, clientID string) error {
	account, err := s.scoped().GetUserByID(ctx, subject)
	if err != nil {
		return err
	}
	// A disabled account's tokens report inactive, which is the whole reason
	// a resource server would ask rather than verify offline.
	if model.Status(account.Status) != model.StatusActive {
		return errInactiveToken
	}
	response.Subject = subject

	// IntrospectionResponse embeds the userinfo claim groups rather than a
	// UserInfo, so it is filled through one and copied across.
	var userinfo oidc.UserInfo
	// The client is known here, unlike at the userinfo endpoint: the library
	// passes it, because introspection authenticates the caller and the caller
	// is the client the token was issued to. So a resource server introspecting
	// sees the same names the relying party was given — which is the whole
	// point of introspecting rather than decoding.
	out, err := s.outboundFor(ctx, clientID)
	if err != nil {
		return err
	}
	err = s.setUserinfo(ctx, &userinfo, subject,
		[]string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail, oidc.ScopePhone},
		out)
	if err != nil {
		return err
	}
	response.SetUserInfo(&userinfo)
	return nil
}

// GetPrivateClaimsFromScopes puts Portico's own claims in the access token.
//
// The client id was discarded here until mappings existed. It is the second
// of the two places one is known, and a resource server reading the access
// token has to see the same names the relying party sees in the ID token —
// a claim renamed in one and not the other would be a rename that half the
// integration missed.
func (s *Storage) GetPrivateClaimsFromScopes(ctx context.Context, userID, clientID string, _ []string) (map[string]any, error) {
	user, err := s.users.Get(ctx, s.tenant.ID, userID)
	if err != nil {
		return nil, err
	}
	out, err := s.outboundFor(ctx, clientID)
	if err != nil {
		return nil, err
	}

	claims := s.privateClaims(user, out)
	if s.catalogue != nil {
		additions, err := s.catalogue.OIDCAdditions(ctx, s.tenant.ID, user, out)
		if err != nil {
			return nil, err
		}
		for name, value := range additions {
			claims[name] = value
		}
	}
	return claims, nil
}

// privateClaims are the claims that are Portico's own rather than OpenID's:
// the tenant, the role, and the organization, which is what §3.8.2 asks the
// token to carry.
//
// They go into the ID token, the access token, and userinfo alike. A
// relying party reads identity from the ID token and a resource server from
// the access token, and a claim present in only one of them is a claim
// half the integrations cannot see.
func (s *Storage) privateClaims(user model.User, out service.Outbound) map[string]any {
	values := map[string]string{
		"tenant_id":   s.tenant.ID,
		"tenant_code": s.tenant.Code,
		"role":        string(user.Role),
	}
	if user.OrganizationID != "" {
		values["organization_id"] = user.OrganizationID
		values["organization_name"] = user.OrganizationName
	}

	// Through the rules even when there are none: an empty set returns every
	// name unchanged, so this is the same map it has always been.
	claims := make(map[string]any, len(values))
	for key, value := range values {
		if name, send, _ := service.ClaimFor(out, key); send {
			claims[name] = value
		}
	}
	return claims
}

// outboundFor reads what one client is configured to receive.
//
// An error is returned rather than swallowed. A suppression is somebody's
// decision that an application must not receive a field, so quietly falling
// back to the defaults would send it anyway — the opposite of what was asked
// for, at the one moment nobody is watching. The account was read from the
// same database a moment earlier, so a failure here is not a routine
// condition.
func (s *Storage) outboundFor(ctx context.Context, clientID string) (service.Outbound, error) {
	if s.mappings == nil || clientID == "" {
		return service.Outbound{}, nil
	}
	client, err := s.clients.Get(ctx, s.tenant.ID, clientID)
	if err != nil {
		return service.Outbound{}, err
	}
	return s.mappings.OutboundFor(ctx, s.tenant.ID, store.RecipientRef{OAuthClientID: client.ID})
}

// claim sends one value under whatever name the rules give it.
//
// under sends the value under a name a rule chose; assign is what to do when
// the name is unchanged, because several of these
// claims are typed fields on UserInfo and setting the field is what puts them
// in the document. A rename cannot use it — for those claims the field *is*
// the name — so the value is appended instead. Doing both would send the fact
// twice under two names, which is what somebody renaming it is trying to stop.
func claim(out service.Outbound, key string, under func(name string), assign func()) {
	name, send, renamed := service.ClaimFor(out, key)
	switch {
	case !send:
		return
	case renamed:
		under(name)
	default:
		assign()
	}
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

func (s *Storage) setUserinfo(ctx context.Context, userinfo *oidc.UserInfo, subject string, scopes []string, out service.Outbound) error {
	user, err := s.users.Get(ctx, s.tenant.ID, subject)
	if err != nil {
		return err
	}

	userinfo.Subject = user.ID
	// The ID token is built from this object, so anything omitted here is
	// absent from the ID token however faithfully the access token carries
	// it.
	for name, value := range s.privateClaims(user, out) {
		userinfo.AppendClaims(name, value)
	}
	for _, scope := range scopes {
		switch scope {
		case oidc.ScopeProfile:
			claim(out, "display_name",
				func(name string) { userinfo.AppendClaims(name, user.DisplayName) },
				func() { userinfo.Name = user.DisplayName })
			claim(out, "username",
				func(name string) { userinfo.AppendClaims(name, user.Username) },
				func() { userinfo.PreferredUsername = user.Username })
			claim(out, "updated_at",
				func(name string) { userinfo.AppendClaims(name, user.UpdatedAt.UTC().Format(time.RFC3339)) },
				func() { userinfo.UpdatedAt = oidc.FromTime(user.UpdatedAt) })
		case oidc.ScopeEmail:
			if user.Email != "" {
				claim(out, "email",
					func(name string) { userinfo.AppendClaims(name, user.Email) },
					func() {
						userinfo.Email = user.Email
						// Not verified: this version never asks anyone to
						// prove an address, so claiming otherwise would be a
						// lie a relying party might act on.
						//
						// Through AppendClaims rather than the field beside
						// it. The field is tagged omitempty over a bool, so
						// assigning false removes the claim from the document
						// altogether — and a relying party that distinguishes
						// "absent" from "false" then learns nothing, while
						// discovery advertises the claim. Saying false is the
						// whole point.
						//
						// Inside this branch, so it follows the claim it
						// describes: a party reading `mail` has no reason to
						// look at `email_verified`, and one no longer
						// receiving the address at all should not be told
						// anything about an address it cannot see.
						userinfo.AppendClaims("email_verified", false)
					})
			}
		case oidc.ScopePhone:
			if user.Phone != "" {
				claim(out, "phone",
					func(name string) { userinfo.AppendClaims(name, user.Phone) },
					func() {
						userinfo.PhoneNumber = user.Phone
						userinfo.AppendClaims("phone_number_verified", false)
					})
			}
		}
	}

	// The facts the default claim set never carried, which is most of the
	// catalogue and the larger half of what this feature is for.
	if s.catalogue != nil {
		additions, err := s.catalogue.OIDCAdditions(ctx, s.tenant.ID, user, out)
		if err != nil {
			return err
		}
		for name, value := range additions {
			userinfo.AppendClaims(name, value)
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
