package scim

import "net/http"

// The discovery endpoints, RFC 7644 §4.
//
// These matter more than they look. An identity provider reads them to
// decide what its own configuration screen offers, so a server that
// advertises a capability it does not have produces an administrator
// configuring a Group push that fails halfway through — while one that
// advertises honestly produces an administrator whose configuration matches
// what will actually happen.
//
// TestAdvertisedCapabilitiesMatchTheImplementation exists because that is
// the failure mode of a partial SCIM implementation, and it is the one thing
// checkable without a real Okta tenant.

// supported describes a capability, in SCIM's own shape.
type supported struct {
	Supported bool `json:"supported"`
}

type bulkSupport struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations"`
	MaxPayloadSize int  `json:"maxPayloadSize"`
}

type filterSupport struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults"`
}

type authScheme struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Primary     bool   `json:"primary"`
}

// ServiceProviderConfig is what this server can do.
type ServiceProviderConfig struct {
	Schemas          []string      `json:"schemas"`
	DocumentationURI string        `json:"documentationUri,omitempty"`
	Patch            supported     `json:"patch"`
	Bulk             bulkSupport   `json:"bulk"`
	Filter           filterSupport `json:"filter"`
	ChangePassword   supported     `json:"changePassword"`
	Sort             supported     `json:"sort"`
	ETag             supported     `json:"etag"`
	AuthSchemes      []authScheme  `json:"authenticationSchemes"`
	Meta             configMeta    `json:"meta"`
}

type configMeta struct {
	ResourceType string `json:"resourceType"`
	Location     string `json:"location"`
}

// MaxFilterResults bounds a single page.
//
// Also the cap applied when a client asks for more, rather than an error: a
// provisioning client that requests 5000 wants as many as it can get, and
// failing the request would stop a sync over a number.
const MaxFilterResults = 200

func (h *Handler) serviceProviderConfig(w http.ResponseWriter, _ *http.Request) {
	base := h.baseURL()
	WriteResource(w, http.StatusOK, ServiceProviderConfig{
		Schemas:          []string{SchemaServiceConfig},
		DocumentationURI: h.docsURL,
		// True, and the handler implements the subset described in patch.go.
		// Unsupported paths answer 400 invalidPath, which is what RFC 7644
		// asks for and what puts the attribute name in the operator's sync
		// log.
		Patch: supported{Supported: true},
		// Not implemented. Advertising bulk and then rejecting a bulk request
		// is worse than not offering it: the client has already decided its
		// strategy by then.
		Bulk:   bulkSupport{Supported: false},
		Filter: filterSupport{Supported: true, MaxResults: MaxFilterResults},
		// Passwords are not settable over SCIM. A provisioning system pushing
		// passwords would mean the directory holds them in a form it can
		// replay, and this deployment's own policy — length, history, expiry
		// — would apply to a value nobody here chose.
		ChangePassword: supported{Supported: false},
		Sort:           supported{Supported: false},
		ETag:           supported{Supported: false},
		AuthSchemes: []authScheme{{
			Type:        "oauthbearertoken",
			Name:        "OAuth Bearer Token",
			Description: "A credential issued from the Portico console, sent as a bearer token.",
			Primary:     true,
		}},
		Meta: configMeta{
			ResourceType: "ServiceProviderConfig",
			Location:     base + "/ServiceProviderConfig",
		},
	})
}

// ResourceType describes one kind of resource this server holds.
type ResourceType struct {
	Schemas     []string     `json:"schemas"`
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Endpoint    string       `json:"endpoint"`
	Description string       `json:"description"`
	Schema      string       `json:"schema"`
	Extensions  []schemaRef  `json:"schemaExtensions,omitempty"`
	Meta        resourceMeta `json:"meta"`
}

// schemaRef names an extension a resource type carries. Several directories
// decide whether to send the enterprise attributes by looking for this, and
// send none when it is absent however willing the server is to store them.
type schemaRef struct {
	Schema   string `json:"schema"`
	Required bool   `json:"required"`
}

