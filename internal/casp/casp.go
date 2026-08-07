// Package casp serves the CAS protocol.
//
// Unlike OpenID Connect and SAML, this one is implemented directly rather
// than through a library, and that is a considered difference rather than an
// inconsistency: CAS has no signatures, no canonicalization, and no
// cryptography of any kind. A ticket is an opaque random string, validation
// is a lookup, and the response is a small XML document the specification
// prints in full. There is nothing here a library would be protecting
// anybody from, and no mature Go CAS server to use.
//
// What the protocol does have is two places to get wrong, and both are
// handled in the service layer where they can be tested: which service URLs
// a ticket may be delivered to, and spending a ticket exactly once.
package casp

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// Paths, relative to an issuer. The shapes are the specification's.
const (
	LoginPath = "/cas/login"
	// LogoutPath ends the session. It redirects to Portico's own sign-in
	// screen, which is what actually clears the session: it lives in a token
	// the single-page application holds, and a plain navigation here cannot
	// reach it.
	LogoutPath = "/cas/logout"
	// ValidatePath is CAS 2.0 validation.
	ValidatePath = "/cas/serviceValidate"
	// ValidatePath3 is CAS 3.0 validation, which adds attributes.
	ValidatePath3 = "/cas/p3/serviceValidate"
)

// TenantPathPrefix mirrors the other protocols'.
const TenantPathPrefix = "/t/"

// TenantMount is the path a tenant's endpoints hang off.
func TenantMount(tenantCode string) string { return TenantPathPrefix + tenantCode }

// Server serves the CAS endpoints for every tenant.
type Server struct {
	publicURL string
	tenants   *service.TenantService
	cas       *service.CASService
	audit     *service.AuditService
}

// New wires the server.
func New(publicURL string, tenants *service.TenantService, cas *service.CASService, audit *service.AuditService) *Server {
	return &Server{
		publicURL: strings.TrimSuffix(publicURL, "/"),
		tenants:   tenants,
		cas:       cas,
		audit:     audit,
	}
}

// Paths are the endpoints Portico serves.
//
// Four. Not `/cas/validate`, which is CAS 1.0 and answers with a bare
// "yes\n<username>\n" carrying no attributes and no way to report why a
// ticket failed; nothing needs it that cannot use serviceValidate. Not
// proxy tickets either — see docs/federation.md.
func Paths() []string {
	return []string{LoginPath, LogoutPath, ValidatePath, ValidatePath3}
}

// LoginURL is where a browser is sent to sign in for a CAS service.
func (s *Server) LoginURL(tenantCode, serviceURL string) string {
	query := url.Values{}
	if tenantCode != model.DefaultTenantCode {
		query.Set("tenant", tenantCode)
	}
	query.Set("cas_service", serviceURL)
	return s.publicURL + "/login?" + query.Encode()
}

// Handler serves the CAS endpoints under a mount.
func (s *Server) Handler(mount string) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := model.DefaultTenantCode
		if mount != "" {
			code = strings.TrimPrefix(mount, TenantPathPrefix)
		}

		tenant, err := s.tenants.Resolve(r.Context(), code)
		if err != nil {
			http.Error(w, "unknown tenant", http.StatusNotFound)
			return
		}

		switch strings.TrimSuffix(r.URL.Path, "/") {
		case LoginPath:
			s.serveLogin(w, r, tenant)
		case LogoutPath:
			s.serveLogout(w, r)
		case ValidatePath:
			s.serveValidate(w, r, tenant, false)
		case ValidatePath3:
			s.serveValidate(w, r, tenant, true)
		default:
			http.NotFound(w, r)
		}
	})
	if mount == "" {
		return inner
	}
	return http.StripPrefix(mount, inner)
}

// serveLogin sends the browser to Portico's own sign-in, having first
// checked that the service is one this tenant will deliver a ticket to.
//
// The check happens here rather than only at ticket issue so that an
// unregistered service is refused before somebody types a password for it.
func (s *Server) serveLogin(w http.ResponseWriter, r *http.Request, tenant model.Tenant) {
	serviceURL := r.URL.Query().Get("service")
	if serviceURL == "" {
		// CAS allows a bare /login, which signs somebody in to the CAS
		// server itself with nowhere to go afterwards. Portico's sign-in
		// screen is that, so send them there.
		http.Redirect(w, r, s.publicURL+"/login", http.StatusFound)
		return
	}

	if _, err := s.cas.Match(r.Context(), tenant.ID, serviceURL); err != nil {
		http.Error(w,
			"that service is not registered with this server", http.StatusForbidden)
		return
	}

	http.Redirect(w, r, s.LoginURL(tenant.Code, serviceURL), http.StatusFound)
}

// serveLogout ends the session.
//
// It redirects to Portico's own sign-in screen with a marker, because that
// is where the session actually is: a token the single-page application
// holds, which this endpoint cannot reach from a plain navigation. The
// application signs out on arrival.
//
// The `service` parameter is deliberately ignored rather than redirected to.
// The specification makes following it optional and warns about it, and an
// endpoint that redirects anywhere a caller names is an open redirect
// wearing a protocol's clothes.
func (s *Server) serveLogout(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, s.publicURL+"/login?cas_logout=1", http.StatusFound)
}

