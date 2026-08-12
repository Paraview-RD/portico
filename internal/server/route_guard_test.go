package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Which callers a route refuses is decided by where its line sits in
// routes.go, and nothing checks that the line is in the right place.
//
// The router puts three groups under /api/v1: a public one, one behind
// RequireAuth, and one behind RequireAuth and RequireAdmin. Adding an
// endpoint means writing r.Post(...) inside a pair of braces, and writing it
// inside the wrong pair produces an unauthenticated administrative endpoint
// that looks, in a diff, exactly like a correct one — the same call, the
// same handler, two lines further up. Nothing fails. The tests for the
// middleware itself all pass, because the middleware is fine; it was simply
// never given the route.
//
// internal/store/tenancy_guard_test.go makes the same argument about
// tenant_id, and this is that argument applied to authentication. Both are
// properties where a single oversight is a breach rather than a bug, and
// both are invisible to review for the same reason: the wrong version reads
// as the right one.
//
// This checks behaviour rather than wiring. It could compare middleware
// function pointers, which would be cheaper and would prove only that
// something named RequireAdmin is in the chain — not that anything was
// refused. So every route is called twice, and what the guard says when it
// refuses is asserted too: MISSING_TOKEN comes from auth.bearerToken and
// ADMIN_REQUIRED from RequireAdmin, so a handler returning 403 for reasons
// of its own cannot stand in for the guard that was supposed to be there.
//
// Scope is /api/v1, which is the part of the surface these two guards are
// responsible for. SCIM authenticates with a provisioning credential of its
// own, the federation endpoints answer to their protocols, and the manual
// and the single-page application are public on purpose; none of them is
// guarded by this middleware and a check that pretended otherwise would be
// asserting the wrong thing.

// publicAPIRoutes may be called with no credential at all.
//
// Each is public by necessity rather than by convenience: the caller is
// someone who cannot sign in yet, or an orchestrator that never will.
// Adding an entry here is a decision about the attack surface — say why the
// caller cannot hold a token.
var publicAPIRoutes = map[string]string{
	"GET /api/v1/health": "liveness, asked by an orchestrator that holds no account",
	"GET /api/v1/ready":  "readiness, the same",

	"POST /api/v1/auth/login":              "the endpoint that issues the credential",
	"POST /api/v1/auth/register":           "self-registration, which by definition precedes an account",
	"GET /api/v1/auth/registration-status": "the sign-in screen asks before anybody has signed in",

	"GET /api/v1/auth/recovery-channels":          "the caller has forgotten their password",
	"POST /api/v1/auth/password-recovery":         "the same",
	"POST /api/v1/auth/password-recovery/confirm": "redeems a token sent out of band; that token is the credential",

	"POST /api/v1/auth/password/expired": "login refuses an expired password rather than issuing a token, so this cannot require one; it takes the current password and refuses if it has not expired",

	"POST /api/v1/auth/register/verify":        "proves an address before the account may sign in",
	"POST /api/v1/auth/register/verify/resend": "the same",
}

// selfServiceAPIRoutes require a session but not an administrator.
//
// The test does not take this list on trust: it calls each one as an
// ordinary account and fails if the route turns out to demand an
// administrator after all, which is what catches an entry left here after
// the route moved.
var selfServiceAPIRoutes = map[string]string{
	"POST /api/v1/auth/logout":            "ends this session",
	"POST /api/v1/auth/logout-everywhere": "ends all of them",

	"GET /api/v1/users/me":                         "your own account",
	"PUT /api/v1/users/me":                         "your own account",
	"PUT /api/v1/users/me/profile":                 "your own descriptive attributes; it cannot reach role, status, or organization",
	"POST /api/v1/users/me/password":               "your own password",
	"POST /api/v1/users/me/close":                  "the one sanctioned way to disable yourself",
	"GET /api/v1/users/me/groups":                  "your own memberships",
	"GET /api/v1/users/me/sessions":                "your own sessions",
	"DELETE /api/v1/users/me/sessions/{sessionID}": "ending one of your own sessions",

	"POST /api/v1/oauth/authorize":   "the seam between Portico's sign-in and the OpenID Provider",
	"POST /api/v1/saml/authenticate": "the same, for SAML",
	"POST /api/v1/cas/authorize":     "the same, for CAS",

	"GET /api/v1/auth/permission-check": "a downstream system asking about the holder of this token",

	"GET /api/v1/organizations":      "the profile screen draws the list; writing is administrator-only",
	"GET /api/v1/organizations/{id}": "the same",

	"GET /api/v1/portal/applications": "the home screen for somebody who is not an administrator",
}

