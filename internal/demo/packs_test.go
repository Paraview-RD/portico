package demo

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// These tests read the packs and nothing else — no database, no services. They
// are here because a pack is data, and the ways data goes wrong are the ways a
// compiler cannot see: a person in an organization that was renamed, a group
// listing somebody who was removed, four industries that turned out to be one
// industry with four vocabularies.
//
// The last of those is the one worth a test. The packs exist to show that
// Portico does not impose a shape, and that claim is falsified quietly — by
// somebody copying a pack to start a new one and changing only the strings.

func TestThereAreFivePacksAndTheKeysAreDistinct(t *testing.T) {
	// A count rather than a range, so that adding a pack is a decision made
	// here rather than a change that slips through. The console, the validator
	// and the documentation all follow from this list.
	if len(packs) != 5 {
		t.Fatalf("there are %d packs, want 5 (generic plus four industries)", len(packs))
	}

	seen := map[string]bool{}
	for _, p := range packs {
		if p.Key == "" || p.Label == "" {
			t.Errorf("a pack has no key or no label: %+v", p.Key)
		}
		if seen[p.Key] {
			t.Errorf("two packs share the key %q; the form submits it, so one of them is unreachable", p.Key)
		}
		seen[p.Key] = true
	}

	if packs[0].Key != IndustryGeneric {
		t.Errorf("the first pack is %q, want %q — it is what the form defaults to "+
			"and what a request naming no industry gets", packs[0].Key, IndustryGeneric)
	}
}

// TestTheFourIndustriesAreActuallyDifferent is the point of the whole file.
//
// Attribute keys are the sharpest test available: they are what a tenant
// decided to record about people, they are invented rather than inherited, and
// two industries that agree on all three have not been thought about.
func TestTheFourIndustriesAreActuallyDifferent(t *testing.T) {
	industries := packs[1:]

	keys := map[string]map[string]bool{}
	for _, p := range industries {
		set := map[string]bool{}
		for _, a := range p.Attributes {
			set[a.Key] = true
		}
		keys[p.Key] = set
	}

	for i, a := range industries {
		for _, b := range industries[i+1:] {
			for key := range keys[a.Key] {
				if keys[b.Key][key] {
					t.Errorf("%s and %s both define the attribute %q. The packs are "+
						"here to show that a tenant names its own facts; two industries "+
						"naming the same ones shows the opposite.", a.Key, b.Key, key)
				}
			}
		}
	}

	// And the trees are not one tree renamed. Comparing depth and breadth
	// rather than node count: two packs may happen to have the same number of
	// organizations and still be shaped differently, but if all four agree on
	// both numbers they are the same tree.
	shapes := map[string]bool{}
	for _, p := range packs {
		shapes[fmt.Sprintf("%d-%d-%d", depthOf(p), rootsOf(p), len(p.Orgs))] = true
	}
	if len(shapes) < 4 {
		t.Errorf("the five packs have only %d distinct organization shapes "+
			"(depth, roots, size). They are meant to show different organizations, "+
			"not one organization with different names.", len(shapes))
	}
}

func TestEveryPackIsBigEnoughToLookAtAndSmallEnoughToCreate(t *testing.T) {
	for _, p := range packs {
		t.Run(p.Key, func(t *testing.T) {
			if len(p.Orgs) < 5 {
				t.Errorf("%d organizations; a tree needs enough nodes to be a tree", len(p.Orgs))
			}

			var disabled int
			for _, o := range p.Orgs {
				if o.Disabled {
					disabled++
				}
			}
			if disabled == 0 {
				t.Error("no disabled organization. Disabling keeps the people already " +
					"in it and only stops new assignments, and that is invisible without one.")
			}

			// The upper bound is not taste. Every account costs one bcrypt hash
			// inside the HTTP request that confirms a trial, and the visitor is
			// waiting on it.
			if n := len(p.People); n < 12 || n > 16 {
				t.Errorf("%d accounts, want 12 to 16 — below that the lists look empty, "+
					"above it the visitor waits on bcrypt", n)
			}

			if apps := len(p.OAuth) + len(p.SAML) + len(p.CAS); apps < 3 {
				t.Errorf("%d applications, want at least 3", apps)
			}

			var protocols int
			for _, has := range []bool{len(p.OAuth) > 0, len(p.SAML) > 0, len(p.CAS) > 0} {
				if has {
					protocols++
				}
			}
			if protocols < 2 {
				t.Error("only one protocol is registered. Telling OAuth, SAML and CAS " +
					"apart on the applications screen needs more than one of them present.")
			}

			if len(p.Attributes) != 3 {
				t.Errorf("%d custom attributes, want 3", len(p.Attributes))
			}
			kinds := map[string]bool{}
			for _, a := range p.Attributes {
				kinds[a.Kind] = true
			}
			// A select is what makes the picker on a mapping form worth looking
			// at, and a date is the one kind with a format to get wrong. A pack
			// of three text fields would show that a form can hold a string,
			// which nobody doubted.
			if !kinds[service.FieldKindSelect] || !kinds[service.FieldKindDate] {
				t.Errorf("attribute kinds are %v; want at least one SELECT and one DATE", kinds)
			}
		})
	}
}

