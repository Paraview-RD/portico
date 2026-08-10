package service

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
)

// CommandLineActor is who an administrative act is attributed to when it was
// performed with the `portico` command rather than through the API.
//
// The user id is deliberately left empty, which the audit service stores as
// null: there was no user. Recording a real administrator's id would be a
// lie, and inventing a synthetic one would put a row in the trail that looks
// like an account somebody could go and disable.
//
// The command line is not a lesser path that can skip the trail. Whoever
// reads the audit log later is asking "who let this application in", and
// "somebody with shell access, at this time" is a far better answer than
// silence.
func CommandLineActor(tenantID string) auth.Principal {
	return auth.Principal{
		TenantID: tenantID,
		Username: "command line",
		Role:     model.RoleSuperAdmin,
	}
}

// Audit target types for the three kinds of registered application. They
// are constants for the same reason the action verbs are: so a trail stays
// queryable and a typo cannot create a silent second category.
const (
	targetOAuthClient = "OAUTH_CLIENT"
	targetSAMLSP      = "SAML_SERVICE_PROVIDER"
	targetCASService  = "CAS_SERVICE"
	targetLDAPSource  = "LDAP_SOURCE"
)

// Page is the limit/offset pair a list query runs with. Handlers translate
// the API's page/pageSize into this.
type Page struct {
	Limit  int
	Offset int
}

// escapeLike neutralizes the wildcards in a user-supplied search term so a
// keyword of "%" does not match every row.
//
// The queries that use this declare ESCAPE '\'.
func escapeLike(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return replacer.Replace(s)
}

// filters accumulates the optional WHERE conditions of a list query along
// with their arguments.
//
// PostgreSQL numbers its placeholders ($1, $2, …), so a query assembled from
// a varying set of filters cannot use a fixed template — the numbering
// depends on which filters were supplied. This keeps the numbering and the
// argument slice in step by construction, which is the part that is easy to
// get wrong by hand and produces either a runtime error or, worse, arguments
// silently bound to the wrong condition.
//
// Only fixed string literals are ever passed to Add. Values go through
// placeholders, never into the SQL text.
type filters struct {
	conditions []string
	args       []any
}

// tenantFilters returns a filter set whose first bound argument is the
// tenant, so $1 in the caller's base query is the tenant and the optional
// filters number from $2.
//
// The tenant predicate itself stays written out in the caller's SQL rather
// than being added here. That is deliberate: it means the tenant constraint
// is visible when reading the query, and it is what lets the guard test in
// internal/store check these hand-written statements the same way it checks
// the generated ones.
func tenantFilters(tenantID string) *filters {
	return &filters{args: []any{tenantID}}
}

// Add appends a condition. Use %s in the format for each placeholder; they
// are numbered automatically in the order the values are given.
//
//	f.Add("status = %s", status)
//	f.Add("(username LIKE %s ESCAPE '\\' OR display_name LIKE %s ESCAPE '\\')", p, p)
func (f *filters) Add(format string, values ...any) {
	placeholders := make([]any, len(values))
	for i, v := range values {
		f.args = append(f.args, v)
		placeholders[i] = fmt.Sprintf("$%d", len(f.args))
	}
	f.conditions = append(f.conditions, fmt.Sprintf(format, placeholders...))
}

// And returns the optional conditions as a continuation of a WHERE clause
// the caller has already opened with its tenant predicate, or an empty
// string when no filter was supplied.
func (f *filters) And() string {
	if len(f.conditions) == 0 {
		return ""
	}
	return " AND " + strings.Join(f.conditions, " AND ")
}

// Args returns the arguments bound so far, in placeholder order.
func (f *filters) Args() []any { return f.args }

// Paginate appends LIMIT and OFFSET and returns the full argument list. It
// is called last, after every filter, so the numbering follows on.
func (f *filters) Paginate(page Page) (clause string, args []any) {
	limit := fmt.Sprintf("$%d", len(f.args)+1)
	offset := fmt.Sprintf("$%d", len(f.args)+2)
	return fmt.Sprintf(" LIMIT %s OFFSET %s", limit, offset),
		append(append([]any{}, f.args...), page.Limit, page.Offset)
}

// ErrInvalidLaunchURL is a launch address that must not be rendered as a link.
var ErrInvalidLaunchURL = httpx.BadRequest("INVALID_LAUNCH_URL",
	"A launch address must be an http or https URL.")

// normalizeLaunchURL checks the address a portal will render as a link.
//
// The rules are not the ones a redirect URI or a webhook destination gets,
// because the risk is not the same. Nothing here is fetched by the server, so
// the private-address checks that stop a webhook becoming a proxy would only
// forbid an intranet application that legitimately lives on 10.0.0.0/8. And
// plain http is allowed for the same reason: this address carries no code and
// no assertion, so an internal tool on http is a real deployment rather than
// a mistake.
//
// What must be refused is a scheme that executes. This value ends up in an
// href, and `javascript:` there is stored cross-site scripting aimed at every
// person who opens the portal — an administrator writes it once and it runs
// in everybody's browser.
//
// Empty is allowed and means the application has no launch address, which is
// how a registration that is not meant to be opened stays registered.
func normalizeLaunchURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ErrInvalidLaunchURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidLaunchURL
	}
	if parsed.Host == "" {
		return "", ErrInvalidLaunchURL
	}
	return trimmed, nil
}

// ErrInvalidLogoURI is a logo address that must not be rendered as a picture.
var ErrInvalidLogoURI = httpx.BadRequest("INVALID_LOGO_URI",
	"A logo address must be an http or https URL, or a path on this server.")

// normalizeLogoURI checks the address a portal will render as an <img> src.
//
// Two shapes are accepted, and the second is the one worth having: an
// absolute http(s) URL, or a path on this server such as /icons/wiki.svg. A
// deployment that ships its own icons under the second form keeps the portal
// working with no outbound network at all, and tells no third party who
// opened it.
//
// A leading slash is not enough to establish the second shape, which is why
// this parses rather than testing the prefix: //evil.example.com/logo.svg
// begins with a slash and is a protocol-relative URL, so a prefix test would
// wave through exactly the external fetch the path form exists to avoid.
// Requiring an empty scheme *and* an empty host is what rules it out.
//
// `data:` is refused along with everything else outside those two shapes.
// An SVG rendered through <img> cannot run its own script, so a data URI
// would not be an immediate hole — but the reason it is safe lives in the
// rendering, not in the value, and a stored blob that is only safe because
// of how one component happens to render it is a trap for whoever changes
// that component.
//
// Empty is allowed and means no logo, which is the common case: the tile
// then carries the first character of the application's name.
func normalizeLogoURI(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ErrInvalidLogoURI
	}

	switch parsed.Scheme {
	case "http", "https":
		if parsed.Host == "" {
			return "", ErrInvalidLogoURI
		}
		return trimmed, nil
	case "":
		if parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
			return "", ErrInvalidLogoURI
		}
		return trimmed, nil
	default:
		return "", ErrInvalidLogoURI
	}
}
