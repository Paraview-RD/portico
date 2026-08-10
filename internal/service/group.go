package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/webhook"
)

// GroupService owns groups and their membership.
//
// Groups are not the organization chart, and the two are kept apart on
// purpose — see the schema comment on the groups table. In short: an
// organization is where somebody sits, one of them, in a tree; a group is a
// set they belong to, any number of them, flat, usually maintained by a
// directory.
//
// Membership grants nothing. That is the same boundary the provisioning code
// holds for accounts: a directory says who somebody is, not what they may
// do.
type GroupService struct {
	store  *store.Store
	audit  *AuditService
	events EventPublisher
}

// NewGroupService wires a GroupService.
func NewGroupService(st *store.Store, audit *AuditService) *GroupService {
	return &GroupService{store: st, audit: audit}
}

// WithEvents attaches a publisher, on the same terms as UserService's.
func (s *GroupService) WithEvents(publisher EventPublisher) *GroupService {
	s.events = publisher
	return s
}

func (s *GroupService) publish(ctx context.Context, tenantID, eventType string, group model.Group) {
	if s.events == nil {
		return
	}
	s.events.Publish(ctx, tenantID, eventType, group)
}

// Errors this service returns.
var (
	ErrGroupNotFound  = httpx.NotFound("GROUP_NOT_FOUND", "No such group.")
	ErrGroupNameTaken = httpx.Conflict("GROUP_NAME_TAKEN",
		"A group with that name already exists.")
	ErrGroupExternalIDTaken = httpx.Conflict("GROUP_EXTERNAL_ID_TAKEN",
		"That externalId is already bound to another group.")
	// ErrMemberNotFound is deliberately not a silent skip. A membership push
	// naming an account that does not exist — or that belongs to another
	// tenant, which the composite foreign key catches — has to be reported,
	// because a silently dropped member is a group that looks synchronized
	// and is not.
	ErrMemberNotFound = httpx.BadRequest("MEMBER_NOT_FOUND",
		"One of the members does not exist in this tenant.")
)

// GroupInput is what a caller supplies.
type GroupInput struct {
	DisplayName string
	Description string
	ExternalID  string
}

// Create adds a group.
func (s *GroupService) Create(ctx context.Context, tenantID string, in GroupInput, source model.GroupSource, actor auth.Principal) (model.Group, error) {
	in.normalize()
	if in.DisplayName == "" {
		return model.Group{}, httpx.BadRequest("DISPLAY_NAME_REQUIRED",
			"A group name is required.")
	}

	now := store.Now()
	id := uuid.NewString()

	err := s.store.ForTenant(tenantID).CreateGroup(ctx, sqlcgen.CreateGroupParams{
		ID:          id,
		DisplayName: in.DisplayName,
		Description: in.Description,
		ExternalID:  optional(in.ExternalID),
		Source:      string(source),
		CreatedAt:   now,
	})
	if err != nil {
		return model.Group{}, translateGroupConflict(err)
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionGroupCreate,
		ActorID: actor.UserID, ActorName: actorNameOr(actor, source),
		TargetType: targetGroup, TargetID: id, TargetName: in.DisplayName,
	})

	group, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return model.Group{}, err
	}
	s.publish(ctx, tenantID, webhook.EventGroupCreated, group)
	return group, nil
}

// Get returns one group with its member count.
func (s *GroupService) Get(ctx context.Context, tenantID, id string) (model.Group, error) {
	q := s.store.ForTenant(tenantID)

	row, err := q.GetGroup(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.Group{}, ErrGroupNotFound
		}
		return model.Group{}, fmt.Errorf("get group: %w", err)
	}

	count, err := q.CountGroupMembers(ctx, id)
	if err != nil {
		return model.Group{}, fmt.Errorf("count members: %w", err)
	}
	return groupFromRow(row, count), nil
}

// FindByExternalID resolves the identifier a directory knows a group by.
func (s *GroupService) FindByExternalID(ctx context.Context, tenantID, externalID string) (model.Group, error) {
	row, err := s.store.ForTenant(tenantID).GetGroupByExternalID(ctx, externalID)
	if err != nil {
		if store.IsNoRows(err) {
			return model.Group{}, ErrGroupNotFound
		}
		return model.Group{}, fmt.Errorf("look up group by external id: %w", err)
	}
	return s.Get(ctx, tenantID, row.ID)
}

