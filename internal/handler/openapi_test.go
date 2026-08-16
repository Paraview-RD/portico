package handler

// The request bodies, held against the document that describes them.
//
// `internal/server/openapi_test.go` does this for responses and cannot do it
// for requests: the type a handler decodes into is unexported, which is
// deliberate — it is the wire shape of one endpoint and nothing outside this
// package has business naming it. So the check lives here, where those types
// are visible.
//
// The gap it closes is real and recent. `POST /webhooks` grew a `headers`
// field, the handler accepted it, and the document did not mention it; the
// only reason anybody noticed is that somebody went looking by hand.
//
// A field the server accepts and the document omits is a feature nobody can
// find. A field the document promises and the server ignores is worse: the
// caller sends it, gets a 200, and believes it took.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const specPath = "../../docs/api/openapi.yaml"

// bodies maps an operationId to the type its handler decodes into.
//
// A hand-written table, and the one direction it could go stale in is closed
// below: every operation in the document that takes a JSON body must appear
// here, so an endpoint added without an entry fails rather than going
// unchecked.
var bodies = map[string]any{
	"login":                           loginRequest{},
	"requestTrial":                    trialRequest{},
	"confirmTrial":                    trialConfirmRequest{},
	"changeExpiredPassword":           changeExpiredPasswordRequest{},
	"register":                        registerRequest{},
	"confirmRegistration":             verifyRequest{},
	"resendVerification":              resendVerificationRequest{},
	"requestPasswordRecovery":         recoveryRequest{},
	"confirmPasswordRecovery":         recoveryConfirmRequest{},
	"updateOwnProfile":                updateProfileRequest{},
	"closeOwnAccount":                 closeAccountRequest{},
	"changeOwnPassword":               changePasswordRequest{},
	"setOwnProfileAttributes":         profileRequest{},
	"setUserProfile":                  profileRequest{},
	"createUser":                      createUserRequest{},
	"updateUser":                      updateUserRequest{},
	"resetUserPassword":               resetPasswordRequest{},
	"startExternalSignIn":             externalSignInRequest{},
	"createExternalIdentityProvider":  externalIDPRequest{},
	"updateExternalIdentityProvider":  externalIDPRequest{},
	"bulkSetUserStatus":               bulkStatusRequest{},
	"bulkSetUserOrganization":         bulkOrganizationRequest{},
	"createOrganization":              createOrganizationRequest{},
	"updateOrganization":              updateOrganizationRequest{},
	"setOrganizationManager":          organizationManagerRequest{},
	"attachUserToOrganization":        organizationAttachmentRequest{},
	"assignOrganizationAdministrator": organizationAdministratorRequest{},
	"createGroup":                     groupRequest{},
	"updateGroup":                     groupRequest{},
	"addGroupMembers":                 membersRequest{},
	"createOAuthClient":               createClientRequest{},
	"updateOAuthClient":               updateClientRequest{},
	"createServiceProvider":           serviceProviderRequest{},
	"updateServiceProvider":           serviceProviderRequest{},
	"createCASService":                casServiceRequest{},
	"updateCASService":                casServiceRequest{},
	"createDirectory":                 directoryRequest{},
	"updateDirectory":                 directoryRequest{},
	"createSCIMCredential":            createSCIMCredentialRequest{},
	"createWebhook":                   webhookRequest{},
	"updateSettings":                  updateSettingsRequest{},
	"setTenantStatus":                 tenantStatusRequest{},
	"authorizeOAuth":                  authorizeRequest{},
	"authorizeSAML":                   samlAuthenticateRequest{},
	"authorizeCAS":                    casAuthorizeRequest{},

	"defineUserAttribute": userAttributeRequest{},
	"updateUserAttribute": userAttributeRequest{},

	// One editor writes all four, so one struct describes all four bodies.
	"replaceOAuthClientFieldMappings":     fieldMappingRequest{},
	"replaceServiceProviderFieldMappings": fieldMappingRequest{},
	"replaceCASServiceFieldMappings":      fieldMappingRequest{},
	"replaceWebhookFieldMappings":         fieldMappingRequest{},
}

