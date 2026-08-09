package server_test

import (
	"net/http"
	"testing"
)

// The descriptive half of an account.
//
// The field names are SCIM's (RFC 7643), which is the point: a directory's
// attributes land where they belong instead of somewhere this project made
// up. So the tests worth having are about the boundary between describing
// somebody and deciding what they may do — and about the round trip through
// SCIM, which is what choosing those names was for.

func TestProfileAttributesSurviveARoundTrip(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	id := api.createUser(admin, "described", "described-password-1", "USER")

	profile := map[string]any{
		"nameFormatted": "Dr Zhang San Jr", "familyName": "Zhang", "givenName": "San",
		"middleName": "Wei", "honorificPrefix": "Dr", "honorificSuffix": "Jr",
		"nickName": "Sanny", "profileUrl": "https://intranet.example.test/zhangsan",
		"photoUrl": "https://intranet.example.test/zhangsan.jpg",
		"title":    "Staff Engineer", "userType": "Employee",
		"preferredLanguage": "zh-CN", "locale": "zh-CN", "timezone": "Asia/Shanghai",
		"addressFormatted": "1 Example Road, Beijing", "streetAddress": "1 Example Road",
		"locality": "Beijing", "region": "Beijing", "postalCode": "100000", "country": "CN",
		"employeeNumber": "E-0001", "costCenter": "CC-42", "department": "Platform",
	}

	res := api.do(http.MethodPut, "/api/v1/users/"+id+"/profile", admin, profile)
	if res.Status != http.StatusOK {
		t.Fatalf("set profile: %d %s %s", res.Status, res.Code, res.Message)
	}

	var user struct {
		Profile map[string]any `json:"profile"`
	}
	api.do(http.MethodGet, "/api/v1/users/"+id, admin, nil).into(t, &user)

	for field, want := range profile {
		if got := user.Profile[field]; got != want {
			t.Errorf("%s = %v, want %v — an attribute that silently stops "+
				"being stored is the kind of defect nobody reports, because "+
				"the screen just shows a blank", field, got, want)
		}
	}
}

// The split is the point: this endpoint must not be able to change what
// somebody may do.
func TestWritingAProfileCannotChangeRoleStatusOrOrganization(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	id := api.createUser(admin, "bounded", "bounded-password-1", "USER")

	res := api.do(http.MethodPut, "/api/v1/users/"+id+"/profile", admin, map[string]any{
		"title": "Intern",
		// None of these are fields of the request type, so the decoder — which
		// rejects unknown fields — refuses the whole thing. That is the
		// property: there is no shape of request to this endpoint that
		// carries a role.
		"role": "SUPER_ADMIN", "status": "DISABLED",
	})
	if res.Status == http.StatusOK {
		t.Fatal("a profile write carrying a role was accepted; the split " +
			"between describing somebody and deciding their access is the " +
			"whole reason this endpoint is separate")
	}

	var user struct {
		Role   string `json:"role"`
		Status string `json:"status"`
	}
	api.do(http.MethodGet, "/api/v1/users/"+id, admin, nil).into(t, &user)
	if user.Role != "USER" || user.Status != "ACTIVE" {
		t.Errorf("role = %s, status = %s after a refused profile write",
			user.Role, user.Status)
	}
}

// Somebody may describe themselves, but not name their own manager: a
// reporting line is an organizational fact, and downstream systems read it
// as an approval chain.
func TestSelfServiceCannotSetItsOwnManager(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "ambitious", "ambitious-password-1", "USER")
	token := api.login("ambitious", "ambitious-password-1")

	// The administrator is a real account, so this would work if the field
	// were honoured.
	var me struct {
		ID string `json:"id"`
	}
	api.do(http.MethodGet, "/api/v1/users/me", admin, nil).into(t, &me)

	res := api.do(http.MethodPut, "/api/v1/users/me/profile", token, map[string]any{
		"title": "Principal", "managerId": me.ID,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("set own profile: %d %s %s", res.Status, res.Code, res.Message)
	}

	var after struct {
		Profile struct {
			Title     string `json:"title"`
			ManagerID string `json:"managerId"`
		} `json:"profile"`
	}
	res.into(t, &after)

	if after.Profile.Title != "Principal" {
		t.Error("the title was not saved, so self-service does not work at all")
	}
	if after.Profile.ManagerID != "" {
		t.Errorf("managerId = %q; anybody could claim anybody as their "+
			"manager, which downstream systems read as an approval chain",
			after.Profile.ManagerID)
	}
}

func TestManagerMustBeARealAccountAndNotYourself(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	id := api.createUser(admin, "reports", "reports-password-1", "USER")

	res := api.do(http.MethodPut, "/api/v1/users/"+id+"/profile", admin,
		map[string]any{"managerId": id})
	if res.Code != "MANAGER_IS_SELF" {
		t.Errorf("reporting to yourself = %d %s, want MANAGER_IS_SELF", res.Status, res.Code)
	}

	res = api.do(http.MethodPut, "/api/v1/users/"+id+"/profile", admin,
		map[string]any{"managerId": "00000000-0000-0000-0000-000000000000"})
	if res.Code != "MANAGER_NOT_FOUND" {
		t.Errorf("an unknown manager = %d %s, want MANAGER_NOT_FOUND", res.Status, res.Code)
	}
}

// An employee number is how an HR system names a person, so two accounts
// claiming one is a reconciliation error rather than something to store.
func TestEmployeeNumberIsUniqueWithinATenant(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	first := api.createUser(admin, "payroll-one", "payroll-one-password-1", "USER")
	second := api.createUser(admin, "payroll-two", "payroll-two-password-1", "USER")

	if res := api.do(http.MethodPut, "/api/v1/users/"+first+"/profile", admin,
		map[string]any{"employeeNumber": "E-9000"}); res.Status != http.StatusOK {
		t.Fatalf("first: %d %s", res.Status, res.Code)
	}

	res := api.do(http.MethodPut, "/api/v1/users/"+second+"/profile", admin,
		map[string]any{"employeeNumber": "E-9000"})
	if res.Code != "EMPLOYEE_NUMBER_TAKEN" {
		t.Errorf("a duplicate employee number = %d %s, want EMPLOYEE_NUMBER_TAKEN",
			res.Status, res.Code)
	}

	// And two accounts with none are fine, which a plain unique index would
	// have forbidden.
	for _, id := range []string{first, second} {
		if res := api.do(http.MethodPut, "/api/v1/users/"+id+"/profile", admin,
			map[string]any{"employeeNumber": ""}); res.Status != http.StatusOK {
			t.Errorf("clearing an employee number: %d %s", res.Status, res.Code)
		}
	}
}
