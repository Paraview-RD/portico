package directory_test

// Against a real LDAP server.
//
// The unit tests next door cover the decisions — which attribute becomes
// which field, what a binary objectGUID renders as. This covers the part no
// amount of reasoning establishes: that the connection, the bind, the paged
// search and the filter actually work against a server that implements the
// protocol rather than against a struct this repository also wrote.
//
// It is the same argument as the PostgreSQL container in internal/testdb. A
// fake directory that returns what the caller expects proves the caller
// consistent with itself.

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/paraview/portico/internal/directory"
)

const (
	adminDN       = "cn=admin,dc=example,dc=org"
	adminPassword = "portico-test-admin"
	baseDN        = "dc=example,dc=org"
)

// seed is the directory these tests read: two people, one of whom has no
// mail attribute, so the optional-attribute path is exercised rather than
// assumed.
const seed = `
dn: ou=people,dc=example,dc=org
objectClass: organizationalUnit
ou: people

dn: uid=zhangsan,ou=people,dc=example,dc=org
objectClass: inetOrgPerson
uid: zhangsan
cn: Zhang San
sn: Zhang
mail: zhangsan@example.org
telephoneNumber: +86 10 1234 5678

dn: uid=lisi,ou=people,dc=example,dc=org
objectClass: inetOrgPerson
uid: lisi
cn: Li Si
sn: Li
`

// startDirectory brings up OpenLDAP with the seed above loaded.
func startDirectory(t *testing.T) directory.Config {
	t.Helper()

	if os.Getenv("PORTICO_SKIP_CONTAINER_TESTS") != "" {
		t.Skip("PORTICO_SKIP_CONTAINER_TESTS is set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seedDir := t.TempDir()
	if err := os.WriteFile(seedDir+"/seed.ldif", []byte(strings.TrimSpace(seed)+"\n"), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			// Pinned. The image is no longer developed upstream, which for a
			// test fixture is a feature: it will not change under the suite.
			Image:        "osixia/openldap:1.5.0",
			ExposedPorts: []string{"389/tcp"},
			Env: map[string]string{
				"LDAP_ORGANISATION":   "Portico Test",
				"LDAP_DOMAIN":         "example.org",
				"LDAP_ADMIN_PASSWORD": adminPassword,
			},
			Files: []testcontainers.ContainerFile{{
				HostFilePath:      seedDir + "/seed.ldif",
				ContainerFilePath: "/container/service/slapd/assets/config/bootstrap/ldif/custom/seed.ldif",
				FileMode:          0o644,
			}},
			Cmd:        []string{"--copy-service"},
			WaitingFor: wait.ForLog("slapd starting").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("no container runtime available: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	endpoint, err := container.PortEndpoint(ctx, "389/tcp", "")
	if err != nil {
		t.Fatalf("container endpoint: %v", err)
	}
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		t.Fatalf("split %q: %v", endpoint, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}

	return directory.Config{
		Host: host, Port: port, Encryption: directory.EncryptionNone,
		BindDN: adminDN, BindPassword: adminPassword,
		BaseDN: baseDN, UserFilter: "(objectClass=inetOrgPerson)",
		Attributes: directory.AttributeMap{
			Username: "uid", DisplayName: "cn",
			Email: "mail", Phone: "telephoneNumber",
			ExternalID: "entryUUID",
		},
		Timeout: 30 * time.Second,
	}
}

func TestReadingARealDirectory(t *testing.T) {
	cfg := startDirectory(t)

	client, err := directory.Dial(cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	entries, skipped, err := client.Users()
	if err != nil {
		t.Fatalf("read users: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped %d entries: %v", len(skipped), skipped)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d entries, want 2: %+v", len(entries), entries)
	}

	byUsername := map[string]directory.Entry{}
	for _, e := range entries {
		byUsername[e.Username] = e
	}

	zhang, ok := byUsername["zhangsan"]
	if !ok {
		t.Fatalf("zhangsan is missing from %+v", entries)
	}
	if zhang.DisplayName != "Zhang San" {
		t.Errorf("displayName = %q, want %q", zhang.DisplayName, "Zhang San")
	}
	if zhang.Email != "zhangsan@example.org" {
		t.Errorf("email = %q", zhang.Email)
	}
	if zhang.Phone == "" {
		t.Error("phone is empty although the entry has telephoneNumber")
	}

	// entryUUID is what a real server generates, and it is the reconciliation
	// key. Its exact value is the server's to choose; what matters is that
	// there is one and that it is stable, which the next check establishes.
	if len(zhang.ExternalID) < 30 || !strings.Contains(zhang.ExternalID, "-") {
		t.Errorf("externalId = %q, which does not look like the UUID the "+
			"server assigns", zhang.ExternalID)
	}

	// An entry with no mail attribute is imported with an empty one rather
	// than skipped. Optional means optional.
	li, ok := byUsername["lisi"]
	if !ok {
		t.Fatalf("lisi is missing from %+v", entries)
	}
	if li.Email != "" {
		t.Errorf("email = %q for an entry with no mail attribute", li.Email)
	}

	// And reading twice returns the same identifiers. If it did not, every
	// run would deactivate everybody and recreate them.
	again, _, err := client.Users()
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	for _, e := range again {
		if e.ExternalID != byUsername[e.Username].ExternalID {
			t.Errorf("%s changed identifier between reads: %q then %q",
				e.Username, byUsername[e.Username].ExternalID, e.ExternalID)
		}
	}
}

func TestFilterDecidesWhoIsRead(t *testing.T) {
	cfg := startDirectory(t)
	cfg.UserFilter = "(uid=lisi)"

	client, err := directory.Dial(cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	entries, _, err := client.Users()
	if err != nil {
		t.Fatalf("read users: %v", err)
	}
	if len(entries) != 1 || entries[0].Username != "lisi" {
		t.Errorf("filter (uid=lisi) returned %+v, want only lisi", entries)
	}
}

func TestWrongCredentialsAreReportedRatherThanIgnored(t *testing.T) {
	cfg := startDirectory(t)
	cfg.BindPassword = "not-the-password"

	client, err := directory.Dial(cfg)
	if err == nil {
		client.Close()
		t.Fatal("dialling with a wrong bind password succeeded; a directory " +
			"connector that silently falls back to an anonymous bind would " +
			"read whatever that identity can see and call it the truth")
	}
	if !strings.Contains(err.Error(), "bind") {
		t.Errorf("error = %v, want it to name the bind as the failure", err)
	}
}

// A base DN that matches nothing returns nothing rather than erroring, which
// is exactly why the service treats an empty result as suspicious rather
// than as fact.
func TestUnmatchedBaseLooksIdenticalToAnEmptyDirectory(t *testing.T) {
	cfg := startDirectory(t)
	cfg.BaseDN = "ou=nobody," + baseDN

	client, err := directory.Dial(cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	entries, _, err := client.Users()
	if err != nil {
		// Some servers answer "no such object" instead, which is also fine —
		// the point is that it is not a populated result.
		if !strings.Contains(strings.ToLower(err.Error()), "no such object") {
			t.Fatalf("read users: %v", err)
		}
		return
	}
	if len(entries) != 0 {
		t.Fatalf("a base DN matching nothing returned %d entries", len(entries))
	}
	fmt.Fprintln(os.Stderr, "note: an unmatched base DN is indistinguishable "+
		"from an empty directory, which is why service.runSync refuses to act on one")
}
