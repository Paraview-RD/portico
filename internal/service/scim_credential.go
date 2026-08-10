package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// SCIMCredentialService issues and resolves the tokens a provisioning system
// authenticates with.
//
// A SCIM client is not a person and does not get an account. It has no
// session, no password to recover, no organization, and no way to reach the
// console — see the schema comment on scim_credentials for why modelling it
// as a user would be a standing invitation to a listing that forgot to
// exclude it.
type SCIMCredentialService struct {
	store *store.Store
	audit *AuditService
}

// NewSCIMCredentialService wires a SCIMCredentialService.
func NewSCIMCredentialService(st *store.Store, audit *AuditService) *SCIMCredentialService {
	return &SCIMCredentialService{store: st, audit: audit}
}

// Errors this service returns.
var (
	ErrSCIMCredentialNotFound = httpx.NotFound("SCIM_CREDENTIAL_NOT_FOUND",
		"No such SCIM credential.")
	ErrSCIMCredentialNameTaken = httpx.Conflict("SCIM_CREDENTIAL_NAME_TAKEN",
		"A SCIM credential with that name already exists.")
	// ErrSCIMUnauthorized is what every authentication failure becomes,
	// whether the token was unknown, malformed, or belonged to a credential
	// somebody disabled. The client is a machine and cannot act on the
	// distinction; the operator can, and gets it from the audit trail and
	// the credential's own status rather than from a response that would
	// also tell an attacker which of their guesses was closest.
	ErrSCIMUnauthorized = httpx.Unauthorized("SCIM_UNAUTHORIZED",
		"The bearer token is not valid for SCIM.")
)

// tokenPrefixLength is how much of a token is kept in the clear.
//
// Enough to tell two credentials apart in a list, far too little to narrow a
// 32-byte secret. The remainder exists only as a SHA-256 digest.
const tokenPrefixLength = 8

// SCIMCredential is a credential as the console sees it: never the token.
type SCIMCredential struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"tokenPrefix"`
	Status      string     `json:"status"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// IssuedSCIMCredential is what creation returns, once.
type IssuedSCIMCredential struct {
	SCIMCredential
	// Token is present on creation and never again. It is not stored, so
	// there is nothing to return later and nothing for a database dump to
	// leak.
	Token string `json:"token"`
}

// Create issues a credential and returns the only copy of its token.
func (s *SCIMCredentialService) Create(ctx context.Context, actor auth.Principal, name string) (IssuedSCIMCredential, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return IssuedSCIMCredential{}, httpx.BadRequest("INVALID_NAME",
			"A name is required, so the credential can be told apart later.")
	}

	token, err := newSCIMToken()
	if err != nil {
		return IssuedSCIMCredential{}, err
	}

	now := store.Now()
	id := uuid.NewString()
	q := s.store.ForTenant(actor.TenantID)

	err = q.CreateSCIMCredential(ctx, sqlcgen.CreateSCIMCredentialParams{
		ID:          id,
		Name:        name,
		TokenHash:   hashSCIMToken(token),
		TokenPrefix: token[:tokenPrefixLength],
		CreatedAt:   now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return IssuedSCIMCredential{}, ErrSCIMCredentialNameTaken
		}
		return IssuedSCIMCredential{}, fmt.Errorf("create scim credential: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionSCIMCredentialCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "SCIM_CREDENTIAL", TargetID: id, TargetName: name,
	})

	return IssuedSCIMCredential{
		SCIMCredential: SCIMCredential{
			ID: id, Name: name, TokenPrefix: token[:tokenPrefixLength],
			Status: string(model.StatusActive), CreatedAt: now,
		},
		Token: token,
	}, nil
}