// FindByDisplayName resolves a group by the name a directory pushes.
func (s *GroupService) FindByDisplayName(ctx context.Context, tenantID, name string) (model.Group, error) {
	row, err := s.store.ForTenant(tenantID).GetGroupByDisplayName(ctx, strings.TrimSpace(name))
	if err != nil {
		if store.IsNoRows(err) {
			return model.Group{}, ErrGroupNotFound
		}
		return model.Group{}, fmt.Errorf("look up group by name: %w", err)
	}
	return s.Get(ctx, tenantID, row.ID)
}

// List returns the tenant's groups with member counts.
func (s *GroupService) List(ctx context.Context, tenantID string) ([]model.Group, error) {
	rows, err := s.store.ForTenant(tenantID).ListGroupsWithMemberCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}

	out := make([]model.Group, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.Group{
			ID: row.ID, DisplayName: row.DisplayName,
			Description: row.Description,
			ExternalID:  deref(row.ExternalID),
			Source:      model.GroupSource(row.Source),
			MemberCount: row.MemberCount,
			CreatedAt:   row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}

// Update changes a group's name, description, and external id.
func (s *GroupService) Update(ctx context.Context, tenantID, id string, in GroupInput, actor auth.Principal) (model.Group, error) {
	in.normalize()
	if in.DisplayName == "" {
		return model.Group{}, httpx.BadRequest("DISPLAY_NAME_REQUIRED",
			"A group name is required.")
	}

	q := s.store.ForTenant(tenantID)
	current, err := q.GetGroup(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.Group{}, ErrGroupNotFound
		}
		return model.Group{}, fmt.Errorf("get group: %w", err)
	}

	// Absent from the request does not mean "unbind" — the same rule as for
	// an account's externalId, and for the same reason: a client that omits
	// it on an update would otherwise detach the group from the directory,
	// and the next sync would create a second one.
	externalID := optional(in.ExternalID)
	if externalID == nil {
		externalID = current.ExternalID
	}

	err = q.UpdateGroup(ctx, sqlcgen.UpdateGroupParams{
		ID:          id,
		DisplayName: in.DisplayName,
		Description: in.Description,
		ExternalID:  externalID,
		UpdatedAt:   store.Now(),
	})
	if err != nil {
		return model.Group{}, translateGroupConflict(err)
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionGroupUpdate,
		ActorID: actor.UserID, ActorName: actorNameOr(actor, model.GroupSource(current.Source)),
		TargetType: targetGroup, TargetID: id, TargetName: in.DisplayName,
	})

	group, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return model.Group{}, err
	}
	s.publish(ctx, tenantID, webhook.EventGroupUpdated, group)
	return group, nil
}

// Delete removes a group and its memberships.
func (s *GroupService) Delete(ctx context.Context, tenantID, id string, actor auth.Principal) error {
	group, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return err
	}

	if err := s.store.ForTenant(tenantID).DeleteGroup(ctx, id); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionGroupDelete,
		ActorID: actor.UserID, ActorName: actorNameOr(actor, group.Source),
		TargetType: targetGroup, TargetID: id, TargetName: group.DisplayName,
	})
	s.publish(ctx, tenantID, webhook.EventGroupDeleted, group)
	return nil
}

// Members returns who is in a group.
func (s *GroupService) Members(ctx context.Context, tenantID, groupID string) ([]model.GroupMember, error) {
	rows, err := s.store.ForTenant(tenantID).ListGroupMembers(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	out := make([]model.GroupMember, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.GroupMember{
			UserID: row.UserID, Username: row.Username, DisplayName: row.DisplayName,
		})
	}
	return out, nil
}

// GroupsForUser returns the groups an account belongs to.
func (s *GroupService) GroupsForUser(ctx context.Context, tenantID, userID string) ([]model.GroupRef, error) {
	rows, err := s.store.ForTenant(tenantID).ListGroupsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list groups for user: %w", err)
	}

	out := make([]model.GroupRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.GroupRef{ID: row.ID, DisplayName: row.DisplayName})
	}
	return out, nil
}

// AddMembers puts accounts into a group.
//
// A member that does not exist fails the whole call rather than being
// skipped. The composite foreign key catches an account from another tenant
// for free; this reports it instead of swallowing it, because a group that
// looks synchronized and quietly lost a member is the failure a directory
// cannot see from its own side.
func (s *GroupService) AddMembers(ctx context.Context, tenantID, groupID string, userIDs []string, actor auth.Principal) error {
	if len(userIDs) == 0 {
		return nil
	}
	q := s.store.ForTenant(tenantID)
	now := store.Now()

	for _, userID := range userIDs {
		if err := q.AddGroupMember(ctx, groupID, userID, now); err != nil {
			if store.IsForeignKeyViolation(err) {
				return ErrMemberNotFound
			}
			return fmt.Errorf("add group member: %w", err)
		}
	}
	return s.recordMembershipChange(ctx, tenantID, groupID, actor, model.ActionGroupMemberAdd, len(userIDs))
}

