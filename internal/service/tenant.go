package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Errors from tenant resolution.
//
// These name the tenant rather than hiding behind a generic failure. A
// tenant code is not a credential — it appears in sign-in URLs and in the
// configuration handed to every user of that tenant — so concealing whether
// one exists buys nothing and costs an operator a diagnosable error. Knowing
// a tenant exists still reveals nothing about the accounts in it.
var (
	ErrTenantNotFound = httpx.NotFound("TENANT_NOT_FOUND",
		"No such tenant.")
	ErrTenantDisabled = httpx.Forbidden("TENANT_DISABLED",
		"This tenant is disabled. Contact whoever operates this deployment.")
	ErrTenantCodeTaken = httpx.Conflict("TENANT_CODE_TAKEN",
		"That tenant code is already in use.")
)

// TenantService owns the tenant records themselves.
//
// Unlike every other service it works through the unscoped store, because
// tenants are the root of the isolation hierarchy and have nothing above
// them to be scoped by. Provisioning is a command-line operation performed by
// whoever runs the deployment, which is what lets this have no cross-tenant
// administrator at all.
//
// One method is reachable over HTTP, and only where a deployment asked:
// Overview, behind PORTICO_TENANT_CONSOLE. It returns counts and never
// contents — see internal/handler/tenant_console.go for what stands in front
// of it and why.
type TenantService struct {
	store *store.Store

	// operatorConsole is whether this deployment asked for the screens that
	// see across tenants. False is what the type's comment above describes,
	// and what every deployment gets unless it says otherwise.
	operatorConsole bool
}

// NewTenantService wires a TenantService.
func NewTenantService(st *store.Store) *TenantService {
	return &TenantService{store: st}
}

// WithOperatorConsole records whether this deployment asked for the screens
// that see across tenants.
//
// Held here rather than read from configuration at the point of use, for the
// same reason TrialService holds its own switch: the routes are the real
// gate, and this is what lets everything downstream of them — including the
// console, which has to decide whether to draw a menu entry — ask one place
// whether the feature exists.
func (s *TenantService) WithOperatorConsole(on bool) *TenantService {
	s.operatorConsole = on
	return s
}

// OperatorConsole reports whether the cross-tenant screens are enabled.
func (s *TenantService) OperatorConsole() bool { return s.operatorConsole }

// Resolve looks up a tenant by code for sign-in, registration, and anything
// else that has to establish a tenant before it has a principal.
//
// An empty code means the default tenant, so a single-tenant deployment
// never has to mention tenants.
func (s *TenantService) Resolve(ctx context.Context, code string) (model.Tenant, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		code = model.DefaultTenantCode
	}

	row, err := s.store.Queries.GetTenantByCode(ctx, code)
	if err != nil {
		if store.IsNoRows(err) {
			return model.Tenant{}, ErrTenantNotFound
		}
		return model.Tenant{}, fmt.Errorf("resolve tenant: %w", err)
	}

	tenant := toTenant(row)
	if tenant.Status != model.StatusActive {
		return model.Tenant{}, ErrTenantDisabled
	}
	return tenant, nil
}

// Get returns a tenant by id. Used to attach the tenant's name to a session
// and to confirm a token's tenant still exists and is enabled.
func (s *TenantService) Get(ctx context.Context, id string) (model.Tenant, error) {
	row, err := s.store.Queries.GetTenantByID(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.Tenant{}, ErrTenantNotFound
		}
		return model.Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	return toTenant(row), nil
}

// List returns every tenant, for the provisioning CLI.
func (s *TenantService) List(ctx context.Context) ([]model.Tenant, error) {
	rows, err := s.store.Queries.ListTenants(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}

	tenants := make([]model.Tenant, 0, len(rows))
	for _, row := range rows {
		tenants = append(tenants, toTenant(row))
	}
	return tenants, nil
}

