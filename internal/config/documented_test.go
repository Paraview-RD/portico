package config_test

import (
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/Paraview-RD/portico/internal/seed"
	"github.com/Paraview-RD/portico/internal/service"
)

// Every setting the server reads is a setting an operator can find.
//
// Configuration here is entirely environment variables, which means there is
// no schema, no defaults file, and nothing that fails when a variable is
// added and nobody says so. `PORTICO_METRICS_ADDR` was read by the server,
// listed in .env.example, and absent from the binary's own `--help` for as
// long as it had existed — an operator running `portico --help` was looking
// at a complete-looking list that was not.
//
// The two places checked are the two an operator actually reaches: the help
// output, which is what somebody types when the server is in front of them,
// and .env.example, which is what they copy when setting one up. Doc prose
// is deliberately not checked — README points at .env.example rather than
// restating it, which is the arrangement that cannot drift.
func TestEveryEnvironmentVariableIsDocumented(t *testing.T) {
	names := regexp.MustCompile(`PORTICO_[A-Z0-9_]+`)

	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	// What the server reads. Taken from the source rather than from a list
	// kept alongside it, because a list kept alongside it is the thing that
	// goes stale.
	used := unique(names.FindAllString(read("config.go"), -1))

	usage := read("../../cmd/server/main.go")
	example := read("../../.env.example")

	for _, name := range used {
		if !contains(usage, name) {
			t.Errorf("%s is read by the server and absent from `portico --help`; "+
				"an operator reading that list believes it is complete", name)
		}
		if !contains(example, name) {
			t.Errorf("%s is read by the server and absent from .env.example, "+
				"which is the file people copy to configure a deployment", name)
		}
	}

	// And the other direction, which catches the rename: a variable
	// documented in both places that nothing reads any more is worse than an
	// undocumented one, because somebody will set it and wonder why it had
	// no effect.
	for _, name := range unique(names.FindAllString(example, -1)) {
		if !contains(read("config.go"), name) {
			t.Errorf(".env.example documents %s, which the server no longer "+
				"reads; somebody will set it and wonder why nothing changed", name)
		}
	}
}

func unique(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Every example connection string names the same port.
//
// Six files print one — README, .env.example, both access guides, the
// dev-stack page, and testdb's own comment — and no two of them were obliged
// to agree. Two said 5443 and the rest 5432, which I read as a typo in the
// two and corrected, when it was the convention: 5443 is the host mapping
// deploy/docker-compose.yml offers, and 5432 is PostgreSQL's default and
// therefore the port most likely to already belong to another project on a
// developer's machine. Following the corrected file connected to somebody
// else's database and succeeded.
//
// The port itself is not the point and this test does not name one. What it
// holds is that the six agree, so the next person to change one changes all
// six or hears about it.
//
// CI is excluded deliberately: its service container binds 5432 in a fresh
// runner, where there is nothing to collide with, and that file is not an
// example anybody copies.
func TestEveryExampleDSNUsesTheSamePort(t *testing.T) {
	pattern := regexp.MustCompile(`postgres://[^@\s]+@(?:localhost|127\.0\.0\.1):(\d+)/`)

	files := []string{
		"../../README.md",
		"../../.env.example",
		"../../docs/access-guide.md",
		"../../docs/access-guide.zh.md",
		"../../docs/dev-stack.md",
		"../../internal/testdb/testdb.go",
	}

	ports := map[string][]string{}
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		found := pattern.FindAllStringSubmatch(string(content), -1)
		if len(found) == 0 {
			t.Errorf("%s no longer shows an example connection string; if that "+
				"is deliberate, take it out of this list", path)
			continue
		}
		for _, match := range found {
			ports[match[1]] = append(ports[match[1]], path)
		}
	}

	if len(ports) > 1 {
		for port, where := range ports {
			t.Errorf("port %s is used by %v", port, where)
		}
		t.Error("the example connection strings disagree about the port; a " +
			"reader who copies the wrong one reaches a database that is not " +
			"this one, and is told nothing")
	}
}

// Every document that states the bootstrap password states the same one.
//
// The default is a published credential now, which means it is written down
// in five places an operator might read and in exactly one place the server
// reads. Nothing obliged them to agree. A constant changed without the
// documents is worse here than in most places: the reader does not discover
// the drift as a broken link or a missing page, they discover it as a sign-in
// that fails on a fresh installation, with the manual insisting the password
// is right.
//
// The same failure has already happened once in this repository with the
// example database port, which is why that check exists directly above.
//
// The value is not written here. This asserts agreement with the constant,
// so changing the constant is a normal edit that reports which documents
// still say the old thing.
//
// CHANGELOG.md and the requirements pages are deliberately absent. They are
// a record of what was true when it was written, and a check that forced
// history to be rewritten every time a default moved would be asking them
// to stop being a record.
func TestEveryDocumentedBootstrapPasswordIsTheOneTheServerUses(t *testing.T) {
	files := []string{
		"../../.env.example",
		"../../CONTRIBUTING.md",
		"../../SECURITY.md",
		"../../docs/access-guide.md",
		"../../docs/access-guide.zh.md",
		"../../docs/dev-stack.md",
	}

	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !contains(string(content), service.DefaultInitialAdminPassword) {
			t.Errorf("%s does not name the bootstrap default password %q.\n"+
				"Either it drifted from service.DefaultInitialAdminPassword, or "+
				"the document stopped mentioning it — in which case take it out "+
				"of this list rather than leaving a check that passes vacuously.",
				path, service.DefaultInitialAdminPassword)
		}
	}
}

// The Codespace's welcome screen names the password its accounts actually
// have.
//
// This is the check above applied to a sixth document, and it is separate
// rather than folded in because it is about a different constant. The
// bootstrap default and the seeded password are two constants that happen to
// hold the same string; nothing ties them, and a check that asserted the
// wrong one would pass by coincidence until the day somebody moved one of
// them.
//
// The failure this prevents already happened. The Codespace was written
// against a branch where the seeded password was something else, the seed
// changed underneath it on main, and neither branch's tests noticed because
// each was green on its own base. What shipped would have been a welcome
// screen printing a password that opened three of the four accounts it
// listed — and the fourth did not exist.
func TestTheCodespaceWelcomeNamesTheSeededPassword(t *testing.T) {
	const path = "../../.devcontainer/setup.sh"

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !contains(string(content), seed.DemoPassword) {
		t.Errorf("%s does not name the seeded password %q.\n"+
			"Either it drifted from seed.DemoPassword, or the welcome text "+
			"stopped naming a password — in which case delete this check "+
			"rather than leaving one that passes vacuously.",
			path, seed.DemoPassword)
	}
}