// TestEveryReferenceInsideAPackResolves is the check the compiler cannot do.
//
// Packs refer to each other by string keys — a person names an organization, a
// group names people, an attribute names the accounts it is filled for. Every
// one of those is a place where a rename leaves a dangling name, and the
// filler's behaviour for a dangling name is to skip it silently, which is right
// at runtime and useless as a warning.
func TestEveryReferenceInsideAPackResolves(t *testing.T) {
	for _, p := range packs {
		t.Run(p.Key, func(t *testing.T) {
			orgs := map[string]bool{}
			codes := map[string]bool{}
			for _, o := range p.Orgs {
				if o.Key == "" || o.Name == "" || o.Code == "" {
					t.Errorf("organization %+v is missing a key, name or code", o)
				}
				if orgs[o.Key] {
					t.Errorf("two organizations share the key %q", o.Key)
				}
				if codes[o.Code] {
					t.Errorf("two organizations share the code %q, which the service rejects", o.Code)
				}
				// Parents are resolved as the tree is created, in order, so a
				// parent later in the list is not a parent at all.
				if o.Parent != "" && !orgs[o.Parent] {
					t.Errorf("organization %s names parent %q, which is not defined before it",
						o.Code, o.Parent)
				}
				orgs[o.Key] = true
				codes[o.Code] = true
			}

			people := map[string]bool{}
			for _, person := range p.People {
				if people[person.Username] {
					t.Errorf("two accounts share the username %q, which the service rejects",
						person.Username)
				}
				people[person.Username] = true

				if person.Org == "" || !orgs[person.Org] {
					t.Errorf("account %s is in organization %q, which the pack does not define",
						person.Username, person.Org)
				}
				if person.NoContact && (person.Email != "" || person.Phone != "") {
					t.Errorf("account %s is marked as having no contact details and has some; "+
						"they would be dropped, which makes the pack lie about itself", person.Username)
				}
				if !person.NoContact && person.Email == "" {
					t.Errorf("account %s has no address and is not marked NoContact", person.Username)
				}
			}

			for _, g := range p.Groups {
				if len(g.Members) == 0 {
					t.Errorf("group %s has no members; an empty group shows nothing", g.Name)
				}
				for _, username := range g.Members {
					if !people[username] {
						t.Errorf("group %s lists %q, who is not in the pack", g.Name, username)
					}
				}
			}

			for _, a := range p.Attributes {
				if len(a.FilledFor) == 0 {
					t.Errorf("attribute %s is filled in for nobody", a.Key)
				}
				if len(a.FilledFor) >= len(p.People) {
					t.Errorf("attribute %s is filled in for everybody. \"Who has not filled "+
						"this in\" is the question asked before mapping or retiring one, and "+
						"it has no answer here.", a.Key)
				}
				for username, value := range a.FilledFor {
					if !people[username] {
						t.Errorf("attribute %s has a value for %q, who is not in the pack",
							a.Key, username)
					}
					switch a.Kind {
					case service.FieldKindDate:
						if _, err := time.Parse(time.DateOnly, value); err != nil {
							t.Errorf("attribute %s holds %q for %s, which is not a date the "+
								"service accepts", a.Key, value, username)
						}
					case service.FieldKindSelect:
						var ok bool
						for _, allowed := range a.Allowed {
							if allowed == value {
								ok = true
							}
						}
						if !ok {
							t.Errorf("attribute %s holds %q for %s, which is not one of %v",
								a.Key, value, username, a.Allowed)
						}
					}
				}
				if a.Kind == service.FieldKindSelect && len(a.Allowed) < 2 {
					t.Errorf("attribute %s is a select with %d options", a.Key, len(a.Allowed))
				}
			}

			for name, as := range map[string]Assignment{
				"manager": p.Manager, "organization administrator": p.OrgAdmin,
			} {
				if as.Org == "" {
					continue
				}
				if !orgs[as.Org] {
					t.Errorf("the %s is recorded against organization %q, which the pack does not define",
						name, as.Org)
				}
				if !people[as.Username] {
					t.Errorf("the %s is %q, who is not in the pack", name, as.Username)
				}
			}
			// Two records of two different kinds, and deliberately two different
			// people: a pack where they are the same account teaches that being
			// responsible for a department and being recorded to administer it
			// are the same fact.
			if p.Manager.Username != "" && p.Manager.Username == p.OrgAdmin.Username {
				t.Errorf("%s is both the manager and the organization administrator", p.Manager.Username)
			}

			seenClient := map[string]bool{}
			for _, c := range p.OAuth {
				if seenClient[c.ClientID] {
					t.Errorf("two clients share the id %q", c.ClientID)
				}
				seenClient[c.ClientID] = true
				if len(c.Redirect) == 0 || len(c.Scopes) == 0 {
					t.Errorf("client %s has no redirect URI or no scopes", c.ClientID)
				}
				// The service accepts three application types and rejects the
				// rest. Checked here rather than left to the contract test,
				// which found this the hard way: an invalid type fails at fill
				// time, the fill is best effort, and the visitor is handed an
				// empty tenant with the reason only in a log.
				switch strings.ToUpper(c.Type) {
				case model.AppTypeWeb, model.AppTypeNative, model.AppTypeUserAgent:
				default:
					t.Errorf("client %s is of type %q; the service accepts only %s, %s and %s",
						c.ClientID, c.Type, model.AppTypeWeb, model.AppTypeNative,
						model.AppTypeUserAgent)
				}
			}

			for _, sp := range p.SAML {
				if sp.EntityID == "" || sp.Host == "" {
					t.Errorf("service provider %s has no entity id or no host, and the "+
						"metadata is generated from both", sp.Name)
				}
			}
			for _, c := range p.CAS {
				// The service matches a ticket against this prefix, and one
				// that does not end in a slash matches more than it should.
				if !strings.HasSuffix(c.Prefix, "/") {
					t.Errorf("CAS service %s has the prefix %q, which does not end in a slash",
						c.Name, c.Prefix)
				}
			}
		})
	}
}

