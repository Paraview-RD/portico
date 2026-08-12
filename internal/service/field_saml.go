package service

import (
	"context"

	"github.com/Paraview-RD/portico/internal/model"
)

// Applying a recipient's mappings to a SAML attribute statement.
//
// Simpler than the other two, because a SAML attribute is a name and a value
// in a list — there are no typed fields to work around and no default document
// to overlay. What lives here is the same thing as in field_oidc.go: which
// catalogue key goes out under which name by default.
//
// A SAML attribute carries two names. `Name` is what a service provider
// actually maps on; `FriendlyName` is beside it for whoever is reading the
// assertion in a debugger, and no conforming implementation matches on it.
// A rule may set both, and a rule that sets only the Name keeps the default
// friendly name rather than emitting an attribute with none.

// SAMLAttribute is the pair of names one catalogue key goes out under.
type SAMLAttribute struct {
	Name         string
	FriendlyName string
}

// samlDefaultAttributes is where each catalogue key already goes out.
//
// The OASIS URNs are the names service providers have configured against for
// twenty years; the bare ones below them are this project's own, matching the
// claims the OpenID Provider sends so that a service integrated over one
// protocol sees the same facts over the other.
var samlDefaultAttributes = map[string]SAMLAttribute{
	"username":     {"urn:oid:0.9.2342.19200300.100.1.1", "uid"},
	"display_name": {"urn:oid:2.16.840.1.113730.3.1.241", "displayName"},
	"email":        {"urn:oid:0.9.2342.19200300.100.1.3", "mail"},
	"phone":        {"urn:oid:2.5.4.20", "telephoneNumber"},

	"tenant_id":         {"tenant_id", "tenantId"},
	"tenant_code":       {"tenant_code", "tenantCode"},
	"role":              {"role", "role"},
	"organization_id":   {"organization_id", "organizationId"},
	"organization_name": {"organization_name", "organizationName"},
}

// SAMLCommonName is the second name the display name goes out under.
//
// `cn` carries the same value as `displayName` and always has — crewjam
// derived it from the session, every assertion 0.1 issued had it, and a good
// many service providers map by it. It is not a catalogue key of its own,
// because it is not a separate fact: it is an alias.
//
// So it follows `display_name`'s rule rather than having one. With no rule
// both go out, exactly as before. With a rule the fact goes out once, under
// the name the rule chose — because a rename that left `cn` still carrying
// the value would send the same fact twice under two names, which is what
// somebody renaming it is trying to stop.
var SAMLCommonName = SAMLAttribute{Name: "urn:oid:2.5.4.3", FriendlyName: "cn"}

// samlAttributeOwners is which catalogue key each default Name belongs to,
// for the refusal that stops a rename landing on a name the assertion already
// uses. Same shape and same reason as the OIDC and webhook guards.
var samlAttributeOwners = func() map[string]string {
	owners := make(map[string]string, len(samlDefaultAttributes)+1)
	for key, attr := range samlDefaultAttributes {
		owners[attr.Name] = key
	}
	// The alias is owned by the fact it aliases, so `display_name → cn`'s URN
	// is a mapping to somewhere it already is rather than a collision.
	owners[SAMLCommonName.Name] = "display_name"
	return owners
}()

// SAMLAttributeNames is the default set, for the guard tests that hold this
// table and the documentation in step.
func SAMLAttributeNames() map[string]SAMLAttribute {
	out := make(map[string]SAMLAttribute, len(samlDefaultAttributes))
	for key, attr := range samlDefaultAttributes {
		out[key] = attr
	}
	return out
}

// AttributeFor decides one default attribute's fate under a recipient's rules.
//
// aliased reports that the display name's second attribute — `cn` — should
// still be sent, which is true only when nothing renamed or suppressed it.
func AttributeFor(out Outbound, key string) (attr SAMLAttribute, send, aliased bool) {
	def, isDefault := samlDefaultAttributes[key]
	if !isDefault {
		return SAMLAttribute{}, false, false
	}

	rule, configured := out.rule(key)
	switch {
	case !configured:
		return def, true, key == "display_name"
	case rule.Suppressed:
		return SAMLAttribute{}, false, false
	default:
		friendly := rule.FriendlyName
		if friendly == "" {
			// A rule that named only the Name keeps the default friendly
			// name. An attribute with none is harder to read in a debugger
			// and no easier for anything to match on.
			friendly = def.FriendlyName
		}
		return SAMLAttribute{Name: rule.TargetName, FriendlyName: friendly}, true, false
	}
}

// SAMLAdditions resolves the attributes a service provider has configured that
// the default statement does not carry.
func (c *FieldCatalogue) SAMLAdditions(ctx context.Context, tenantID string, user model.User, out Outbound) ([]AddedAttribute, error) {
	if out.Empty() {
		return nil, nil
	}

	defaults := make(map[string]bool, len(samlDefaultAttributes))
	for key := range samlDefaultAttributes {
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

	added := make([]AddedAttribute, 0, len(additions))
	for _, rule := range additions {
		value, ok := values[rule.SourceKey]
		if !ok {
			continue // Nothing is ever sent empty.
		}
		friendly := rule.FriendlyName
		if friendly == "" {
			// No default to fall back on for a field that was never sent, so
			// the key itself — which is at least a name somebody recognises
			// from the picker they chose it in.
			friendly = rule.SourceKey
		}
		added = append(added, AddedAttribute{
			Attribute: SAMLAttribute{Name: rule.TargetName, FriendlyName: friendly},
			Value:     value,
		})
	}
	return added, nil
}

// AddedAttribute is one configured attribute, resolved for an account.
type AddedAttribute struct {
	Attribute SAMLAttribute
	Value     string
}

// rule exposes one entry, for the callers that need more than a name.
func (o Outbound) rule(sourceKey string) (model.FieldMapping, bool) {
	rule, ok := o.byKey[sourceKey]
	return rule, ok
}
