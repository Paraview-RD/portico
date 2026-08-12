package samlp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/store"
)

// signatureMethodRSASHA256 is what assertions are signed with. SHA-1 is
// still what several service providers default to and is deliberately not
// offered.
const signatureMethodRSASHA256 = dsig.RSASHA256SignatureMethod

// Handler serves a tenant's SAML endpoints under a mount.
func (p *Providers) Handler(mount string) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idp, tenant, err := p.For(r.Context(), mount)
		if err != nil {
			http.Error(w, "unknown tenant", http.StatusNotFound)
			return
		}

		switch strings.TrimSuffix(r.URL.Path, "/") {
		case MetadataPath:
			p.serveMetadata(w, idp)
		case SSOPath:
			idp.ServeSSO(w, r)
		case CallbackPath:
			p.serveCallback(w, r, idp, tenant)
		default:
			http.NotFound(w, r)
		}
	})
	if mount == "" {
		return inner
	}
	return http.StripPrefix(mount, inner)
}

// serveMetadata publishes the document a service provider is configured
// from: the entity id, the SSO endpoint, and the certificate assertions are
// signed with.
func (p *Providers) serveMetadata(w http.ResponseWriter, idp *saml.IdentityProvider) {
	descriptor := idp.Metadata()

	// The library fills this in with transient, which is not what gets sent.
	// Corrected here for the same reason three fields of the OpenID Connect
	// discovery document are: a service provider reads this once, before
	// anybody is watching, and configures itself from it. Transient tells it
	// the identifier is a throwaway that must not be stored — while every
	// assertion carries a persistent one, the account id, chosen precisely so
	// that a service provider can key its own record on it.
	for i := range descriptor.IDPSSODescriptors {
		descriptor.IDPSSODescriptors[i].NameIDFormats =
			[]saml.NameIDFormat{saml.PersistentNameIDFormat}
	}

	document, err := xml.MarshalIndent(descriptor, "", "  ")
	if err != nil {
		http.Error(w, "could not build metadata", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	w.Header().Set("Content-Disposition", `attachment; filename="portico-idp-metadata.xml"`)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(document)
}

// serveCallback resumes a flow that sign-in interrupted: it mints the
// assertion for the person who signed in and posts it to the service
// provider.
func (p *Providers) serveCallback(w http.ResponseWriter, r *http.Request, idp *saml.IdentityProvider, tenant model.Tenant) {
	q := p.store.ForTenant(tenant.ID)

	row, err := q.GetSAMLAuthRequest(r.Context(), r.URL.Query().Get("id"), store.Now())
	if err != nil {
		http.Error(w, "this sign-in request has expired or was already used", http.StatusNotFound)
		return
	}
	if !row.Done || row.Subject == nil {
		// Reached without signing in. Nothing here decides who anybody is —
		// that happens against Portico's own API, with a password.
		http.Error(w, "this sign-in request has not been completed", http.StatusForbidden)
		return
	}

	// And that it is the same browser that signed in.
	//
	// This endpoint cannot ask for a credential: it is a top-level
	// navigation, so there is no header to put one in. Without the secret,
	// the id alone mints an assertion for the person who signed in and hands
	// it to whoever presented it — and the id is in the sign-in URL, which
	// is to say in browser history and any proxy log along the way. The
	// OpenID Connect callback does not have this problem because it hands
	// its code to the relying party's registered address; this one hands the
	// assertion to the caller, to be posted onward.
	//
	// The row is deliberately not deleted on a mismatch. Doing so would let
	// anybody holding a leaked id destroy a sign-in in progress, which
	// trades a hard attack for an easy one.
	if !matchesCompletionSecret(row.CompletionSecret, r.URL.Query().Get(CompletionSecretParam)) {
		http.Error(w, "this sign-in request cannot be completed from here", http.StatusForbidden)
		return
	}

	user, err := p.users.Get(r.Context(), tenant.ID, *row.Subject)
	if err != nil {
		http.Error(w, "the account this sign-in was for is no longer available", http.StatusForbidden)
		return
	}
	if user.Status != model.StatusActive {
		// Disabled between signing in and arriving here. Rare, and the one
		// case where letting it through would hand out an assertion for an
		// account somebody had just switched off.
		http.Error(w, "this account has been disabled", http.StatusForbidden)
		return
	}

	req := &saml.IdpAuthnRequest{
		IDP:           idp,
		HTTPRequest:   r,
		RequestBuffer: []byte(row.RequestXml),
		RelayState:    row.RelayState,
		// Freshness is judged against the moment Portico accepted the
		// request, not now. The library refuses an authentication request
		// more than ninety seconds older than its issue instant — which is
		// correct at the front door and impossible here, because ninety
		// seconds is less time than a person takes to find a password. What
		// bounds the wait is this row's own expiry.
		Now: row.CreatedAt,
	}
	if err := req.Validate(); err != nil {
		http.Error(w, "this authentication request is no longer valid", http.StatusBadRequest)
		return
	}

	// The assertion itself is stamped now, so its validity window starts
	// when it is issued rather than when the request arrived.
	req.Now = saml.TimeNow()

	session, err := p.session(r.Context(), user, tenant, row.SpEntityID)
	if err != nil {
		http.Error(w, "could not build the session", http.StatusInternalServerError)
		return
	}

	if err := (saml.DefaultAssertionMaker{}).MakeAssertion(req, session); err != nil {
		http.Error(w, "could not build the assertion", http.StatusInternalServerError)
		return
	}

	// Spent. An assertion has been minted for this request, and a request
	// that could mint a second one is a request somebody could replay.
	if err := q.DeleteSAMLAuthRequest(r.Context(), row.ID); err != nil {
		http.Error(w, "could not complete the sign-in", http.StatusInternalServerError)
		return
	}

	// The one response in this application that is allowed to post a form to
	// another origin and run an inline script. Set before WriteResponse,
	// because headers cannot be changed once a body has begun.
	//
	// Validate resolves the assertion consumer service or fails, so a nil
	// endpoint here should be unreachable. It is checked anyway: an empty
	// form-action is a malformed directive, under which the browser blocks
	// the form exactly as it did before this policy existed — and "should be
	// unreachable, so it does not matter what happens" is the reasoning that
	// produced that bug in the first place.
	if req.ACSEndpoint == nil {
		http.Error(w, "the assertion has nowhere to be delivered", http.StatusInternalServerError)
		return
	}
	applyPostBindingHeaders(w, req.ACSEndpoint.Location)

	if err := req.WriteResponse(w); err != nil {
		// WriteResponse has already begun writing an HTML form by the time
		// most failures happen, so there is nothing useful left to say to
		// the browser.
		return
	}
}

// session describes the person to the service provider.
func (p *Providers) session(ctx context.Context, user model.User, tenant model.Tenant, spEntityID string) (*saml.Session, error) {
	index, err := sessionIndex()
	if err != nil {
		return nil, err
	}

	out, err := p.outboundFor(ctx, tenant.ID, spEntityID)
	if err != nil {
		return nil, err
	}
	attrs, err := p.attributes(ctx, user, tenant, out)
	if err != nil {
		return nil, err
	}

	now := store.Now()
	return &saml.Session{
		ID:         index,
		CreateTime: now,
		ExpireTime: now.Add(model.SAMLAssertionLifetime),
		Index:      index,

		// The subject is the account id, not the username. A username can be
		// changed by an administrator; a service provider keying its local
		// record on one would silently create a second account for the same
		// person the day it was.
		NameID:       user.ID,
		NameIDFormat: string(saml.PersistentNameIDFormat),
		SubjectID:    user.ID,

		// UserName, UserEmail, and UserCommonName are deliberately left
		// unset, and it is not that Portico does not state those facts.
		//
		// crewjam's assertion maker turns each of those fields into an
		// attribute of its own, using the same OASIS names the list below
		// uses. Setting both sent `uid` and `mail` twice — and, because
		// UserEmail alone is enough to trigger it, a copy of the address as
		// `eduPersonPrincipalName`, a name from a federation this product
		// has nothing to do with. A service provider that maps on the
		// attribute name got whichever of the pair its parser happened to
		// keep.
		//
		// So there is one source, and it is the list below.
		//
		// Portico's own attributes match the claims the OpenID Provider puts
		// in a token, so a service provider integrated over one protocol
		// sees the same facts over the other.
		CustomAttributes: attrs,
	}, nil
}

func sessionIndex() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session index: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// CompletionSecretParam names the one-time secret in the callback URL.
//
// Short because it sits beside the id in an address bar, and opaque because
// naming it "token" would invite somebody to try their access token in it.
const CompletionSecretParam = "s"

// newCompletionSecret returns the value the callback must present.
//
// 32 bytes, which is not a size chosen for guessing — an attacker gets one
// attempt per request and the request is gone after it — but because it also
// has to survive being in a URL that may end up somewhere it should not, for
// as long as that URL is anywhere.
func newCompletionSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashSecret is what is stored. A value that mints an assertion should not
// be readable in a database dump, on the same terms as an authorization
// code.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// matchesCompletionSecret reports whether presented is the secret this row
// was completed with. Constant time, and an empty stored secret never
// matches — a request from before this existed, or one that somehow reached
// the callback without being completed, has nothing to compare against and
// must not be treated as having passed.
func matchesCompletionSecret(stored, presented string) bool {
	if stored == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(hashSecret(presented))) == 1
}

func newRequestID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand does not fail on any platform this runs on, and a
		// predictable id here would be a request somebody else could
		// complete.
		panic("samlp: crypto/rand is unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// attributes are the facts Portico states about a person, beyond the name
// identifier.
//
// The names are the ones SAML deployments already agree on where such an
// agreement exists — the OASIS X.500 attribute profile for the personal
// details — and Portico's own, unprefixed, where it does not. A service
// provider maps them by name, so inventing new names for `mail` or
// `displayName` would mean every integration needing a custom mapping for
// facts every directory already publishes.
// outboundFor reads what one service provider is configured to receive.
//
// An error is returned rather than swallowed, for the reason the OpenID
// Provider gives: a suppression is somebody's decision that this service
// provider must not receive a field, and falling back to the defaults would
// send it anyway.
func (p *Providers) outboundFor(ctx context.Context, tenantID, spEntityID string) (service.Outbound, error) {
	if p.mappings == nil || spEntityID == "" {
		return service.Outbound{}, nil
	}
	sp, err := p.providers.Get(ctx, tenantID, spEntityID)
	if err != nil {
		return service.Outbound{}, err
	}
	return p.mappings.OutboundFor(ctx, tenantID, store.RecipientRef{SAMLSPID: sp.ID})
}

// attributes is the statement, in the order it has always been written.
//
// Order is preserved deliberately: an assertion is signed, and a service
// provider that logs one for comparison should see the same document it saw
// before anybody configured anything.
func (p *Providers) attributes(ctx context.Context, user model.User, tenant model.Tenant, out service.Outbound) ([]saml.Attribute, error) {
	values := []struct {
		key   string
		value string
	}{
		{"username", user.Username},
		{"display_name", user.DisplayName},
		{"tenant_id", tenant.ID},
		{"tenant_code", tenant.Code},
		{"role", string(user.Role)},
		// The three that are only stated when there is something to state.
		{"email", user.Email},
		{"phone", user.Phone},
		{"organization_id", user.OrganizationID},
		{"organization_name", user.OrganizationName},
	}

	attrs := make([]saml.Attribute, 0, len(values)+1)
	for _, v := range values {
		if v.value == "" && v.key != "username" && v.key != "display_name" &&
			v.key != "tenant_id" && v.key != "tenant_code" && v.key != "role" {
			continue
		}
		attr, send, aliased := service.AttributeFor(out, v.key)
		if !send {
			continue
		}
		attrs = append(attrs, stringAttribute(attr.Name, attr.FriendlyName, v.value))
		if aliased {
			// cn, beside displayName and carrying the same value. See
			// service.SAMLCommonName for why it follows rather than leads.
			attrs = append(attrs, stringAttribute(
				service.SAMLCommonName.Name, service.SAMLCommonName.FriendlyName, v.value))
		}
	}

	if p.catalogue == nil {
		return attrs, nil
	}
	added, err := p.catalogue.SAMLAdditions(ctx, tenant.ID, user, out)
	if err != nil {
		return nil, err
	}
	for _, a := range added {
		attrs = append(attrs, stringAttribute(a.Attribute.Name, a.Attribute.FriendlyName, a.Value))
	}
	return attrs, nil
}

func stringAttribute(name, friendlyName, value string) saml.Attribute {
	return saml.Attribute{
		Name:         name,
		FriendlyName: friendlyName,
		NameFormat:   "urn:oasis:names:tc:SAML:2.0:attrname-format:uri",
		Values: []saml.AttributeValue{{
			Type:  "xs:string",
			Value: value,
		}},
	}
}
