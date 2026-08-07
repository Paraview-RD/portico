package handler

import (
	"net/http"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/service"
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