type resourceMeta struct {
	ResourceType string `json:"resourceType"`
	Location     string `json:"location"`
}

func (h *Handler) resourceTypes(w http.ResponseWriter, _ *http.Request) {
	base := h.baseURL()
	// Both, and an identity provider reads this to decide what its own
	// configuration screen offers — so anything listed here has to work.
	// TestEveryAdvertisedResourceTypeHasARoute holds that.
	types := []ResourceType{
		{
			Schemas:     []string{SchemaResourceType},
			ID:          "User",
			Name:        "User",
			Endpoint:    "/Users",
			Description: "Portico accounts.",
			Schema:      SchemaUser,
			Extensions: []schemaRef{
				{Schema: SchemaEnterpriseUser, Required: false},
			},
			Meta: resourceMeta{
				ResourceType: "ResourceType",
				Location:     base + "/ResourceTypes/User",
			},
		},
		{
			Schemas:  []string{SchemaResourceType},
			ID:       "Group",
			Name:     "Group",
			Endpoint: "/Groups",
			Description: "Sets of people. Separate from organizations, " +
				"which are the org chart; membership grants no permissions.",
			Schema: SchemaGroup,
			Meta: resourceMeta{
				ResourceType: "ResourceType",
				Location:     base + "/ResourceTypes/Group",
			},
		},
	}

	WriteResource(w, http.StatusOK, map[string]any{
		"schemas":      []string{SchemaListResponse},
		"totalResults": len(types),
		"startIndex":   1,
		"itemsPerPage": len(types),
		"Resources":    types,
	})
}

// schemas serves the attribute definitions for the resources above.
//
// What is listed here is what this server actually reads and returns, and
// TestTheAdvertisedSchemaIsTheResourceServed holds the two together by
// reflection — a schema listing attributes nothing stores would invite a
// directory to push them and report success, and a schema omitting
// attributes this server does read is how an administrator ends up believing
// a department cannot be provisioned when it can.
//
// It was the second: six attributes were published for a resource carrying
// twenty-two, and the enterprise extension — where employeeNumber, costCenter
// and department live — was not published at all.
func (h *Handler) schemas(w http.ResponseWriter, _ *http.Request) {
	base := h.baseURL()
	resources := []any{userSchema(base), enterpriseSchema(base), groupSchema(base)}

	WriteResource(w, http.StatusOK, map[string]any{
		"schemas":      []string{SchemaListResponse},
		"totalResults": len(resources),
		"startIndex":   1,
		"itemsPerPage": len(resources),
		"Resources":    resources,
	})
}

func userSchema(base string) map[string]any {
	return map[string]any{
		"schemas":     []string{SchemaSchema},
		"id":          SchemaUser,
		"name":        "User",
		"description": "Portico account",
		"attributes": []map[string]any{
			attr("userName", "string", "readWrite", true, true),
			attr("externalId", "string", "readWrite", false, true),
			complexAttr("name", false,
				attr("formatted", "string", "readWrite", false, false),
				attr("familyName", "string", "readWrite", false, false),
				attr("givenName", "string", "readWrite", false, false),
				attr("middleName", "string", "readWrite", false, false),
				attr("honorificPrefix", "string", "readWrite", false, false),
				attr("honorificSuffix", "string", "readWrite", false, false)),
			attr("displayName", "string", "readWrite", false, false),
			attr("nickName", "string", "readWrite", false, false),
			attr("profileUrl", "reference", "readWrite", false, false),
			attr("title", "string", "readWrite", false, false),
			attr("userType", "string", "readWrite", false, false),
			attr("preferredLanguage", "string", "readWrite", false, false),
			attr("locale", "string", "readWrite", false, false),
			attr("timezone", "string", "readWrite", false, false),
			attr("active", "boolean", "readWrite", false, false),
			multiAttr("emails"),
			multiAttr("phoneNumbers"),
			multiAttr("photos"),
			complexAttr("addresses", true,
				attr("formatted", "string", "readWrite", false, false),
				attr("streetAddress", "string", "readWrite", false, false),
				attr("locality", "string", "readWrite", false, false),
				attr("region", "string", "readWrite", false, false),
				attr("postalCode", "string", "readWrite", false, false),
				attr("country", "string", "readWrite", false, false),
				attr("type", "string", "readWrite", false, false),
				attr("primary", "boolean", "readWrite", false, false)),
			// Read-only, and that is not a limitation of this server: a
			// person is put into a group through the Group resource, which is
			// where the membership lives. Writing it from both ends would
			// make two requests disagree about which won.
			complexAttr("groups", true,
				attr("value", "string", "readOnly", false, false),
				attr("display", "string", "readOnly", false, false),
				attr("$ref", "reference", "readOnly", false, false),
				attr("type", "string", "readOnly", false, false)),
		},
		"meta": map[string]any{
			"resourceType": "Schema",
			"location":     base + "/Schemas/" + SchemaUser,
		},
	}
}

