package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Key sizes and lifetimes.
const (
	// signingKeyBits is the RSA modulus size. 2048 is what every relying
	// party can verify and what the JOSE specifications assume; 4096 buys
	// margin nobody is asking for at a signing cost paid on every token.
	signingKeyBits = 2048

	// SigningKeyRetention is how long a retired key stays in the published
	// key set.
	//
	// It has to exceed the longest lifetime of anything the key signed, or
	// rotation invalidates live tokens. An hour of margin over the ID token
	// lifetime is the whole requirement; a day is generous and costs one row.
	SigningKeyRetention = 24 * time.Hour
)

// SigningKeyService owns the asymmetric keys that sign ID tokens.
//
// These exist separately from the HS256 secret that signs Portico's own
// sessions, and the difference is not incidental. A session token is
// verified by this server, which can keep a secret. An ID token is verified
// by somebody else, offline, against a published key set — so it must be
// signed by a key whose public half can be given out, and the private half
// can never leave.
//
// Keys are per tenant because issuers are per tenant. A relying party
// configured for one tenant fetches that tenant's key set and will not
// verify a token signed for another, which is what makes cross-tenant token
// confusion impossible rather than merely unlikely.
type SigningKeyService struct {
	store *store.Store

	// Parsing a PEM on every token would be a needless cost on the hottest
	// path in the protocol, so the parsed form is kept. Keyed by the key id,
	// which changes on rotation, so a stale entry is unreachable rather than
	// wrong.
	mu     sync.RWMutex
	parsed map[string]*rsa.PrivateKey
}

// NewSigningKeyService wires a SigningKeyService.
func NewSigningKeyService(st *store.Store) *SigningKeyService {
	return &SigningKeyService{store: st, parsed: map[string]*rsa.PrivateKey{}}
}

// SigningKey is a key with its private half parsed.
type SigningKey struct {
	ID        string
	Algorithm string
	Private   *rsa.PrivateKey
	CreatedAt time.Time
	Retired   bool
}

// Active returns the key new tokens are signed with, generating one if the
// tenant has none.
//
// Generating on demand rather than at tenant creation means an existing
// deployment gains federation by upgrading, without a migration step that
// backfills keys — and a tenant that never issues a token never pays the
// cost of an RSA keygen.
func (s *SigningKeyService) Active(ctx context.Context, tenantID string) (SigningKey, error) {
	q := s.store.ForTenant(tenantID)

	row, err := q.GetActiveSigningKey(ctx)
	if err == nil {
		return s.parse(row)
	}
	if !store.IsNoRows(err) {
		return SigningKey{}, fmt.Errorf("read signing key: %w", err)
	}

	if err := s.generate(ctx, tenantID); err != nil {
		return SigningKey{}, err
	}

	// Re-read rather than returning what was just generated: two callers can
	// race here, and whichever key won the insert is the one to use. Reading
	// back makes them agree.
	row, err = q.GetActiveSigningKey(ctx)
	if err != nil {
		return SigningKey{}, fmt.Errorf("read signing key after generating one: %w", err)
	}
	return s.parse(row)
}

// Published returns every key the JWKS should advertise: the active one and
// any retired key whose tokens may still be in flight.
func (s *SigningKeyService) Published(ctx context.Context, tenantID string) ([]SigningKey, error) {
	// Touch the active key first so a tenant that has never issued a token
	// still publishes a usable key set rather than an empty one.
	if _, err := s.Active(ctx, tenantID); err != nil {
		return nil, err
	}

	// Retired longer ago than the retention window means nothing it signed
	// can still be alive, so publishing it only keeps an old key trusted.
	rows, err := s.store.ForTenant(tenantID).ListPublishedSigningKeys(
		ctx, store.Now().Add(-SigningKeyRetention))
	if err != nil {
		return nil, fmt.Errorf("list signing keys: %w", err)
	}

	keys := make([]SigningKey, 0, len(rows))
	for _, row := range rows {
		key, err := s.parse(row)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// Rotate retires the current key and generates a replacement.
//
// The old key stays in the key set for SigningKeyRetention, so tokens signed
// a moment before the rotation keep verifying. Anything retired longer than
// that is dropped in the same pass — a key set that only grows is a key set
// nobody prunes.
func (s *SigningKeyService) Rotate(ctx context.Context, tenantID string) (SigningKey, error) {
	q := s.store.ForTenant(tenantID)
	now := store.Now()

	if err := q.RetireSigningKeys(ctx, now); err != nil {
		return SigningKey{}, fmt.Errorf("retire signing keys: %w", err)
	}
	if err := q.DeleteExpiredSigningKeys(ctx, now.Add(-SigningKeyRetention)); err != nil {
		return SigningKey{}, fmt.Errorf("prune retired signing keys: %w", err)
	}
	return s.Active(ctx, tenantID)
}

func (s *SigningKeyService) generate(ctx context.Context, tenantID string) error {
	private, err := rsa.GenerateKey(rand.Reader, signingKeyBits)
	if err != nil {
		return fmt.Errorf("generate signing key: %w", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return fmt.Errorf("encode signing key: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return fmt.Errorf("encode public key: %w", err)
	}

	err = s.store.ForTenant(tenantID).CreateSigningKey(ctx, sqlcgen.CreateSigningKeyParams{
		ID:         uuid.NewString(),
		Algorithm:  model.SigningAlgorithm,
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
		PublicKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
		Status:     "ACTIVE",
		CreatedAt:  store.Now(),
	})
	if err != nil {
		return fmt.Errorf("store signing key: %w", err)
	}
	return nil
}

func (s *SigningKeyService) parse(row sqlcgen.OauthSigningKey) (SigningKey, error) {
	key := SigningKey{
		ID:        row.ID,
		Algorithm: row.Algorithm,
		CreatedAt: row.CreatedAt,
		Retired:   row.Status == "RETIRED",
	}

	s.mu.RLock()
	cached, ok := s.parsed[row.ID]
	s.mu.RUnlock()
	if ok {
		key.Private = cached
		return key, nil
	}

	block, _ := pem.Decode([]byte(row.PrivateKey))
	if block == nil {
		return SigningKey{}, fmt.Errorf("signing key %s is not valid PEM", row.ID)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return SigningKey{}, fmt.Errorf("parse signing key %s: %w", row.ID, err)
	}
	private, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return SigningKey{}, fmt.Errorf("signing key %s is not an RSA key", row.ID)
	}

	s.mu.Lock()
	s.parsed[row.ID] = private
	s.mu.Unlock()

	key.Private = private
	return key, nil
}
