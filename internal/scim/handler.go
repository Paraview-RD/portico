package scim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/service"
)

// Provisioner is the slice of the user service SCIM needs.
//
// An interface rather than the concrete service so that this package states
// its dependency as the four operations it performs, and so a test can
// exercise the protocol without a database.
type Provisioner interface {
	ProvisionUser(ctx context.Context, tenantID string, in service.ProvisionUserInput) (model.User, error)
	UpdateProvisionedUser(ctx context.Context, tenantID, userID string, in service.ProvisionUserInput) (model.User, error)
	SetProvisionedUserActive(ctx context.Context, tenantID, userID string, active bool) (model.User, error)
	Get(ctx context.Context, tenantID, userID string) (model.User, error)
	List(ctx context.Context, tenantID string, q service.UserQuery, page service.Page) ([]model.User, int64, error)
	FindByExternalID(ctx context.Context, tenantID, externalID string) (model.User, error)
}

// Handler serves the SCIM endpoints.
type Handler struct {
	users       Provisioner
	groups      GroupProvisioner
	credentials *service.SCIMCredentialService
	publicURL   string
	docsURL     string
}

// NewHandler wires a SCIM handler.
func NewHandler(users Provisioner, groups GroupProvisioner, credentials *service.SCIMCredentialService, publicURL string) *Handler {
	return &Handler{
		users:       users,
		groups:      groups,
		credentials: credentials,
		publicURL:   strings.TrimSuffix(publicURL, "/"),
		docsURL:     "https://github.com/paraview/portico/blob/main/docs/scim.md",
	}
}

// Mount is where the SCIM API lives.
const Mount = "/scim/v2"

// Routes returns the SCIM router, already authenticated.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()

	// Every route, including discovery. RFC 7644 permits ServiceProviderConfig
	// to be unauthenticated, and it is not here: it describes this
	// deployment's provisioning setup, and there is no client that needs it
	// before it has a credential.
	r.Use(h.authenticate)

	r.Get("/ServiceProviderConfig", h.serviceProviderConfig)
	r.Get("/ResourceTypes", h.resourceTypes)
	r.Get("/Schemas", h.schemas)

	r.Route("/Users", func(r chi.Router) {
		r.Get("/", h.listUsers)
		r.Post("/", h.createUser)
		r.Get("/{id}", h.getUser)
		r.Put("/{id}", h.replaceUser)
		r.Patch("/{id}", h.patchUser)
		r.Delete("/{id}", h.deleteUser)
	})

	r.Route("/Groups", func(r chi.Router) {
		r.Get("/", h.listGroups)
		r.Post("/", h.createGroup)
		r.Get("/{id}", h.getGroup)
		r.Put("/{id}", h.replaceGroup)
		r.Patch("/{id}", h.patchGroup)
		r.Delete("/{id}", h.deleteGroup)
	})

	// Anything else under /scim/v2 answers in SCIM's error shape rather than
	// Portico's, so a client gets something it can parse and report rather
	// than an envelope it does not understand.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotFound, "",
			"No such SCIM endpoint: "+r.URL.Path+". See /ResourceTypes.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusMethodNotAllowed, "",
			"That method is not allowed on this resource.")
	})

	return r
}

type principalKey struct{}

