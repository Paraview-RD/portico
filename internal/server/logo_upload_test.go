package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// Uploading the picture on an application's tile.
//
// The column that names one has existed since migration 00003 and accepts a
// path on this server, which is the form worth having: the portal then works
// with no outbound network and tells no third party who opened it. What was
// missing is a way to put a file at such a path without shell access to the
// container.
//
// Most of what follows is about refusing things. The bytes arrive from a web
// form, they are stored, and they are served back from this origin — which is
// the origin the administrative console is on, so what may be served is the
// whole question.

// pngBytes is a real PNG, encoded rather than pasted, so the magic bytes and
// the structure are whatever the standard library writes.
func pngBytes(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for x := range size {
		for y := range size {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x40, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// uploadLogo posts bytes to the logo endpoint under the given filename.
func uploadLogo(t *testing.T, api *apiTest, token, filename string, content []byte) response {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/logos", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	api.srv.Handler().ServeHTTP(rec, req)

	var out response
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("upload returned a non-envelope body: %s", rec.Body.String())
	}
	out.Status = rec.Code
	return out
}

// fetch performs a plain GET with no credentials, which is how a browser asks
// for an image.
func fetchRaw(t *testing.T, api *apiTest, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	api.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAnUploadedLogoIsServedBackFromThisOrigin(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	original := pngBytes(t, 32)
	res := uploadLogo(t, api, token, "wiki.png", original)
	if res.Status != http.StatusOK {
		t.Fatalf("upload: %d %s %s", res.Status, res.Code, res.Message)
	}

	var uploaded struct {
		Path string `json:"path"`
	}
	res.into(t, &uploaded)

	// The path carries the tenant, and that is not cosmetic: this row belongs
	// to a tenant, so the query that reads it has to be able to say which —
	// see internal/store/scoped.go. A bare /logos/{id} would be a query that
	// could have taken a tenant and did not.
	wantPrefix := "/t/" + model.DefaultTenantCode + "/logos/"
	if len(uploaded.Path) <= len(wantPrefix) || uploaded.Path[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("path = %q, want it under %q", uploaded.Path, wantPrefix)
	}

	// Fetched without a token, because a tile on the sign-in screen is drawn
	// before anybody has one.
	rec := fetchRaw(t, api, uploaded.Path)
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch %s: %d", uploaded.Path, rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), original) {
		t.Errorf("served %d bytes, uploaded %d; they must be the same file",
			rec.Body.Len(), len(original))
	}

	// Not under /api, deliberately: SecurityHeaders sets Cache-Control:
	// no-store for that prefix, which is right for a payload carrying account
	// data and wrong for an immutable image fetched on every page load.
	if got := rec.Header().Get("Cache-Control"); got == "no-store" {
		t.Error("the image is sent with no-store, so every tile is refetched " +
			"on every visit; it is immutable and should be cacheable")
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("no ETag, so a conditional request cannot be answered")
	}
}

// An SVG is a document that can carry script, and this one would be served
// from the origin the console is on.
func TestAnSVGUploadIsRefused(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">` +
		`<script>fetch("/api/v1/users")</script></svg>`)

	res := uploadLogo(t, api, token, "logo.svg", svg)
	if res.Status != http.StatusBadRequest {
		t.Errorf("an SVG was accepted (status %d). Rendered through <img> it "+
			"cannot run its script, but served from this origin and opened "+
			"directly it can — and it would be same-origin with the console "+
			"that disables accounts.", res.Status)
	}
	if res.Code != "UNSUPPORTED_IMAGE" {
		t.Errorf("code = %q, want UNSUPPORTED_IMAGE", res.Code)
	}
}

// The filename and the declared type are the uploader's claims, and neither is
// evidence. Only the bytes are.
func TestTheFormatComesFromTheBytesNotTheName(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)

	res := uploadLogo(t, api, token, "innocent.png", svg)
	if res.Status != http.StatusBadRequest {
		t.Errorf("an SVG named .png was accepted (status %d); the extension is "+
			"chosen by whoever uploads it", res.Status)
	}
}

