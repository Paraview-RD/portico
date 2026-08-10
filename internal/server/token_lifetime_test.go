package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/Paraview-RD/portico/internal/model"
)

// The lifetimes of the tokens this server issues as an OpenID Provider.
//
// Three settings, and the reason they are settings at all is that the
// alternative was worse: they used to be constants, so the only answer to
// "how long is an access token valid here" was to read the source, and the
// only way to change it was to fork. What makes them safe to expose is the
// ceiling on each one — an access token is verified offline and cannot be
// revoked, so how soon it expires is the only control over a permission that
// has been withdrawn. An administrator who could set a day would be deleting
// that control without being told.
//
// So these tests are mostly about the refusals. The happy path is one case;
// the bounds are the feature.

// settingsShape is the part of the settings payload these tests read.
type settingsShape struct {
	TokenTTLMinutes           int `json:"tokenTtlMinutes"`
	OIDCAccessTokenTTLMinutes int `json:"oidcAccessTokenTtlMinutes"`
	OIDCRefreshTokenTTLDays   int `json:"oidcRefreshTokenTtlDays"`
	OIDCSessionMaxAgeDays     int `json:"oidcSessionMaxAgeDays"`
}

func readSettings(t *testing.T, api *apiTest, token string) settingsShape {
	t.Helper()
	res := api.do(http.MethodGet, "/api/v1/settings", token, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("read settings: %d %s %s", res.Status, res.Code, res.Message)
	}
	var got settingsShape
	res.into(t, &got)
	return got
}

func TestTokenLifetimeDefaults(t *testing.T) {
	api := newAPITest(t)
	got := readSettings(t, api, api.adminToken())

	// Fifteen minutes is the value SECURITY.md documents and the one the
	// constants shipped with; a default that changed on the way to being
	// configurable would alter every existing deployment silently.
	if got.OIDCAccessTokenTTLMinutes != 15 {
		t.Errorf("access token TTL = %d, want 15", got.OIDCAccessTokenTTLMinutes)
	}
	if got.OIDCRefreshTokenTTLDays != 30 {
		t.Errorf("refresh token TTL = %d days, want 30", got.OIDCRefreshTokenTTLDays)
	}
	// Zero, and this one matters more than the other two. An absolute cap
	// ends a session that is being actively refreshed, so shipping a default
	// of ninety days would sign every long-lived integration out ninety days
	// after an upgrade — for a reason nobody chose and nothing logged. The
	// same argument audit retention makes for keeping everything by default.
	if got.OIDCSessionMaxAgeDays != 0 {
		t.Errorf("session max age = %d days, want 0 (no cap)", got.OIDCSessionMaxAgeDays)
	}
}

func TestTokenLifetimesRoundTrip(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPut, "/api/v1/settings", token, map[string]any{
		"oidcAccessTokenTtlMinutes": 30,
		"oidcRefreshTokenTtlDays":   7,
		"oidcSessionMaxAgeDays":     90,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("update: %d %s %s", res.Status, res.Code, res.Message)
	}

	got := readSettings(t, api, token)
	if got.OIDCAccessTokenTTLMinutes != 30 {
		t.Errorf("access token TTL = %d, want 30", got.OIDCAccessTokenTTLMinutes)
	}
	if got.OIDCRefreshTokenTTLDays != 7 {
		t.Errorf("refresh token TTL = %d days, want 7", got.OIDCRefreshTokenTTLDays)
	}
	if got.OIDCSessionMaxAgeDays != 90 {
		t.Errorf("session max age = %d days, want 90", got.OIDCSessionMaxAgeDays)
	}
}

// Sending one of these must not reset the others.
//
// Every field on this endpoint is a pointer for exactly this reason, and a
// new field added as a plain value would quietly zero itself on every
// unrelated save — which for the session cap would mean switching it off.
func TestUpdatingOneLifetimeLeavesTheOthers(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPut, "/api/v1/settings", token, map[string]any{
		"oidcAccessTokenTtlMinutes": 20,
		"oidcSessionMaxAgeDays":     30,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("first update: %d %s %s", res.Status, res.Code, res.Message)
	}

	// A save from a different part of the page, naming nothing about tokens.
	res = api.do(http.MethodPut, "/api/v1/settings", token, map[string]any{
		"systemName": "Renamed",
	})
	if res.Status != http.StatusOK {
		t.Fatalf("second update: %d %s %s", res.Status, res.Code, res.Message)
	}

	got := readSettings(t, api, token)
	if got.OIDCAccessTokenTTLMinutes != 20 {
		t.Errorf("access token TTL = %d after an unrelated save, want 20",
			got.OIDCAccessTokenTTLMinutes)
	}
	if got.OIDCSessionMaxAgeDays != 30 {
		t.Errorf("session max age = %d after an unrelated save, want 30",
			got.OIDCSessionMaxAgeDays)
	}
}

