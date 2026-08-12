package service

import (
	"context"

	"github.com/Paraview-RD/portico/internal/model"
)

// Applying a recipient's mappings to an OpenID Connect claim set.
//
// Unlike a webhook body, a claim set is assembled field by field, so there is
// no default document to overlay — the rules are consulted while it is being
// built. What lives here is the half that does not depend on the protocol
// library: which catalogue key goes out under which claim name by default, and
// what a rule does to that.
//
// The emission itself stays in internal/oidcp, because several of these claims
// are typed fields on the library's UserInfo rather than entries in a map. A
// rename means not assigning the field and appending a claim instead, and that
// is a fact about the library, not about mappings.

// oidcDefaultClaims is where each catalogue key already goes out.
//
// Keys absent from this table are not in the claim set today, so a rule naming
// one is an addition rather than a rename.
//
// Written out rather than derived. The names on the right are OpenID Connect's
// and, for the four below the line, this project's own §3.8.2 claims — either
// way they are a wire format other people's code already reads, so deriving
// them from anything would mean an internal rename changing what relying
// parties receive.
var oidcDefaultClaims = map[string]string{
	"username":     "preferred_username",
	"display_name": "name",
	"email":        "email",
	"phone":        "phone_number",
	"updated_at":   "updated_at",

	// Portico's own, which go out unconditionally rather than under a scope.
	"tenant_id":         "tenant_id",
	"tenant_code":       "tenant_code",
	"role":              "role",
	"organization_id":   "organization_id",
	"organization_name": "organization_name",
}

// Deliberately absent: `sub`, and the two `_verified` claims.
//
// `sub` is refused as a target by the reserved-claim list, so it can never be
// renamed onto; it is also not a catalogue key, so it can never be renamed
// away. Both halves are needed — an application's whole trust model rests on
// that claim identifying one person consistently.
//
// `email_verified` and `phone_number_verified` are not mappable and follow the
// claim they describe: sent when that claim is sent under its default name,
// and not otherwise. A relying party reading `mail` would have no reason to
// look at `email_verified`, and one that no longer receives `email` at all
// should not be told anything about an address it cannot see.

// oidcClaimOwners is which catalogue key each default claim name belongs to.
//
// It refuses a rename onto a name the claim set already uses for something
// else. `tenant_id` is not reserved by OpenID Connect — it is this project's
// own claim — so nothing else would have stopped `department → tenant_id`, and
// a relying party would then read a department where it reads the tenant. The
// duplicate-target refusal does not cover this: that one catches two rules
// colliding, and this is a rule colliding with a default nobody wrote a rule
// for.
//
// Owned rather than merely listed, so that a mapping to a claim's own name
// stays legal — `email → email` is a no-op somebody may well save.
var oidcClaimOwners = func() map[string]string {
	owners := make(map[string]string, len(oidcDefaultClaims))
	for key, name := range oidcDefaultClaims {
		owners[name] = key
	}
	return owners
}()

// OIDCDefaultClaim is the claim name a catalogue key goes out as by default,
// and whether it goes out at all.
func OIDCDefaultClaim(key string) (string, bool) {
	name, ok := oidcDefaultClaims[key]
	return name, ok
}

// OIDCClaimNames is the default claim set, for the guard tests that hold the
// documentation and this table in step.
func OIDCClaimNames() map[string]string {
	out := make(map[string]string, len(oidcDefaultClaims))
	for key, name := range oidcDefaultClaims {
		out[key] = name
	}
	return out
}

// ClaimFor decides one default claim's fate under a recipient's rules.
//
// renamed reports that the caller must append a claim under name rather than
// assign the typed field it would otherwise have set — which is the whole
// reason this returns three values instead of the two NameFor does.
func ClaimFor(out Outbound, key string) (name string, send, renamed bool) {
	def, isDefault := oidcDefaultClaims[key]
	if !isDefault {
		return "", false, false
	}
	name, send = out.NameFor(key, def)
	return name, send, send && name != def
}

// OIDCAdditions resolves the claims a recipient has configured that the
// default set does not carry — which is most of the catalogue, and the larger
// half of what this feature is for.
//
// Not gated by scope, and that is a decision rather than an oversight. Portico's
// own claims are already sent regardless of scope, so this is the file's
// existing precedent rather than a new rule; and a mapping configured for one
// application *is* the decision that this application receives this fact. A
// scope gate on top would mean a rule somebody configured silently doing
// nothing, which is the failure this whole feature exists to remove.
//
// Renames and suppressions of scope-gated defaults stay gated, because they
// only fire where the default would have gone out anyway.
func (c *FieldCatalogue) OIDCAdditions(ctx context.Context, tenantID string, user model.User, out Outbound) (map[string]any, error) {
	if out.Empty() {
		return nil, nil
	}

	defaults := make(map[string]bool, len(oidcDefaultClaims))
	for key := range oidcDefaultClaims {
		defaults[key] = true
	}
	additions := out.Additions(defaults)
	if len(additions) == 0 {
		return nil, nil
	}

	values, err := c.FieldValues(ctx, tenantID, user)
	if err != nil {
		return nil, err
	}

	claims := make(map[string]any, len(additions))
	for _, rule := range additions {
		// Nothing is ever sent empty: a relying party that mapped a field and
		// never receives it should look at the account rather than at the
		// mapping.
		if value, ok := values[rule.SourceKey]; ok {
			claims[rule.TargetName] = value
		}
	}
	return claims, nil
}
