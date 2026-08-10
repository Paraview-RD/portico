package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml"
)

const (
	samlLoginPath    = "/saml"
	samlACSPath      = "/saml/acs"
	samlMetadataPath = "/saml/metadata"
)

type samlParty struct {
	sp           *saml.ServiceProvider
	metadataPath string

	// outstanding holds the IDs of authentication requests that have been
	// sent and not yet answered.
	//
	// A cookie would be the obvious place for this, and it is what the
	// OpenID Connect half does for its state. It does not work here: the
	// assertion arrives as a top-level cross-site POST from Portico's
	// origin, and a browser does not send a SameSite=Lax cookie on one of
	// those — which is the default a cookie with no SameSite is treated as.
	// SameSite=None would be sent, but only together with Secure, which a
	// plain-http demonstration cannot have. So the request IDs live in this
	// process, where the cross-site rules do not reach.
	mu          sync.Mutex
	outstanding map[string]time.Time
}

// newSAML prepares a service provider and publishes its metadata.
//
// The exchange SAML asks for is two documents. Portico's is fetched here, at
// start-up, over plain http — which is fine in this direction, because
// nothing in it is secret and the signature on an assertion is what makes
// the certificate inside it load-bearing. Ours is written to a file, because
// `portico sp register --metadata` refuses a plain-http URL: that document
// names where assertions get delivered, so anybody on the path could point
// them somewhere else.
func newSAML(portico, base, stateDir string) (*samlParty, error) {
	key, cert, err := loadOrCreateKeypair(stateDir)
	if err != nil {
		return nil, err
	}

	idp, err := fetchIDPMetadata(portico + "/saml/metadata")
	if err != nil {
		return nil, err
	}

	acs, err := url.Parse(base + samlACSPath)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", base+samlACSPath, err)
	}
	ours, err := url.Parse(base + samlMetadataPath)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", base+samlMetadataPath, err)
	}

	sp := &saml.ServiceProvider{
		EntityID:    base + samlMetadataPath,
		Key:         key,
		Certificate: cert,
		MetadataURL: *ours,
		AcsURL:      *acs,
		IDPMetadata: idp,
		// SignatureMethod is deliberately left empty, which is what turns
		// request signing off. Portico documents signed authentication
		// requests as not implemented, and sending a signature nothing looks
		// at would suggest otherwise to anybody reading this as an example.
		AuthnNameIDFormat: saml.PersistentNameIDFormat,
	}

	party := &samlParty{sp: sp, outstanding: map[string]time.Time{}}
	if party.metadataPath, err = writeMetadata(stateDir, sp); err != nil {
		return nil, err
	}
	return party, nil
}

// fetchIDPMetadata reads Portico's metadata document.
//
// crewjam ships samlsp.FetchMetadata, which does this and also copes with a
// document describing several entities. It is not used here because
// importing samlsp pulls its session machinery — and a dependency this
// example has no other use for — into a module that documents every one it
// has. Portico publishes a single entity, so a fetch and a decode is the
// whole job.
func fetchIDPMetadata(from string) (*saml.EntityDescriptor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, from, nil)
	if err != nil {
		return nil, fmt.Errorf("build a request for %s: %w", from, err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch Portico's SAML metadata from %s: %w", from, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s, not 200 — is that a Portico deployment?",
			from, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", from, err)
	}

	var descriptor saml.EntityDescriptor
	if err := xml.Unmarshal(body, &descriptor); err != nil {
		return nil, fmt.Errorf("parse the metadata at %s: %w", from, err)
	}
	return &descriptor, nil
}

func (s *samlParty) mount(mux *http.ServeMux) {
	mux.HandleFunc(samlLoginPath, s.begin)
	mux.HandleFunc(samlACSPath, s.consume)
	mux.HandleFunc(samlMetadataPath, s.metadata)
}

// begin sends the browser to Portico with an authentication request.
func (s *samlParty) begin(w http.ResponseWriter, r *http.Request) {
	binding := saml.HTTPRedirectBinding
	request, err := s.sp.MakeAuthenticationRequest(
		s.sp.GetSSOBindingLocation(binding), binding, saml.HTTPPostBinding)
	if err != nil {
		s.fail(w, "building the authentication request", err)
		return
	}

	// Remembered before the redirect, not after: the assertion can arrive
	// before this handler's own goroutine would have got to it otherwise.
	s.remember(request.ID)

	redirect, err := request.Redirect("", s.sp)
	if err != nil {
		s.fail(w, "encoding the authentication request", err)
		return
	}
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

// consume validates the assertion Portico posted back.
func (s *samlParty) consume(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, "reading the posted form", err)
		return
	}
	encrypted := assertionWasEncrypted(r.PostFormValue("SAMLResponse"))

	// ParseResponse does the whole of it: base64, XML, the signature against
	// the certificate in Portico's metadata, decryption if the assertion was
	// encrypted, the audience, the time bounds, and whether InResponseTo
	// names a request this process actually sent. Nothing below re-checks any
	// of it — a second implementation of any one of those checks is how a
	// SAML integration ends up accepting a forged assertion.
	assertion, err := s.sp.ParseResponse(r, s.pending())
	if err != nil {
		s.fail(w, "validating the assertion", err)
		return
	}
	s.forget(assertion)

	page(w, http.StatusOK, "samlsignedin", samlView(assertion, encrypted))
}

