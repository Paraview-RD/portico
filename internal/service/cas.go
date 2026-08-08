package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// Errors from CAS registration and ticket validation.
var (
	ErrCASServiceNotFound = httpx.NotFound("CAS_SERVICE_NOT_FOUND",
		"No such CAS service.")
	ErrCASServiceTaken = httpx.Conflict("CAS_SERVICE_TAKEN",
		"That URL prefix is already registered in this tenant.")
	// ErrCASServiceNotRegistered is what an unregistered `service` parameter
	// gets. It is deliberately the same answer as a disabled one: a caller
	// probing for which services exist learns nothing either way.
	ErrCASServiceNotRegistered = httpx.Forbidden("CAS_SERVICE_NOT_REGISTERED",
		"That service is not registered with this server.")
)

// CASService issues and validates CAS service tickets.
//
// There is no ticket-granting ticket. CAS's own design puts one in a
// long-lived cookie so a browser can obtain further tickets without signing
// in again — but Portico already has a session for exactly that, and a
// second long-lived credential would be a third thing that has to be
// revoked when somebody signs out, changes a password, or is disabled.
// Riding on the existing session means those three already cover it.
type CASService struct {
	store *store.Store
	users *UserService
	audit *AuditService
}

// NewCASService wires the service.
func NewCASService(st *store.Store, users *UserService, audit *AuditService) *CASService {
	return &CASService{store: st, users: users, audit: audit}
}

// RegisterCASInput describes a service to register.
type RegisterCASInput struct {
	Name string
	// URLPrefix is what a service parameter must begin with.
	URLPrefix string
}

// Register adds a CAS service to the actor's tenant.
func (s *CASService) Register(ctx context.Context, actor auth.Principal, in RegisterCASInput) (model.CASService, error) {
	tenantID := actor.TenantID

	prefix, err := normalizeCASPrefix(in.URLPrefix)
	if err != nil {
		return model.CASService{}, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = prefix
	}

	now := store.Now()
	err = s.store.ForTenant(tenantID).CreateCASService(ctx, sqlcgen.CreateCASServiceParams{
		ID:        uuid.NewString(),
		Name:      name,
		UrlPrefix: prefix,
		Status:    string(model.StatusActive),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.CASService{}, ErrCASServiceTaken
		}
		return model.CASService{}, fmt.Errorf("register CAS service: %w", err)
	}

	registered, err := s.Get(ctx, tenantID, prefix)
	if err != nil {
		return model.CASService{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionCASServiceCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetCASService, TargetID: registered.URLPrefix, TargetName: registered.Name,
		Detail: "URL prefix: " + registered.URLPrefix,
	})

	return registered, nil
}

// UpdateCASInput is the editable part of a CAS registration.
type UpdateCASInput struct {
	Name string
	// URLPrefix may be changed: it is a deployment address rather than an
	// identity, and an application that moves host has to be followable
	// without de-registering it.
	URLPrefix string
}

// Update changes a CAS registration's name and URL prefix.
func (s *CASService) Update(ctx context.Context, actor auth.Principal, currentPrefix string, in UpdateCASInput) (model.CASService, error) {
	tenantID := actor.TenantID

	current, err := s.Get(ctx, tenantID, currentPrefix)
	if err != nil {
		return model.CASService{}, err
	}

	prefix := strings.TrimSpace(in.URLPrefix)
	if prefix == "" {
		prefix = current.URLPrefix
	}
	normalized, err := normalizeCASPrefix(prefix)
	if err != nil {
		return model.CASService{}, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = current.Name
	}

	err = s.store.ForTenant(tenantID).UpdateCASService(
		ctx, currentPrefix, name, normalized, store.Now())
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.CASService{}, ErrCASServiceTaken
		}
		return model.CASService{}, fmt.Errorf("update CAS service: %w", err)
	}

	updated, err := s.Get(ctx, tenantID, normalized)
	if err != nil {
		return model.CASService{}, err
	}

	detail := "URL prefix: " + updated.URLPrefix
	if normalized != current.URLPrefix {
		detail = "URL prefix changed from " + current.URLPrefix + " to " + updated.URLPrefix
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionCASServiceUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetCASService, TargetID: updated.URLPrefix, TargetName: updated.Name,
		Detail: detail,
	})

	return updated, nil
}

// Get returns one registration by its exact prefix.
func (s *CASService) Get(ctx context.Context, tenantID, prefix string) (model.CASService, error) {
	row, err := s.store.ForTenant(tenantID).GetCASService(ctx, prefix)
	if err != nil {
		if store.IsNoRows(err) {
			return model.CASService{}, ErrCASServiceNotFound
		}
		return model.CASService{}, fmt.Errorf("get CAS service: %w", err)
	}
	return toCASService(row), nil
}

