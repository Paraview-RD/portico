package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/paraview/keylite/internal/auth"
	"github.com/paraview/keylite/internal/httpx"
	"github.com/paraview/keylite/internal/model"
)

// Import column order. The template writes these headers, and the parser
// reads by position so a translated header row still imports correctly.
var importColumns = []string{
	"username", "displayName", "password", "phone", "email", "role", "organizationCode",
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

	// Resolve organization codes once rather than per row.
	orgIDsByCode, err := s.organizationIDsByCode(ctx)
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

		if err := s.importOneRow(ctx, record, orgIDsByCode); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, toRowError(rowNumber, record.username, err))
			continue
		}
		result.Imported++
	}

	s.audit.Log(ctx, AuditEntry{
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
}

func (r importRecord) isBlank() bool {
	return r.username == "" && r.displayName == "" && r.password == "" &&
		r.phone == "" && r.email == "" && r.role == "" && r.organizationCode == ""
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
	}
}

func (s *UserService) importOneRow(ctx context.Context, rec importRecord, orgIDsByCode map[string]string) error {
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

	_, err := s.Create(ctx, CreateUserInput{
		Username:       rec.username,
		DisplayName:    rec.displayName,
		Password:       rec.password,
		Phone:          rec.phone,
		Email:          rec.email,
		Role:           role,
		OrganizationID: orgID,
		Source:         model.SourceImport,
	})
	return err
}

// organizationIDsByCode maps active organization codes to ids.
func (s *UserService) organizationIDsByCode(ctx context.Context) (map[string]string, error) {
	orgs, err := s.store.Queries.ListActiveOrganizations(ctx)
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
	example := []any{"jane.doe", "Jane Doe", "initial-password", "13800000000", "jane@example.com", "USER", ""}
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
