package samlp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/Paraview-RD/portico/internal/model"
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

	session, err := p.session(user, tenant)
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
func (p *Providers) session(user model.User, tenant model.Tenant) (*saml.Session, error) {
	index, err := sessionIndex()
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
		CustomAttributes: attributes(user, tenant),
	}, nil
}

func sessionIndex() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session index: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
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
func attributes(user model.User, tenant model.Tenant) []saml.Attribute {
	attrs := []saml.Attribute{
		stringAttribute("urn:oid:0.9.2342.19200300.100.1.1", "uid", user.Username),
		stringAttribute("urn:oid:2.16.840.1.113730.3.1.241", "displayName", user.DisplayName),
		// cn carries the same value as displayName, which looks redundant
		// and is kept on purpose: it was in every assertion 0.1 issued —
		// crewjam derived it from the session — and it is the name a good
		// many service providers map by default. Dropping it while removing
		// the duplicates would have been a silent break for them.
		stringAttribute("urn:oid:2.5.4.3", "cn", user.DisplayName),
		stringAttribute("tenant_id", "tenantId", tenant.ID),
		stringAttribute("tenant_code", "tenantCode", tenant.Code),
		stringAttribute("role", "role", string(user.Role)),
	}
	if user.Email != "" {
		attrs = append(attrs, stringAttribute("urn:oid:0.9.2342.19200300.100.1.3", "mail", user.Email))
	}
	if user.Phone != "" {
		attrs = append(attrs, stringAttribute("urn:oid:2.5.4.20", "telephoneNumber", user.Phone))
	}
	if user.OrganizationID != "" {
		attrs = append(attrs,
			stringAttribute("organization_id", "organizationId", user.OrganizationID),
			stringAttribute("organization_name", "organizationName", user.OrganizationName))
	}
	return attrs
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