// Overview returns every tenant with a count of what is inside it.
//
// The one read in this system that crosses the tenant boundary, and the
// narrowest crossing that answers the question: how many, never who. What
// stands in front of it is not this method — it is the route, which is not
// registered unless the deployment asked for an operator console and which
// then admits only an administrator of the default tenant.
func (s *TenantService) Overview(ctx context.Context) ([]model.TenantOverview, error) {
	rows, err := s.store.Queries.TenantOverview(ctx)
	if err != nil {
		return nil, fmt.Errorf("tenant overview: %w", err)
	}

	out := make([]model.TenantOverview, 0, len(rows))
	for _, row := range rows {
		overview := model.TenantOverview{
			Tenant: model.Tenant{
				ID:        row.ID,
				Code:      row.Code,
				Name:      row.Name,
				Status:    model.Status(row.Status),
				CreatedAt: row.CreatedAt,
			},
			Users:         row.UserCount,
			ActiveUsers:   row.ActiveUserCount,
			Organizations: row.OrganizationCount,
			Applications:  row.ApplicationCount,
		}
		// max() over an empty set is NULL, which sqlc surfaces as an untyped
		// any rather than a sql.NullTime. A tenant with no audit trail at all
		// is a real and interesting row — one created and never used — so
		// this is a missing value to carry, not one to invent.
		if at, ok := row.LastActivity.(time.Time); ok {
			overview.LastActivity = &at
		}
		out = append(out, overview)
	}
	return out, nil
}

// Create adds a tenant.
func (s *TenantService) Create(ctx context.Context, code, name string) (model.Tenant, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)

	if err := validateTenantCode(code); err != nil {
		return model.Tenant{}, err
	}
	if name == "" {
		name = code
	}

	now := store.Now()
	tenant := model.Tenant{
		ID:        uuid.NewString(),
		Code:      code,
		Name:      name,
		Status:    model.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := s.store.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID:        tenant.ID,
		Code:      tenant.Code,
		Name:      tenant.Name,
		Status:    string(tenant.Status),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.Tenant{}, ErrTenantCodeTaken
		}
		return model.Tenant{}, fmt.Errorf("create tenant: %w", err)
	}
	return tenant, nil
}

// SetStatus enables or disables a tenant. Disabling refuses sign-in but
// keeps every record, so it is reversible.
func (s *TenantService) SetStatus(ctx context.Context, code string, status model.Status) (model.Tenant, error) {
	if !status.Valid() {
		return model.Tenant{}, httpx.BadRequest("INVALID_STATUS", "Status must be ACTIVE or DISABLED.")
	}

	row, err := s.store.Queries.GetTenantByCode(ctx, strings.TrimSpace(code))
	if err != nil {
		if store.IsNoRows(err) {
			return model.Tenant{}, ErrTenantNotFound
		}
		return model.Tenant{}, fmt.Errorf("get tenant: %w", err)
	}

	err = s.store.Queries.UpdateTenantStatus(ctx, sqlcgen.UpdateTenantStatusParams{
		ID:        row.ID,
		Status:    string(status),
		UpdatedAt: store.Now(),
	})
	if err != nil {
		return model.Tenant{}, fmt.Errorf("update tenant status: %w", err)
	}

	return s.Get(ctx, row.ID)
}

// EnsureDefault creates the default tenant if the deployment has none, and
// returns it either way. It runs at every start, so an existing deployment
// is untouched.
func (s *TenantService) EnsureDefault(ctx context.Context) (model.Tenant, error) {
	tenant, err := s.Resolve(ctx, model.DefaultTenantCode)
	switch {
	case err == nil:
		return tenant, nil
	case isNotFound(err):
		return s.Create(ctx, model.DefaultTenantCode, "Default")
	default:
		// A disabled default tenant is a deliberate operator choice — a
		// deployment that has moved on to named tenants — so it is left
		// alone rather than re-enabled behind their back.
		return model.Tenant{}, err
	}
}

func toTenant(row sqlcgen.Tenant) model.Tenant {
	return model.Tenant{
		ID:        row.ID,
		Code:      row.Code,
		Name:      row.Name,
		Status:    model.Status(row.Status),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func isNotFound(err error) bool {
	var apiErr *httpx.Error
	return errors.As(err, &apiErr) && apiErr.Code == "TENANT_NOT_FOUND"
}

func validateTenantCode(code string) error {
	if code == "" {
		return httpx.BadRequest("CODE_REQUIRED", "A tenant code is required.")
	}
	if len(code) < 2 || len(code) > 64 {
		return httpx.BadRequest("INVALID_CODE", "Tenant code must be between 2 and 64 characters.")
	}
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			// Lowercase only, because the code is compared exactly and a
			// tenant reachable as "Acme" but not "acme" is a support ticket
			// waiting to happen.
			return httpx.BadRequest("INVALID_CODE",
				"Tenant code may contain only lowercase letters, digits, hyphens, and underscores.")
		}
	}
	return nil
}