// enterpriseSchema is the extension, published as its own schema because
// that is how a client discovers it exists. Without this document a directory
// configured to send a department has no way to learn that this server reads
// one — and several will not send an extension they cannot find here.
func enterpriseSchema(base string) map[string]any {
	return map[string]any{
		"schemas":     []string{SchemaSchema},
		"id":          SchemaEnterpriseUser,
		"name":        "EnterpriseUser",
		"description": "The enterprise extension attributes Portico stores",
		"attributes": []map[string]any{
			attr("employeeNumber", "string", "readWrite", false, false),
			attr("costCenter", "string", "readWrite", false, false),
			attr("department", "string", "readWrite", false, false),
			complexAttr("manager", false,
				attr("value", "string", "readWrite", false, false),
				attr("displayName", "string", "readOnly", false, false),
				attr("$ref", "reference", "readWrite", false, false)),
		},
		"meta": map[string]any{
			"resourceType": "Schema",
			"location":     base + "/Schemas/" + SchemaEnterpriseUser,
		},
	}
}

func groupSchema(base string) map[string]any {
	return map[string]any{
		"schemas":     []string{SchemaSchema},
		"id":          SchemaGroup,
		"name":        "Group",
		"description": "A set of Portico accounts",
		"attributes": []map[string]any{
			attr("displayName", "string", "readWrite", true, true),
			attr("externalId", "string", "readWrite", false, true),
			complexAttr("members", true,
				attr("value", "string", "readWrite", false, false),
				attr("display", "string", "readOnly", false, false),
				attr("$ref", "reference", "readWrite", false, false),
				attr("type", "string", "readWrite", false, false)),
		},
		"meta": map[string]any{
			"resourceType": "Schema",
			"location":     base + "/Schemas/" + SchemaGroup,
		},
	}
}

func attr(name, typ, mutability string, required, unique bool) map[string]any {
	uniqueness := "none"
	if unique {
		uniqueness = "server"
	}
	return map[string]any{
		"name": name, "type": typ, "multiValued": false,
		"required": required, "caseExact": false,
		"mutability": mutability, "returned": "default", "uniqueness": uniqueness,
	}
}

// multiAttr is the value/type/primary shape SCIM uses for the several
// contact attributes that share it.
func multiAttr(name string) map[string]any {
	return complexAttr(name, true,
		attr("value", "string", "readWrite", true, false),
		attr("type", "string", "readWrite", false, false),
		attr("primary", "boolean", "readWrite", false, false))
}

func complexAttr(name string, multiValued bool, sub ...map[string]any) map[string]any {
	return map[string]any{
		"name": name, "type": "complex", "multiValued": multiValued,
		"required": false, "mutability": "readWrite", "returned": "default",
		"subAttributes": sub,
	}
}