// notJSON are the two operations that take a file rather than a document.
// There is no struct behind a multipart upload to compare against.
var notJSON = map[string]bool{
	"uploadApplicationLogo": true,
	"importUsers":           true,
	// A free-form object rather than a document with named fields: the keys
	// are whatever attributes this tenant defined, so there is no struct to
	// compare against and there could not be one. What the keys may be is
	// checked at a different boundary — the service refuses a key the
	// catalogue does not hold.
	"setUserAttributeValues": true,
}

func TestEveryRequestBodyDescribesWhatTheHandlerReads(t *testing.T) {
	doc := loadSpec(t)

	described := map[string]bool{}
	for path, item := range doc.Paths {
		if !strings.HasPrefix(path, "/api/v1") {
			continue
		}
		for method, op := range item {
			if !isMethod(method) || op.RequestBody == nil {
				continue
			}
			id := op.OperationID
			described[id] = true

			if notJSON[id] {
				continue
			}
			value, known := bodies[id]
			if !known {
				t.Errorf("%s %s takes a request body and no entry in this "+
					"test says which type reads it, so nothing compares the "+
					"two", strings.ToUpper(method), path)
				continue
			}

			documented := bodyProperties(t, doc, op)
			accepted := jsonFields(reflect.TypeOf(value))

			for name := range accepted {
				if !documented[name] {
					t.Errorf("%s accepts %s and the document does not mention "+
						"it; a caller reading the specification cannot find "+
						"the field", id, name)
				}
			}
			for name := range documented {
				if !accepted[name] {
					t.Errorf("the document says %s takes %s, which the handler "+
						"does not read; a caller sends it, gets a 200, and "+
						"believes it took", id, name)
				}
			}
		}
	}

	// And the other way: an entry here for an operation that no longer takes
	// a body is a comparison against nothing, quietly passing.
	for id := range bodies {
		if !described[id] {
			t.Errorf("this test names %s, which the document does not "+
				"describe as taking a request body", id)
		}
	}
}

// --- reading the document -------------------------------------------------

type spec struct {
	Paths      map[string]map[string]operation `yaml:"paths"`
	Components struct {
		Schemas map[string]schema `yaml:"schemas"`
	} `yaml:"components"`
}

type operation struct {
	OperationID string `yaml:"operationId"`
	RequestBody *struct {
		Content map[string]struct {
			Schema schema `yaml:"schema"`
		} `yaml:"content"`
	} `yaml:"requestBody"`
}

type schema struct {
	Ref        string               `yaml:"$ref"`
	Properties map[string]yaml.Node `yaml:"properties"`
	AllOf      []schema             `yaml:"allOf"`
}

func loadSpec(t *testing.T) spec {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specPath))
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	var doc spec
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	if len(doc.Paths) == 0 {
		t.Fatalf("%s describes no paths; the document or this test is broken", specPath)
	}
	return doc
}

func bodyProperties(t *testing.T, doc spec, op operation) map[string]bool {
	t.Helper()

	for _, media := range op.RequestBody.Content {
		return resolve(t, doc, media.Schema, map[string]bool{})
	}
	t.Errorf("%s has a request body with no content", op.OperationID)
	return nil
}

// resolve flattens $ref and allOf, which is how the document says "that
// shape" and "that shape plus these".
func resolve(t *testing.T, doc spec, s schema, seen map[string]bool) map[string]bool {
	t.Helper()

	names := map[string]bool{}
	for name := range s.Properties {
		names[name] = true
	}
	if s.Ref != "" {
		referenced := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		if seen[referenced] {
			t.Errorf("the %s schema refers to itself", referenced)
			return names
		}
		seen[referenced] = true
		target, known := doc.Components.Schemas[referenced]
		if !known {
			t.Errorf("a request body refers to %s, which is not defined", s.Ref)
			return names
		}
		for name := range resolve(t, doc, target, seen) {
			names[name] = true
		}
	}
	for _, part := range s.AllOf {
		for name := range resolve(t, doc, part, seen) {
			names[name] = true
		}
	}
	return names
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

func isMethod(name string) bool { return httpMethods[strings.ToLower(name)] }

// jsonFields is what encoding/json will read into a struct, following
// embedded ones as it does.
func jsonFields(structType reflect.Type) map[string]bool {
	names := map[string]bool{}
	for i := range structType.NumField() {
		field := structType.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		if field.Anonymous && tag == "" && field.Type.Kind() == reflect.Struct {
			for name := range jsonFields(field.Type) {
				names[name] = true
			}
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		names[name] = true
	}
	return names
}