// The ceilings, which are the reason these are safe to expose at all.
func TestTokenLifetimesOutsideTheirBoundsAreRefused(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	for _, tt := range []struct {
		name  string
		field string
		value int
		why   string
	}{
		{
			"access token far too long", "oidcAccessTokenTtlMinutes", 24 * 60,
			"an access token is verified offline and cannot be revoked, so its " +
				"expiry is the only limit on a withdrawn permission",
		},
		{"access token zero", "oidcAccessTokenTtlMinutes", 0, "nothing would ever be usable"},
		{
			"refresh token beyond the ceiling", "oidcRefreshTokenTtlDays", 365,
			"a year-long refresh token is a password that never changes",
		},
		{"refresh token zero", "oidcRefreshTokenTtlDays", 0, "no refresh would ever succeed"},
		{
			"session cap beyond the ceiling", "oidcSessionMaxAgeDays", 366,
			"a cap this far out is not a cap; it reads as one to whoever set it",
		},
		{"session cap negative", "oidcSessionMaxAgeDays", -1, "cannot mean anything"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := api.do(http.MethodPut, "/api/v1/settings", token, map[string]any{
				tt.field: tt.value,
			})
			if res.Status != http.StatusBadRequest {
				t.Errorf("%s=%d was accepted (status %d) — %s",
					tt.field, tt.value, res.Status, tt.why)
			}
			if res.Code != "INVALID_SETTINGS" {
				t.Errorf("code = %q, want INVALID_SETTINGS", res.Code)
			}
		})
	}
}

// The setting has to reach the token, which is the only part that matters to
// anybody outside this console.
//
// Written as the whole flow rather than as a unit test on the storage adapter
// because there were two places that computed an access token's expiry — the
// authorization-code path and the refresh path — and a unit test on either one
// would have passed while the other kept the old constant. This drives both.
func TestTheConfiguredAccessTokenLifetimeReachesTheToken(t *testing.T) {
	f := newFederationTest(t)

	const minutes = 3
	res := f.api.do(http.MethodPut, "/api/v1/settings", f.api.adminToken(), map[string]any{
		"oidcAccessTokenTtlMinutes": minutes,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("set the lifetime: %d %s %s", res.Status, res.Code, res.Message)
	}

	registered := f.registerClient(model.DefaultTenantCode, "lifetime-app", false)
	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "a-code-verifier-long-enough-to-be-worth-something"
	authURL := rp.AuthURL("state-lifetime", party,
		rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier)))
	code, _ := f.signIn(authURL, model.DefaultTenantCode, adminUsername, adminPassword)

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}

	// Nothing here is measured against a time.Now() read in this test, and that
	// is the point.
	//
	// The first version of these assertions compared the expiry against a local
	// clock reading with a minute of slack, and it was wrong twice over. It
	// passed alone and failed in the full -race suite, because what it actually
	// measured was the gap between this process reading its clock and the server
	// signing — a minute of it on a loaded machine, which is a fact about the
	// runner. And the slack it needed to survive that was the same order as the
	// thing being tested, so it had no way to tell three minutes from the
	// fifteen this shipped with.
	//
	// What is asserted instead is relative: expires_in is a duration on the
	// wire, and exp − iat comes from inside one token. Neither can be moved by
	// scheduling or by whose clock is right.
	//
	// The skew is real and belongs in the expectation rather than in a wider
	// tolerance. Portico reports a one-minute clock skew to relying parties
	// (client.ClockSkew, internal/oidcp/types.go) so that one whose clock is
	// slightly behind does not reject a token it should accept; the protocol
	// library applies it at both ends, moving iat back and exp forward. So a
	// three-minute setting is four minutes on the wire and five between iat and
	// exp. Absorbing that into the tolerance would mean tolerating two minutes
	// of error on a three-minute value.
	const clockSkew = time.Minute
	const lifetime = minutes * time.Minute

	// A second of slack, for the sub-second rounding in expires_in.
	wantExpiresIn := lifetime + clockSkew
	gotExpiresIn := time.Duration(tokens.ExpiresIn) * time.Second
	if gotExpiresIn < wantExpiresIn-2*time.Second || gotExpiresIn > wantExpiresIn+time.Second {
		t.Errorf("expires_in = %s, want about %s (%s configured plus %s of "+
			"reported clock skew) — the configured lifetime is not reaching the token",
			gotExpiresIn, wantExpiresIn, lifetime, clockSkew)
	}

	// And the ID token with it. One setting on purpose: an ID token outliving
	// its access token would describe an authentication that may already have
	// been withdrawn.
	idIssued := tokens.IDTokenClaims.GetIssuedAt()
	idExpiry := tokens.IDTokenClaims.GetExpiration()
	wantIDSpan := lifetime + 2*clockSkew
	if got := idExpiry.Sub(idIssued); got < wantIDSpan-2*time.Second || got > wantIDSpan+time.Second {
		t.Errorf("id token exp − iat = %s, want about %s", got, wantIDSpan)
	}

	// The two expiries are the same instant, which is the assertion that would
	// catch them drifting apart if one of them ever stopped reading the setting.
	if drift := tokens.Expiry.Sub(idExpiry); drift > 5*time.Second || drift < -5*time.Second {
		t.Errorf("access token expires %s away from the id token; they are one "+
			"setting and should land together", drift)
	}
}

