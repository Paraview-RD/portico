package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/auth"
	"github.com/paraview/portico/internal/httpx"
	"github.com/paraview/portico/internal/store"
)

// Password policy: composition rules, reuse, and expiry.
//
// A word on what these are for, because the evidence does not support all of
// them. NIST SP 800-63B recommends *against* composition rules and against
// routine expiry: forcing a symbol produces "Password1!", and forcing a
// change every 90 days produces "Summer2026" followed by "Autumn2026". Both
// make passwords more guessable while feeling stricter, and both push people
// towards writing them down. Length and a check against known-breached
// passwords do more than all of it.
//
// They are here anyway, off by default, because plenty of deployments are
// audited against regimes that require them and "the standards body says
// this is counterproductive" is not an answer an auditor accepts. An
// operator who has the choice should leave them off and raise the minimum
// length instead; an operator who does not have the choice now has the
// controls. The settings screen says so too, in one line.
//
// The policy lives in the service layer rather than in auth because it comes
// from per-tenant settings, and auth is a leaf that services depend on. What
// stays in auth is the absolute floor — a minimum length nothing can go
// below, and bcrypt's 72-byte ceiling — which applies whatever a tenant
// configures.

// PasswordPolicy is a tenant's rules, derived from its settings.
type PasswordPolicy struct {
	MinLength        int
	RequireUppercase bool
	RequireLowercase bool
	RequireDigit     bool
	RequireSymbol    bool
	// HistoryDepth is how many previous passwords may not be reused. Zero
	// means reuse is not checked.
	HistoryDepth int
	// MaxAgeDays is how long a password stays usable. Zero means forever.
	MaxAgeDays int
}

// PasswordPolicyFor reads a tenant's policy.
func (s *UserService) PasswordPolicyFor(ctx context.Context, tenantID string) (PasswordPolicy, error) {
	settings, err := s.settings.Get(ctx, tenantID)
	if err != nil {
		return PasswordPolicy{}, err
	}
	return settings.PasswordPolicy(), nil
}

// MaxAge is the expiry interval, or zero if passwords do not expire.
func (p PasswordPolicy) MaxAge() time.Duration {
	return time.Duration(p.MaxAgeDays) * 24 * time.Hour
}

// Expired reports whether a password set at changedAt is past its life.
//
// A nil changedAt means the password has never been changed since the
// account was created, which counts as due. Treating unknown as fresh would
// exempt exactly the accounts an expiry policy is turned on to catch —
// imported ones, and ones created with a password somebody dictated.
func (p PasswordPolicy) Expired(changedAt *time.Time, now time.Time) bool {
	if p.MaxAgeDays <= 0 {
		return false
	}
	if changedAt == nil {
		return true
	}
	return now.Sub(*changedAt) >= p.MaxAge()
}

// Check applies the composition rules.
//
// It reports every unmet rule at once rather than the first. A form that
// says "needs a digit", then "needs a symbol", then "too short" on three
// successive attempts is the interaction that makes people give up and
// reuse something.
func (p PasswordPolicy) Check(plaintext string) error {
	// The absolute floor first, whatever the tenant configured. This is also
	// what stops a policy with a minimum of 4 from being settable in effect.
	if err := auth.ValidatePassword(plaintext); err != nil {
		return httpx.BadRequest("WEAK_PASSWORD", err.Error())
	}

	var unmet []string
	if length := len([]rune(plaintext)); length < p.MinLength {
		unmet = append(unmet, fmt.Sprintf("at least %d characters", p.MinLength))
	}

	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range plaintext {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		// Anything that is not a letter, a digit, or whitespace. Defining a
		// symbol by exclusion rather than by a list means a policy is not
		// quietly unsatisfiable for somebody typing on a keyboard whose
		// punctuation nobody thought of.
		case !unicode.IsSpace(r):
			hasSymbol = true
		}
	}

	if p.RequireUppercase && !hasUpper {
		unmet = append(unmet, "an uppercase letter")
	}
	if p.RequireLowercase && !hasLower {
		unmet = append(unmet, "a lowercase letter")
	}
	if p.RequireDigit && !hasDigit {
		unmet = append(unmet, "a digit")
	}
	if p.RequireSymbol && !hasSymbol {
		unmet = append(unmet, "a symbol")
	}

	if len(unmet) > 0 {
		return httpx.BadRequest("WEAK_PASSWORD",
			"The password must contain "+joinWithAnd(unmet)+".")
	}
	return nil
}

// joinWithAnd renders a list as prose: "a, b and c".
func joinWithAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// historyDepth narrows the configured depth to the width the queries take.
//
// Update refuses anything above MaxPasswordHistoryDepth, so this cannot
// truncate in practice — but that bound lives in a validation elsewhere,
// where neither a reader nor a scanner can see it from here.
func (p PasswordPolicy) historyDepth() int32 {
	if p.HistoryDepth > MaxPasswordHistoryDepth {
		return MaxPasswordHistoryDepth
	}
	if p.HistoryDepth < 0 {
		return 0
	}
	return int32(p.HistoryDepth)
}

// ErrPasswordReused is returned when a new password matches a recent one.
var ErrPasswordReused = httpx.BadRequest("PASSWORD_REUSED",
	"That password has been used recently. Choose one you have not used before.")

// checkReuse refuses a password the account has used within the configured
// depth.
//
// Every comparison is a bcrypt evaluation, which is the reason the depth is
// capped rather than left open: a depth of fifty would make every password
// change cost fifty hashes, and a change is exactly when somebody is
// waiting on a form.
func (s *UserService) checkReuse(ctx context.Context, q *store.Scoped, userID, plaintext string, policy PasswordPolicy) error {
	if policy.HistoryDepth <= 0 {
		return nil
	}

	hashes, err := q.RecentPasswordHashes(ctx, userID, policy.historyDepth())
	if err != nil {
		return fmt.Errorf("read password history: %w", err)
	}
	for _, hash := range hashes {
		if auth.CheckPassword(hash, plaintext) {
			return ErrPasswordReused
		}
	}
	return nil
}

// rememberPassword adds the hash just replaced to the account's history and
// trims what falls outside the depth.
//
// The hash stored is the *new* one. Storing the old one instead would leave
// the current password absent from its own history, so setting the same
// password twice in a row would be accepted.
func (s *UserService) rememberPassword(ctx context.Context, q *store.Scoped, userID, hash string, policy PasswordPolicy) error {
	if policy.HistoryDepth <= 0 {
		return nil
	}

	now := store.Now()
	if err := q.RecordPasswordInHistory(ctx, uuid.NewString(), userID, hash, now); err != nil {
		return fmt.Errorf("record password history: %w", err)
	}
	if err := q.TrimPasswordHistory(ctx, userID, policy.historyDepth()); err != nil {
		return fmt.Errorf("trim password history: %w", err)
	}
	return nil
}
