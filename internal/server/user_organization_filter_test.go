package server_test

// Filtering the user list by organization.
//
// The filter is reached from a tree, and a tree is a claim about containment:
// picking a division means the division. So the question these hold down is
// not "does the parameter work" but "how much does picking a node mean" —
// which is a decision, not an implementation detail, and one that four
// different pieces of the system now depend on agreeing about.
//
// The last two are the ones worth having. An empty subtree that quietly fell
// back to no filter at all would turn a mistyped id into a listing of the
// whole tenant, and a subtree walk that left the tenant would do the same
// across a boundary nothing else in this system lets you cross.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/Paraview-RD/portico/internal/service"
)

// createUserIn creates an account filed under an organization. The shared
// createUser helper deliberately does not take one — most tests do not care —
// and every test in this file does.
func (a *apiTest) createUserIn(token, username, organizationID string) {
	a.t.Helper()

	res := a.do(http.MethodPost, "/api/v1/users", token, map[string]any{
		"username":       username,
		"displayName":    username,
		"password":       "password-12345",
		"role":           "USER",
		"organizationId": organizationID,
	})
	if res.Status != http.StatusOK {
		a.t.Fatalf("create %s in %s: %d %s %s",
			username, organizationID, res.Status, res.Code, res.Message)
	}
}

