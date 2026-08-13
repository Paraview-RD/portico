package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/oidcrp"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Signing in, and binding, through an external provider.
//
// Two journeys share one callback, and telling them apart is the security
// property this file exists to keep. A sign-in resolves to whoever the
// provider names; a binding lands on the account that asked for it. The
// difference is decided when the journey starts and remembered in the row,
// never read from what comes back — a callback cannot say which of the two
// it is, because a crafted one would say the wrong thing.

// AuthRequestTTL is how long a sign-in may stay out at a provider.
//
// Minutes. This is the window in which a stolen state is worth something,
// and somebody who wandered off mid-sign-in starts again for one click.
const AuthRequestTTL = 15 * time.Minute

// Errors from the external sign-in journey.
var (
	// ErrExternalStateUnknown covers every way a callback fails to name a
	// live request: forged, replayed, expired, or already used. They are one
	// error deliberately — a caller who could tell them apart could use the
	// difference to learn which states existed.
	ErrExternalStateUnknown = httpx.UnprocessableEntity("EXTERNAL_STATE_UNKNOWN",
		"That sign-in could not be matched to one this server started. Begin again.")

	// ErrExternalIdentityUnknown is the refusal a first-time arrival meets
	// when nothing binds them to an account.
	ErrExternalIdentityUnknown = httpx.Unauthorized("EXTERNAL_IDENTITY_UNKNOWN",
		"That account is not linked here. Sign in with your password first, then link it from your profile.")

	ErrExternalIdentityTaken = httpx.Conflict("EXTERNAL_IDENTITY_TAKEN",
		"That identity is already linked to an account.")

	ErrExternalIdentityNotFound = httpx.NotFound("EXTERNAL_IDENTITY_NOT_FOUND",
		"No such linked identity.")
)

// StartExternalSignIn sends somebody to a provider.
//
// userID empty is an ordinary sign-in. Set, it is a person already signed in
// asking to bind an identity to their own account, and the callback will
// land there whatever the provider says about who else it might be.
func (s *ExternalIDPService) StartExternalSignIn(ctx context.Context, tenant model.Tenant, providerID, userID string) (string, error) {
	provider, err := s.load(ctx, tenant.ID, providerID)
	if err != nil {
		return "", err
	}
	if model.Status(provider.Status) != model.StatusActive {
		return "", httpx.UnprocessableEntity("EXTERNAL_IDP_DISABLED",
			"That identity provider is switched off.")
	}

	party, err := s.party(ctx, provider, tenant.Code)
	if err != nil {
		return "", err
	}

	state, err := randomToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", err
	}

	now := store.Now()
	var owner *string
	if userID != "" {
		owner = &userID
	}
	err = s.store.ForTenant(tenant.ID).CreateExternalAuthRequest(ctx,
		sqlcgen.CreateExternalAuthRequestParams{
			State: state, ProviderID: provider.ID, Nonce: nonce,
			CodeVerifier: verifier, UserID: owner,
			CreatedAt: now, ExpiresAt: now.Add(AuthRequestTTL),
		})
	if err != nil {
		return "", fmt.Errorf("record the sign-in request: %w", err)
	}

	return party.AuthURL(state, nonce, verifier), nil
}

// ExternalOutcome is what a completed callback produced.
//
// Exactly one of the two is set. A binding does not issue a session — the
// person already had one — and a sign-in does not report a binding, because
// the identity it used was bound long ago.
type ExternalOutcome struct {
	Session *Session
	Bound   *ExternalIdentity
}

// ExternalIdentity is one binding, as its owner sees it.
type ExternalIdentity struct {
	ID           string     `json:"id"`
	ProviderID   string     `json:"providerId"`
	ProviderName string     `json:"providerName"`
	Subject      string     `json:"subject"`
	Email        string     `json:"email"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt"`
}

