package config_test

// The Blueprint asks for a handful of values every time it is applied, and
// the demo is applied again roughly monthly — a free Render Postgres expires
// after thirty days plus a fortnight of grace, and the whole thing has to be
// rebuilt. So the list of what has to be re-entered is a document somebody
// depends on at a moment when nothing else remembers.
//
// Which makes it the shape of thing that goes stale by omission. Adding a
// `sync: false` key to render.yaml does not make anybody update the list, and
// a missing entry is not visibly wrong — it is invisible, and it surfaces as
// an unexplained prompt during a rebuild, a month later, from somebody who no
// longer remembers what that value was. Same failure as docs/README.md and
// the layout diagrams: a list claiming to be the whole of something has to be
// checked against the thing.

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type blueprint struct {
	Services []struct {
		EnvVars []struct {
			Key  string `yaml:"key"`
			Sync *bool  `yaml:"sync"`
		} `yaml:"envVars"`
	} `yaml:"services"`
}

func TestEveryValueTheBlueprintAsksForIsOnTheList(t *testing.T) {
	raw, err := os.ReadFile("../../render.yaml")
	if err != nil {
		t.Fatalf("read render.yaml: %v", err)
	}
	var bp blueprint
	if err := yaml.Unmarshal(raw, &bp); err != nil {
		t.Fatalf("parse render.yaml: %v", err)
	}
	if len(bp.Services) == 0 {
		t.Fatal("render.yaml declares no services")
	}

	list, err := os.ReadFile("../../deploy/demo-values.example.env")
	if err != nil {
		t.Fatalf("read the value list: %v", err)
	}

	// `sync: false` is Render's way of saying "ask the human". Those are
	// exactly the values nothing else in the repository holds.
	asked := 0
	for _, env := range bp.Services[0].EnvVars {
		if env.Sync == nil || *env.Sync {
			continue
		}
		asked++
		if !strings.Contains(string(list), env.Key+"=") {
			t.Errorf("render.yaml asks for %s at apply time, and "+
				"deploy/demo-values.example.env does not list it — so a rebuild "+
				"will ask for a value nobody wrote down", env.Key)
		}
	}

	// A rule that matches nothing passes. If the parse silently yielded no
	// prompted keys, the loop above would be vacuous and this file would go on
	// claiming to guard something.
	if asked == 0 {
		t.Error("found no `sync: false` keys in render.yaml, which means this " +
			"test checked nothing — the parse or the file has changed shape")
	}
}
