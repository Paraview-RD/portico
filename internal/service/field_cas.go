package service

import (
	"context"

	"github.com/Paraview-RD/portico/internal/model"
)

// Applying a recipient's mappings to a CAS validation response.
//
// The shape of the problem is SAML's — a list of names and values — with one
// difference that decides the implementation: a CAS attribute is an XML
// element, so its name is the element name. That is why internal/casp stopped
// having a struct with fixed tags: a rename changes the element, and a struct
// field cannot.
//
// The `cas:` prefix is not part of a name here. It is added when the document
// is written, so an administrator types `orgCode` rather than `cas:orgCode` —
// the prefix is a fact about the wire format, not about their integration.

// casDefaultAttributes is where each catalogue key already goes out.
//
// The names match the OpenID claims and the SAML friendly names on purpose:
// a service integrated over one protocol sees the same facts under the same
// names over another. That is a property worth keeping, and it is the reason
// this table looks like a duplicate of the other two and is not one.
var casDefaultAttributes = map[string]string{
	"display_name":      "displayName",
	"email":             "email",
	"phone":             "phone",
	"tenant_id":         "tenant_id",
	"tenant_code":       "tenant_code",
	"role":              "role",
	"organization_id":   "organization_id",
	"organization_name": "organization_name",
}

// Deliberately absent: the username.
//
// It is `cas:user`, not an attribute — the element every CAS client keys its
// local record on, and the counterpart of `sub` one protocol over. It is
// unreachable from both directions for the same reason: not a catalogue key,
// so nothing can be renamed away from it, and claimed below, so nothing can
// be renamed onto it.

// casAttributeOwners is which catalogue key each default element belongs to,
// for the refusal that stops a rename landing on a name the document already
// uses. Same shape and same reason as the OIDC, SAML, and webhook guards.
var casAttributeOwners = func() map[string]string {
	owners := make(map[string]string, len(casDefaultAttributes)+1)
	for key, name := range casDefaultAttributes {
		owners[name] = key
	}
	// Owned by nothing, which makes every rule onto it a collision.
	owners["user"] = ""
	return owners
}()

// CASAttributeNames is the default set, for the guard tests that hold this
// table and the documentation in step.
func CASAttributeNames() map[string]string {
	out := make(map[string]string, len(casDefaultAttributes))
	for key, name := range casDefaultAttributes {
		out[key] = name
	}
	return out
}

// CASAttributeFor decides one default attribute's fate under a recipient's
// rules.
func CASAttributeFor(out Outbound, key string) (name string, send bool) {
	def, isDefault := casDefaultAttributes[key]
	if !isDefault {
		return "", false
	}
	return out.NameFor(key, def)
}

// CASAddition is one configured attribute, resolved for an account.
type CASAddition struct {
	Name  string
	Value string
}

// CASAdditions resolves the attributes a service has configured that the
// default response does not carry.
func (c *FieldCatalogue) CASAdditions(ctx context.Context, tenantID string, user model.User, out Outbound) ([]CASAddition, error) {
	if out.Empty() {
		return nil, nil
	}

	defaults := make(map[string]bool, len(casDefaultAttributes))
	for key := range casDefaultAttributes {
		defaults[key] = true
	}
	additions := out.Additions(defaults)
	if len(additions) == 0 {
		return nil, nil
	}

	values, err := c.FieldValues(ctx, tenantID, user)
	if err != nil {
		return nil, err
	}

	added := make([]CASAddition, 0, len(additions))
	for _, rule := range additions {
		if value, ok := values[rule.SourceKey]; ok {
			// Nothing is ever sent empty.
			added = append(added, CASAddition{Name: rule.TargetName, Value: value})
		}
	}
	return added, nil
}