// The absolute cap on a session's age.
//
// Time is not injectable here — store.Now() is time.Now() — so these back-date
// the sign-in in the database rather than waiting. That is the honest way to
// write it: what the cap measures is the distance between auth_time and now,
// and moving one end is the same test as moving the other.
//
// backdateSession sets auth_time on every refresh token of a session, as
// though the sign-in had happened that long ago.
func backdateSession(t *testing.T, f *federationTest, subject string, age time.Duration) {
	t.Helper()
	res, err := f.db.Exec(
		"UPDATE oauth_refresh_tokens SET auth_time = $1 WHERE subject = $2",
		time.Now().Add(-age), subject)
	if err != nil {
		t.Fatalf("back-date the session: %v", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected: %v", err)
	}
	// Guard against the test silently passing because it updated nothing —
	// a refresh token that was never issued would make every assertion below
	// vacuous.
	if affected == 0 {
		t.Fatal("no refresh token was back-dated; the flow did not issue one")
	}
}

// A session issued a refresh token and then a fresh one, to establish that
// rotation works before the cap is brought into it.
func signInForRefreshToken(t *testing.T, f *federationTest, clientID string) (rp.RelyingParty, string) {
	t.Helper()

	registered := f.registerClient(model.DefaultTenantCode, clientID, false)
	issuer := f.publicURL + "/t/" + model.DefaultTenantCode
	party := f.relyingParty(issuer, registered.Client.ClientID, registered.Secret)

	verifier := "a-code-verifier-long-enough-to-be-worth-something"
	// offline_access is what asks for a refresh token at all.
	authURL := rp.AuthURL("state-cap", party,
		rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier)))
	code, _ := f.signIn(authURL, model.DefaultTenantCode, adminUsername, adminPassword)

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](context.Background(), code, party,
		rp.WithCodeVerifier(verifier))
	if err != nil {
		t.Fatalf("code exchange: %v", err)
	}
	if tokens.RefreshToken == "" {
		t.Fatal("no refresh token was issued, so there is no chain to cap")
	}
	return party, tokens.RefreshToken
}

