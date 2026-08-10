package scim

import (
	"strings"

	"github.com/Paraview-RD/portico/internal/model"
)

// Group is the SCIM representation of a group.
//
// Portico's own groups are deliberately thin — a name, a description, and a
// set of people — and this maps onto them one for one. Nothing here carries
// permissions, because membership grants nothing: a directory says who
// somebody is, not what they may do.
type Group struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName"`
	Members     []Member `json:"members"`
	Meta        Meta     `json:"meta"`
}

// Member is one entry in a group's membership.
type Member struct {
	// Value is the member's id, which is what a client sends and matches on.
	Value string `json:"value"`
	// Display is a convenience for a human reading the response. Ignored on
	// the way in: a client that sent a display name and expected it to
	// resolve to somebody would be relying on this server guessing.
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
	Type    string `json:"type,omitempty"`
}

// SchemaGroup is the core group schema URN.
const SchemaGroup = "urn:ietf:params:scim:schemas:core:2.0:Group"

// GroupFromModel renders a group as a SCIM resource.
//
// Members is never null, on the same reasoning as a list response's
// Resources: an empty group is an empty array, and at least one client reads
// null as a protocol error rather than as "nobody is in it".
func GroupFromModel(g model.Group, members []model.GroupMember, baseURL string) Group {
	base := strings.TrimSuffix(baseURL, "/")

	out := Group{
		Schemas:     []string{SchemaGroup},
		ID:          g.ID,
		ExternalID:  g.ExternalID,
		DisplayName: g.DisplayName,
		Members:     make([]Member, 0, len(members)),
		Meta: Meta{
			ResourceType: "Group",
			Created:      g.CreatedAt,
			LastModified: g.UpdatedAt,
			Location:     base + "/Groups/" + g.ID,
		},
	}

	for _, member := range members {
		out.Members = append(out.Members, Member{
			Value:   member.UserID,
			Display: member.DisplayName,
			Ref:     base + "/Users/" + member.UserID,
			Type:    "User",
		})
	}
	return out
}

// GroupListResponse is SCIM's paged collection of groups.
type GroupListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []Group  `json:"Resources"`
}

// NewGroupListResponse builds a group list response.
func NewGroupListResponse(groups []Group, total, startIndex int) GroupListResponse {
	if groups == nil {
		groups = []Group{}
	}
	return GroupListResponse{
		Schemas:      []string{SchemaListResponse},
		TotalResults: total,
		StartIndex:   startIndex,
		ItemsPerPage: len(groups),
		Resources:    groups,
	}
}

// MemberIDs extracts the ids from a member list, dropping blanks.
//
// Only Value is read. A client that sent a display name without a value has
// not identified anybody, and resolving one by name would mean this server
// guessing which of two people called the same thing was meant.
func MemberIDs(members []Member) []string {
	ids := make([]string, 0, len(members))
	for _, member := range members {
		if id := strings.TrimSpace(member.Value); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
