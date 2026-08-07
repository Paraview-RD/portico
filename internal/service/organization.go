package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// ErrOrganizationCodeTaken is returned when a code is already in use within
// the tenant. Codes are unique per tenant, not globally: two tenants both
// having a "SALES" is expected.
var ErrOrganizationCodeTaken = httpx.Conflict("ORGANIZATION_CODE_TAKEN",
	"That organization code is already in use.")

// OrganizationService owns the flat organization list and membership.
type OrganizationService struct {
	store *store.Store
	audit *AuditService
}

// NewOrganizationService wires an OrganizationService.
func NewOrganizationService(st *store.Store, audit *AuditService) *OrganizationService {
	return &OrganizationService{store: st, audit: audit}
}

// List returns every organization in the tenant in display order, with
// member counts.
//
// The MVP has no hierarchy and no pagination here: the list is expected to
// be small enough to render whole (§3.4).
func (s *OrganizationService) List(ctx context.Context, tenantID string, activeOnly bool) ([]model.Organization, error) {
	q := s.store.ForTenant(tenantID)

	var (
		rows []sqlcgen.Organization
		err  error
	)
	if activeOnly {
		rows, err = q.ListActiveOrganizations(ctx)
	} else {
		rows, err = q.ListOrganizations(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}

	counts, err := s.memberCounts(ctx, q)
	if err != nil {
		return nil, err
	}

	orgs := make([]model.Organization, 0, len(rows))
	for _, row := range rows {
		orgs = append(orgs, toOrganization(row, counts[row.ID]))
	}
	return orgs, nil
}

// Get returns one organization.
func (s *OrganizationService) Get(ctx context.Context, tenantID, id string) (model.Organization, error) {
	q := s.store.ForTenant(tenantID)

	row, err := q.GetOrganizationByID(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.Organization{}, ErrOrganizationNotFound
		}
		return model.Organization{}, fmt.Errorf("get organization: %w", err)
	}

	count, err := q.CountUsersByOrganization(ctx, &id)
	if err != nil {
		return model.Organization{}, fmt.Errorf("count members: %w", err)
	}
	return toOrganization(row, count), nil
}

// OrganizationInput is the writable part of an organization.
type OrganizationInput struct {
	Name      string
	Code      string
	Remark    string
	SortOrder int
}

// Create adds an organization to the actor's tenant.
func (s *OrganizationService) Create(ctx context.Context, actor auth.Principal, in OrganizationInput) (model.Organization, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Code = strings.TrimSpace(in.Code)

	if in.Name == "" {
		return model.Organization{}, httpx.BadRequest("NAME_REQUIRED", "An organization name is required.")
	}
	if err := validateOrganizationCode(in.Code); err != nil {
		return model.Organization{}, err
	}

	q := s.store.ForTenant(actor.TenantID)

	if _, err := q.GetOrganizationByCode(ctx, in.Code); err == nil {
		return model.Organization{}, ErrOrganizationCodeTaken
	} else if !store.IsNoRows(err) {
		return model.Organization{}, fmt.Errorf("check organization code: %w", err)
	}

	now := store.Now()
	id := uuid.NewString()
	err := q.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
		ID:        id,
		Name:      in.Name,
		Code:      in.Code,
		Remark:    strings.TrimSpace(in.Remark),
		Status:    string(model.StatusActive),
		SortOrder: int64(in.SortOrder),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.Organization{}, ErrOrganizationCodeTaken
		}
		return model.Organization{}, fmt.Errorf("create organization: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOrganization, Action: model.ActionOrgCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "ORGANIZATION", TargetID: id, TargetName: in.Name,
	})

	return s.Get(ctx, actor.TenantID, id)
}

// Update changes an organization's name, remark, and ordering.
//
// The code is immutable: downstream systems may have stored it, and letting
// it change would silently break those references.
func (s *OrganizationService) Update(ctx context.Context, actor auth.Principal, id string, in OrganizationInput) (model.Organization, error) {
	q := s.store.ForTenant(actor.TenantID)

	if _, err := q.GetOrganizationByID(ctx, id); err != nil {
		if store.IsNoRows(err) {
			return model.Organization{}, ErrOrganizationNotFound
		}
		return model.Organization{}, fmt.Errorf("get organization: %w", err)
	}

	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return model.Organization{}, httpx.BadRequest("NAME_REQUIRED", "An organization name is required.")
	}

	err := q.UpdateOrganization(ctx, sqlcgen.UpdateOrganizationParams{
		ID:        id,
		Name:      in.Name,
		Remark:    strings.TrimSpace(in.Remark),
		SortOrder: int64(in.SortOrder),
		UpdatedAt: store.Now(),
	})
	if err != nil {
		return model.Organization{}, fmt.Errorf("update organization: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOrganization, Action: model.ActionOrgUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "ORGANIZATION", TargetID: id, TargetName: in.Name,
	})

	return s.Get(ctx, actor.TenantID, id)
}

// SetStatus enables or disables an organization.
//
// Disabling keeps existing members in place and only blocks new assignments
// (§3.4.1). Members are deliberately not detached: doing so would silently
// erase the record of who belonged where.
func (s *OrganizationService) SetStatus(ctx context.Context, actor auth.Principal, id string, status model.Status) (model.Organization, error) {
	if !status.Valid() {
		return model.Organization{}, httpx.BadRequest("INVALID_STATUS", "Status must be ACTIVE or DISABLED.")
	}

	q := s.store.ForTenant(actor.TenantID)

	current, err := q.GetOrganizationByID(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.Organization{}, ErrOrganizationNotFound
		}
		return model.Organization{}, fmt.Errorf("get organization: %w", err)
	}

	err = q.UpdateOrganizationStatus(ctx, sqlcgen.UpdateOrganizationStatusParams{
		ID:        id,
		Status:    string(status),
		UpdatedAt: store.Now(),
	})
	if err != nil {
		return model.Organization{}, fmt.Errorf("update organization status: %w", err)
	}

	action := model.ActionOrgEnable
	if status == model.StatusDisabled {
		action = model.ActionOrgDisable
	}
	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOrganization, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "ORGANIZATION", TargetID: id, TargetName: current.Name,
	})

	return s.Get(ctx, actor.TenantID, id)
}

// memberCounts returns the number of users in each organization, in one
// query rather than one per organization.
func (s *OrganizationService) memberCounts(ctx context.Context, q *store.Scoped) (map[string]int64, error) {
	rows, err := q.CountUsersPerOrganization(ctx)
	if err != nil {
		return nil, fmt.Errorf("count organization members: %w", err)
	}

	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		if row.OrganizationID == nil {
			continue
		}
		counts[*row.OrganizationID] = row.MemberCount
	}
	return counts, nil
}

func toOrganization(row sqlcgen.Organization, userCount int64) model.Organization {
	return model.Organization{
		ID:        row.ID,
		Name:      row.Name,
		Code:      row.Code,
		Remark:    row.Remark,
		Status:    model.Status(row.Status),
		SortOrder: int(row.SortOrder),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		UserCount: userCount,
	}
}

func validateOrganizationCode(code string) error {
	if code == "" {
		return httpx.BadRequest("CODE_REQUIRED", "An organization code is required.")
	}
	if len(code) > 64 {
		return httpx.BadRequest("INVALID_CODE", "Organization code must be at most 64 characters.")
	}
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return httpx.BadRequest("INVALID_CODE",
				"Organization code may contain only letters, digits, and the characters . _ -")
		}
	}
	return nil
}