func TestASessionPastItsMaximumAgeCannotRefresh(t *testing.T) {
	f := newFederationTest(t)
	admin := f.api.adminToken()

	res := f.api.do(http.MethodPut, "/api/v1/settings", admin, map[string]any{
		"oidcSessionMaxAgeDays": 1,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("set the cap: %d %s %s", res.Status, res.Code, res.Message)
	}

	party, refreshToken := signInForRefreshToken(t, f, "cap-app")

	// Within the cap, refreshing works. Asserted first so that the refusal
	// below cannot be explained by refresh being broken outright.
	fresh, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		refreshToken, "", "")
	if err != nil {
		t.Fatalf("refresh inside the cap: %v", err)
	}
	if fresh.RefreshToken == "" {
		t.Fatal("rotation returned no replacement")
	}

	// Now the sign-in was two days ago and the cap is one.
	backdateSession(t, f, adminUserID(t, f), 48*time.Hour)

	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		fresh.RefreshToken, "", ""); err == nil {
		t.Error("a session older than the cap was refreshed. Rotation renews " +
			"the token's own expiry, so without a cap measured from auth_time " +
			"a chain that is exchanged regularly never ends.")
	}
}

// Ageing out is not a leak, and must not be reported as one.
//
// Revoking the chain is this server's way of saying a copy of a token got
// loose. A session that simply reached its configured limit is the system
// working; if both produced the same database state, the one signal that means
// "somebody has your token" would stop meaning anything.
func TestAgeingOutDoesNotRevokeTheChain(t *testing.T) {
	f := newFederationTest(t)
	admin := f.api.adminToken()

	res := f.api.do(http.MethodPut, "/api/v1/settings", admin, map[string]any{
		"oidcSessionMaxAgeDays": 1,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("set the cap: %d %s %s", res.Status, res.Code, res.Message)
	}

	party, refreshToken := signInForRefreshToken(t, f, "no-revoke-app")
	subject := adminUserID(t, f)
	backdateSession(t, f, subject, 48*time.Hour)

	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		refreshToken, "", ""); err == nil {
		t.Fatal("the aged-out session refreshed successfully")
	}

	var revoked int
	if err := f.db.QueryRow(
		"SELECT count(*) FROM oauth_refresh_tokens WHERE subject = $1 AND revoked_at IS NOT NULL",
		subject).Scan(&revoked); err != nil {
		t.Fatalf("count revoked tokens: %v", err)
	}
	if revoked != 0 {
		t.Errorf("%d refresh tokens were revoked. Chain revocation means a "+
			"token leaked; a session reaching its age limit is not that, and "+
			"overloading the same response makes the leak signal ambiguous.",
			revoked)
	}
}

// With no cap — the default — age is not a reason to refuse anything.
func TestWithNoCapAnOldSessionStillRefreshes(t *testing.T) {
	f := newFederationTest(t)

	// Left at the default rather than set to 0, so this also asserts what the
	// default is: a deployment that upgrades into this feature must not start
	// ending sessions it was not asked to end.
	party, refreshToken := signInForRefreshToken(t, f, "uncapped-app")
	backdateSession(t, f, adminUserID(t, f), 400*24*time.Hour)

	if _, err := rp.RefreshTokens[*oidc.IDTokenClaims](context.Background(), party,
		refreshToken, "", ""); err != nil {
		t.Errorf("an old session was refused with no cap configured: %v", err)
	}
}

// adminUserID is the subject the refresh tokens above are issued to.
func adminUserID(t *testing.T, f *federationTest) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(
		"SELECT id FROM users WHERE username = $1", adminUsername).Scan(&id); err != nil {
		t.Fatalf("look up the administrator: %v", err)
	}
	return id
}

// Zero switches the cap off and must stay reachable, because turning a
// control off is a decision an operator is entitled to make. It is the one
// value below the minimum that is not a mistake.
func TestSessionCapCanBeSwitchedOff(t *testing.T) {
	api := newAPITest(t)
	token := api.adminToken()

	res := api.do(http.MethodPut, "/api/v1/settings", token, map[string]any{
		"oidcSessionMaxAgeDays": 90,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("switch on: %d %s %s", res.Status, res.Code, res.Message)
	}

	res = api.do(http.MethodPut, "/api/v1/settings", token, map[string]any{
		"oidcSessionMaxAgeDays": 0,
	})
	if res.Status != http.StatusOK {
		t.Fatalf("switch off: %d %s %s", res.Status, res.Code, res.Message)
	}

	if got := readSettings(t, api, token); got.OIDCSessionMaxAgeDays != 0 {
		t.Errorf("session max age = %d, want 0", got.OIDCSessionMaxAgeDays)
	}
}