// CompleteExternalSignIn judges a callback.
func (s *ExternalIDPService) CompleteExternalSignIn(ctx context.Context, tenant model.Tenant, state, code, ip, userAgent string) (ExternalOutcome, error) {
	q := s.store.ForTenant(tenant.ID)

	// Consumed, not read: the row is deleted by the same statement that
	// returns it, so a replayed callback finds nothing.
	request, err := q.TakeExternalAuthRequest(ctx, state, store.Now())
	if err != nil {
		return ExternalOutcome{}, ErrExternalStateUnknown
	}

	provider, err := s.load(ctx, tenant.ID, request.ProviderID)
	if err != nil {
		return ExternalOutcome{}, err
	}
	party, err := s.party(ctx, provider, tenant.Code)
	if err != nil {
		return ExternalOutcome{}, err
	}

	identity, err := party.Exchange(ctx, code, request.CodeVerifier, request.Nonce)
	if err != nil {
		return ExternalOutcome{}, httpx.Unauthorized("EXTERNAL_EXCHANGE_FAILED",
			"The identity provider's answer could not be accepted.")
	}

	if request.UserID != nil {
		bound, err := s.bind(ctx, tenant, provider, *request.UserID, identity)
		if err != nil {
			return ExternalOutcome{}, err
		}
		return ExternalOutcome{Bound: &bound}, nil
	}
	return s.signIn(ctx, tenant, provider, identity, ip, userAgent)
}

// signIn resolves an identity to an account, or refuses.
func (s *ExternalIDPService) signIn(ctx context.Context, tenant model.Tenant, provider sqlcgen.ExternalIdentityProvider, identity oidcrp.Identity, ip, userAgent string) (ExternalOutcome, error) {
	q := s.store.ForTenant(tenant.ID)

	existing, err := q.GetExternalIdentity(ctx, provider.ID, identity.Subject)
	switch {
	case err == nil:
		if err := q.TouchExternalIdentity(ctx, provider.ID, identity.Subject, store.Now()); err != nil {
			return ExternalOutcome{}, fmt.Errorf("record the sign-in: %w", err)
		}
		session, err := s.users.IssueSessionForExternalIdentity(ctx, tenant, existing.UserID, ip, userAgent)
		if err != nil {
			return ExternalOutcome{}, err
		}
		return ExternalOutcome{Session: &session}, nil

	case !store.IsNoRows(err):
		return ExternalOutcome{}, fmt.Errorf("look up the linked identity: %w", err)
	}

	// Nothing binds this person here. Whether an address may stand in for a
	// binding is the one decision that can hand an account to a stranger,
	// and it is off unless somebody turned it on for this provider knowing
	// what it means.
	if !provider.TrustVerifiedEmail || !identity.EmailVerified || identity.Email == "" {
		return ExternalOutcome{}, ErrExternalIdentityUnknown
	}

	row, err := q.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(identity.Email)))
	if err != nil {
		if store.IsNoRows(err) {
			return ExternalOutcome{}, ErrExternalIdentityUnknown
		}
		return ExternalOutcome{}, fmt.Errorf("look up by address: %w", err)
	}

	// Bound on the way through, so the address is consulted once ever. From
	// the next sign-in this account is reached by subject, and an address
	// that changes at the provider — or is reassigned to somebody else —
	// cannot repoint it.
	if _, err := s.bind(ctx, tenant, provider, row.ID, identity); err != nil {
		return ExternalOutcome{}, err
	}
	session, err := s.users.IssueSessionForExternalIdentity(ctx, tenant, row.ID, ip, userAgent)
	if err != nil {
		return ExternalOutcome{}, err
	}
	return ExternalOutcome{Session: &session}, nil
}

// bind ties an identity to an account.
func (s *ExternalIDPService) bind(ctx context.Context, tenant model.Tenant, provider sqlcgen.ExternalIdentityProvider, userID string, identity oidcrp.Identity) (ExternalIdentity, error) {
	q := s.store.ForTenant(tenant.ID)

	id := uuid.NewString()
	now := store.Now()
	err := q.CreateExternalIdentity(ctx, sqlcgen.CreateExternalIdentityParams{
		ID: id, ProviderID: provider.ID, UserID: userID,
		Subject: identity.Subject, Email: identity.Email, CreatedAt: now,
	})
	if err != nil {
		// The unique constraint is the check. Asking first and inserting
		// afterwards would leave a window where two requests both find
		// nothing and both bind — and the second account would be reachable
		// by somebody else's identity.
		if store.IsUniqueViolation(err) {
			return ExternalIdentity{}, ErrExternalIdentityTaken
		}
		return ExternalIdentity{}, fmt.Errorf("link identity: %w", err)
	}

	s.audit.Log(ctx, tenant.ID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionExternalIdentityBind,
		ActorID: userID, TargetType: "USER", TargetID: userID,
		Detail: provider.Name + " " + identity.Subject,
	})

	return ExternalIdentity{
		ID: id, ProviderID: provider.ID, ProviderName: provider.Name,
		Subject: identity.Subject, Email: identity.Email, CreatedAt: now,
	}, nil
}

