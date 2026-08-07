package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paraview/portico/internal/httpx"
)

type payload struct {
	Name string `json:"name"`
}

func postJSON(body string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return httptest.NewRecorder(), req
}

func TestDecodeJSONAcceptsValidBody(t *testing.T) {
	rec, req := postJSON(`{"name":"alice"}`)

	var got payload
	if err := httpx.DecodeJSON(rec, req, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "alice" {
		t.Errorf("name = %q, want alice", got.Name)
	}
}

func TestDecodeJSONRejectsBadInput(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"empty body", "", "EMPTY_BODY"},
		{"malformed json", `{"name":`, "MALFORMED_BODY"},
		// Unknown fields are rejected so a client typo fails loudly instead
		// of silently dropping the value.
		{"unknown field", `{"nameTypo":"alice"}`, "MALFORMED_BODY"},
		// Two concatenated objects must not be accepted as one.
		{"trailing object", `{"name":"a"}{"name":"b"}`, "MALFORMED_BODY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, req := postJSON(tt.body)

			var got payload
			err := httpx.DecodeJSON(rec, req, &got)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("error is %T, want *httpx.Error", err)
			}
			if apiErr.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", apiErr.Code, tt.wantCode)
			}
			if apiErr.Status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", apiErr.Status)
			}
		})
	}
}

func TestDecodeJSONRejectsNonJSONContentType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	var got payload
	err := httpx.DecodeJSON(rec, req, &got)
	if err == nil {
		t.Fatal("expected an error for a form-encoded body")
	}
	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "UNSUPPORTED_MEDIA_TYPE" {
		t.Errorf("error = %v, want UNSUPPORTED_MEDIA_TYPE", err)
	}
}

func TestParsePaginationClampsToSaneBounds(t *testing.T) {
	tests := []struct {
		query        string
		wantPage     int
		wantPageSize int
	}{
		{"", 1, 20},
		{"?page=3&pageSize=50", 3, 50},
		// Out-of-range values fall back to defaults rather than erroring, so
		// a stray query param never breaks a list screen.
		{"?page=0", 1, 20},
		{"?page=-5", 1, 20},
		{"?page=abc", 1, 20},
		{"?pageSize=0", 1, 20},
		{"?pageSize=99999", 1, 200},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/users"+tt.query, nil)
			got := httpx.ParsePagination(req)

			if got.Page != tt.wantPage {
				t.Errorf("page = %d, want %d", got.Page, tt.wantPage)
			}
			if got.PageSize != tt.wantPageSize {
				t.Errorf("pageSize = %d, want %d", got.PageSize, tt.wantPageSize)
			}
		})
	}
}

func TestPaginationOffset(t *testing.T) {
	p := httpx.Pagination{Page: 3, PageSize: 20}
	if got := p.Offset(); got != 40 {
		t.Errorf("offset = %d, want 40", got)
	}
	if got := p.Limit(); got != 20 {
		t.Errorf("limit = %d, want 20", got)
	}
}

// An empty page must serialize as [] rather than null so clients can iterate
// without a nil check.
func TestNewPageResultNormalizesNilItems(t *testing.T) {
	got := httpx.NewPageResult[payload](nil, 0, httpx.Pagination{Page: 1, PageSize: 20})
	if got.Items == nil {
		t.Fatal("items is nil, want an empty slice")
	}
	if len(got.Items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(got.Items))
	}
}