// usernamesFilteredBy lists the accounts the filter selects, sorted so a
// failure names a set rather than an order.
func (a *apiTest) usernamesFilteredBy(token, organizationID string) []string {
	a.t.Helper()

	res := a.do(http.MethodGet,
		"/api/v1/users?pageSize=100&organizationId="+organizationID, token, nil)
	if res.Status != http.StatusOK {
		a.t.Fatalf("list by organization %q: %d %s %s",
			organizationID, res.Status, res.Code, res.Message)
	}

	var page struct {
		Items []struct {
			Username string `json:"username"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	res.into(a.t, &page)

	names := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		names = append(names, item.Username)
	}
	sort.Strings(names)

	// The count drives the pager, so a total that disagrees with the rows is
	// a screen that offers a second page of nothing — or hides people.
	if int64(len(names)) != page.Total {
		a.t.Errorf("filtering by %q returned %d rows but a total of %d",
			organizationID, len(names), page.Total)
	}
	return names
}

func equalNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// chart builds the shape every test here uses:
//
//	engineering ── platform ── storage
//	sales
//
// with one person filed at each node, one unfiled, and the tenant's own
// administrator, who is also unfiled.
type chart struct {
	engineering, platform, storage, sales string
}

func buildChart(a *apiTest, token string) chart {
	a.t.Helper()

	c := chart{}
	c.engineering = a.createOrg(token, "Engineering", "ENG", "")
	c.platform = a.createOrg(token, "Platform", "PLATFORM", c.engineering)
	c.storage = a.createOrg(token, "Storage", "STORAGE", c.platform)
	c.sales = a.createOrg(token, "Sales", "SALES", "")

	a.createUserIn(token, "erin-eng", c.engineering)
	a.createUserIn(token, "pat-platform", c.platform)
	a.createUserIn(token, "sam-storage", c.storage)
	a.createUserIn(token, "sally-sales", c.sales)
	a.createUser(token, "nobody-home", "password-12345", "USER")

	return c
}

func TestPickingAnOrganizationIncludesEverybodyUnderIt(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	c := buildChart(api, admin)

	for _, tc := range []struct {
		label string
		id    string
		want  []string
	}{
		{
			// The whole point. Three levels, and the sibling branch stays out
			// — a walk that returned everybody would pass a test that only
			// checked the depth.
			label: "the top of a branch",
			id:    c.engineering,
			want:  []string{"erin-eng", "pat-platform", "sam-storage"},
		},
		{
			label: "the middle of a branch",
			id:    c.platform,
			want:  []string{"pat-platform", "sam-storage"},
		},
		{
			// A leaf is the case the old exact-match behaviour got right, so
			// it is the one that would keep passing if the change were
			// reverted. It is here to say what did not change.
			label: "a leaf",
			id:    c.storage,
			want:  []string{"sam-storage"},
		},
		{
			label: "the sibling branch",
			id:    c.sales,
			want:  []string{"sally-sales"},
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			got := api.usernamesFilteredBy(admin, tc.id)
			if !equalNames(got, tc.want) {
				t.Errorf("filtering by %s returned %v, want %v", tc.label, got, tc.want)
			}
		})
	}
}

func TestThePeopleInNoOrganizationCanBeAskedFor(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	buildChart(api, admin)

	got := api.usernamesFilteredBy(admin, service.UnassignedOrganization)

	// The administrator is unfiled too, and belongs in this answer: the
	// question is "who is not in the chart", and quietly excluding the
	// account asking it would make the screen disagree with itself.
	want := []string{adminUsername, "nobody-home"}
	sort.Strings(want)
	if !equalNames(got, want) {
		t.Errorf("the unfiled are %v, want %v", got, want)
	}
}

// An organization id that matches nothing has to match nobody. The dangerous
// failure is the opposite one: an empty set of ids collapsing to no filter,
// so a mistyped or stale id lists the entire tenant while looking like it
// applied a filter.
func TestAnOrganizationThatIsNotThereMatchesNobodyRatherThanEverybody(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	buildChart(api, admin)

	got := api.usernamesFilteredBy(admin, "3f7c1b52-0000-4000-8000-000000000000")
	if len(got) != 0 {
		t.Errorf("an organization that does not exist matched %v", got)
	}
}

// The walk must not leave the tenant. Two tenants build the same chart, and
// neither may see the other's people through it — including when one is
// handed the other's organization id, which is the only way a caller can
// name a node outside their own chart at all.
//
// What this does not discriminate, having been checked: the tenant
// predicates inside the subtree query itself. Take either one out and this
// still passes, because the outer filter on users.tenant_id and the
// composite foreign key on users.organization_id already make a foreign
// organization unmatchable. The predicates are redundancy and are documented
// as such in List; this test holds the behaviour, not that particular
// belt-and-braces.
func TestPickingAnOrganizationStopsAtTheTenantBoundary(t *testing.T) {
	api, first, second := newMultiTenantTest(t)

	firstChart := buildChart(api, first.token)

	secondEngineering := api.createOrg(second.token, "Engineering", "ENG", "")
	secondPlatform := api.createOrg(second.token, "Platform", "PLATFORM", secondEngineering)
	api.createUserIn(second.token, "other-tenant-person", secondPlatform)

	got := api.usernamesFilteredBy(first.token, firstChart.engineering)
	want := []string{"erin-eng", "pat-platform", "sam-storage"}
	if !equalNames(got, want) {
		t.Errorf("filtering in one tenant returned %v, want %v", got, want)
	}
	for _, name := range got {
		if name == "other-tenant-person" {
			t.Fatal("the subtree walk crossed into another tenant's chart")
		}
	}

	if crossed := api.usernamesFilteredBy(first.token, secondEngineering); len(crossed) != 0 {
		t.Errorf("another tenant's organization id matched %v; it must name nothing "+
			"in this tenant, not reach across", crossed)
	}
}

// The export shares List, and shares it precisely so that "export what I am
// looking at" is one set of filters rather than two that drift. The claim is
// in docs and in the handler's comment; this is what makes it true rather
// than intended.
func TestTheExportCarriesTheSameSubtreeTheScreenShows(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	c := buildChart(api, admin)

	onScreen := api.usernamesFilteredBy(admin, c.engineering)

	// Fetched directly rather than through api.do, which decodes an
	// envelope: this response is a workbook.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/users/export?organizationId="+c.engineering, nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	rec := httptest.NewRecorder()
	api.srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export with an organization filter: %d %s", rec.Code, rec.Body.String())
	}

	book, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("the export is not a workbook: %v", err)
	}
	defer func() { _ = book.Close() }()

	rows, err := book.GetRows(book.GetSheetName(0))
	if err != nil {
		t.Fatalf("read the workbook: %v", err)
	}
	if len(rows) < 1 {
		t.Fatal("the workbook has no header row")
	}

	var exported []string
	for _, row := range rows[1:] {
		if len(row) > 0 && row[0] != "" {
			exported = append(exported, row[0])
		}
	}
	sort.Strings(exported)

	if !equalNames(exported, onScreen) {
		t.Errorf("the export holds %v and the screen shows %v; one filter, two answers",
			exported, onScreen)
	}
}
