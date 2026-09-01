package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/service"
)

// maxLogoUploadBytes caps the request body.
//
// A little above the service's own cap on the file, so that a file just over
// the limit is refused with a reason rather than by the connection being cut
// mid-upload. The service is what decides; this only stops a body large enough
// to be a denial of service from being read at all.
const maxLogoUploadBytes = service.MaxLogoBytes + (64 << 10)

// maxBgImageUploadBytes caps the request body for a branding background
// image, the same margin above the service's own cap as maxLogoUploadBytes.
const maxBgImageUploadBytes = service.MaxBgImageBytes + (64 << 10)

// UploadApplicationLogo stores a picture and returns the path to reference it
// by.
//
// The response is a path rather than an id, because a path is what the caller
// needs: it goes straight into the logo_uri field of whichever application is
// being registered, and that field has accepted a path on this server since
// migration 00003. Returning an id would leave the console to assemble the
// address, which means the console would have to know how the mount is spelled.
func (h *Handler) UploadApplicationLogo(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxLogoUploadBytes)
	// #nosec G120 -- the body is already capped by the MaxBytesReader above;
	// this argument is the in-memory buffer size, not a limit.
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		httpx.Fail(w, r, httpx.BadRequest("INVALID_UPLOAD",
			"Send the image as multipart/form-data in a field named \"file\"."))
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

	id, err := h.logos.Upload(r.Context(), principal, file)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.tenants.Get(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	httpx.OK(w, map[string]string{
		"path": service.ApplicationLogoPath(tenant.Code, id),
	})
}

// UploadBrandingBgImage stores a branding background image and returns the
// path to reference it by. Same shape as UploadApplicationLogo, same
// storage, same serving path — see service.ApplicationLogoService.UploadBgImage
// for why a background image is not a distinct kind of row.
func (h *Handler) UploadBrandingBgImage(w http.ResponseWriter, r *http.Request) {
	principal := auth.MustPrincipal(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxBgImageUploadBytes)
	// #nosec G120 -- the body is already capped by the MaxBytesReader above;
	// this argument is the in-memory buffer size, not a limit.
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		httpx.Fail(w, r, httpx.BadRequest("INVALID_UPLOAD",
			"Send the image as multipart/form-data in a field named \"file\"."))
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

	id, err := h.logos.UploadBgImage(r.Context(), principal, file)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	tenant, err := h.tenants.Get(r.Context(), principal.TenantID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	httpx.OK(w, map[string]string{
		"path": service.ApplicationLogoPath(tenant.Code, id),
	})
}

// ApplicationLogo serves a stored picture.
//
// Public, and it has to be: a tile is drawn on the portal and on the sign-in
// screen, where nobody has a token yet. The tenant comes from the path — which
// is why the path has one — so the lookup is still scoped, and a logo is
// unreachable through a tenant that does not own it.
func (h *Handler) ApplicationLogo(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.tenants.Resolve(r.Context(), chi.URLParam(r, "tenant"))
	if err != nil {
		// The same answer as an unknown logo. Which tenant codes exist is not
		// something an unauthenticated caller should be able to enumerate by
		// watching the difference between a 404 and something else.
		httpx.Fail(w, r, httpx.NotFound("LOGO_NOT_FOUND", "No such logo."))
		return
	}

	logo, err := h.logos.Get(r.Context(), tenant.ID, chi.URLParam(r, "logoID"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The bytes never change: a new upload gets a new id. So the ETag is the
	// content hash and the response may be cached for a year — which is what
	// keeps a portal of thirty tiles from being thirty database reads on every
	// page load.
	etag := `"` + logo.SHA256 + `"`
	h2 := w.Header()
	h2.Set("ETag", etag)
	h2.Set("Cache-Control", "public, max-age=31536000, immutable")
	h2.Set("Content-Type", logo.ContentType)
	h2.Set("Content-Length", strconv.Itoa(len(logo.Bytes)))
	// Belt and braces with the global nosniff header: the type was determined
	// from the file's own bytes, so this says the same thing twice rather than
	// something new.
	h2.Set("X-Content-Type-Options", "nosniff")

	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(logo.Bytes)
}