func (s *samlParty) metadata(w http.ResponseWriter, _ *http.Request) {
	encoded, err := xml.MarshalIndent(s.sp.Metadata(), "", "  ")
	if err != nil {
		http.Error(w, "mock-sp: encode metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write(encoded)
}

// fail renders why a sign-in did not complete.
//
// InvalidResponseError carries the real reason in PrivateErr and keeps its
// own message vague, so that an application cannot accidentally return the
// details to whoever sent the response. Here the details are the entire
// point, so they are unwrapped.
func (s *samlParty) fail(w http.ResponseWriter, stage string, err error) {
	detail := err.Error()
	var invalid *saml.InvalidResponseError
	if errors.As(err, &invalid) && invalid.PrivateErr != nil {
		detail = invalid.PrivateErr.Error()
	}
	page(w, http.StatusBadRequest, "samlerror", map[string]string{
		"Stage": stage, "Detail": detail,
	})
}

// assertionWasEncrypted reports whether Portico encrypted the assertion,
// which it does whenever the service provider publishes an encryption key.
//
// Read off the document rather than inferred from our own metadata: the
// question is what Portico did, and answering it from what we asked for
// would show the same thing whether or not it happened.
func assertionWasEncrypted(response string) bool {
	decoded, err := base64.StdEncoding.DecodeString(response)
	if err != nil {
		return false
	}
	return strings.Contains(string(decoded), "EncryptedAssertion")
}

type samlAttribute struct {
	Name   string
	Values []string
}

type samlSignedInView struct {
	Issuer       string
	NameID       string
	Format       string
	NotOnOrAfter string
	Encrypted    bool
	Attributes   []samlAttribute
}

func samlView(assertion *saml.Assertion, encrypted bool) samlSignedInView {
	view := samlSignedInView{
		Issuer:    assertion.Issuer.Value,
		Encrypted: encrypted,
	}
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		view.NameID = assertion.Subject.NameID.Value
		view.Format = assertion.Subject.NameID.Format
	}
	if assertion.Conditions != nil {
		view.NotOnOrAfter = assertion.Conditions.NotOnOrAfter.Format(time.RFC3339)
	}
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			values := make([]string, 0, len(attribute.Values))
			for _, value := range attribute.Values {
				values = append(values, value.Value)
			}
			name := attribute.Name
			if attribute.FriendlyName != "" && attribute.FriendlyName != name {
				name = attribute.FriendlyName + " (" + attribute.Name + ")"
			}
			view.Attributes = append(view.Attributes, samlAttribute{Name: name, Values: values})
		}
	}
	sort.Slice(view.Attributes, func(i, j int) bool {
		return view.Attributes[i].Name < view.Attributes[j].Name
	})
	return view
}

func (s *samlParty) remember(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Requests older than the sign-in deadline Portico documents can never
	// be answered, and keeping them would grow this map for the life of the
	// process.
	for old, at := range s.outstanding {
		if time.Since(at) > 15*time.Minute {
			delete(s.outstanding, old)
		}
	}
	s.outstanding[id] = time.Now()
}

func (s *samlParty) pending() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.outstanding))
	for id := range s.outstanding {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (s *samlParty) forget(assertion *saml.Assertion) {
	if assertion.Subject == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, confirmation := range assertion.Subject.SubjectConfirmations {
		if confirmation.SubjectConfirmationData != nil {
			delete(s.outstanding, confirmation.SubjectConfirmationData.InResponseTo)
		}
	}
}

// loadOrCreateKeypair keeps the service provider's key between runs.
//
// It has to persist. Portico encrypts the assertion whenever the service
// provider publishes an encryption key, and the key it encrypts to is the
// one in the metadata document that was registered. A fresh key each run
// would mean re-registering before every demonstration, and forgetting would
// surface as a decryption error rather than as anything naming the cause.
func loadOrCreateKeypair(dir string) (*rsa.PrivateKey, *x509.Certificate, error) {
	keyPath := filepath.Join(dir, "saml-key.pem")
	certPath := filepath.Join(dir, "saml-cert.pem")

	keyPEM, keyErr := os.ReadFile(keyPath)    //nolint:gosec // operator-supplied path
	certPEM, certErr := os.ReadFile(certPath) //nolint:gosec // operator-supplied path
	if keyErr == nil && certErr == nil {
		if key, cert, err := parseKeypair(keyPEM, certPEM); err == nil {
			return key, cert, nil
		}
		// Fall through and generate a new pair. A half-written pair from an
		// interrupted run should not need somebody to work out which of two
		// files to delete.
	}

	key, cert, err := generateKeypair()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), 0o600); err != nil {
		return nil, nil, fmt.Errorf("write %s: %w", keyPath, err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: cert.Raw,
	}), 0o600); err != nil {
		return nil, nil, fmt.Errorf("write %s: %w", certPath, err)
	}
	return key, cert, nil
}

func parseKeypair(keyPEM, certPEM []byte) (*rsa.PrivateKey, *x509.Certificate, error) {
	keyBlock, _ := pem.Decode(keyPEM)
	certBlock, _ := pem.Decode(certPEM)
	if keyBlock == nil || certBlock == nil {
		return nil, nil, errors.New("the stored key or certificate is not PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse the stored key: %w", err)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse the stored certificate: %w", err)
	}
	return key, cert, nil
}

func generateKeypair() (*rsa.PrivateKey, *x509.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate a key: %w", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mock-sp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create a certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("parse the new certificate: %w", err)
	}
	return key, cert, nil
}

// writeMetadata publishes the service provider's document to a file, which
// is what `portico sp register --metadata` takes.
func writeMetadata(dir string, sp *saml.ServiceProvider) (string, error) {
	encoded, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode the service provider metadata: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, "saml-metadata.xml")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute, nil
	}
	return path, nil
}
