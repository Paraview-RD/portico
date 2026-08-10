package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

const (
	// samlKeyBits matches the OIDC key size for the same reason: it is what
	// every counterparty can verify.
	samlKeyBits = 2048

	// SAMLCertificateLifetime is how long a generated certificate is valid
	// for.
	//
	// Ten years, which is longer than this project would choose for anything
	// it could rotate on its own schedule. It cannot: a service provider
	// pins the certificate in configuration, often by hand, and an expiry
	// arriving unannounced takes the integration down at a moment nobody
	// chose. Rotation here is an operator's decision, not a clock's.
	SAMLCertificateLifetime = 10 * 365 * 24 * time.Hour
)

// SAMLKeyService owns the certificates that sign SAML assertions.
//
// Separate from SigningKeyService, whose keys the JWKS publishes, because
// the two have incompatible rotation contracts rather than merely different
// callers. A relying party refetches a key set, so an OIDC key can be
// retired and deleted a day later without anybody noticing. A SAML service
// provider is configured with the certificate and has no way to learn of a
// new one — so retired certificates are kept indefinitely, and rotating is
// something an operator does while moving service providers across, not
// something that happens on a timer.
type SAMLKeyService struct {
	store *store.Store

	mu     sync.RWMutex
	parsed map[string]SAMLKey
}

// NewSAMLKeyService wires a SAMLKeyService.
func NewSAMLKeyService(st *store.Store) *SAMLKeyService {
	return &SAMLKeyService{store: st, parsed: map[string]SAMLKey{}}
}

// SAMLKey is a signing key with its certificate, both parsed.
type SAMLKey struct {
	ID          string
	Private     *rsa.PrivateKey
	Certificate *x509.Certificate
	// CertificatePEM is what an operator hands to a service provider that
	// wants the certificate rather than the metadata document.
	CertificatePEM string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	Retired        bool
}

// Active returns the key assertions are signed with, generating one on first
// use.
func (s *SAMLKeyService) Active(ctx context.Context, tenantID string) (SAMLKey, error) {
	q := s.store.ForTenant(tenantID)

	row, err := q.GetActiveSAMLSigningKey(ctx)
	if err == nil {
		return s.parse(row)
	}
	if !store.IsNoRows(err) {
		return SAMLKey{}, fmt.Errorf("read SAML signing key: %w", err)
	}

	if err := s.generate(ctx, tenantID); err != nil {
		return SAMLKey{}, err
	}

	// Re-read rather than returning what was just generated: two callers can
	// race here, and whichever key won the insert is the one to use.
	row, err = q.GetActiveSAMLSigningKey(ctx)
	if err != nil {
		return SAMLKey{}, fmt.Errorf("read SAML signing key after generating one: %w", err)
	}
	return s.parse(row)
}

// List returns every key a tenant has had, active first.
func (s *SAMLKeyService) List(ctx context.Context, tenantID string) ([]SAMLKey, error) {
	rows, err := s.store.ForTenant(tenantID).ListSAMLSigningKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list SAML signing keys: %w", err)
	}

	keys := make([]SAMLKey, 0, len(rows))
	for _, row := range rows {
		key, err := s.parse(row)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// Rotate retires the current certificate and generates a replacement.
//
// Nothing is deleted. Every service provider has to be reconfigured with the
// new certificate by hand, and until each one has been, the old certificate
// is what the operator needs to be able to show. Deleting it would make
// "which certificate is that integration still pinning" unanswerable at the
// moment it is being asked.
func (s *SAMLKeyService) Rotate(ctx context.Context, tenantID string) (SAMLKey, error) {
	if err := s.store.ForTenant(tenantID).RetireSAMLSigningKeys(ctx, store.Now()); err != nil {
		return SAMLKey{}, fmt.Errorf("retire SAML signing keys: %w", err)
	}
	return s.Active(ctx, tenantID)
}

func (s *SAMLKeyService) generate(ctx context.Context, tenantID string) error {
	private, err := rsa.GenerateKey(rand.Reader, samlKeyBits)
	if err != nil {
		return fmt.Errorf("generate SAML signing key: %w", err)
	}

	now := store.Now()
	expires := now.Add(SAMLCertificateLifetime)

	// A serial large enough that two independently generated certificates
	// will not collide, which some service providers care about when they
	// keep a certificate store keyed by issuer and serial.
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate certificate serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			// The subject carries nothing a service provider should act on.
			// SAML trust is "this is the certificate I was configured with",
			// not a name checked against a hostname or a chain — there is no
			// certificate authority in this picture, and pretending
			// otherwise by putting a domain here would invite somebody to
			// validate against it.
			CommonName:   "Portico SAML signing key",
			Organization: []string{"Portico"},
		},
		NotBefore:             now.Add(-time.Hour), // clock skew at the far end
		NotAfter:              expires,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &private.PublicKey, private)
	if err != nil {
		return fmt.Errorf("create SAML certificate: %w", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return fmt.Errorf("encode SAML signing key: %w", err)
	}

	err = s.store.ForTenant(tenantID).CreateSAMLSigningKey(ctx, sqlcgen.CreateSAMLSigningKeyParams{
		ID:          uuid.NewString(),
		PrivateKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
		Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		CreatedAt:   now,
		ExpiresAt:   expires,
	})
	if err != nil {
		// The unique partial index means a concurrent generation loses here.
		// That is the intended outcome: the caller re-reads and finds the
		// winner.
		if store.IsUniqueViolation(err) {
			return nil
		}
		return fmt.Errorf("store SAML signing key: %w", err)
	}
	return nil
}

func (s *SAMLKeyService) parse(row sqlcgen.SamlSigningKey) (SAMLKey, error) {
	s.mu.RLock()
	cached, ok := s.parsed[row.ID]
	s.mu.RUnlock()
	if ok {
		return cached, nil
	}

	keyBlock, _ := pem.Decode([]byte(row.PrivateKey))
	if keyBlock == nil {
		return SAMLKey{}, fmt.Errorf("SAML signing key %s is not valid PEM", row.ID)
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return SAMLKey{}, fmt.Errorf("parse SAML signing key %s: %w", row.ID, err)
	}
	private, ok := parsedKey.(*rsa.PrivateKey)
	if !ok {
		return SAMLKey{}, fmt.Errorf("SAML signing key %s is not RSA", row.ID)
	}

	certBlock, _ := pem.Decode([]byte(row.Certificate))
	if certBlock == nil {
		return SAMLKey{}, fmt.Errorf("SAML certificate %s is not valid PEM", row.ID)
	}
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return SAMLKey{}, fmt.Errorf("parse SAML certificate %s: %w", row.ID, err)
	}

	key := SAMLKey{
		ID:             row.ID,
		Private:        private,
		Certificate:    certificate,
		CertificatePEM: row.Certificate,
		CreatedAt:      row.CreatedAt,
		ExpiresAt:      row.ExpiresAt,
		Retired:        row.RetiredAt != nil,
	}

	s.mu.Lock()
	s.parsed[row.ID] = key
	s.mu.Unlock()
	return key, nil
}