// GetByID returns one registration by its own id. See
// SAMLServiceProviderService.GetByID for why the console addresses
// registrations this way rather than by URL prefix.
func (s *CASService) GetByID(ctx context.Context, tenantID, id string) (model.CASService, error) {
	row, err := s.store.ForTenant(tenantID).GetCASServiceByID(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.CASService{}, ErrCASServiceNotFound
		}
		return model.CASService{}, fmt.Errorf("get CAS service: %w", err)
	}
	return toCASService(row), nil
}

// List returns every CAS service in a tenant.
func (s *CASService) List(ctx context.Context, tenantID string) ([]model.CASService, error) {
	rows, err := s.store.ForTenant(tenantID).ListCASServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CAS services: %w", err)
	}

	services := make([]model.CASService, 0, len(rows))
	for _, row := range rows {
		services = append(services, toCASService(row))
	}
	return services, nil
}

// SetStatus enables or disables a CAS service.
func (s *CASService) SetStatus(ctx context.Context, actor auth.Principal, prefix string, status model.Status) (model.CASService, error) {
	tenantID := actor.TenantID

	if !status.Valid() {
		return model.CASService{}, httpx.BadRequest("INVALID_STATUS",
			"Status must be ACTIVE or DISABLED.")
	}
	current, err := s.Get(ctx, tenantID, prefix)
	if err != nil {
		return model.CASService{}, err
	}

	err = s.store.ForTenant(tenantID).UpdateCASServiceStatus(ctx, prefix, string(status), store.Now())
	if err != nil {
		return model.CASService{}, fmt.Errorf("update CAS service status: %w", err)
	}

	action := model.ActionCASServiceEnable
	if status == model.StatusDisabled {
		action = model.ActionCASServiceDisable
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetCASService, TargetID: prefix, TargetName: current.Name,
	})

	return s.Get(ctx, tenantID, prefix)
}

// Match finds the registration covering a service URL, or reports that
// nothing does.
func (s *CASService) Match(ctx context.Context, tenantID, service string) (model.CASService, error) {
	services, err := s.List(ctx, tenantID)
	if err != nil {
		return model.CASService{}, err
	}

	for _, candidate := range services {
		if candidate.Status != model.StatusActive {
			continue
		}
		if MatchCASService(candidate.URLPrefix, service) {
			return candidate, nil
		}
	}
	return model.CASService{}, ErrCASServiceNotRegistered
}

// MatchCASService reports whether a service URL is covered by a registered
// prefix.
//
// A literal prefix match with a boundary. Without the boundary check, a
// registration for https://app.example.com would match
// https://app.example.com.attacker.test — which is the whole reason CAS
// deployments get told to be careful with service matching, and it is not
// something to leave to whoever types the registration.
func MatchCASService(prefix, service string) bool {
	if prefix == "" || !strings.HasPrefix(service, prefix) {
		return false
	}
	if len(service) == len(prefix) {
		return true
	}
	// Longer than the prefix, so the prefix has to have ended at a boundary
	// for the remainder to be inside it rather than alongside it.
	// Registration normalizes a trailing "/" on for exactly this, but the
	// check belongs here too: this function is exported, and a rule enforced
	// only where rows are written is a rule that stops applying the moment
	// somebody writes one another way.
	return strings.HasSuffix(prefix, "/")
}

// normalizeCASPrefix checks a registration and puts it in the one form
// matching expects.
func normalizeCASPrefix(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", httpx.BadRequest("CAS_SERVICE_REQUIRED",
			"A service URL prefix is required.")
	}
	if strings.Contains(raw, "*") {
		return "", httpx.BadRequest("CAS_SERVICE_WILDCARD",
			"Wildcards are not accepted. Register the URL prefix itself; anything beginning with it matches.")
	}

	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return "", httpx.BadRequest("CAS_SERVICE_INVALID",
			"A service URL prefix must be an absolute URL with a host, such as https://app.example.com/.")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", httpx.BadRequest("CAS_SERVICE_INVALID",
			"A service URL prefix must be http or https.")
	}
	if parsed.Scheme == "http" && !isLoopback(parsed.Hostname()) {
		return "", httpx.BadRequest("CAS_SERVICE_INSECURE",
			"A service URL prefix must not use plain http over a network: a ticket delivered there is readable in transit.")
	}
	if parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", httpx.BadRequest("CAS_SERVICE_INVALID",
			"A service URL prefix must not carry a query string or fragment; a service appends its own.")
	}

	// Always ends in a path separator, which is what makes the boundary
	// check above a one-liner instead of a special case.
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	return parsed.String(), nil
}

