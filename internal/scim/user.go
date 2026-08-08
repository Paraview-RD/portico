package scim

import (
	"strings"
	"time"

	"github.com/paraview/portico/internal/model"
)

// User is the SCIM representation of an account.
//
// The mapping is deliberately narrow. SCIM's core schema has around thirty
// attributes and Portico stores eight of them; inventing storage for the
// rest so the resource looks complete would mean an identity provider
// pushing values that nothing here reads and nobody can act on, which is
// worse than a resource that plainly does not have them.
type User struct {
	Schemas      []string `json:"schemas"`
	ID           string   `json:"id"`
	ExternalID   string   `json:"externalId,omitempty"`
	UserName     string   `json:"userName"`
	Name         *Name    `json:"name,omitempty"`
	DisplayName  string   `json:"displayName,omitempty"`
	Emails       []Multi  `json:"emails,omitempty"`
	PhoneNumbers []Multi  `json:"phoneNumbers,omitempty"`
	Active       bool     `json:"active"`
	Meta         Meta     `json:"meta"`
}

// Name is SCIM's structured name.
//
// Only formatted is populated. Portico stores one display name and does not
// know which part of it is a family name — a split would be a guess, and a
// guess that is wrong for most of the world's naming conventions.
type Name struct {
	Formatted string `json:"formatted,omitempty"`
}

// Multi is a SCIM multi-valued attribute: emails, phone numbers.
type Multi struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// Meta is SCIM's resource metadata.
type Meta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location"`
}

// FromModel renders an account as a SCIM user.
func FromModel(u model.User, baseURL string) User {
	out := User{
		Schemas:     []string{SchemaUser},
		ID:          u.ID,
		UserName:    u.Username,
		DisplayName: u.DisplayName,
		// SCIM's active is the whole of deprovisioning as a provisioning
		// system sees it, and it maps exactly onto the status this system
		// already had. Anything else would be a second notion of "switched
		// off" that the console and the sync could disagree about.
		Active: u.Status == model.StatusActive,
		Meta: Meta{
			ResourceType: "User",
			Created:      u.CreatedAt,
			LastModified: u.UpdatedAt,
			Location:     strings.TrimSuffix(baseURL, "/") + "/Users/" + u.ID,
		},
	}
	if u.ExternalID != "" {
		out.ExternalID = u.ExternalID
	}
	if u.DisplayName != "" {
		out.Name = &Name{Formatted: u.DisplayName}
	}
	if u.Email != "" {
		out.Emails = []Multi{{Value: u.Email, Type: "work", Primary: true}}
	}
	if u.Phone != "" {
		out.PhoneNumbers = []Multi{{Value: u.Phone, Type: "work", Primary: true}}
	}
	return out
}

// PrimaryValue picks the value a SCIM client meant from a multi-valued
// attribute.
//
// The primary one if it is marked, otherwise the first. RFC 7643 says at
// most one may be primary and does not require any to be; a client that
// sends three addresses and marks none is not malformed, and rejecting it
// would fail a sync over a formatting preference.
func PrimaryValue(values []Multi) string {
	for _, v := range values {
		if v.Primary {
			return strings.TrimSpace(v.Value)
		}
	}
	if len(values) > 0 {
		return strings.TrimSpace(values[0].Value)
	}
	return ""
}

// ListResponse is SCIM's paged collection.
type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []User   `json:"Resources"`
}

// NewListResponse builds a list response.
//
// Resources is never null. SCIM says an empty result is an empty array, and
// at least one client treats null as a protocol error rather than as no
// results — which turns "this filter matched nothing", a perfectly ordinary
// outcome, into a failed sync.
func NewListResponse(users []User, total, startIndex int) ListResponse {
	if users == nil {
		users = []User{}
	}
	return ListResponse{
		Schemas:      []string{SchemaListResponse},
		TotalResults: total,
		StartIndex:   startIndex,
		ItemsPerPage: len(users),
		Resources:    users,
	}
}
