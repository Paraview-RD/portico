package server_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Paraview-RD/portico/internal/model"
)

// Uploading the branding background image — same storage and serving path
// as an application tile logo (internal/server/logo_upload_test.go), with
// wider bounds because a background fills the whole sign-in screen rather
// than a small tile. Most of what these tests exercise (SVG refusal,
// content-sniffed format, tenant isolation, the sweep) already has a full
// pass over there against the shared machinery; these focus on what is
// actually different: the endpoint and its bounds.

// uploadBgImage posts bytes to the branding background-image endpoint.
func uploadBgImage(t *testing.T, api *apiTest, token, filename string, content []byte) response {
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/branding/bg-image", body)
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

func TestAnUploadedBackgroundImageIsServedBackFromThisOrigin(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	original := pngBytes(t, 32)
	res := uploadBgImage(t, api, token, "bg.png", original)
	if res.Status != http.StatusOK {
		t.Fatalf("upload: %d %s %s", res.Status, res.Code, res.Message)
	}

	var uploaded struct {
		Path string `json:"path"`
	}
	res.into(t, &uploaded)

	// Same path shape as an application logo — this is the same table and
	// the same serving route, only a different setting field ends up
	// pointing at the row.
	wantPrefix := "/t/" + model.DefaultTenantCode + "/logos/"
	if len(uploaded.Path) <= len(wantPrefix) || uploaded.Path[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("path = %q, want it under %q", uploaded.Path, wantPrefix)
	}

	rec := fetchRaw(t, api, uploaded.Path)
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch %s: %d", uploaded.Path, rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), original) {
		t.Errorf("served %d bytes, uploaded %d; they must be the same file",
			rec.Body.Len(), len(original))
	}
}

// A background image is allowed to be much larger than a tile logo — this
// is the bound that actually differs between the two endpoints, so it is
// worth its own test rather than trusting the shared logo test to cover it.
func TestABackgroundImageWithinTheLogosLimitButOverItsOwnIsStillAccepted(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	// Past MaxLogoPixels (1024) but comfortably under MaxBgImagePixels
	// (2560) — accepted here even though the same file would be refused by
	// /applications/logos.
	res := uploadBgImage(t, api, token, "bg.png", pngBytes(t, 1600))
	if res.Status != http.StatusOK {
		t.Fatalf("a background image within its own bound was refused: %d %s %s",
			res.Status, res.Code, res.Message)
	}
}

func TestAnOversizedBackgroundImageIsRefused(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	// Comfortably past MaxBgImagePixels (2560).
	res := uploadBgImage(t, api, token, "huge.png", pngBytes(t, 3000))
	if res.Status == http.StatusOK {
		t.Error("an oversized background image was accepted")
	}
	if res.Code != "BG_IMAGE_TOO_LARGE" {
		t.Errorf("code = %q, want BG_IMAGE_TOO_LARGE", res.Code)
	}
}

// The format check is shared code with the logo endpoint
// (detectLogoFormat), but this pins that the branding endpoint actually
// calls it rather than skipping validation because it is "just a
// background."
func TestAnSVGBackgroundImageIsRefused(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">` +
		`<script>fetch("/api/v1/users")</script></svg>`)

	res := uploadBgImage(t, api, token, "bg.svg", svg)
	if res.Status != http.StatusBadRequest {
		t.Errorf("an SVG was accepted (status %d)", res.Status)
	}
	if res.Code != "UNSUPPORTED_IMAGE" {
		t.Errorf("code = %q, want UNSUPPORTED_IMAGE", res.Code)
	}
}

func TestUploadingABackgroundImageRequiresAnAdministrator(t *testing.T) {
	api := newAPITest(t)

	res := uploadBgImage(t, api, "", "bg.png", pngBytes(t, 16))
	if res.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a token", res.Status)
	}
}