// TestIndustriesAnswersWithEveryPack holds the list the console reads to the
// list the filler can actually create. They are the same slice today; this is
// what notices if one of them grows a filter.
func TestIndustriesAnswersWithEveryPack(t *testing.T) {
	f := &Filler{}
	names := f.Industries()

	if len(names) != len(packs) {
		t.Fatalf("Industries offers %d packs and there are %d", len(names), len(packs))
	}
	for _, name := range names {
		if packByKey(name) == nil {
			t.Errorf("Industries offers %q, which packByKey cannot find — the form would "+
				"accept a choice that fails at confirmation", name)
		}
	}
	if packByKey("no-such-industry") != nil {
		t.Error("packByKey found an industry that does not exist")
	}
}

// depthOf is the longest chain from a root, counting the root as one.
func depthOf(p Pack) int {
	parent := map[string]string{}
	for _, o := range p.Orgs {
		parent[o.Key] = o.Parent
	}

	var deepest int
	for _, o := range p.Orgs {
		depth := 1
		for at := o.Parent; at != ""; at = parent[at] {
			depth++
		}
		if depth > deepest {
			deepest = depth
		}
	}
	return deepest
}

// rootsOf is how many organizations have no parent.
func rootsOf(p Pack) int {
	var roots int
	for _, o := range p.Orgs {
		if o.Parent == "" {
			roots++
		}
	}
	return roots
}
