package config_test

import (
	"os"
	"regexp"
	"sort"
	"testing"
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
