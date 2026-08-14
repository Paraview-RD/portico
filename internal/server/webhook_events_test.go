package server_test

// Every event type the console offers has to be one something sends.
//
// organization.created and organization.updated were declared, listed in
// AllEvents, offered as checkboxes, and covered by the wildcard — and no code
// anywhere published them. A subscriber selecting them got silence, and
// silence from a webhook is indistinguishable from "nothing happened". There
// was no error to find, in any log, on either side.
//
// Two things had to be true for that bug, so there are two tests. The list
// has to name only events some service actually sends, and the service that
// sends them has to have been handed a publisher — the second is a single
// line in server.go, and forgetting it produces exactly the same silence.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/webhook"
)

// eventConstantNames maps each event's wire value to the Go constant that
// holds it, read from the declaration rather than restated here — a copy
// would go stale in the direction that makes this test pass wrongly.
func eventConstantNames(t *testing.T) map[string]string {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("..", "webhook", "event.go"))
	if err != nil {
		t.Fatalf("read event declarations: %v", err)
	}

	names := map[string]string{}
	declaration := regexp.MustCompile(`(?m)^\s*(Event\w+)\s*=\s*"([^"]+)"`)
	for _, match := range declaration.FindAllStringSubmatch(string(source), -1) {
		names[match[2]] = match[1]
	}
	if len(names) == 0 {
		t.Fatal("found no event constants; the declaration moved or this test stopped working")
	}
	return names
}

func TestEveryOfferedEventIsOneSomethingSends(t *testing.T) {
	constants := eventConstantNames(t)

	// Every non-test file in the service package, as one body of text. Which
	// file publishes an event does not matter; that none does is the bug.
	var published strings.Builder
	entries, err := os.ReadDir(filepath.Join("..", "service"))
	if err != nil {
		t.Fatalf("read service package: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join("..", "service", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		published.Write(source)
	}
	haystack := published.String()

	for _, event := range webhook.AllEvents {
		constant, ok := constants[event]
		if !ok {
			t.Errorf("%q is in AllEvents but is not a declared constant", event)
			continue
		}
		if !strings.Contains(haystack, "webhook."+constant) {
			t.Errorf("%q (webhook.%s) is offered to subscribers and no service sends it.\n"+
				"A subscription selecting it would wait forever, and nothing would "+
				"report that. Either publish it where it happens, or take it out of "+
				"AllEvents so it is not offered.", event, constant)
		}
	}
}

// The other half. A service can publish correctly and still send nothing,
// because the publisher is attached after construction and the attaching is a
// line somebody has to remember to write.
func TestEveryServiceThatPublishesIsGivenAPublisher(t *testing.T) {
	fset := token.NewFileSet()

	// Which types declare WithEvents, and are therefore expecting one.
	expecting := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join("..", "service"))
	if err != nil {
		t.Fatalf("read service package: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		syntax, err := parser.ParseFile(fset, filepath.Join("..", "service", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(syntax, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "WithEvents" || fn.Recv == nil {
				return true
			}
			if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if ident, ok := star.X.(*ast.Ident); ok {
					expecting[ident.Name] = true
				}
			}
			return true
		})
	}
	if len(expecting) == 0 {
		t.Fatal("no service declares WithEvents; either it was renamed or this test stopped working")
	}

	wiring, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}

	// The constructor names the type, and the variable it is assigned to is
	// what WithEvents is called on — so the check is that server.go both
	// builds the service and calls WithEvents on the same variable.
	assignment := regexp.MustCompile(`(?m)^\s*(\w+)\s*:?=\s*service\.New(\w+)\(`)
	built := map[string]string{}
	for _, match := range assignment.FindAllStringSubmatch(string(wiring), -1) {
		built[match[2]] = match[1]
	}

	for typeName := range expecting {
		variable, ok := built[typeName]
		if !ok {
			t.Errorf("%s declares WithEvents but server.go never constructs it", typeName)
			continue
		}
		if !strings.Contains(string(wiring), variable+".WithEvents(") {
			t.Errorf("%s publishes events and server.go never calls %s.WithEvents.\n"+
				"Its events are built, queued to nobody, and dropped — the same "+
				"silence as not publishing them at all.", typeName, variable)
		}
	}
}

// And the whole chain, once, against the real wiring: creating an
// organization has to reach a subscriber's queue. The two guards above are
// source checks and would both pass on a publish that threw its event away.
func TestCreatingAnOrganizationQueuesAnEvent(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	// Seeded directly, because registering a destination refuses addresses
	// like this one — and rightly: that rule is what stops this server being
	// used as a proxy into its own network. It is tested where it lives. What
	// is under test here is whether an organization produces an event at all.
	const subscriptionID = "sub-organization-events"
	api.execSQL(t,
		`INSERT INTO webhook_subscriptions (id, tenant_id, name, url, secret, events, status, created_at, updated_at)
		 VALUES ($1, (SELECT id FROM tenants ORDER BY created_at LIMIT 1),
		         'events', 'https://receiver.example.com/hooks', 'whsec_test', '*', 'ACTIVE', now(), now())`,
		subscriptionID)

	res := api.do(http.MethodPost, "/api/v1/organizations", admin,
		map[string]any{"name": "Events", "code": "EVENTS"})
	if res.Status != http.StatusOK {
		t.Fatalf("create organization: %d %s %s", res.Status, res.Code, res.Message)
	}

	var page struct {
		Items []struct {
			EventType string `json:"eventType"`
		} `json:"items"`
	}
	api.do(http.MethodGet, "/api/v1/webhooks/"+subscriptionID+"/deliveries", admin, nil).
		into(t, &page)

	for _, delivery := range page.Items {
		if delivery.EventType == webhook.EventOrgCreated {
			return
		}
	}
	t.Errorf("creating an organization queued %v, and none of them is %s",
		page.Items, webhook.EventOrgCreated)
}
