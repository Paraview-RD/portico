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
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	ExternalID  string   `json:"externalId,omitempty"`
	UserName    string   `json:"userName"`
	Name        *Name    `json:"name,omitempty"`
	DisplayName string   `json:"displayName,omitempty"`
	NickName    string   `json:"nickName,omitempty"`
	ProfileURL  string   `json:"profileUrl,omitempty"`
	Title       string   `json:"title,omitempty"`
	UserType    string   `json:"userType,omitempty"`
	// SCIM's preferredLanguage is an HTTP Accept-Language value; locale is a
	// language tag. They are different fields in the specification and are
	// kept apart here rather than collapsed, because a directory that sends
	// both means two different things by them.
	PreferredLanguage string    `json:"preferredLanguage,omitempty"`
	Locale            string    `json:"locale,omitempty"`
	Timezone          string    `json:"timezone,omitempty"`
	Emails            []Multi   `json:"emails,omitempty"`
	PhoneNumbers      []Multi   `json:"phoneNumbers,omitempty"`
	Photos            []Photo   `json:"photos,omitempty"`
	Addresses         []Address `json:"addresses,omitempty"`
	Active            bool      `json:"active"`

	// Enterprise is carried under the extension's own URN, which is what a
	// directory looks for — a flattened copy at the top level would be
	// ignored by every client that follows the specification.
	Enterprise *EnterpriseUser `json:"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User,omitempty"`
	// Groups the account belongs to. Read-only, per RFC 7643 §4.1.2:
	// membership is changed through the Group resource, never here, so that
	// there is one way to do it and no question of which side wins.
	Groups []Member `json:"groups,omitempty"`
	Meta   Meta     `json:"meta"`
}

// Name is SCIM's structured name (RFC 7643 §4.1.1).
//
// The parts are stored rather than guessed at. An earlier version populated
// only `formatted`, on the grounds that splitting a display name into a
// family name and a given one is a guess wrong for most of the world's
// naming conventions — which is true, and is exactly why the parts are now
// their own columns: a directory that knows them can send them, and Portico
// keeps what it was told instead of inventing or discarding it.
//
// Absent parts stay absent. A directory that sends only `formatted` gets
// only `formatted` back.
type Name struct {
	Formatted       string `json:"formatted,omitempty"`
	FamilyName      string `json:"familyName,omitempty"`
	GivenName       string `json:"givenName,omitempty"`
	MiddleName      string `json:"middleName,omitempty"`
	HonorificPrefix string `json:"honorificPrefix,omitempty"`
	HonorificSuffix string `json:"honorificSuffix,omitempty"`
}

// Address is SCIM's structured address (RFC 7643 §4.1.2).
type Address struct {
	Formatted     string `json:"formatted,omitempty"`
	StreetAddress string `json:"streetAddress,omitempty"`
	Locality      string `json:"locality,omitempty"`
	Region        string `json:"region,omitempty"`
	PostalCode    string `json:"postalCode,omitempty"`
	Country       string `json:"country,omitempty"`
	Type          string `json:"type,omitempty"`
	Primary       bool   `json:"primary,omitempty"`
}

// Photo is SCIM's multi-valued photo attribute, carrying the one URL this
// system stores.
type Photo struct {
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

// EnterpriseUser is the enterprise extension (RFC 7643 §4.3), carried under
// its own schema URN as the specification requires.
type EnterpriseUser struct {
	EmployeeNumber string         `json:"employeeNumber,omitempty"`
	CostCenter     string         `json:"costCenter,omitempty"`
	Department     string         `json:"department,omitempty"`
	Manager        *EnterpriseRef `json:"manager,omitempty"`
}

// EnterpriseRef names another account.
type EnterpriseRef struct {
	Value       string `json:"value"`
	DisplayName string `json:"displayName,omitempty"`
	Ref         string `json:"$ref,omitempty"`
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
	return fromModelWithGroups(u, nil, baseURL)
}

// FromModelWithGroups is the same, with the account's group membership.
func FromModelWithGroups(u model.User, groups []model.GroupRef, baseURL string) User {
	return fromModelWithGroups(u, groups, baseURL)
}

func fromModelWithGroups(u model.User, groups []model.GroupRef, baseURL string) User {
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
	// The name, from the parts that were stored. Formatted falls back to the
	// display name, which is what a client with no structured name to show
	// will use.
	formatted := u.Profile.NameFormatted
	if formatted == "" {
		formatted = u.DisplayName
	}
	name := Name{
		Formatted:       formatted,
		FamilyName:      u.Profile.FamilyName,
		GivenName:       u.Profile.GivenName,
		MiddleName:      u.Profile.MiddleName,
		HonorificPrefix: u.Profile.HonorificPrefix,
		HonorificSuffix: u.Profile.HonorificSuffix,
	}
	if name != (Name{}) {
		out.Name = &name
	}

	out.NickName = u.Profile.NickName
	out.ProfileURL = u.Profile.ProfileURL
	out.Title = u.Profile.Title
	out.UserType = u.Profile.UserType
	out.PreferredLanguage = u.Profile.PreferredLanguage
	out.Locale = u.Profile.Locale
	out.Timezone = u.Profile.Timezone

	if u.Profile.PhotoURL != "" {
		out.Photos = []Photo{{Value: u.Profile.PhotoURL, Type: "photo"}}
	}

	address := Address{
		Formatted:     u.Profile.AddressFormatted,
		StreetAddress: u.Profile.StreetAddress,
		Locality:      u.Profile.Locality,
		Region:        u.Profile.Region,
		PostalCode:    u.Profile.PostalCode,
		Country:       u.Profile.Country,
	}
	if address != (Address{}) {
		address.Type = "work"
		address.Primary = true
		out.Addresses = []Address{address}
	}

	enterprise := EnterpriseUser{
		EmployeeNumber: u.Profile.EmployeeNumber,
		CostCenter:     u.Profile.CostCenter,
		Department:     u.Profile.Department,
	}
	if u.Profile.ManagerID != "" {
		enterprise.Manager = &EnterpriseRef{
			Value:       u.Profile.ManagerID,
			DisplayName: u.Profile.ManagerName,
			Ref:         strings.TrimSuffix(baseURL, "/") + "/Users/" + u.Profile.ManagerID,
		}
	}
	if enterprise != (EnterpriseUser{}) {
		out.Schemas = append(out.Schemas, SchemaEnterpriseUser)
		out.Enterprise = &enterprise
	}
	if u.Email != "" {
		out.Emails = []Multi{{Value: u.Email, Type: "work", Primary: true}}
	}
	if u.Phone != "" {
		out.PhoneNumbers = []Multi{{Value: u.Phone, Type: "work", Primary: true}}
	}
	for _, group := range groups {
		out.Groups = append(out.Groups, Member{
			Value:   group.ID,
			Display: group.DisplayName,
			Ref:     strings.TrimSuffix(baseURL, "/") + "/Groups/" + group.ID,
		})
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