// List returns a tenant's credentials, without tokens.
func (s *SCIMCredentialService) List(ctx context.Context, tenantID string) ([]SCIMCredential, error) {
	rows, err := s.store.ForTenant(tenantID).ListSCIMCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("list scim credentials: %w", err)
	}

	out := make([]SCIMCredential, 0, len(rows))
	for _, row := range rows {
		out = append(out, SCIMCredential{
			ID: row.ID, Name: row.Name, TokenPrefix: row.TokenPrefix,
			Status: row.Status, LastUsedAt: row.LastUsedAt,
			CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

// SetStatus enables or disables a credential.
func (s *SCIMCredentialService) SetStatus(ctx context.Context, actor auth.Principal, id string, status model.Status) error {
	q := s.store.ForTenant(actor.TenantID)

	err := q.SetSCIMCredentialStatus(ctx, sqlcgen.SetSCIMCredentialStatusParams{
		ID: id, Status: string(status), UpdatedAt: store.Now(),
	})
	if err != nil {
		return fmt.Errorf("set scim credential status: %w", err)
	}

	action := model.ActionSCIMCredentialEnable
	if status != model.StatusActive {
		action = model.ActionSCIMCredentialDisable
	}
	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "SCIM_CREDENTIAL", TargetID: id,
	})
	return nil
}

// Delete revokes a credential permanently.
func (s *SCIMCredentialService) Delete(ctx context.Context, actor auth.Principal, id string) error {
	q := s.store.ForTenant(actor.TenantID)

	if err := q.DeleteSCIMCredential(ctx, id); err != nil {
		return fmt.Errorf("delete scim credential: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionSCIMCredentialDelete,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "SCIM_CREDENTIAL", TargetID: id,
	})
	return nil
}

// SCIMPrincipal is what a resolved credential authorizes: a tenant, and
// nothing else. There is no role and no subject, because a provisioning
// client acts as itself and its reach is decided by the routes the token is
// accepted on.
type SCIMPrincipal struct {
	CredentialID string
	TenantID     string
	Name         string
}

// Authenticate resolves a bearer token to the tenant it acts in.
//
// This is the query that determines the tenant, so it cannot be scoped to
// one. Everything the request goes on to do is scoped to what this returned.
func (s *SCIMCredentialService) Authenticate(ctx context.Context, token string) (SCIMPrincipal, error) {
	if token == "" {
		return SCIMPrincipal{}, ErrSCIMUnauthorized
	}

	row, err := s.store.Queries.GetSCIMCredentialByTokenHash(ctx, hashSCIMToken(token))
	if err != nil {
		if store.IsNoRows(err) {
			return SCIMPrincipal{}, ErrSCIMUnauthorized
		}
		return SCIMPrincipal{}, fmt.Errorf("look up scim credential: %w", err)
	}

	// The lookup was by digest, so this can only fail on a hash collision.
	// It is here because a constant-time comparison of the value that
	// decides access costs nothing, and because an index lookup silently
	// becoming a prefix match one day should not become an authentication
	// bypass.
	if subtle.ConstantTimeCompare([]byte(row.TokenHash), []byte(hashSCIMToken(token))) != 1 {
		return SCIMPrincipal{}, ErrSCIMUnauthorized
	}

	if model.Status(row.Status) != model.StatusActive {
		return SCIMPrincipal{}, ErrSCIMUnauthorized
	}

	// Best effort: a sync that works should not fail because the bookkeeping
	// write did. The value answers "is this integration still running",
	// which is worth having and not worth an outage.
	if err := s.store.ForTenant(row.TenantID).TouchSCIMCredential(ctx, sqlcgen.TouchSCIMCredentialParams{
		ID: row.ID, LastUsedAt: ptrTime(store.Now()),
	}); err != nil && !errors.Is(err, context.Canceled) {
		// Deliberately not returned. See above.
		_ = err
	}

	return SCIMPrincipal{
		CredentialID: row.ID, TenantID: row.TenantID, Name: row.Name,
	}, nil
}

// newSCIMToken returns a fresh token: 32 bytes of randomness, URL-safe.
//
// Prefixed so that a token found in a log or a configuration file is
// identifiable as this system's, and so secret scanners have something to
// match — the reason GitHub asks for a prefix at all.
func newSCIMToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate scim token: %w", err)
	}
	return "portico_scim_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashSCIMToken is the digest stored in place of the token.
//
// SHA-256, not bcrypt, and that difference from password storage is
// deliberate: the input is 32 bytes from crypto/rand rather than something a
// person chose, so there is no dictionary to slow down and nothing a work
// factor buys. Meanwhile the token arrives on every request of a sync that
// may run to thousands, where a deliberately slow comparison would be a
// denial of service against the operator's own directory push.
func hashSCIMToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func ptrTime(t time.Time) *time.Time { return &t }
