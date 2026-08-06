package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildWorkbook writes rows into an .xlsx in memory. The first row is the
// header, matching what the import template produces.
func buildWorkbook(t *testing.T, rows [][]any) *bytes.Buffer {
	t.Helper()

	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	sheet := file.GetSheetName(0)

	header := []any{"username", "displayName", "password", "phone", "email", "role", "organizationCode"}
	all := append([][]any{header}, rows...)
	for r, row := range all {
		for c, value := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatalf("cell name: %v", err)
			}
			if err := file.SetCellValue(sheet, cell, value); err != nil {
				t.Fatalf("set cell: %v", err)
			}
		}
	}

	buf := &bytes.Buffer{}
	if err := file.Write(buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buf
}

// upload posts a workbook to the import endpoint.
func (a *apiTest) upload(path, token string, workbook io.Reader) response {
	a.t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "users.xlsx")
	if err != nil {
		a.t.Fatalf("create form file: %v", err)
	}
	if _, err := io.Copy(part, workbook); err != nil {
		a.t.Fatalf("copy workbook: %v", err)
	}
	if err := writer.Close(); err != nil {
		a.t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	a.srv.Handler().ServeHTTP(rec, req)

	var out response
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		a.t.Fatalf("upload returned a non-envelope body: %s", rec.Body.String())
	}
	out.Status = rec.Code
	return out
}

type importResult struct {
	Total    int `json:"total"`
	Imported int `json:"imported"`
	Failed   int `json:"failed"`
	Errors   []struct {
		Row      int    `json:"row"`
		Username string `json:"username"`
		Code     string `json:"code"`
	} `json:"errors"`
}

func TestImportUsersHappyPath(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	workbook := buildWorkbook(t, [][]any{
		{"import.one", "Import One", "password-12345", "13800000001", "one@example.com", "USER", ""},
		{"import.two", "Import Two", "password-12345", "", "", "USER", ""},
	})

	res := api.upload("/api/v1/users/import", token, workbook)
	if res.Status != http.StatusOK {
		t.Fatalf("import failed: %d %s %s", res.Status, res.Code, res.Message)
	}

	var result importResult
	res.into(t, &result)

	if result.Imported != 2 || result.Failed != 0 {
		t.Fatalf("imported=%d failed=%d, want 2 and 0 (errors=%+v)",
			result.Imported, result.Failed, result.Errors)
	}

	// The imported accounts must actually work.
	api.login("import.one", "password-12345")
}

// One bad row must not abort the batch: a migration file of a thousand rows
// should import the 999 good ones and report the one to fix.
func TestImportContinuesPastBadRows(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	api.createUser(token, "already.here", "password-12345", "USER")

	workbook := buildWorkbook(t, [][]any{
		{"good.one", "Good One", "password-12345", "", "", "USER", ""},
		{"already.here", "Duplicate", "password-12345", "", "", "USER", ""}, // conflicts
		{"bad.password", "Weak", "short", "", "", "USER", ""},               // too short
		{"", "No Username", "password-12345", "", "", "USER", ""},           // missing username
		{"bad.org", "Bad Org", "password-12345", "", "", "USER", "NOPE"},    // unknown org
		{"good.two", "Good Two", "password-12345", "", "", "USER", ""},
	})

	res := api.upload("/api/v1/users/import", token, workbook)
	if res.Status != http.StatusOK {
		t.Fatalf("import failed: %d %s", res.Status, res.Code)
	}

	var result importResult
	res.into(t, &result)

	if result.Imported != 2 {
		t.Errorf("imported = %d, want 2", result.Imported)
	}
	if result.Failed != 4 {
		t.Errorf("failed = %d, want 4 (errors=%+v)", result.Failed, result.Errors)
	}

	// Errors must be attributable to a specific spreadsheet row, and the
	// numbers must match what the user sees in Excel (header is row 1).
	byRow := map[int]string{}
	for _, e := range result.Errors {
		byRow[e.Row] = e.Code
	}
	want := map[int]string{
		3: "USERNAME_TAKEN",
		4: "WEAK_PASSWORD",
		5: "USERNAME_REQUIRED",
		6: "ORGANIZATION_NOT_FOUND",
	}
	for row, code := range want {
		if byRow[row] != code {
			t.Errorf("row %d: code = %q, want %q", row, byRow[row], code)
		}
	}

	// The good rows really landed.
	api.login("good.one", "password-12345")
	api.login("good.two", "password-12345")
}

