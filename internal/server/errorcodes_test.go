package server_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/service"
)

// Every error code the server can return has something for a reader.
//
// The console already checks its two bundles against each other, and the type
// system already requires the Chinese one to cover every English key. Neither
// can see the dimension that actually goes wrong: both bundles missing the
// same code. That is the normal case, because a feature adds its codes to the
// server and to neither bundle, and it is invisible in English — falling back
// to the server's own message produces English either way. Only a Chinese
// reader sees it, as an English sentence in the middle of their screen.
//
// Fifteen codes had accumulated that way, all from the three most recent
// features, before anybody looked.
//
// This lives in Go because Go is where the codes are written. It reads the
// English bundle as text rather than as a module, which is the same trade the
// OpenAPI guard makes with its YAML.

const errorBundlePath = "../../web/src/i18n/errors-en-US.ts"

// codeAtCallSite matches the constructors errors are built with. A code
// written in some other shape is missed rather than invented, so this fails
// quietly rather than falsely — hence the floor asserted below.
var codeAtCallSite = regexp.MustCompile(
	`httpx\.(?:BadRequest|NotFound|Unauthorized|Forbidden|Conflict|` +
		`UnprocessableEntity|Internal|NewError)\(\s*(?:[^,()]+,\s*)?"([A-Z][A-Z0-9_]+)"`)

// bundleKey matches an entry in the English bundle.
var bundleKey = regexp.MustCompile(`(?m)^\s*([A-Z][A-Z0-9_]+):`)

// neverReachesTheConsole are codes no person reads in the interface, and
// which therefore need no translation. Each is listed with its reason; the
// list is the documentation, so an entry without one does not belong here.
var neverReachesTheConsole = map[string]string{
	"NOT_READY": "answered to an orchestrator polling /api/v1/ready; " +
		"the console never calls it, and a load balancer does not read Chinese",
}

func TestEveryServerErrorCodeHasAMessageForAReader(t *testing.T) {
	codes := serverErrorCodes(t)

	// Without this, a regular expression that stopped matching would turn
	// this test into one that checks nothing and still passes.
	if len(codes) < 50 {
		t.Fatalf("found only %d error codes in the server; the pattern has "+
			"probably stopped matching, which would make this test vacuous",
			len(codes))
	}

	bundle := errorBundleCodes(t)
	if len(bundle) < 50 {
		t.Fatalf("found only %d entries in %s; the bundle or this test is broken",
			len(bundle), errorBundlePath)
	}

	var missing []string
	for _, code := range codes {
		if bundle[code] {
			continue
		}
		if _, excused := neverReachesTheConsole[code]; excused {
			continue
		}
		missing = append(missing, code)
	}

	if len(missing) > 0 {
		t.Errorf("these codes have no entry in %s, so a Chinese reader gets "+
			"the server's English sentence:\n  %s",
			errorBundlePath, strings.Join(missing, "\n  "))
	}
}

// TestTheExcusedListDoesNotOutliveItsCodes keeps the exception list honest.
//
// An excuse for a code that no longer exists is worse than no list: it reads
// as a considered decision about something that is not there.
func TestTheExcusedListDoesNotOutliveItsCodes(t *testing.T) {
	codes := map[string]bool{}
	for _, code := range serverErrorCodes(t) {
		codes[code] = true
	}

	for code := range neverReachesTheConsole {
		if !codes[code] {
			t.Errorf("%s is excused from translation but the server no longer "+
				"returns it; remove the exception", code)
		}
	}
}

// TestTheConsoleFiltersOnTheActorTheServerRecords keeps one string in step
// across two languages.
//
// The provisioning screen lists what a directory changed by asking the audit
// log for one actor. That actor is a literal in Go and a literal in
// TypeScript, and if they stop agreeing the screen does not break — it shows
// an empty table, which is indistinguishable from a directory that has done
// nothing, and is exactly the confusion the screen was added to remove.
func TestTheConsoleFiltersOnTheActorTheServerRecords(t *testing.T) {
	const endpointsPath = "../../web/src/api/endpoints.ts"

	source, err := os.ReadFile(filepath.Clean(endpointsPath))
	if err != nil {
		t.Fatalf("read %s: %v", endpointsPath, err)
	}

	match := regexp.MustCompile(`PROVISIONING_ACTOR\s*=\s*"([^"]+)"`).
		FindSubmatch(source)
	if match == nil {
		t.Fatalf("no PROVISIONING_ACTOR in %s; either it was renamed or the "+
			"console has stopped filtering on an actor at all", endpointsPath)
	}

	if got := string(match[1]); got != service.ProvisioningActor {
		t.Errorf("the console filters on %q but the server records %q, so the "+
			"provisioning screen shows an empty table and looks like a "+
			"directory that has done nothing", got, service.ProvisioningActor)
	}
}

func serverErrorCodes(t *testing.T) []string {
	t.Helper()

	found := map[string]bool{}
	err := filepath.WalkDir("../..", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// The frontend's node_modules is large and contains no Go.
			if entry.Name() == "node_modules" || entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return err
		}
		for _, match := range codeAtCallSite.FindAllSubmatch(source, -1) {
			found[string(match[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the source tree: %v", err)
	}

	codes := make([]string, 0, len(found))
	for code := range found {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func errorBundleCodes(t *testing.T) map[string]bool {
	t.Helper()

	source, err := os.ReadFile(filepath.Clean(errorBundlePath))
	if err != nil {
		t.Fatalf("read %s: %v", errorBundlePath, err)
	}

	codes := map[string]bool{}
	for _, match := range bundleKey.FindAllSubmatch(source, -1) {
		codes[string(match[1])] = true
	}
	return codes
}
