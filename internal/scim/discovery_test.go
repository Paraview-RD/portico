package scim

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// What /Schemas publishes is what the resource actually carries.
//
// A directory reads this document to decide what its own mapping screen
// offers. Six attributes were published for a User carrying twenty-two, and
// the enterprise extension — employeeNumber, costCenter, department, manager
// — was not published at all, so an administrator looking for somewhere to
// map a department concluded this server had none and left it unmapped. The
// server would have stored it.
//
// Held by reflection rather than by a second list, because a second list is
// what went stale. Adding a field to the User struct and not to the schema
// now fails here, naming the field.
func TestTheAdvertisedSchemaIsTheResourceServed(t *testing.T) {
	for _, resource := range []struct {
		name       string
		schema     map[string]any
		structType any
		// The common attributes RFC 7643 §3.1 defines for every resource.
		// They are not listed in a resource's own schema, so they are not
		// expected in one.
		common []string
	}{
		{
			name:       "User",
			schema:     userSchema("http://example.test/scim/v2"),
			structType: User{},
			common:     []string{"schemas", "id", "meta"},
		},
		{
			name:       "EnterpriseUser",
			schema:     enterpriseSchema("http://example.test/scim/v2"),
			structType: EnterpriseUser{},
		},
		{
			name:       "Group",
			schema:     groupSchema("http://example.test/scim/v2"),
			structType: Group{},
			common:     []string{"schemas", "id", "meta"},
		},
	} {
		t.Run(resource.name, func(t *testing.T) {
			carried := jsonFieldNames(resource.structType, resource.common)
			advertised := attributeNames(t, resource.schema)

			for name := range carried {
				if !advertised[name] {
					t.Errorf("the %s resource carries %s and /Schemas does not "+
						"advertise it; a directory reading that document has "+
						"nowhere to map it", resource.name, name)
				}
			}
			for name := range advertised {
				if !carried[name] {
					t.Errorf("/Schemas advertises %s on %s, which the resource "+
						"does not carry; a directory will push it and be told "+
						"it succeeded", name, resource.name)
				}
			}
		})
	}
}

// Every mutability and uniqueness value is one the specification defines.
//
// `userName` was advertised with mutability `server`, which is not one of the
// four allowed values — it is a uniqueness value, in the argument next to it.
// A strict client rejects the whole schema document over one bad enum, and
// the failure it reports is about parsing rather than about userName.
func TestTheSchemaUsesOnlyTheValuesTheSpecificationDefines(t *testing.T) {
	mutability := map[string]bool{
		"readOnly": true, "readWrite": true, "immutable": true, "writeOnly": true,
	}
	uniqueness := map[string]bool{"none": true, "server": true, "global": true}
	returned := map[string]bool{
		"always": true, "never": true, "default": true, "request": true,
	}

	const base = "http://example.test/scim/v2"
	for _, schema := range []map[string]any{
		userSchema(base), enterpriseSchema(base), groupSchema(base),
	} {
		name, _ := schema["name"].(string)
		walkAttributes(t, schema, func(path string, attribute map[string]any) {
			for field, allowed := range map[string]map[string]bool{
				"mutability": mutability,
				"uniqueness": uniqueness,
				"returned":   returned,
			} {
				value, present := attribute[field].(string)
				if !present {
					continue
				}
				if !allowed[value] {
					t.Errorf("%s.%s has %s = %q, which is not a value RFC 7643 "+
						"defines; a client that validates the document rejects "+
						"all of it", name, path, field, value)
				}
			}
		})
	}
}

// jsonFieldNames is the set of attribute names a resource struct serializes,
// less the common ones and the extension URNs, which are schemas rather than
// attributes of this one.
func jsonFieldNames(value any, common []string) map[string]bool {
	skip := map[string]bool{}
	for _, name := range common {
		skip[name] = true
	}

	names := map[string]bool{}
	structType := reflect.TypeOf(value)
	for i := range structType.NumField() {
		tag := structType.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || skip[name] || strings.HasPrefix(name, "urn:") {
			continue
		}
		names[name] = true
	}
	return names
}

// attributeNames is the set of top-level attribute names a schema document
// declares.
func attributeNames(t *testing.T, schema map[string]any) map[string]bool {
	t.Helper()

	attributes, ok := schema["attributes"].([]map[string]any)
	if !ok {
		t.Fatalf("schema %v has no attribute list this test can read", schema["id"])
	}

	names := map[string]bool{}
	for _, attribute := range attributes {
		name, _ := attribute["name"].(string)
		names[name] = true
	}
	return names
}

// walkAttributes visits every attribute in a schema, including sub-attributes.
func walkAttributes(t *testing.T, schema map[string]any, visit func(path string, attribute map[string]any)) {
	t.Helper()

	attributes, ok := schema["attributes"].([]map[string]any)
	if !ok {
		t.Fatalf("schema %v has no attribute list this test can read", schema["id"])
	}

	var walk func(prefix string, list []map[string]any)
	walk = func(prefix string, list []map[string]any) {
		names := make([]string, 0, len(list))
		for _, attribute := range list {
			name, _ := attribute["name"].(string)
			names = append(names, name)
			visit(prefix+name, attribute)
			if sub, ok := attribute["subAttributes"].([]map[string]any); ok {
				walk(prefix+name+".", sub)
			}
		}
		sort.Strings(names)
	}
	walk("", attributes)
}