// IssuedTicket is a service ticket and where to send the browser with it.
type IssuedTicket struct {
	Ticket string
	// RedirectTo is the service URL with the ticket appended, which is what
	// CAS expects the browser to be sent to.
	RedirectTo string
	// ServiceName is what the sign-in screen shows.
	ServiceName string
}

// IssueTicket mints a service ticket for a signed-in person.
func (s *CASService) IssueTicket(ctx context.Context, tenantID, userID, service string) (IssuedTicket, error) {
	registration, err := s.Match(ctx, tenantID, service)
	if err != nil {
		return IssuedTicket{}, err
	}

	ticket, err := newServiceTicket()
	if err != nil {
		return IssuedTicket{}, err
	}

	now := store.Now()
	err = s.store.ForTenant(tenantID).CreateCASTicket(ctx, sqlcgen.CreateCASTicketParams{
		ID:         uuid.NewString(),
		TicketHash: hashTicket(ticket),
		// Stored as presented. Validation compares the service parameter
		// byte for byte against this, which is what the specification
		// requires and what stops a service spending a ticket elsewhere.
		Service:   service,
		Subject:   userID,
		CreatedAt: now,
		ExpiresAt: now.Add(model.CASTicketLifetime),
	})
	if err != nil {
		return IssuedTicket{}, fmt.Errorf("issue service ticket: %w", err)
	}

	return IssuedTicket{
		Ticket:      ticket,
		RedirectTo:  appendTicket(service, ticket),
		ServiceName: registration.Name,
	}, nil
}

// ValidatedTicket is who a spent ticket was about.
type ValidatedTicket struct {
	User model.User
}

// ValidateTicket spends a ticket and reports who it was issued for.
func (s *CASService) ValidateTicket(ctx context.Context, tenantID, ticket, service string) (ValidatedTicket, error) {
	if ticket == "" || service == "" {
		return ValidatedTicket{}, ErrCASTicketInvalid
	}

	row, err := s.store.ForTenant(tenantID).ConsumeCASTicket(ctx, hashTicket(ticket), store.Now())
	if err != nil {
		// Unknown, expired, or already spent — one answer for all three, so
		// that a caller cannot tell a ticket that never existed from one
		// somebody else already used.
		return ValidatedTicket{}, ErrCASTicketInvalid
	}

	// The service must be the one the ticket was issued for. Without this, a
	// service that legitimately receives a ticket could present it to
	// another service's validation and impersonate the person there.
	if row.Service != service {
		return ValidatedTicket{}, ErrCASServiceMismatch
	}

	user, err := s.users.Get(ctx, tenantID, row.Subject)
	if err != nil {
		return ValidatedTicket{}, ErrCASTicketInvalid
	}
	if user.Status != model.StatusActive {
		// Disabled between the ticket being issued and validated. The window
		// is a minute, and letting it through would hand a service a session
		// for an account somebody had just switched off.
		return ValidatedTicket{}, ErrCASTicketInvalid
	}

	return ValidatedTicket{User: user}, nil
}

// The two failures CAS distinguishes, which its own response format names.
var (
	ErrCASTicketInvalid   = fmt.Errorf("cas: the ticket is not valid")
	ErrCASServiceMismatch = fmt.Errorf("cas: the ticket was issued for another service")
)

// SweepExpiredTickets deletes tickets nobody validated.
func (s *CASService) SweepExpiredTickets(ctx context.Context, tenantID string) error {
	return s.store.ForTenant(tenantID).DeleteExpiredCASTickets(ctx, store.Now())
}

// appendTicket puts the ticket on the service URL, keeping whatever query
// string the service already carries.
func appendTicket(service, ticket string) string {
	separator := "?"
	if strings.Contains(service, "?") {
		separator = "&"
	}
	return service + separator + "ticket=" + url.QueryEscape(ticket)
}

// newServiceTicket makes a ticket with the ST- prefix the specification
// requires.
func newServiceTicket() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate service ticket: %w", err)
	}
	return "ST-" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashTicket(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}

func toCASService(row sqlcgen.CasService) model.CASService {
	return model.CASService{
		ID:        row.ID,
		TenantID:  row.TenantID,
		Name:      row.Name,
		URLPrefix: row.UrlPrefix,
		Status:    model.Status(row.Status),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