// serveValidate spends a ticket and reports who it was for.
func (s *Server) serveValidate(w http.ResponseWriter, r *http.Request, tenant model.Tenant, withAttributes bool) {
	query := r.URL.Query()
	ticket := query.Get("ticket")
	serviceURL := query.Get("service")

	if ticket == "" {
		writeFailure(w, "INVALID_REQUEST", "no ticket was presented")
		return
	}
	if serviceURL == "" {
		writeFailure(w, "INVALID_REQUEST", "no service was named")
		return
	}

	validated, err := s.cas.ValidateTicket(r.Context(), tenant.ID, ticket, serviceURL)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCASServiceMismatch):
			writeFailure(w, "INVALID_SERVICE", "the ticket was issued for another service")
		default:
			// Unknown, expired, and already spent are one answer, so that a
			// caller cannot tell them apart.
			writeFailure(w, "INVALID_TICKET", "the ticket is not valid")
		}
		return
	}

	writeSuccess(w, validated.User, tenant, withAttributes)
}

// The CAS 2.0/3.0 response documents. The element and attribute names are
// the specification's, including the cas: prefix, which several clients
// match on literally.
type serviceResponse struct {
	XMLName xml.Name `xml:"cas:serviceResponse"`
	XMLNS   string   `xml:"xmlns:cas,attr"`

	Success *authenticationSuccess `xml:"cas:authenticationSuccess,omitempty"`
	Failure *authenticationFailure `xml:"cas:authenticationFailure,omitempty"`
}

type authenticationSuccess struct {
	User       string      `xml:"cas:user"`
	Attributes *attributes `xml:"cas:attributes,omitempty"`
}

// attributes is the CAS 3.0 addition. The names match the OpenID claims and
// the SAML attributes, so a service integrated over one protocol sees the
// same facts under the same names over another.
type attributes struct {
	DisplayName      string `xml:"cas:displayName,omitempty"`
	Email            string `xml:"cas:email,omitempty"`
	Phone            string `xml:"cas:phone,omitempty"`
	TenantID         string `xml:"cas:tenant_id,omitempty"`
	TenantCode       string `xml:"cas:tenant_code,omitempty"`
	Role             string `xml:"cas:role,omitempty"`
	OrganizationID   string `xml:"cas:organization_id,omitempty"`
	OrganizationName string `xml:"cas:organization_name,omitempty"`
}

type authenticationFailure struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

const casNamespace = "http://www.yale.edu/tp/cas"

func writeSuccess(w http.ResponseWriter, user model.User, tenant model.Tenant, withAttributes bool) {
	success := &authenticationSuccess{
		// The username, not the account id: CAS clients show this to people
		// and key local records on it, and every CAS deployment in existence
		// expects a username here.
		User: user.Username,
	}
	if withAttributes {
		success.Attributes = &attributes{
			DisplayName:      user.DisplayName,
			Email:            user.Email,
			Phone:            user.Phone,
			TenantID:         tenant.ID,
			TenantCode:       tenant.Code,
			Role:             string(user.Role),
			OrganizationID:   user.OrganizationID,
			OrganizationName: user.OrganizationName,
		}
	}
	writeResponse(w, &serviceResponse{XMLNS: casNamespace, Success: success})
}

func writeFailure(w http.ResponseWriter, code, message string) {
	writeResponse(w, &serviceResponse{
		XMLNS:   casNamespace,
		Failure: &authenticationFailure{Code: code, Message: message},
	})
}

// writeResponse always answers 200, including for a failure.
//
// That is the specification's, not an oversight: a CAS client parses the
// document to find out what happened, and several stop reading on a non-200
// and report a transport error instead of the reason.
func writeResponse(w http.ResponseWriter, response *serviceResponse) {
	document, err := xml.MarshalIndent(response, "", "  ")
	if err != nil {
		http.Error(w, "could not build the response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(document)
}

// Authorization is where a browser goes once a person has signed in.
type Authorization struct {
	// RedirectTo is the service URL with a ticket appended.
	RedirectTo string `json:"redirectTo"`
	// ServiceName is shown by the sign-in screen.
	ServiceName string `json:"serviceName"`
}

// Complete issues a ticket for a signed-in person and returns where to send
// the browser.
func (s *Server) Complete(ctx context.Context, actor auth.Principal, serviceURL, ip string) (Authorization, error) {
	issued, err := s.cas.IssueTicket(ctx, actor.TenantID, actor.UserID, serviceURL)
	if err != nil {
		return Authorization{}, err
	}

	s.audit.Log(ctx, actor.TenantID, service.AuditEntry{
		Kind: model.LogLogin, Action: model.ActionCASAuthenticate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "CAS_SERVICE", TargetName: issued.ServiceName,
		Detail: serviceURL,
		IP:     ip,
	})

	return Authorization{
		RedirectTo:  issued.RedirectTo,
		ServiceName: issued.ServiceName,
	}, nil
}

// SweepExpired deletes tickets nobody validated.
func (s *Server) SweepExpired(ctx context.Context) error {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	for _, tenant := range tenants {
		if err := s.cas.SweepExpiredTickets(ctx, tenant.ID); err != nil {
			return fmt.Errorf("sweep tenant %s: %w", tenant.Code, err)
		}
	}
	return nil
}