func TestEveryAPIRouteRefusesTheCallersItShould(t *testing.T) {
	api := newAPITest(t)
	routes := apiRoutes(t, api)
	if len(routes) == 0 {
		t.Fatal("walked the router and found no /api/v1 routes, which means " +
			"this test has stopped reading the route tree rather than that " +
			"the routes changed")
	}

	adminToken := api.adminToken()

	// One ordinary account for every route that is supposed to refuse one.
	// It survives the loop because those refusals happen in the middleware,
	// so no handler ever runs against it.
	api.createUser(adminToken, "guard-check", "guard-check-password-1", "USER")
	userToken := api.login("guard-check", "guard-check-password-1")

	// The self-service routes are the opposite case: proving they do not
	// demand an administrator means actually reaching them, and some of them
	// end the session or close the account they are called with. Each gets
	// an account of its own, so what one route does to its caller cannot
	// decide what the next route reports.
	freshUser := func(n int) string {
		name := fmt.Sprintf("guard-self-%d", n)
		api.createUser(adminToken, name, "guard-check-password-1", "USER")
		return api.login(name, "guard-check-password-1")
	}

	selfServiceSeen := 0

	for _, route := range routes {
		method, path, _ := strings.Cut(route, " ")
		request := requestPath(path)

		if _, public := publicAPIRoutes[route]; public {
			// The other direction, so an entry cannot sit here describing a
			// route that has since been moved behind the guard: a public
			// route must not answer the anonymous caller with a refusal.
			if status, code := call(api, method, request, ""); status == http.StatusUnauthorized && code == "MISSING_TOKEN" {
				t.Errorf("%s is listed as public and refused an anonymous caller.\n"+
					"Either it moved behind RequireAuth, in which case take it "+
					"out of publicAPIRoutes, or the list is describing a route "+
					"that no longer exists.", route)
			}
			continue
		}

		// Guarded: the anonymous caller must be refused, and refused by the
		// middleware rather than by whatever the handler makes of an empty
		// request.
		status, code := call(api, method, request, "")
		if status != http.StatusUnauthorized || code != "MISSING_TOKEN" {
			t.Errorf("%s answered an anonymous caller with %d %s, want 401 MISSING_TOKEN.\n"+
				"Either it is registered outside the RequireAuth group in "+
				"routes.go — which is an unauthenticated endpoint, whatever it "+
				"was meant to be — or it is genuinely public and belongs in "+
				"publicAPIRoutes with a reason.", route, status, code)
			continue
		}

		_, selfService := selfServiceAPIRoutes[route]
		token := userToken
		if selfService {
			selfServiceSeen++
			token = freshUser(selfServiceSeen)
		}

		status, code = call(api, method, request, token)
		refusedForRole := status == http.StatusForbidden && code == "ADMIN_REQUIRED"

		switch {
		case selfService && refusedForRole:
			t.Errorf("%s is listed as self-service and refused an ordinary account.\n"+
				"It is inside the RequireAdmin group in routes.go; either move "+
				"it out or take it out of selfServiceAPIRoutes.", route)
		case !selfService && !refusedForRole:
			t.Errorf("%s answered an ordinary account with %d %s, want 403 ADMIN_REQUIRED.\n"+
				"Every /api/v1 route is administrator-only unless it is named "+
				"in selfServiceAPIRoutes. If this one is meant to be reachable "+
				"by anybody signed in, say so there, with why.", route, status, code)
		}
	}
}

// The allowlists above are only worth having if they describe routes that
// exist. An entry naming a route that has been renamed or removed is a
// permission granted to nothing — harmless today, and indistinguishable from
// a live entry when somebody reads the list to decide whether a new endpoint
// belongs in it.
func TestTheGuardAllowlistsNameRoutesThatExist(t *testing.T) {
	api := newAPITest(t)

	registered := map[string]bool{}
	for _, route := range apiRoutes(t, api) {
		registered[route] = true
	}

	for _, list := range []struct {
		name    string
		entries map[string]string
	}{
		{"publicAPIRoutes", publicAPIRoutes},
		{"selfServiceAPIRoutes", selfServiceAPIRoutes},
	} {
		for route := range list.entries {
			if !registered[route] {
				t.Errorf("%s names %q, which the router does not serve.\n"+
					"Remove it: an allowlist entry for a route that does not "+
					"exist is one nobody can evaluate.", list.name, route)
			}
		}
	}
}

// apiRoutes returns every registered route under /api/v1 as "METHOD /path".
func apiRoutes(t *testing.T, api *apiTest) []string {
	t.Helper()

	router, ok := api.srv.Handler().(chi.Routes)
	if !ok {
		t.Fatal("the server's handler is not a chi router; this test walks its route tree")
	}

	var routes []string
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		routes = append(routes, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	sort.Strings(routes)
	return routes
}

// pathParameter matches the {name} segments chi uses.
var pathParameter = regexp.MustCompile(`\{[^}]*\}`)

// requestPath turns a route pattern into something that can be requested.
//
// The value substituted never matches anything, which does not matter: every
// assertion here is about a refusal that happens before the handler runs, and
// for the routes that are not refused it is about the absence of one. A
// trailing slash is left alone — chi registers "/users/" and does not treat
// it as "/users".
func requestPath(route string) string {
	return pathParameter.ReplaceAllString(route, "guard-check")
}

// call issues a request and returns the status and the envelope's error
// code, without requiring the body to be an envelope — a route that answers
// this caller rather than refusing them may return anything at all.
func call(api *apiTest, method, path, token string) (status int, code string) {
	request := httptest.NewRequest(method, path, http.NoBody)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	api.srv.Handler().ServeHTTP(recorder, request)

	var envelope struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &envelope)
	return recorder.Code, envelope.Code
}
