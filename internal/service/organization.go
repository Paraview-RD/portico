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

// OrganizationService owns the organization tree and membership.
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
// Flat on the wire, each row naming its parent; the tree is assembled for
// display. There is no pagination, and that is not a size assumption: a page
// boundary drawn through a tree separates children from their parent and
// leaves something that is neither a tree nor a list.
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
	Name   string
	Code   string
	Remark string
	// ParentID is empty for a root. On an update it is the move: a
	// different value reparents, an empty one promotes to a root.
	ParentID  string
	SortOrder int
}

// MaxOrganizationDepth bounds how deep the tree may go.
//
// Not a schema constraint, because the schema cannot express it, and not
// arbitrary either: every check that walks upwards has to stop somewhere,
// and a bound that is never reached in practice is what makes those walks
// safe to write as simple loops. Ten is far past any organization chart
// anybody navigates willingly.
const MaxOrganizationDepth = 10

// ErrOrganizationCycle is returned when a move would put an organization
// inside itself.
var ErrOrganizationCycle = httpx.BadRequest("ORGANIZATION_CYCLE",
	"That would put the organization inside itself or one of its own descendants.")

// ErrOrganizationTooDeep is returned when a move would exceed the depth
// limit.
var ErrOrganizationTooDeep = httpx.BadRequest("ORGANIZATION_TOO_DEEP",
	fmt.Sprintf("Organizations may be nested at most %d levels deep.", MaxOrganizationDepth))

// resolveParent validates a proposed parent and returns it in the form the
// column takes.
//
// childID is empty when creating, in which case there is nothing to be a
// cycle with. On a move it is the organization being moved, and the walk
// upwards from the proposed parent must not meet it.
//
// The walk is a bounded loop of single-row lookups rather than a recursive
// query. At a depth of ten that is ten indexed reads on the one operation
// that moves an organization, which is not a hot path — and it reads as what
// it is, which a recursive CTE does not.
func (s *OrganizationService) resolveParent(ctx context.Context, q *store.Scoped, childID, parentID string) (*string, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil, nil
	}
	if parentID == childID {
		return nil, ErrOrganizationCycle
	}

	parent, err := q.GetOrganizationByID(ctx, parentID)
	if err != nil {
		if store.IsNoRows(err) {
			return nil, ErrOrganizationNotFound
		}
		return nil, fmt.Errorf("get parent organization: %w", err)
	}

	// Walk up from the proposed parent. Meeting childID means the parent is
	// somewhere below it, so the move would detach a subtree into a loop —
	// which the foreign key cannot catch, because every row in a cycle
	// satisfies it individually.
	// ancestors counts the proposed parent and everything above it, so the
	// child would sit at ancestors+1. The loop is bounded independently of
	// that limit as well: if a cycle ever did reach the table, an unbounded
	// walk would spin until the connection died, which is a far worse
	// failure than the one being checked for.
	current := parent
	ancestors := 1
	for ancestors <= MaxOrganizationDepth {
		if childID != "" && current.ID == childID {
			return nil, ErrOrganizationCycle
		}
		if current.ParentID == nil {
			break
		}

		current, err = q.GetOrganizationByID(ctx, *current.ParentID)
		if err != nil {
			// A dangling parent is not a state this system produces; the
			// foreign key forbids it. Reporting it as a cycle would be a
			// lie, so it surfaces as what it is.
			return nil, fmt.Errorf("walk organization ancestry: %w", err)
		}
		ancestors++
	}

	if ancestors >= MaxOrganizationDepth {
		return nil, ErrOrganizationTooDeep
	}
	return &parent.ID, nil
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

	parentID, err := s.resolveParent(ctx, q, "", in.ParentID)
	if err != nil {
		return model.Organization{}, err
	}

	now := store.Now()
	id := uuid.NewString()
	err = q.CreateOrganization(ctx, sqlcgen.CreateOrganizationParams{
		ID:        id,
		Name:      in.Name,
		Code:      in.Code,
		Remark:    strings.TrimSpace(in.Remark),
		ParentID:  parentID,
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

// Update changes an organization's name, remark, parent, and ordering.
//
// The code is immutable: downstream systems may have stored it, and letting
// it change would silently break those references. The parent is not — an
// organization chart is exactly the thing that gets rearranged — but a move
// that would put an organization inside its own subtree is refused.
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

	parentID, err := s.resolveParent(ctx, q, id, in.ParentID)
	if err != nil {
		return model.Organization{}, err
	}

	err = q.UpdateOrganization(ctx, sqlcgen.UpdateOrganizationParams{
		ID:        id,
		Name:      in.Name,
		Remark:    strings.TrimSpace(in.Remark),
		ParentID:  parentID,
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
	org := model.Organization{
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
	if row.ParentID != nil {
		org.ParentID = *row.ParentID
	}
	return org
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
