package server_test

import (
	"os"
	"regexp"
	"testing"
)

// One Go version, stated in four places, and go.mod is the one that decides.
//
// Every workflow resolves its toolchain through `go-version-file: go.mod`,
// so raising that line raises CI and a release build without anybody
// touching them. What it does not raise is the three places a person reads:
// README and CONTRIBUTING, which are what somebody consults before
// installing anything, and the release image's base, which is what actually
// compiles the binary shipped.
//
// Those went stale exactly as you would expect — README and CONTRIBUTING
// named a version two releases behind what the module required, so a
// contributor who installed what the documents asked for got a build
// failure from the first `go build`, with an error about a language
// feature rather than about a version.
//
// The check is deliberately narrow: these files, this pattern, every
// occurrence. A file that stops mentioning it at all fails too, because a
// contributor arriving at README needs the answer to be there.
func TestTheDocumentedGoVersionIsTheOneTheModuleRequires(t *testing.T) {
	module, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	declared := regexp.MustCompile(`(?m)^go (\d+\.\d+)`).FindSubmatch(module)
	if declared == nil {
		t.Fatal("go.mod has no `go` directive")
	}
	want := string(declared[1])

	// The `go` directive is a floor, so the documents say "1.26+" and the
	// image pins the same minor. Both are the same number.
	for path, pattern := range map[string]*regexp.Regexp{
		"../../README.md":         regexp.MustCompile(`Go (\d+\.\d+)\+`),
		"../../CONTRIBUTING.md":   regexp.MustCompile(`Go (\d+\.\d+)\+`),
		"../../deploy/Dockerfile": regexp.MustCompile(`golang:(\d+\.\d+)`),
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		found := pattern.FindAllSubmatch(content, -1)
		if len(found) == 0 {
			t.Errorf("%s no longer states which Go version is needed; go.mod "+
				"requires %s and somebody has to be told", path, want)
			continue
		}
		for _, match := range found {
			if got := string(match[1]); got != want {
				t.Errorf("%s says Go %s, go.mod requires %s; whoever installs "+
					"what this file asks for cannot build the module",
					path, got, want)
			}
		}
	}
}