// IdentitiesFor lists what one account has bound.
func (s *ExternalIDPService) IdentitiesFor(ctx context.Context, tenantID, userID string) ([]ExternalIdentity, error) {
	q := s.store.ForTenant(tenantID)

	rows, err := q.ListExternalIdentitiesForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list linked identities: %w", err)
	}
	providers, err := q.ListExternalIdentityProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list identity providers: %w", err)
	}
	names := make(map[string]string, len(providers))
	for _, provider := range providers {
		names[provider.ID] = provider.Name
	}

	out := make([]ExternalIdentity, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExternalIdentity{
			ID: row.ID, ProviderID: row.ProviderID, ProviderName: names[row.ProviderID],
			Subject: row.Subject, Email: row.Email,
			CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt,
		})
	}
	return out, nil
}

// Unbind removes one of an account's own identities.
//
// No guard against removing the last one, unlike the last-administrator
// rules elsewhere. Every account here still has a password — external
// sign-in is an addition rather than a replacement in this version — so
// unbinding removes a convenience, not the only way in. The day a
// password-less account becomes possible, this needs the guard.
func (s *ExternalIDPService) Unbind(ctx context.Context, tenantID, userID, id string) error {
	if err := s.store.ForTenant(tenantID).DeleteExternalIdentity(ctx, userID, id); err != nil {
		return fmt.Errorf("unlink identity: %w", err)
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionExternalIdentityUnbind,
		ActorID: userID, TargetType: "USER", TargetID: userID,
	})
	return nil
}

// SignInOption is one button on the sign-in screen.
//
// A label and an id, and nothing else. What a button says is public the
// moment it is drawn; the issuer, the client id and whether an address is
// trusted are not, and this endpoint answers before anybody has proved
// anything.
type SignInOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// SignInOptions lists the buttons for a tenant.
func (s *ExternalIDPService) SignInOptions(ctx context.Context, tenantID string) ([]SignInOption, error) {
	rows, err := s.store.ForTenant(tenantID).ListActiveExternalIdentityProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list identity providers: %w", err)
	}
	out := make([]SignInOption, 0, len(rows))
	for _, row := range rows {
		label := row.ButtonLabel
		if label == "" {
			label = row.Name
		}
		out = append(out, SignInOption{ID: row.ID, Label: label})
	}
	return out, nil
}

func (s *ExternalIDPService) load(ctx context.Context, tenantID, id string) (sqlcgen.ExternalIdentityProvider, error) {
	row, err := s.store.ForTenant(tenantID).GetExternalIdentityProvider(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return sqlcgen.ExternalIdentityProvider{}, ErrExternalIDPNotFound
		}
		return sqlcgen.ExternalIdentityProvider{}, fmt.Errorf("get identity provider: %w", err)
	}
	return row, nil
}

// party unseals what is needed to talk to a provider and prepares a client.
func (s *ExternalIDPService) party(ctx context.Context, provider sqlcgen.ExternalIdentityProvider, tenantCode string) (*oidcrp.Party, error) {
	secret := ""
	if provider.ClientSecret != "" {
		opened, err := s.vault.Open(provider.ClientSecret)
		if err != nil {
			// A configured provider whose secret cannot be opened is a
			// deployment that changed or lost PORTICO_ENCRYPTION_KEY. Said
			// plainly, because the alternative is an authentication failure
			// at the provider that reads as their problem.
			return nil, httpx.UnprocessableEntity("EXTERNAL_IDP_SECRET_UNREADABLE",
				"This provider's client secret cannot be read. It was sealed with a different encryption key; re-enter it.")
		}
		secret = opened
	}

	party, err := oidcrp.Discover(ctx, oidcrp.Config{
		Issuer: provider.Issuer, ClientID: provider.ClientID, ClientSecret: secret,
		RedirectURI: s.RedirectURI(tenantCode), Scopes: strings.Fields(provider.Scopes),
	})
	if err != nil {
		return nil, httpx.UnprocessableEntity("EXTERNAL_IDP_UNREACHABLE",
			"That identity provider could not be reached.")
	}
	return party, nil
}

// randomToken is a state, a nonce, or a code verifier.
//
// One generator for all three because they need the same thing:
// unpredictability. 32 bytes, URL-safe and unpadded, which also satisfies
// PKCE's requirement that a verifier be 43 to 128 characters of the
// unreserved set.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