func TestAnOversizedLogoIsRefused(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	// Comfortably past the cap. A gradient rather than one flat colour, so PNG
	// compression cannot shrink it back under.
	res := uploadLogo(t, api, token, "huge.png", pngBytes(t, 1200))
	if res.Status == http.StatusOK {
		t.Error("an oversized image was accepted")
	}
}

func TestUploadingALogoRequiresAnAdministrator(t *testing.T) {
	api := newAPITest(t)

	res := uploadLogo(t, api, "", "wiki.png", pngBytes(t, 16))
	if res.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a token", res.Status)
	}
}

// One tenant's logo is not reachable through another's path, which is the
// whole reason the tenant is in the path rather than assumed.
func TestALogoIsNotReachableThroughAnotherTenantsPath(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := uploadLogo(t, api, token, "wiki.png", pngBytes(t, 16))
	if res.Status != http.StatusOK {
		t.Fatalf("upload: %d %s %s", res.Status, res.Code, res.Message)
	}
	var uploaded struct {
		Path string `json:"path"`
	}
	res.into(t, &uploaded)

	id := uploaded.Path[len("/t/"+model.DefaultTenantCode+"/logos/"):]
	rec := fetchRaw(t, api, "/t/other-tenant/logos/"+id)
	if rec.Code == http.StatusOK {
		t.Error("a logo was served through a tenant that does not own it")
	}
}

// The orphan sweep, which is the price of letting an upload happen before the
// form that would reference it is saved.
//
// Two properties, and the second is the one worth the test: an upload nothing
// points at goes, and an upload something points at stays. A sweep that got the
// second wrong would delete the logo off a working application — silently, on a
// timer, some time after anybody changed anything.
func TestTheSweepRemovesAnUnreferencedLogoAndKeepsAReferencedOne(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	orphan := uploadLogo(t, api, token, "orphan.png", pngBytes(t, 16))
	if orphan.Status != http.StatusOK {
		t.Fatalf("upload orphan: %d %s %s", orphan.Status, orphan.Code, orphan.Message)
	}
	referenced := uploadLogo(t, api, token, "used.png", pngBytes(t, 24))
	if referenced.Status != http.StatusOK {
		t.Fatalf("upload referenced: %d %s %s",
			referenced.Status, referenced.Code, referenced.Message)
	}

	var orphanPath, referencedPath struct {
		Path string `json:"path"`
	}
	orphan.into(t, &orphanPath)
	referenced.into(t, &referencedPath)

	// An application that names the second one.
	res := api.do(http.MethodPost, "/api/v1/applications/oauth-clients", token, map[string]any{
		"clientId": "logo-holder", "name": "Logo Holder",
		"redirectUris": []string{"https://app.example.com/callback"},
		"logoUri":      referencedPath.Path,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("register client: %d %s %s", res.Status, res.Code, res.Message)
	}

	// Both are younger than the retention window, so nothing should go yet.
	if err := api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if rec := fetchRaw(t, api, orphanPath.Path); rec.Code != http.StatusOK {
		t.Fatalf("the orphan was removed while still inside its retention "+
			"window (status %d). Somebody who uploads a picture and takes an "+
			"hour to finish the form must still find it there.", rec.Code)
	}

	// Age both past the window.
	api.execSQL(t, "UPDATE application_logos SET created_at = $1",
		time.Now().Add(-2*service.OrphanRetention))

	if err := api.srv.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if rec := fetchRaw(t, api, orphanPath.Path); rec.Code == http.StatusOK {
		t.Error("an unreferenced logo survived the sweep")
	}
	if rec := fetchRaw(t, api, referencedPath.Path); rec.Code != http.StatusOK {
		t.Errorf("a referenced logo was swept (status %d) — the application "+
			"pointing at it now has a tile that 404s", rec.Code)
	}
}
