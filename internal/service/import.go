package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
)

// Import column order. The template writes these headers, and the parser
// reads by position so a translated header row still imports correctly.
// Columns are **appended, never inserted**. The parser reads by position so
// that a translated header row still imports — which also means putting a
// new column in the middle silently remaps every spreadsheet anybody has
// already prepared, turning a phone number into an email address without a
// single error.
var importColumns = []string{
	"username", "displayName", "password", "phone", "email", "role", "organizationCode",
	// Appended in V0.2, along with the attributes themselves.
	"title", "department", "employeeNumber", "userType",
	"givenName", "familyName", "preferredLanguage", "timezone",
}

// maxImportRows bounds one upload. Beyond this the request should be split;
// the limit exists so a huge file cannot hold the single writer connection
// for an unbounded time.
const maxImportRows = 5000

// ImportRowError is one row that could not be imported.
type ImportRowError struct {
	// Row is the 1-based row number in the spreadsheet, including the
	// header, so it matches what the user sees in Excel.
	Row      int    `json:"row"`
	Username string `json:"username"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// ImportResult summarizes an upload.
type ImportResult struct {
	Total    int              `json:"total"`
	Imported int              `json:"imported"`
	Failed   int              `json:"failed"`
	Errors   []ImportRowError `json:"errors"`
}

// ImportUsers creates accounts from an uploaded spreadsheet.
//
// Rows are independent: a bad row is reported and skipped rather than
// aborting the batch. A partial import is far more useful than an
// all-or-nothing failure on a thousand-row migration file, and the caller
// gets a per-row report of what to fix and re-upload.
func (s *UserService) ImportUsers(ctx context.Context, actor auth.Principal, r io.Reader, ip string) (ImportResult, error) {
	// Without an explicit limit excelize will happily inflate gigabytes from
	// a small archive. The cap is generous next to the row limit below but
	// small enough that a zip bomb fails instead of exhausting memory.
	file, err := excelize.OpenReader(r, excelize.Options{
		UnzipSizeLimit:    64 << 20,
		UnzipXMLSizeLimit: 16 << 20,
	})
	if err != nil {
		return ImportResult{}, httpx.BadRequest("INVALID_SPREADSHEET",
			"The uploaded file could not be read as an .xlsx workbook.")
	}
	defer func() { _ = file.Close() }()

	sheets := file.GetSheetList()
	if len(sheets) == 0 {
		return ImportResult{}, httpx.BadRequest("EMPTY_SPREADSHEET", "The workbook has no sheets.")
	}

	// Streamed rather than read whole: GetRows would materialize every row
	// before the row limit could reject the file, which makes the limit
	// decorative on exactly the input it exists to stop.
	dataRows, err := readImportRows(file, sheets[0])
	if err != nil {
		return ImportResult{}, err
	}
	if len(dataRows) == 0 {
		return ImportResult{}, httpx.BadRequest("EMPTY_SPREADSHEET",
			"The sheet has a header row but no data rows.")
	}

	// Resolve organization codes once rather than per row. Only the actor's
	// own tenant is consulted, so a code that exists elsewhere is reported
	// as unknown rather than silently linking across the boundary.
	orgIDsByCode, err := s.organizationIDsByCode(ctx, actor.TenantID)
	if err != nil {
		return ImportResult{}, err
	}

	result := ImportResult{Total: len(dataRows), Errors: []ImportRowError{}}

	for i, row := range dataRows {
		rowNumber := i + 2 // 1-based, and the header occupies row 1
		record := parseImportRow(row)

		// A row that is entirely blank is trailing whitespace in the file,
		// not a mistake worth reporting.
		if record.isBlank() {
			result.Total--
			continue
		}

		if err := s.importOneRow(ctx, actor.TenantID, record, orgIDsByCode); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, toRowError(rowNumber, record.username, err))
			continue
		}
		result.Imported++
	}

	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogRegistration, Action: model.ActionUserImport,
		ActorID: actor.UserID, ActorName: actor.Username,
		Detail: fmt.Sprintf("imported %d of %d rows, %d failed",
			result.Imported, result.Total, result.Failed),
		IP: ip,
	})

	return result, nil
}

// readImportRows returns the data rows, stopping as soon as the limit is
// exceeded so an oversized sheet is never fully allocated.
func readImportRows(file *excelize.File, sheet string) ([][]string, error) {
	iter, err := file.Rows(sheet)
	if err != nil {
		return nil, httpx.BadRequest("INVALID_SPREADSHEET", "The first sheet could not be read.")
	}
	defer func() { _ = iter.Close() }()

	var (
		rows       [][]string
		seenHeader bool
	)
	for iter.Next() {
		if !seenHeader {
			seenHeader = true
			continue
		}
		if len(rows) >= maxImportRows {
			return nil, httpx.UnprocessableEntity("TOO_MANY_ROWS",
				fmt.Sprintf("At most %d rows may be imported at once.", maxImportRows))
		}
		columns, err := iter.Columns()
		if err != nil {
			return nil, httpx.BadRequest("INVALID_SPREADSHEET", "A row could not be read.")
		}
		rows = append(rows, columns)
	}
	if err := iter.Error(); err != nil {
		return nil, httpx.BadRequest("INVALID_SPREADSHEET", "The sheet could not be read to the end.")
	}
	return rows, nil
}

type importRecord struct {
	username         string
	displayName      string
	password         string
	phone            string
	email            string
	role             string
	organizationCode string

	title             string
	department        string
	employeeNumber    string
	userType          string
	givenName         string
	familyName        string
	preferredLanguage string
	timezone          string
}

func (r importRecord) isBlank() bool {
	// A row is blank when every cell is. Written as a comparison against the
	// zero value rather than as a chain of ands, so that appending a column
	// to the struct cannot leave a row with only that column set looking
	// blank — which would silently skip it.
	return r == importRecord{}
}

func parseImportRow(row []string) importRecord {
	cell := func(i int) string {
		if i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	return importRecord{
		username:         cell(0),
		displayName:      cell(1),
		password:         cell(2),
		phone:            cell(3),
		email:            cell(4),
		role:             cell(5),
		organizationCode: cell(6),

		title:             cell(7),
		department:        cell(8),
		employeeNumber:    cell(9),
		userType:          cell(10),
		givenName:         cell(11),
		familyName:        cell(12),
		preferredLanguage: cell(13),
		timezone:          cell(14),
	}
}

// profile assembles the descriptive attributes a row carried.
func (r importRecord) profile() model.UserProfile {
	return model.UserProfile{
		Title:             r.title,
		Department:        r.department,
		EmployeeNumber:    r.employeeNumber,
		UserType:          r.userType,
		GivenName:         r.givenName,
		FamilyName:        r.familyName,
		PreferredLanguage: r.preferredLanguage,
		Timezone:          r.timezone,
	}
}

func (s *UserService) importOneRow(ctx context.Context, tenantID string, rec importRecord, orgIDsByCode map[string]string) error {
	role := model.Role(strings.ToUpper(rec.role))
	if rec.role == "" {
		role = model.RoleUser
	}
	if !role.Valid() {
		return httpx.BadRequest("INVALID_ROLE", "Role must be SUPER_ADMIN or USER.")
	}

	orgID := ""
	if rec.organizationCode != "" {
		id, found := orgIDsByCode[rec.organizationCode]
		if !found {
			return httpx.UnprocessableEntity("ORGANIZATION_NOT_FOUND",
				fmt.Sprintf("No active organization has the code %q.", rec.organizationCode))
		}
		orgID = id
	}

	user, err := s.Create(ctx, tenantID, CreateUserInput{
		Username:       rec.username,
		DisplayName:    rec.displayName,
		Password:       rec.password,
		Phone:          rec.phone,
		Email:          rec.email,
		Role:           role,
		OrganizationID: orgID,
		Source:         model.SourceImport,
	})
	if err != nil {
		return err
	}

	// The descriptive attributes, through the same statement everything else
	// uses — so a duplicate employee number in a spreadsheet is refused here
	// exactly as it would be in the console, and lands in the per-row error
	// report rather than failing the whole upload.
	profile := rec.profile()
	if profile == (model.UserProfile{}) {
		return nil
	}
	actor := auth.Principal{TenantID: tenantID, Username: importActor, Role: model.RoleSuperAdmin}
	if _, err := s.SetProfile(ctx, actor, user.ID, profile); err != nil {
		return err
	}
	return nil
}

// importActor is who an imported row's attributes are attributed to.
//
// The import itself is already audited as one act by the administrator who
// uploaded the file; attributing a further entry per row to them would put a
// thousand entries in the trail for one decision.
const importActor = "spreadsheet import"

// organizationIDsByCode maps a tenant's active organization codes to ids.
func (s *UserService) organizationIDsByCode(ctx context.Context, tenantID string) (map[string]string, error) {
	orgs, err := s.store.ForTenant(tenantID).ListActiveOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	byCode := make(map[string]string, len(orgs))
	for _, org := range orgs {
		byCode[org.Code] = org.ID
	}
	return byCode, nil
}

func toRowError(row int, username string, err error) ImportRowError {
	out := ImportRowError{Row: row, Username: username, Code: "IMPORT_FAILED", Message: err.Error()}
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		out.Code = apiErr.Code
		out.Message = apiErr.Message
	}
	return out
}

// ImportTemplate builds the blank workbook administrators fill in. Serving a
// generated template rather than a static file keeps the columns and the
// parser from drifting apart.
func ImportTemplate() (*excelize.File, error) {
	file := excelize.NewFile()
	sheet := file.GetSheetName(0)

	for i, header := range importColumns {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return nil, err
		}
		if err := file.SetCellValue(sheet, cell, header); err != nil {
			return nil, err
		}
	}

	// One example row, so the expected shape is obvious without reading the
	// docs. The organization column is left blank on purpose: a code that
	// does not exist on a fresh instance would make the very first import
	// fail for no good reason. Blank means "no organization", which is
	// always valid.
	example := []any{
		"jane.doe", "Jane Doe", "initial-password", "13800000000", "jane@example.com", "USER", "",
		"Staff Engineer", "Platform", "E-0001", "Employee", "Jane", "Doe", "en-GB", "Europe/London",
	}
	for i, value := range example {
		cell, err := excelize.CoordinatesToCellName(i+1, 2)
		if err != nil {
			return nil, err
		}
		if err := file.SetCellValue(sheet, cell, value); err != nil {
			return nil, err
		}
	}

	return file, nil
}

// ExportUsers writes a tenant's accounts as a spreadsheet.
//
// The same column order the import template uses, so a file exported here
// can be edited and fed back in — which is what "bulk operations" means in
// practice for most of the people who ask for it.
//
// Passwords are not exported, and there is no column for them. The import
// template has one because creating an account needs an initial password;
// an export is a report, and a report that carries credentials is a
// credential-distribution mechanism nobody meant to build.
func (s *UserService) ExportUsers(ctx context.Context, actor auth.Principal, q UserQuery) (*excelize.File, int, error) {
	// Everything matching the filter, in pages, rather than one unbounded
	// query: a tenant with fifty thousand accounts should produce a large
	// file slowly rather than hold the connection while assembling one
	// enormous result set in memory.
	const page = 500

	orgs, err := s.organizationCodesByID(ctx, actor.TenantID)
	if err != nil {
		return nil, 0, err
	}

	file := excelize.NewFile()
	sheet := file.GetSheetName(0)
	for i, header := range importColumns {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return nil, 0, err
		}
		if err := file.SetCellValue(sheet, cell, header); err != nil {
			return nil, 0, err
		}
	}

	row := 2
	for offset := 0; ; offset += page {
		users, _, err := s.List(ctx, actor.TenantID, q, Page{Limit: page, Offset: offset})
		if err != nil {
			return nil, 0, err
		}
		if len(users) == 0 {
			break
		}
		for _, user := range users {
			values := []any{
				user.Username, user.DisplayName,
				// The password column, deliberately empty. Present so the
				// file's shape matches the import template; blank because
				// there is nothing here to put in it and nothing that
				// should be.
				"",
				user.Phone, user.Email, string(user.Role), orgs[user.OrganizationID],
				user.Profile.Title, user.Profile.Department, user.Profile.EmployeeNumber,
				user.Profile.UserType, user.Profile.GivenName, user.Profile.FamilyName,
				user.Profile.PreferredLanguage, user.Profile.Timezone,
			}
			for i, value := range values {
				cell, err := excelize.CoordinatesToCellName(i+1, row)
				if err != nil {
					return nil, 0, err
				}
				if err := file.SetCellValue(sheet, cell, value); err != nil {
					return nil, 0, err
				}
			}
			row++
		}
		if len(users) < page {
			break
		}
	}

	exported := row - 2

	// Audited, and this is the entry somebody will want.
	//
	// An export is every attribute of every account in the tenant leaving
	// through one request. Nothing else in this system hands over that much
	// at once, so "who took a copy of the directory, and when" is a question
	// that gets asked after a data-handling incident and cannot be answered
	// afterwards if it was not recorded at the time.
	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionUserExport,
		ActorID: actor.UserID, ActorName: actor.Username,
		Detail: fmt.Sprintf("exported %d accounts", exported),
	})

	return file, exported, nil
}

// organizationCodesByID is the reverse of organizationIDsByCode: an export
// names an organization by the code downstream systems store, not by an id
// nobody outside this database can resolve.
func (s *UserService) organizationCodesByID(ctx context.Context, tenantID string) (map[string]string, error) {
	orgs, err := s.store.ForTenant(tenantID).ListOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	byID := make(map[string]string, len(orgs))
	for _, org := range orgs {
		byID[org.ID] = org.Code
	}
	return byID, nil
}
