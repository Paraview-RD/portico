package service

import (
	"testing"

	"github.com/Paraview-RD/portico/internal/model"
)

// A claim set is assembled rather than overlaid, so what is tested here is the
// decision — send it, send it as something else, do not send it — and not a
// document. The emission is in internal/oidcp, and its guard is that the names
// documented in federation.md still come back from a real userinfo request.

// With nothing configured, every default keeps its name.
//
// This is the property the whole feature rests on, stated at the one place a
// rename could leak into a deployment that configured nothing.
func TestWithNoRulesEveryClaimKeepsItsName(t *testing.T) {
	for key, def := range OIDCClaimNames() {
		name, send, renamed := ClaimFor(Outbound{}, key)
		if !send {
			t.Errorf("%s is not sent with no rules configured", key)
		}
		if name != def {
			t.Errorf("%s goes out as %q with no rules configured, want %q", key, name, def)
		}
		if renamed {
			t.Errorf("%s reports itself renamed with no rules configured, which would "+
				"make the caller append a claim instead of setting the field it "+
				"has always set", key)
		}
	}
}

// A rename says so, because the caller has to do something different.
//
// Several of these claims are typed fields on the library's UserInfo. A rename
// means leaving the field unset and appending a claim under the new name; a
// caller that did both would send the fact twice, under two names, which is
// what an integrator renaming it is trying to stop.
func TestARenameTellsTheCallerToAppendRatherThanAssign(t *testing.T) {
	out := rules(model.FieldMapping{SourceKey: "email", TargetName: "mail"})

	name, send, renamed := ClaimFor(out, "email")
	if !send || name != "mail" {
		t.Fatalf("email goes out as %q (send=%v), want mail", name, send)
	}
	if !renamed {
		t.Error("a renamed claim does not report itself renamed, so the caller " +
			"would assign the typed field and send it under both names")
	}
}

// A rule that names the claim's own default name is not a rename.
func TestAMappingToAClaimsOwnNameIsNotARename(t *testing.T) {
	out := rules(model.FieldMapping{SourceKey: "email", TargetName: "email"})

	name, send, renamed := ClaimFor(out, "email")
	if !send || name != "email" || renamed {
		t.Errorf("email → email reports name=%q send=%v renamed=%v, want the "+
			"ordinary assignment", name, send, renamed)
	}
}

// Suppression is a refusal to send, not a rename to nothing.
func TestASuppressedClaimIsNotSent(t *testing.T) {
	out := rules(model.FieldMapping{SourceKey: "phone", Suppressed: true})

	if _, send, _ := ClaimFor(out, "phone"); send {
		t.Error("a suppressed claim is still sent")
	}
	// And only that one.
	if _, send, _ := ClaimFor(out, "email"); !send {
		t.Error("suppressing the phone number stopped the email address too")
	}
}

// A key the claim set never carried is not a default, so ClaimFor declines it
// and it goes through OIDCAdditions instead. Answering "send it under its
// default name" here would be inventing a default that does not exist.
func TestAKeyTheClaimSetDoesNotCarryIsNotADefault(t *testing.T) {
	for _, key := range []string{"department", "organization_path", "badge_number"} {
		if name, send, _ := ClaimFor(Outbound{}, key); send {
			t.Errorf("%s reports a default claim name %q; it is an addition, and "+
				"treating it as a default would send it to every application",
				key, name)
		}
	}
}

// `sub` cannot be reached from either side.
//
// Not a catalogue key, so nothing can be renamed away from it; and on the
// reserved list, so nothing can be renamed onto it. An application's whole
// trust model rests on that claim naming one person consistently, and both
// halves are needed to keep it that way.
func TestTheSubjectClaimIsUnreachable(t *testing.T) {
	if IsBuiltInFieldKey("sub") {
		t.Error("`sub` is a catalogue key, so it could be renamed or suppressed")
	}
	for _, name := range oidcDefaultClaims {
		if name == "sub" {
			t.Error("a catalogue key goes out as `sub`, so a rule could rename it away")
		}
	}
	if !reservedClaims["sub"] {
		t.Error("`sub` is not reserved, so another field could be sent as it")
	}
}

// The two `_verified` claims are not mappable.
//
// They are always false in this version, and offering a mappable field whose
// value never changes is a trap: an integrator who mapped it would conclude
// that nobody's address is verified rather than that the fact is not kept.
func TestTheVerifiedClaimsAreNotOnOffer(t *testing.T) {
	for _, key := range []string{"email_verified", "phone_number_verified", "phone_verified"} {
		if IsBuiltInFieldKey(key) {
			t.Errorf("%s is in the catalogue; it is always false, so a mapping "+
				"for it would carry a constant somebody would read as a fact", key)
		}
	}
}

// Every claim name the table produces is one a mapping may not take.
//
// Otherwise a rule could rename `department` onto `tenant_id` — not a
// protocol-reserved name, but one this system's own claims already occupy, and
// a relying party reading it would get a department where it reads the tenant.
func TestTheDefaultClaimNamesCannotBeTakenByAnotherField(t *testing.T) {
	for key, name := range OIDCClaimNames() {
		owner, claimed := oidcClaimOwners[name]
		if !claimed {
			t.Errorf("%s goes out as %q and nothing claims that name, so another "+
				"field could be mapped onto it — and a relying party would read "+
				"one fact where it expects another", key, name)
			continue
		}
		if owner != key {
			t.Errorf("%q is owned by %s rather than by %s", name, owner, key)
		}
	}

	// The case that motivated it. `tenant_id` is this project's own claim, so
	// OpenID Connect's reserved list has nothing to say about it.
	if reservedClaims["tenant_id"] {
		t.Fatal("tenant_id is on the OIDC reserved list, which it is not; " +
			"this test is guarding the wrong thing")
	}
	if owner := oidcClaimOwners["tenant_id"]; owner != "tenant_id" {
		t.Errorf("the tenant_id claim is owned by %q, want the tenant_id field", owner)
	}
}
