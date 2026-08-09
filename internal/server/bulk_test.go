package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Bulk operations and export.
//
// The property that matters for bulk is that it is not a way around
// anything: every account goes through the path a single one takes, so the
// rule protecting the last administrator still applies when somebody selects
// everybody. For export it is that a spreadsheet of the whole directory is
// recorded as having left.

func TestBulkDisableStillProtectsTheLastAdministrator(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	first := api.createUser(admin, "bulk-one", "bulk-one-password-1", "USER")
	second := api.createUser(admin, "bulk-two", "bulk-two-password-1", "USER")

	var me struct {
		ID string `json:"id"`
	}
	api.do(http.MethodGet, "/api/v1/users/me", admin, nil).into(t, &me)

	// The administrator is in the selection, which is what somebody does
	// when they select everything on the screen.
	res := api.do(http.MethodPost, "/api/v1/users/bulk/status", admin, map[string]any{
		"userIds": []string{first, second, me.ID}, "status": "DISABLED",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("bulk disable: %d %s %s", res.Status, res.Code, res.Message)
	}

	var result struct {
		Total     int `json:"total"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
		Outcomes  []struct {
			UserID string `json:"userId"`
			Code   string `json:"code"`
		} `json:"outcomes"`
	}
	res.into(t, &result)

	if result.Succeeded != 2 || result.Failed != 1 {
		t.Errorf("succeeded=%d failed=%d, want 2 and 1 — the two ordinary "+
			"accounts done and the administrator refused",
			result.Succeeded, result.Failed)
	}
	for _, outcome := range result.Outcomes {
		if outcome.UserID == me.ID && outcome.Code == "" {
			t.Error("the administrator disabled themselves through the bulk " +
				"path; every rule that applies to disabling one account has " +
				"to apply to disabling forty")
		}
	}

	// And they can still sign in, which is the part that would have hurt.
	if res := api.do(http.MethodGet, "/api/v1/users", admin, nil); res.Status != http.StatusOK {
		t.Errorf("the administrator's session broke: %d %s", res.Status, res.Code)
	}
}

// A failure in the middle does not abandon the rest. Somebody who selected
// forty people wants the thirty-nine done.
func TestBulkReportsEachAccountSeparately(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	existing := api.createUser(admin, "bulk-real", "bulk-real-password-1", "USER")

	res := api.do(http.MethodPost, "/api/v1/users/bulk/status", admin, map[string]any{
		"userIds": []string{"00000000-0000-0000-0000-000000000000", existing},
		"status":  "DISABLED",
	})
	var result struct {
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
		Outcomes  []struct {
			UserID string `json:"userId"`
			Code   string `json:"code"`
		} `json:"outcomes"`
	}
	res.into(t, &result)

	if result.Succeeded != 1 || result.Failed != 1 {
		t.Errorf("succeeded=%d failed=%d, want one of each", result.Succeeded, result.Failed)
	}
	if result.Outcomes[0].Code != "USER_NOT_FOUND" {
		t.Errorf("the missing account reported %q, want USER_NOT_FOUND — a "+
			"per-account code is what tells somebody which one to look at",
			result.Outcomes[0].Code)
	}
}

// Moving accounts between organizations must not disturb anything else about
// them. The update endpoint replaces an account's editable fields, so a bulk
// path that sent only the organization would blank display names and demote
// administrators.
func TestBulkOrganizationMoveChangesNothingElse(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	orgID := api.createOrg(admin, "Platform", "PLATFORM", "")
	userID := api.createUser(admin, "mover", "mover-password-1", "SUPER_ADMIN")

	if res := api.do(http.MethodPost, "/api/v1/users/bulk/organization", admin, map[string]any{
		"userIds": []string{userID}, "organizationId": orgID,
	}); res.Status != http.StatusOK {
		t.Fatalf("bulk move: %d %s %s", res.Status, res.Code, res.Message)
	}

	var user struct {
		DisplayName    string `json:"displayName"`
		Role           string `json:"role"`
		OrganizationID string `json:"organizationId"`
	}
	api.do(http.MethodGet, "/api/v1/users/"+userID, admin, nil).into(t, &user)

	if user.OrganizationID != orgID {
		t.Errorf("organization = %q, want %q", user.OrganizationID, orgID)
	}
	if user.Role != "SUPER_ADMIN" {
		t.Errorf("role = %s; a bulk organization move demoted an administrator", user.Role)
	}
	if user.DisplayName != "mover" {
		t.Errorf("displayName = %q; a bulk organization move blanked it", user.DisplayName)
	}
}

func TestBulkRefusesMoreThanItWillDo(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	ids := make([]string, 501)
	for i := range ids {
		ids[i] = "00000000-0000-0000-0000-000000000000"
	}

	res := api.do(http.MethodPost, "/api/v1/users/bulk/status", admin, map[string]any{
		"userIds": ids, "status": "DISABLED",
	})
	if res.Code != "TOO_MANY_USERS" {
		t.Errorf("501 accounts = %d %s, want TOO_MANY_USERS", res.Status, res.Code)
	}
}

// An export is every attribute of every account leaving in one request. That
// it happened has to be recoverable afterwards, because the question is
// asked after an incident rather than before one.
func TestExportIsRecordedInTheTrail(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "exported", "exported-password-1", "USER")

	// Fetched directly rather than through api.do, which decodes an
	// envelope: this response is a workbook, not JSON.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/export", nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	rec := httptest.NewRecorder()
	api.srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got == "" {
		t.Error("no Content-Disposition header; the browser will not download it")
	}
	// An .xlsx is a zip, and its first bytes say so. Enough to establish
	// this is a workbook rather than an error page with a 200 on it.
	if !strings.HasPrefix(rec.Body.String(), "PK") {
		t.Errorf("the export does not look like a workbook: %.20q", rec.Body.String())
	}

	logs := api.do(http.MethodGet, "/api/v1/audit-logs?kind=OPERATION&pageSize=50", admin, nil)
	var page struct {
		Items []struct {
			Action    string `json:"action"`
			ActorName string `json:"actorName"`
			Detail    string `json:"detail"`
		} `json:"items"`
	}
	logs.into(t, &page)

	for _, entry := range page.Items {
		if entry.Action == "USER_EXPORT" && entry.ActorName == adminUsername {
			if !strings.Contains(entry.Detail, "accounts") {
				t.Errorf("the entry does not say how many left: %q", entry.Detail)
			}
			return
		}
	}
	t.Error("exporting the directory left no entry in the trail; nobody can " +
		"answer 'who took a copy, and when' afterwards")
}

// A normal user may not take a copy of the directory.
func TestExportIsAdministratorOnly(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	api.createUser(admin, "curious", "curious-password-1", "USER")
	token := api.login("curious", "curious-password-1")

	if res := api.do(http.MethodGet, "/api/v1/users/export", token, nil); res.Status != http.StatusForbidden {
		t.Errorf("export as a normal user = %d %s, want 403", res.Status, res.Code)
	}
}
