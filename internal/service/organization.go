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

// ErrOrganizationCodeTaken is returned when a code is already in use.
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

// List returns every organization in display order, with member counts.
//
// The MVP has no hierarchy and no pagination here: the list is expected to
// be small enough to render whole (§3.4).
func (s *OrganizationService) List(ctx context.Context, activeOnly bool) ([]model.Organization, error) {
	var (
		rows []sqlcgen.Organization
		err  error
	)
	if activeOnly {
		rows, err = s.store.Queries.ListActiveOrganizations(ctx)
	} else {
		rows, err = s.store.Queries.ListOrganizations(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}

	counts, err := s.memberCounts(ctx)
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
func (s *OrganizationService) Get(ctx context.Context, id string) (model.Organization, error) {
	row, err := s.store.Queries.GetOrganizationByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return model.Organization{}, ErrOrganizationNotFound
		}
		return model.Organization{}, fmt.Errorf("get organization: %w", err)
	}

	count, err := s.store.Queries.CountUsersByOrganization(ctx, &id)
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

// Create adds an organization.
func (s *OrganizationService) Create(ctx context.Context, actor auth.Principal, in OrganizationInput) (model.Organization, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Code = strings.TrimSpace(in.Code)

	if in.Name == "" {
		return model.Organization{}, httpx.BadRequest("NAME_REQUIRED", "An organization name is required.")
	}
	if err := validateOrganizationCode(in.Code); err != nil {
		return model.Organization{}, err
	}

	if _, err := s.store.Queries.GetOrganizationByCode(ctx, in.Code); err == nil {
		return model.Organization{}, ErrOrganizationCodeTaken
	} else if !isNoRows(err) {
		return model.Organization{}, fmt.Errorf("check organization code: %w", err)
	}

	now := store.Now()
	id := uuid.NewString()
	err := s.store.Queries.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
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
		if isUniqueViolation(err) {
			return model.Organization{}, ErrOrganizationCodeTaken
		}
		return model.Organization{}, fmt.Errorf("create organization: %w", err)
	}

	s.audit.Log(ctx, AuditEntry{
		Kind: model.LogOrganization, Action: model.ActionOrgCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "ORGANIZATION", TargetID: id, TargetName: in.Name,
	})

	return s.Get(ctx, id)
}

// Update changes an organization's name, remark, and ordering.
//
// The code is immutable: downstream systems may have stored it, and letting
// it change would silently break those references.
func (s *OrganizationService) Update(ctx context.Context, actor auth.Principal, id string, in OrganizationInput) (model.Organization, error) {
	if _, err := s.store.Queries.GetOrganizationByID(ctx, id); err != nil {
		if isNoRows(err) {
			return model.Organization{}, ErrOrganizationNotFound
		}
		return model.Organization{}, fmt.Errorf("get organization: %w", err)
	}

	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return model.Organization{}, httpx.BadRequest("NAME_REQUIRED", "An organization name is required.")
	}

	err := s.store.Queries.UpdateOrganization(ctx, sqlcgen.UpdateOrganizationParams{
		ID:        id,
		Name:      in.Name,
		Remark:    strings.TrimSpace(in.Remark),
		SortOrder: int64(in.SortOrder),
		UpdatedAt: store.Now(),
	})
	if err != nil {
		return model.Organization{}, fmt.Errorf("update organization: %w", err)
	}

	s.audit.Log(ctx, AuditEntry{
		Kind: model.LogOrganization, Action: model.ActionOrgUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "ORGANIZATION", TargetID: id, TargetName: in.Name,
	})

	return s.Get(ctx, id)
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

	current, err := s.store.Queries.GetOrganizationByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return model.Organization{}, ErrOrganizationNotFound
		}
		return model.Organization{}, fmt.Errorf("get organization: %w", err)
	}

	err = s.store.Queries.UpdateOrganizationStatus(ctx, sqlcgen.UpdateOrganizationStatusParams{
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
	s.audit.Log(ctx, AuditEntry{
		Kind: model.LogOrganization, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "ORGANIZATION", TargetID: id, TargetName: current.Name,
	})

	return s.Get(ctx, id)
}

// memberCounts returns the number of users in each organization, in one
// query rather than one per organization.
func (s *OrganizationService) memberCounts(ctx context.Context) (map[string]int64, error) {
	rows, err := s.store.DB().QueryContext(ctx,
		`SELECT organization_id, COUNT(*) FROM users
		 WHERE organization_id IS NOT NULL
		 GROUP BY organization_id`)
	if err != nil {
		return nil, fmt.Errorf("count organization members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string]int64{}
	for rows.Next() {
		var (
			id    string
			count int64
		)
		if err := rows.Scan(&id, &count); err != nil {
			return nil, fmt.Errorf("scan member count: %w", err)
		}
		counts[id] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate member counts: %w", err)
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
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
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
