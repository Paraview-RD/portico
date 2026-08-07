package server_test

import (
	"net/http"
	"testing"
)

// Sign-in accepts a username, an email address, or a phone number (§3.4).
// The requirement is not just that all three work but that they are
// interchangeable: same session, same token, nothing downstream able to tell
// which was used.
func TestSignInWithAnyIdentifier(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	const password = "identifier-test-pass-1"
	res := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "carol", "displayName": "Carol", "password": password,
		"email": "carol@example.com", "phone": "13800000001",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("create user: %d %s %s", res.Status, res.Code, res.Message)
	}
	var created struct {
		ID string `json:"id"`
	}
	res.into(t, &created)

	for _, identifier := range []string{"carol", "carol@example.com", "13800000001"} {
		t.Run(identifier, func(t *testing.T) {
			token := api.loginTo("", identifier, password)

			me := api.do(http.MethodGet, "/api/v1/users/me", token, nil)
			var profile struct {
				ID string `json:"id"`
			}
			me.into(t, &profile)

			if profile.ID != created.ID {
				t.Errorf("signed in as %s, want the account created above", profile.ID)
			}
		})
	}
}

// A wrong password is a wrong password whichever identifier named the
// account, and none of the three may reveal whether an account exists.
func TestIdentifierSignInFailuresAreUniform(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
		"username": "dave", "displayName": "Dave", "password": "dave-password-1",
		"email": "dave@example.com", "phone": "13800000002",
	})

	identifiers := []string{
		"dave", "dave@example.com", "13800000002", // exist
		"nobody", "nobody@example.com", "13900000000", // do not
	}

	for _, identifier := range identifiers {
		res := api.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"identifier": identifier, "password": "wrong-password-9",
		})
		if res.Status != http.StatusUnauthorized || res.Code != "INVALID_CREDENTIALS" {
			t.Errorf("%s: %d %s, want 401 INVALID_CREDENTIALS", identifier, res.Status, res.Code)
		}
	}
}

// Every one of the three is unique within a tenant, and a collision has to
// say which field collided. Reporting all three as "username taken" is what
// sends whoever is fixing a bulk-import row to the wrong column.
func TestCollidingIdentifiersReportTheRightField(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	base := map[string]string{
		"username": "erin", "displayName": "Erin", "password": "erin-password-1",
		"email": "erin@example.com", "phone": "13800000003",
	}
	if res := api.do(http.MethodPost, "/api/v1/users", admin, base); res.Status != http.StatusOK {
		t.Fatalf("create user: %d %s", res.Status, res.Message)
	}

	cases := []struct {
		name    string
		changes map[string]string
		want    string
	}{
		{"username", map[string]string{"email": "other@example.com", "phone": "13800000004"}, "USERNAME_TAKEN"},
		{"email", map[string]string{"username": "erin2", "phone": "13800000005"}, "EMAIL_TAKEN"},
		{"phone", map[string]string{"username": "erin3", "email": "other3@example.com"}, "PHONE_TAKEN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]string{}
			for k, v := range base {
				body[k] = v
			}
			for k, v := range tc.changes {
				body[k] = v
			}

			res := api.do(http.MethodPost, "/api/v1/users", admin, body)
			if res.Status != http.StatusConflict {
				t.Fatalf("status = %d (%s), want 409", res.Status, res.Code)
			}
			if res.Code != tc.want {
				t.Errorf("code = %q, want %q", res.Code, tc.want)
			}
		})
	}
}

// Empty means "not bound", and the partial indexes are what let more than
// one account leave either field blank. A plain unique index would allow
// exactly one, which would break the very first bulk import.
func TestManyAccountsMayLeaveContactFieldsBlank(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for _, name := range []string{"blank.one", "blank.two", "blank.three"} {
		res := api.do(http.MethodPost, "/api/v1/users", admin, map[string]string{
			"username": name, "displayName": name, "password": "blank-password-1",
		})
		if res.Status != http.StatusOK {
			t.Fatalf("create %s: %d %s %s", name, res.Status, res.Code, res.Message)
		}
	}
}

// The same address in two tenants is legitimate. Making email globally
// unique would let one tenant's users deny another's, in a schema whose
// whole point is that they cannot.
func TestContactDetailsAreUniquePerTenantOnly(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	create := func(token string) response {
		return api.do(http.MethodPost, "/api/v1/users", token, map[string]string{
			"username": "shared", "displayName": "Shared", "password": "shared-password-1",
			"email": "shared@example.com", "phone": "13800000009",
		})
	}

	if res := create(first.token); res.Status != http.StatusOK {
		t.Fatalf("first tenant: %d %s", res.Status, res.Message)
	}
	if res := create(second.token); res.Status != http.StatusOK {
		t.Fatalf("second tenant could not reuse the address: %d %s %s — "+
			"contact details must be unique per tenant, not globally",
			res.Status, res.Code, res.Message)
	}
}

func TestContactDetailsAreValidated(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	cases := []struct {
		name  string
		field string
		value string
		want  string
	}{
		{"email without a domain", "email", "not-an-address", "INVALID_EMAIL"},
		{"email with a display name", "email", "Erin <erin@example.com>", "INVALID_EMAIL"},
		{"phone with letters", "phone", "138-CALL-NOW", "INVALID_PHONE"},
		{"phone too short", "phone", "12", "INVALID_PHONE"},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]string{
				"username":    "validate" + string(rune('a'+i)),
				"displayName": "Validate",
				"password":    "validate-password-1",
				tc.field:      tc.value,
			}
			res := api.do(http.MethodPost, "/api/v1/users", admin, body)
			if res.Status != http.StatusBadRequest || res.Code != tc.want {
				t.Errorf("status = %d, code = %q; want 400 %s", res.Status, res.Code, tc.want)
			}
		})
	}
}