// RemoveMembers takes accounts out of a group.
func (s *GroupService) RemoveMembers(ctx context.Context, tenantID, groupID string, userIDs []string, actor auth.Principal) error {
	if len(userIDs) == 0 {
		return nil
	}
	q := s.store.ForTenant(tenantID)

	for _, userID := range userIDs {
		if err := q.RemoveGroupMember(ctx, groupID, userID); err != nil {
			return fmt.Errorf("remove group member: %w", err)
		}
	}
	return s.recordMembershipChange(ctx, tenantID, groupID, actor, model.ActionGroupMemberRemove, len(userIDs))
}

// ReplaceMembers sets a group's membership to exactly this list.
//
// One transaction: a replacement that emptied the group and then failed
// halfway through refilling it would leave everybody out, which is the worst
// possible intermediate state for something that decides nothing but is read
// as if it does.
func (s *GroupService) ReplaceMembers(ctx context.Context, tenantID, groupID string, userIDs []string, actor auth.Principal) error {
	now := store.Now()

	err := s.store.WithTx(func(tx *sqlcgen.Queries) error {
		if err := tx.RemoveAllGroupMembers(ctx, sqlcgen.RemoveAllGroupMembersParams{
			TenantID: tenantID, GroupID: groupID,
		}); err != nil {
			return err
		}
		for _, userID := range userIDs {
			if err := tx.AddGroupMember(ctx, sqlcgen.AddGroupMemberParams{
				TenantID: tenantID, GroupID: groupID, UserID: userID, AddedAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if store.IsForeignKeyViolation(err) {
			return ErrMemberNotFound
		}
		return fmt.Errorf("replace group members: %w", err)
	}
	return s.recordMembershipChange(ctx, tenantID, groupID, actor, model.ActionGroupMemberReplace, len(userIDs))
}

// recordMembershipChange audits and announces a membership change.
func (s *GroupService) recordMembershipChange(ctx context.Context, tenantID, groupID string, actor auth.Principal, action string, count int) error {
	group, err := s.Get(ctx, tenantID, groupID)
	if err != nil {
		return err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actorNameOr(actor, group.Source),
		TargetType: targetGroup, TargetID: groupID, TargetName: group.DisplayName,
		Detail: fmt.Sprintf("%d member(s)", count),
	})
	// One event type for membership, carrying the group as it now stands. A
	// subscriber wanting to know who is in a group reads the group; an event
	// per member would make a bulk replacement a burst nobody asked for.
	s.publish(ctx, tenantID, webhook.EventGroupMembersChanged, group)
	return nil
}

const targetGroup = "GROUP"

// actorNameOr names the actor, falling back to the provisioning marker when
// a directory made the change and there is no person to name.
func actorNameOr(actor auth.Principal, source model.GroupSource) string {
	if actor.Username != "" {
		return actor.Username
	}
	if source == model.GroupSourceSCIM {
		return ProvisioningActor
	}
	return ""
}

func (in *GroupInput) normalize() {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Description = strings.TrimSpace(in.Description)
	in.ExternalID = strings.TrimSpace(in.ExternalID)
}

func groupFromRow(row sqlcgen.Group, memberCount int64) model.Group {
	return model.Group{
		ID: row.ID, DisplayName: row.DisplayName, Description: row.Description,
		ExternalID: deref(row.ExternalID), Source: model.GroupSource(row.Source),
		MemberCount: memberCount,
		CreatedAt:   row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// translateGroupConflict names the field that collided, using the constraint
// names the migration declares.
func translateGroupConflict(err error) error {
	if !store.IsUniqueViolation(err) {
		return fmt.Errorf("write group: %w", err)
	}
	switch store.ViolatedConstraint(err) {
	case "uq_groups_tenant_display_name":
		return ErrGroupNameTaken
	case "uq_groups_tenant_external_id":
		return ErrGroupExternalIDTaken
	default:
		return httpx.Conflict("ALREADY_EXISTS",
			"Those details conflict with an existing group.")
	}
}

func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
