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
	Meta        resourceMeta `json:"meta"`
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
// Deliberately minimal: the attributes this server actually reads, and no
// more. A schema listing attributes nothing stores would invite a directory
// to push them and report success.
func (h *Handler) schemas(w http.ResponseWriter, _ *http.Request) {
	base := h.baseURL()
	userSchema := map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
		"id":          SchemaUser,
		"name":        "User",
		"description": "Portico account",
		"attributes": []map[string]any{
			attr("userName", "string", "server", true, true),
			attr("externalId", "string", "readWrite", false, true),
			attr("displayName", "string", "readWrite", false, false),
			attr("active", "boolean", "readWrite", false, false),
			multiAttr("emails"),
			multiAttr("phoneNumbers"),
		},
		"meta": map[string]any{
			"resourceType": "Schema",
			"location":     base + "/Schemas/" + SchemaUser,
		},
	}

	groupSchema := map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
		"id":          SchemaGroup,
		"name":        "Group",
		"description": "A set of Portico accounts",
		"attributes": []map[string]any{
			attr("displayName", "string", "readWrite", true, true),
			attr("externalId", "string", "readWrite", false, true),
			multiAttr("members"),
		},
		"meta": map[string]any{
			"resourceType": "Schema",
			"location":     base + "/Schemas/" + SchemaGroup,
		},
	}

	WriteResource(w, http.StatusOK, map[string]any{
		"schemas":      []string{SchemaListResponse},
		"totalResults": 2,
		"startIndex":   1,
		"itemsPerPage": 2,
		"Resources":    []any{userSchema, groupSchema},
	})
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

func multiAttr(name string) map[string]any {
	return map[string]any{
		"name": name, "type": "complex", "multiValued": true,
		"required": false, "mutability": "readWrite", "returned": "default",
		"subAttributes": []map[string]any{
			attr("value", "string", "readWrite", true, false),
			attr("type", "string", "readWrite", false, false),
			attr("primary", "boolean", "readWrite", false, false),
		},
	}
}