// authenticate resolves the bearer token to a tenant.
//
// Its own middleware rather than the application's. auth.Middleware resolves
// a user and a session on every request, and a SCIM client has neither —
// making one satisfy it would mean a synthetic account row that every
// listing, every role check, and the tenancy guards would each have to
// remember to exclude.
//
// The tenant comes from the credential and from nowhere else. This is the
// first authenticated surface where it does not come from a Principal, so
// the usual guards do not cover it; taking a tenant from a header here would
// be a cross-tenant write with a valid token.
func (h *Handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := h.credentials.Authenticate(r.Context(), BearerToken(r))
		if err != nil {
			// WWW-Authenticate so a client library can tell "authenticate"
			// from "you are authenticated and not allowed".
			w.Header().Set("WWW-Authenticate", `Bearer realm="scim"`)
			WriteError(w, r, http.StatusUnauthorized, "",
				"The bearer token is not valid for SCIM.")
			return
		}

		ctx := context.WithValue(r.Context(), principalKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tenantOf returns the tenant the request authenticated into.
func tenantOf(r *http.Request) string {
	p, _ := r.Context().Value(principalKey{}).(service.SCIMPrincipal)
	return p.TenantID
}

// baseURL is where this SCIM service lives, for the Location headers and the
// meta.location every resource carries.
//
// Built from the configured public URL rather than from the request, for the
// same reason password-recovery links are: behind a reverse proxy the Host
// header is whatever the proxy passes on, and a resource that told a client
// to come back to an internal hostname would work in testing and fail in
// deployment.
func (h *Handler) baseURL() string {
	return h.publicURL + Mount
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantOf(r)
	user, err := h.users.Get(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// Groups are included on the single-resource read and not on the
	// listing: a client reads one back to confirm a push landed, while a
	// listing of a thousand accounts would cost a query each.
	groups, err := h.groups.GroupsForUser(r.Context(), tenantID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	WriteResource(w, http.StatusOK, FromModelWithGroups(user, groups, h.baseURL()))
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var body User
	if err := decode(r, &body); err != nil {
		WriteError(w, r, http.StatusBadRequest, TypeInvalidSyntax, err.Error())
		return
	}

	in := service.ProvisionUserInput{
		Username:    strings.TrimSpace(body.UserName),
		DisplayName: displayNameOf(body),
		Email:       PrimaryValue(body.Emails),
		Phone:       PrimaryValue(body.PhoneNumbers),
		ExternalID:  strings.TrimSpace(body.ExternalID),
		Active:      body.Active,
	}

	user, err := h.users.ProvisionUser(r.Context(), tenantOf(r), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Location", h.baseURL()+"/Users/"+user.ID)
	WriteResource(w, http.StatusCreated, FromModel(user, h.baseURL()))
}

func (h *Handler) replaceUser(w http.ResponseWriter, r *http.Request) {
	var body User
	if err := decode(r, &body); err != nil {
		WriteError(w, r, http.StatusBadRequest, TypeInvalidSyntax, err.Error())
		return
	}

	in := service.ProvisionUserInput{
		Username:    strings.TrimSpace(body.UserName),
		DisplayName: displayNameOf(body),
		Email:       PrimaryValue(body.Emails),
		Phone:       PrimaryValue(body.PhoneNumbers),
		ExternalID:  strings.TrimSpace(body.ExternalID),
		Active:      body.Active,
	}

	user, err := h.users.UpdateProvisionedUser(
		r.Context(), tenantOf(r), chi.URLParam(r, "id"), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	WriteResource(w, http.StatusOK, FromModel(user, h.baseURL()))
}

// deleteUser deprovisions rather than deletes.
//
// Portico disables and never deletes accounts, so that the audit trail keeps
// naming something that exists. A provisioning system sending DELETE is
// asking for the person to lose access, and that is exactly what disabling
// does — the deviation is documented in the package comment and in
// ServiceProviderConfig's documentationUri, because a silent one would leave
// an operator believing rows were removed.
//
// It shares its code path with PATCH active=false deliberately: deprovisioning
// that works one way and not the other is the kind of half-working that only
// shows up when somebody leaves.
func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	_, err := h.users.SetProvisionedUserActive(
		r.Context(), tenantOf(r), chi.URLParam(r, "id"), false)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listUsers serves a page, with the one filter that matters.
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantOf(r)
	query := r.URL.Query()

	// SCIM filters are a small language; this implements the two comparisons
	// a provisioning client actually sends when reconciling — userName eq and
	// externalId eq — and refuses the rest rather than approximating them.
	// A filter that is silently ignored returns everybody, and a client that
	// asked "does this user exist" and received the whole directory will
	// conclude yes.
	if filter := strings.TrimSpace(query.Get("filter")); filter != "" {
		h.listFiltered(w, r, tenantID, filter)
		return
	}

	startIndex, count := pagination(query)
	users, total, err := h.users.List(r.Context(), tenantID, service.UserQuery{},
		service.Page{Offset: startIndex - 1, Limit: count})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	WriteResource(w, http.StatusOK,
		NewListResponse(toResources(users, h.baseURL()), int(total), startIndex))
}

func (h *Handler) listFiltered(w http.ResponseWriter, r *http.Request, tenantID, filter string) {
	attribute, value, err := parseEqualityFilter(filter)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, TypeInvalidValue, err.Error())
		return
	}

	var found []model.User
	switch attribute {
	case "username":
		users, _, err := h.users.List(r.Context(), tenantID,
			service.UserQuery{Keyword: value}, service.Page{Offset: 0, Limit: MaxFilterResults})
		if err != nil {
			h.fail(w, r, err)
			return
		}
		// Keyword search is a substring match, and this filter is equality.
		// Returning the substring matches would make "does bob exist" true
		// because bobby does.
		for _, u := range users {
			if strings.EqualFold(u.Username, value) {
				found = append(found, u)
			}
		}
	case "externalid":
		user, err := h.users.FindByExternalID(r.Context(), tenantID, value)
		switch {
		case err == nil:
			found = append(found, user)
		case errors.Is(err, service.ErrUserNotFound):
			// An empty result, not an error: "no such user" is the answer a
			// reconciling client is asking for, and the one that tells it to
			// create the account.
		default:
			h.fail(w, r, err)
			return
		}
	default:
		WriteError(w, r, http.StatusBadRequest, TypeInvalidPath,
			"This server filters on userName and externalId only; "+
				attribute+" is not filterable.")
		return
	}

	WriteResource(w, http.StatusOK,
		NewListResponse(toResources(found, h.baseURL()), len(found), 1))
}

// parseEqualityFilter reads `attribute eq "value"`.
//
// Deliberately not a SCIM filter parser. The full grammar has and/or/not,
// grouping, and nine operators, and implementing a fraction of it while
// accepting the syntax of the rest is how a filter comes to mean something
// other than what it says. Anything but a single equality is refused.
func parseEqualityFilter(filter string) (attribute, value string, err error) {
	parts := strings.Fields(filter)
	if len(parts) < 3 || !strings.EqualFold(parts[1], "eq") {
		return "", "", errors.New(
			`only filters of the form 'attribute eq "value"' are supported`)
	}

	raw := strings.Join(parts[2:], " ")
	value = strings.Trim(raw, `"`)
	if value == "" {
		return "", "", errors.New("the filter value is empty")
	}
	return strings.ToLower(parts[0]), value, nil
}

// pagination reads SCIM's 1-based paging parameters.
func pagination(query map[string][]string) (startIndex, count int) {
	startIndex, count = 1, MaxFilterResults

	if v := firstValue(query, "startIndex"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			startIndex = n
		}
	}
	if v := firstValue(query, "count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			count = n
		}
	}
	// Capped rather than refused: a client asking for 5000 wants as many as
	// it can have, and failing the request would stop a sync over a number.
	if count > MaxFilterResults {
		count = MaxFilterResults
	}
	return startIndex, count
}

func firstValue(query map[string][]string, key string) string {
	if vs := query[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func toResources(users []model.User, baseURL string) []User {
	out := make([]User, 0, len(users))
	for _, u := range users {
		out = append(out, FromModel(u, baseURL))
	}
	return out
}

// displayNameOf prefers the explicit display name, falling back to the
// structured name's formatted form. Entra sends one, Okta the other.
func displayNameOf(body User) string {
	if name := strings.TrimSpace(body.DisplayName); name != "" {
		return name
	}
	if body.Name != nil {
		return strings.TrimSpace(body.Name.Formatted)
	}
	return ""
}

func decode(r *http.Request, into any) error {
	dec := json.NewDecoder(r.Body)
	// Unknown fields are refused rather than ignored. SCIM clients send
	// attributes this server does not store, and accepting them silently
	// would report success for a push that changed nothing — the specific
	// failure being that an administrator believes a directory attribute is
	// flowing through when it is being dropped.
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	return nil
}

// fail maps a service error onto SCIM's error shape.
//
// The mapping matters: a provisioning client acts on the status. 409 means
// "it already exists, go find it", which is how a reconciling sync recovers;
// a 500 for the same condition means "retry forever".
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrUserNotFound):
		WriteError(w, r, http.StatusNotFound, "", "No such user.")
	case errors.Is(err, service.ErrUsernameTaken):
		WriteError(w, r, http.StatusConflict, TypeUniqueness,
			"That userName is already in use.")
	case errors.Is(err, service.ErrEmailTaken):
		WriteError(w, r, http.StatusConflict, TypeUniqueness,
			"That email address is already in use.")
	case errors.Is(err, service.ErrPhoneTaken):
		WriteError(w, r, http.StatusConflict, TypeUniqueness,
			"That phone number is already in use.")
	case errors.Is(err, service.ErrExternalIDTaken):
		WriteError(w, r, http.StatusConflict, TypeUniqueness,
			"That externalId is already bound to another account.")
	default:
		var apiErr *httpx.Error
		if errors.As(err, &apiErr) && apiErr.Status < http.StatusInternalServerError {
			WriteError(w, r, apiErr.Status, TypeInvalidValue, apiErr.Message)
			return
		}
		WriteError(w, r, http.StatusInternalServerError, "", err.Error())
	}
}
