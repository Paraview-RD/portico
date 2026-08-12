package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Paraview-RD/portico/internal/model"
)

// Resolving the catalogue against one account.
//
// This is the other half of the catalogue: field_catalogue.go says what may be
// named, and this says what each name is worth for a particular person. It is
// deliberately one function rather than three — the three protocols disagree
// about names and formats and not about facts, so a fact resolved differently
// per protocol would be a bug nobody could see from either side.
//
// Empty values are absent from the result rather than present and empty. That is
// the rule the whole feature rests on: nothing is ever sent empty, so a service
// provider mapping a field it never receives should look at the account rather
// than at the mapping.

// FieldValues assembles every catalogue value this account has.
//
// Keyed by catalogue key, so a mapping — which stores a key — can look up what
// to send without knowing where the value came from. The three sources are the
// account row, the organization it belongs to, and the tenant's own attributes.
func (c *FieldCatalogue) FieldValues(ctx context.Context, tenantID string, user model.User) (map[string]string, error) {
	values := map[string]string{
		"user_id":      user.ID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"email":        user.Email,
		"phone":        user.Phone,
		"role":         string(user.Role),
		"status":       string(user.Status),
		"external_id":  user.ExternalID,
		"updated_at":   user.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),

		// The twenty-five from SCIM's schema. Listed rather than reflected: a
		// reflection over the struct would silently start sending whatever
		// column somebody added next, and what leaves this system should be a
		// decision rather than a consequence.
		"name_formatted":     user.Profile.NameFormatted,
		"family_name":        user.Profile.FamilyName,
		"given_name":         user.Profile.GivenName,
		"middle_name":        user.Profile.MiddleName,
		"honorific_prefix":   user.Profile.HonorificPrefix,
		"honorific_suffix":   user.Profile.HonorificSuffix,
		"nick_name":          user.Profile.NickName,
		"profile_url":        user.Profile.ProfileURL,
		"photo_url":          user.Profile.PhotoURL,
		"title":              user.Profile.Title,
		"user_type":          user.Profile.UserType,
		"preferred_language": user.Profile.PreferredLanguage,
		"locale":             user.Profile.Locale,
		"timezone":           user.Profile.Timezone,
		"address_formatted":  user.Profile.AddressFormatted,
		"street_address":     user.Profile.StreetAddress,
		"locality":           user.Profile.Locality,
		"region":             user.Profile.Region,
		"postal_code":        user.Profile.PostalCode,
		"country":            user.Profile.Country,
		"employee_number":    user.Profile.EmployeeNumber,
		"cost_center":        user.Profile.CostCenter,
		"department":         user.Profile.Department,
		"manager_id":         user.Profile.ManagerID,
		"manager_name":       user.Profile.ManagerName,

		"organization_id":   user.OrganizationID,
		"organization_name": user.OrganizationName,
	}

	if err := c.addOrganizationValues(ctx, tenantID, user.OrganizationID, values); err != nil {
		return nil, err
	}
	if err := c.addCustomValues(ctx, tenantID, user.ID, values); err != nil {
		return nil, err
	}

	// The tenant, last and unconditionally: it is the one fact that does not
	// depend on the account at all.
	values["tenant_id"] = tenantID
	// And its code, which is what a downstream system actually recognises —
	// an id is a UUID nobody outside this database has ever seen.
	//
	// Read rather than assumed absent. The catalogue offers `tenant_code` as
	// a mappable field, and a mappable field that never has a value is worse
	// than one that is not offered: this feature promises that a field never
	// received means the account has no value for it, so an empty one here
	// would make the promise a lie.
	if tenant, err := c.store.Queries.GetTenantByID(ctx, tenantID); err == nil {
		values["tenant_code"] = tenant.Code
	}

	// Dropped here rather than at every call site. An absent key and an empty
	// one would otherwise mean the same thing to a caller and different things
	// to a reader.
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			delete(values, key)
		}
	}
	return values, nil
}

// addOrganizationValues fills in the facts that live on the organization row
// rather than on the account.
//
// A missing organization is not an error: most of these keys simply have no
// value for somebody who belongs to none, and the caller drops empties anyway.
func (c *FieldCatalogue) addOrganizationValues(ctx context.Context, tenantID, organizationID string, values map[string]string) error {
	if organizationID == "" {
		return nil
	}
	q := c.store.ForTenant(tenantID)

	org, err := q.GetOrganizationByID(ctx, organizationID)
	if err != nil {
		// The account names an organization that is not there, which the
		// foreign key makes impossible — so this is the database being
		// unreachable rather than the data being wrong, and it is not
		// something to send a token without.
		return fmt.Errorf("read organization %s: %w", organizationID, err)
	}

	values["organization_code"] = org.Code
	if org.ManagerID != nil && *org.ManagerID != "" {
		if manager, err := q.GetUserByID(ctx, *org.ManagerID); err == nil {
			values["organization_manager_name"] = manager.DisplayName
		}
	}

	// The path, from the root down, and the parent's code on the way.
	//
	// Bounded by the depth the service already refuses to exceed, so the walk
	// is a simple loop rather than a recursive query — and a loop that cannot
	// run away even if a cycle somehow existed, which is worth more here than
	// the elegance of the recursive form.
	codes := []string{org.Code}
	parentID := org.ParentID
	for depth := 0; parentID != nil && *parentID != "" && depth < MaxOrganizationDepth; depth++ {
		parent, err := q.GetOrganizationByID(ctx, *parentID)
		if err != nil {
			break
		}
		if depth == 0 {
			values["organization_parent_code"] = parent.Code
		}
		codes = append([]string{parent.Code}, codes...)
		parentID = parent.ParentID
	}
	values["organization_path"] = strings.Join(codes, "/")

	return nil
}

// addCustomValues fills in the tenant's own attributes. Values of retired
// attributes are absent, because the query that reads them leaves those out:
// one that is neither shown nor sent is not part of the account.
func (c *FieldCatalogue) addCustomValues(ctx context.Context, tenantID, userID string, values map[string]string) error {
	rows, err := c.store.ForTenant(tenantID).ListUserAttributeValues(ctx, userID)
	if err != nil {
		return fmt.Errorf("read custom attribute values: %w", err)
	}
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	return nil
}
