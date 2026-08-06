package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// maxBodyBytes caps request bodies. Excel uploads use their own limit.
const maxBodyBytes = 1 << 20 // 1 MiB

// DecodeJSON reads a JSON object body into dst, rejecting unknown fields so
// that typos in client payloads surface as errors instead of being silently
// ignored.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if mediaType := strings.TrimSpace(strings.Split(ct, ";")[0]); mediaType != "application/json" {
			return BadRequest("UNSUPPORTED_MEDIA_TYPE", "Request body must be application/json.")
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return BadRequest("EMPTY_BODY", "Request body is required.")
		case errors.As(err, &maxBytesErr):
			return BadRequest("BODY_TOO_LARGE", "Request body is too large.")
		default:
			return BadRequest("MALFORMED_BODY", "Request body is not valid JSON: "+err.Error())
		}
	}

	// Reject trailing content so that two concatenated JSON objects are not
	// silently accepted as one.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return BadRequest("MALFORMED_BODY", "Request body must contain exactly one JSON object.")
	}

	return nil
}

// Pagination is the standard page/pageSize pair for list endpoints.
type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// Offset is the SQL OFFSET corresponding to the current page.
func (p Pagination) Offset() int { return (p.Page - 1) * p.PageSize }

// Limit is the SQL LIMIT corresponding to the current page size.
func (p Pagination) Limit() int { return p.PageSize }

// ParsePagination reads page and pageSize query parameters, clamping them to
// sane bounds rather than erroring, so a stray value never breaks a list
// screen.
func ParsePagination(r *http.Request) Pagination {
	p := Pagination{Page: 1, PageSize: defaultPageSize}

	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.Page = n
		}
	}
	if v := r.URL.Query().Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.PageSize = min(n, maxPageSize)
		}
	}

	return p
}

// PageResult is the payload shape returned by every list endpoint.
type PageResult[T any] struct {
	// Items is never null: an empty page serializes as [].
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// NewPageResult builds a PageResult, normalizing a nil slice to an empty one.
func NewPageResult[T any](items []T, total int64, p Pagination) PageResult[T] {
	if items == nil {
		items = []T{}
	}
	return PageResult[T]{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}
}
