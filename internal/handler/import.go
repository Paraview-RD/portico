package handler

import (
	"net/http"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// maxUploadBytes caps an import upload. Large enough for the row limit the
// service enforces, small enough that a hostile upload cannot exhaust memory.
const maxUploadBytes = 10 << 20 // 10 MiB

// ImportUsers creates accounts from an uploaded .xlsx file.
func (h *Handler) ImportUsers(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	// maxMemory, not the size cap: anything beyond this spills to a temp
	// file instead of being held in RAM per concurrent request. The wire
	// size is already bounded by MaxBytesReader above.
	// #nosec G120 -- the request body is already capped by the MaxBytesReader
	// above; this argument is the in-memory buffer size, not the size limit.
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		httpx.Fail(w, r, httpx.BadRequest("INVALID_UPLOAD",
			"Send the workbook as multipart/form-data in a field named \"file\"."))
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.Fail(w, r, httpx.BadRequest("MISSING_FILE",
			"No file was uploaded in the \"file\" field."))
		return
	}
	defer func() { _ = file.Close() }()

	result, err := h.users.ImportUsers(r.Context(), principal, file, httpx.ClientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// A partially successful import is still a successful request: the
	// per-row outcome is the payload, not the status code.
	httpx.OK(w, result)
}

// ImportTemplate serves the blank workbook to fill in.
func (h *Handler) ImportTemplate(w http.ResponseWriter, r *http.Request) {
	file, err := service.ImportTemplate()
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer func() { _ = file.Close() }()

	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="portico-user-import-template.xlsx"`)

	if err := file.Write(w); err != nil {
		// The headers are already sent, so this can only be logged; Fail
		// would corrupt the response.
		return
	}
}

// ExportUsers serves the tenant's accounts as a spreadsheet.
//
// The same columns the import template has, so a file taken from here can be
// edited and fed back in — which is what bulk operations means in practice
// for most of the people who ask for one.
//
// It takes the same filters the user list does, so "export what I am looking
// at" is one call rather than a second concept.
func (h *Handler) ExportUsers(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	file, _, err := h.users.ExportUsers(r.Context(), principal, userQueryFrom(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer func() { _ = file.Close() }()

	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="portico-users.xlsx"`)

	if err := file.Write(w); err != nil {
		// The headers are already sent, so this can only be logged; Fail
		// would corrupt the response.
		return
	}
}

type bulkStatusRequest struct {
	UserIDs []string `json:"userIds"`
	Status  string   `json:"status"`
}

// BulkSetUserStatus enables or disables several accounts.
//
// Each goes through the same path a single one does, so every rule that
// applies to disabling one applies here — including that the last
// administrator cannot be disabled and that nobody can disable themselves.
// A bulk path that wrote straight to the table would be a way around all of
// them, and an invisible one.
func (h *Handler) BulkSetUserStatus(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req bulkStatusRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	status := model.Status(req.Status)
	if !status.Valid() {
		httpx.Fail(w, r, httpx.BadRequest("INVALID_STATUS",
			"Status must be ACTIVE or DISABLED."))
		return
	}

	result, err := h.users.BulkSetStatus(r.Context(), principal, req.UserIDs, status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, result)
}

type bulkOrganizationRequest struct {
	UserIDs []string `json:"userIds"`
	// Empty moves them out of any organization, which is a real request and
	// not an omission — so this is a plain string rather than a pointer.
	OrganizationID string `json:"organizationId"`
}

// BulkSetUserOrganization moves several accounts into one organization.
func (h *Handler) BulkSetUserOrganization(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	var req bulkOrganizationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	result, err := h.users.BulkSetOrganization(r.Context(), principal, req.UserIDs, req.OrganizationID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.OK(w, result)
}
