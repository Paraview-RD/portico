package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// MaxInvitationQuota bounds how many redemptions a single code may grant.
// Comfortably above any real deployment's needs, and — the reason it exists
// at all — safely below the int32 the database column stores, so the
// conversion in Create can never wrap.
const MaxInvitationQuota = 1_000_000

// ErrInvitationNotFound is returned by administrative reads (List/Disable),
// distinct from ErrInvitationNotUsable which is what a redeeming
// registration sees — see the comment on that error for why the two are not
// merged.
var ErrInvitationNotFound = httpx.NotFound("INVITATION_NOT_FOUND", "No such invitation.")

// InvitationService manages invitation codes: administrator-issued,
// quota-limited credentials that let self-registration stay closed to the
// public while still admitting specific people without an administrator
// creating each account by hand.
//
// Redemption itself — validating a code and spending it — lives in
// UserService.Register, not here, because it has to run in the same
// database transaction as the account it pays for. This service only
// handles the administrative side: issuing, listing, and disabling codes.
// See docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md.
type InvitationService struct {
	store *store.Store
	audit *AuditService
}

// NewInvitationService constructs an InvitationService.
func NewInvitationService(st *store.Store, audit *AuditService) *InvitationService {
	return &InvitationService{store: st, audit: audit}
}

// CreateInvitationInput describes a new invitation code.
type CreateInvitationInput struct {
	Code           string
	OrganizationID string
	GroupIDs       []string
	Quota          int
	ExpiresAt      *time.Time
}

// Create issues a new invitation code. OrganizationID and GroupIDs, if
// given, must exist in this tenant now — and are checked again at
// redemption time, since either could be removed in between issuing the
// code and somebody using it.
func (s *InvitationService) Create(ctx context.Context, actor auth.Principal, in CreateInvitationInput) (model.Invitation, error) {
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return model.Invitation{}, httpx.BadRequest("CODE_REQUIRED", "An invitation code is required.")
	}
	if in.Quota <= 0 || in.Quota > MaxInvitationQuota {
		return model.Invitation{}, httpx.BadRequest("INVALID_QUOTA",
			fmt.Sprintf("Quota must be between 1 and %d.", MaxInvitationQuota))
	}

	q := s.store.ForTenant(actor.TenantID)

	var orgID *string
	if trimmed := strings.TrimSpace(in.OrganizationID); trimmed != "" {
		org, err := q.GetOrganizationByID(ctx, trimmed)
		if err != nil {
			if store.IsNoRows(err) {
				return model.Invitation{}, ErrOrganizationNotFound
			}
			return model.Invitation{}, fmt.Errorf("get organization: %w", err)
		}
		if model.Status(org.Status) != model.StatusActive {
			return model.Invitation{}, ErrOrganizationDisabled
		}
		orgID = &org.ID
	}

	groupIDs := in.GroupIDs
	if groupIDs == nil {
		groupIDs = []string{}
	}
	for _, groupID := range groupIDs {
		if _, err := q.GetGroup(ctx, groupID); err != nil {
			if store.IsNoRows(err) {
				return model.Invitation{}, httpx.BadRequest("GROUP_NOT_FOUND", "No such group.")
			}
			return model.Invitation{}, fmt.Errorf("get group %s: %w", groupID, err)
		}
	}

	now := store.Now()
	id := uuid.NewString()
	err := q.CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
		ID: id, Code: code, OrganizationID: orgID, GroupIds: groupIDs,
		Quota: int32(in.Quota), ExpiresAt: in.ExpiresAt, Status: "ACTIVE", CreatedAt: now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.Invitation{}, httpx.Conflict("CODE_TAKEN", "That invitation code is already in use.")
		}
		return model.Invitation{}, fmt.Errorf("create invitation: %w", err)
	}

	row, err := q.GetInvitation(ctx, id)
	if err != nil {
		return model.Invitation{}, fmt.Errorf("get invitation: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionInvitationCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "INVITATION", TargetID: id, TargetName: code,
	})

	return toInvitation(row), nil
}

// List returns every invitation code issued in this tenant, most recent
// first.
func (s *InvitationService) List(ctx context.Context, tenantID string) ([]model.Invitation, error) {
	rows, err := s.store.ForTenant(tenantID).ListInvitations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	out := make([]model.Invitation, len(rows))
	for i, row := range rows {
		out[i] = toInvitation(row)
	}
	return out, nil
}

// Disable an invitation code. This is a terminal decision — see the ADR —
// there is no operation that returns a code to ACTIVE. An administrator who
// wants the same access available again issues a new code.
func (s *InvitationService) Disable(ctx context.Context, actor auth.Principal, id string) (model.Invitation, error) {
	q := s.store.ForTenant(actor.TenantID)
	row, err := q.GetInvitation(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.Invitation{}, ErrInvitationNotFound
		}
		return model.Invitation{}, fmt.Errorf("get invitation: %w", err)
	}

	now := store.Now()
	if err := q.UpdateInvitationStatus(ctx, id, "DISABLED", now); err != nil {
		return model.Invitation{}, fmt.Errorf("disable invitation: %w", err)
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionInvitationDisable,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "INVITATION", TargetID: id, TargetName: row.Code,
	})

	row.Status = "DISABLED"
	row.UpdatedAt = now
	return toInvitation(row), nil
}

// invitationUsable reports whether row can be redeemed right now, checking
// every reason except quota — quota is re-checked atomically by
// RedeemInvitation itself, in the same statement as the increment, because
// a check made here would be stale by the time a transaction opens. This is
// a fast pre-check so an obviously dead code (disabled, or past its
// expiry) never causes a transaction to be opened just to fail inside it.
func invitationUsable(row sqlcgen.Invitation, now time.Time) error {
	if row.Status != "ACTIVE" {
		return ErrInvitationNotUsable
	}
	if row.ExpiresAt != nil && !row.ExpiresAt.After(now) {
		return ErrInvitationNotUsable
	}
	if row.UsedCount >= row.Quota {
		return ErrInvitationNotUsable
	}
	return nil
}

func toInvitation(row sqlcgen.Invitation) model.Invitation {
	return model.Invitation{
		ID:             row.ID,
		TenantID:       row.TenantID,
		Code:           row.Code,
		OrganizationID: deref(row.OrganizationID),
		GroupIDs:       row.GroupIds,
		Quota:          int(row.Quota),
		UsedCount:      int(row.UsedCount),
		ExpiresAt:      row.ExpiresAt,
		Status:         row.Status,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