func TestImportResolvesOrganizationByCode(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPost, "/api/v1/organizations", token, map[string]any{
		"name": "Engineering", "code": "ENG",
	})
	var org struct {
		ID string `json:"id"`
	}
	res.into(t, &org)

	workbook := buildWorkbook(t, [][]any{
		{"eng.person", "Eng Person", "password-12345", "", "", "USER", "ENG"},
	})

	res = api.upload("/api/v1/users/import", token, workbook)
	var result importResult
	res.into(t, &result)
	if result.Imported != 1 {
		t.Fatalf("imported = %d, want 1 (errors=%+v)", result.Imported, result.Errors)
	}

	res = api.do(http.MethodGet, "/api/v1/users?keyword=eng.person", token, nil)
	var page struct {
		Items []struct {
			OrganizationID   string `json:"organizationId"`
			OrganizationName string `json:"organizationName"`
			Source           string `json:"source"`
		} `json:"items"`
	}
	res.into(t, &page)

	if len(page.Items) != 1 {
		t.Fatalf("found %d users, want 1", len(page.Items))
	}
	if page.Items[0].OrganizationID != org.ID {
		t.Errorf("organizationId = %q, want %q", page.Items[0].OrganizationID, org.ID)
	}
	// Source records how the account was created, for the registration log.
	if page.Items[0].Source != "IMPORT" {
		t.Errorf("source = %q, want IMPORT", page.Items[0].Source)
	}
}

func TestImportRejectsBadUploads(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	t.Run("not a spreadsheet", func(t *testing.T) {
		res := api.upload("/api/v1/users/import", token, bytes.NewBufferString("this is not xlsx"))
		if res.Status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", res.Status)
		}
		if res.Code != "INVALID_SPREADSHEET" {
			t.Errorf("code = %q, want INVALID_SPREADSHEET", res.Code)
		}
	})

	t.Run("header only", func(t *testing.T) {
		res := api.upload("/api/v1/users/import", token, buildWorkbook(t, nil))
		if res.Status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", res.Status)
		}
		if res.Code != "EMPTY_SPREADSHEET" {
			t.Errorf("code = %q, want EMPTY_SPREADSHEET", res.Code)
		}
	})

	t.Run("requires administrator", func(t *testing.T) {
		api.createUser(token, "notadmin", "password-12345", "USER")
		userToken := api.login("notadmin", "password-12345")

		res := api.upload("/api/v1/users/import", userToken, buildWorkbook(t, [][]any{
			{"sneaky", "Sneaky", "password-12345", "", "", "SUPER_ADMIN", ""},
		}))
		if res.Status != http.StatusForbidden {
			t.Errorf("status = %d, want 403", res.Status)
		}
	})
}

// Trailing blank rows are an artifact of how spreadsheets are edited, not
// errors to report back to the user.
func TestImportIgnoresBlankRows(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	workbook := buildWorkbook(t, [][]any{
		{"real.user", "Real User", "password-12345", "", "", "USER", ""},
		{"", "", "", "", "", "", ""},
		{"", "", "", "", "", "", ""},
	})

	res := api.upload("/api/v1/users/import", token, workbook)
	var result importResult
	res.into(t, &result)

	if result.Imported != 1 {
		t.Errorf("imported = %d, want 1", result.Imported)
	}
	if result.Failed != 0 {
		t.Errorf("failed = %d, want 0; blank rows should be skipped silently (%+v)",
			result.Failed, result.Errors)
	}
	if result.Total != 1 {
		t.Errorf("total = %d, want 1; blank rows should not be counted", result.Total)
	}
}

// The generated template must be importable as-is once the example row is
// filled in, which is what keeps the columns and the parser aligned.
func TestImportTemplateIsDownloadableAndParsable(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/import/template", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	api.srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got == "" {
		t.Error("no Content-Disposition header; the browser will not download it")
	}

	// Round-trip it: the downloaded template must parse, and its example
	// row must import cleanly.
	res := api.upload("/api/v1/users/import", token, bytes.NewReader(rec.Body.Bytes()))
	if res.Status != http.StatusOK {
		t.Fatalf("re-importing the template failed: %d %s", res.Status, res.Code)
	}
	var result importResult
	res.into(t, &result)

	if result.Imported != 1 {
		t.Errorf("imported = %d, want 1; the template's example row should be valid (%+v)",
			result.Imported, result.Errors)
	}
}
