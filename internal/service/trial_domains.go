package service

import "strings"

// Throwaway mailbox providers, refused for trial signups.
//
// The whole identity claim a trial makes is that a tenant traces to a mailbox
// somebody could read. A ten-minute address satisfies the form and none of the
// intent: it makes the address check a formality, the one-tenant-per-mailbox
// rule a formality with it, and a tenant created through one is attributable
// to nobody.
//
// This list is short and will never be complete — there are thousands of these
// services and new ones daily, and a project that tried to track them would be
// maintaining a blocklist instead of an identity server. It is not a wall. It
// raises the price of the cheapest attack from nothing to slightly more than
// nothing, and an operator who needs more adds their own with
// PORTICO_TRIAL_BLOCKED_EMAIL_DOMAINS.
//
// Only names whose entire purpose is disposability are here. Nothing that
// anybody uses as their real address belongs on this list: refusing a real
// mailbox is a worse failure than accepting a temporary one, because the
// person it happens to did nothing wrong and has no way to argue.
var disposableEmailDomains = []string{
	"10minutemail.com",
	"20minutemail.com",
	"dispostable.com",
	"fakeinbox.com",
	"getairmail.com",
	"getnada.com",
	"guerrillamail.com",
	"guerrillamail.info",
	"guerrillamail.net",
	"guerrillamailblock.com",
	"maildrop.cc",
	"mailinator.com",
	"mailnesia.com",
	"mintemail.com",
	"mohmal.com",
	"mytemp.email",
	"sharklasers.com",
	"spam4.me",
	"temp-mail.org",
	"tempmail.net",
	"tempmailo.com",
	"throwawaymail.com",
	"trashmail.com",
	"yopmail.com",
	"yopmail.fr",
	"yopmail.net",
}

// blockedEmailDomains builds the set a deployment refuses: the list above,
// plus whatever the operator added.
//
// Subdomains count. mailinator runs dozens of alternative domains and hands
// out addresses at arbitrary subdomains of them, so matching the exact string
// only would be matching the one spelling nobody has to use.
func blockedEmailDomains(extra []string) map[string]bool {
	set := make(map[string]bool, len(disposableEmailDomains)+len(extra))
	for _, domain := range disposableEmailDomains {
		set[domain] = true
	}
	for _, domain := range extra {
		// Space first, then the @. The other order leaves " @example.test"
		// keyed as "@example.test", which matches nothing and reports itself
		// as a configured block that quietly does nothing.
		domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "@"))
		if domain = strings.TrimSpace(domain); domain != "" {
			set[domain] = true
		}
	}
	return set
}

// domainIsBlocked reports whether an address's domain, or any domain it sits
// under, is refused.
func domainIsBlocked(blocked map[string]bool, email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])

	// Walk up: mail.mailinator.com, then mailinator.com, then com. The last
	// step can only match if an operator blocked a whole top-level domain,
	// which is a thing somebody might legitimately want.
	for domain != "" {
		if blocked[domain] {
			return true
		}
		dot := strings.Index(domain, ".")
		if dot < 0 {
			return false
		}
		domain = domain[dot+1:]
	}
	return false
}

// mailboxKey is the mailbox an address reaches, as opposed to how it was
// spelled.
//
// Lower-cased, and with any +sub-address (RFC 5233) removed, because
// me+one@example.com and me+two@example.com are one inbox. Without this, "one
// tenant per address" is one tenant per spelling, and a single mailbox can
// take the whole quota one plus-sign at a time.
//
// Dots are deliberately left alone. Ignoring them is true at Gmail and false
// almost everywhere else, and collapsing them would tell two colleagues at a
// provider that treats dots as ordinary characters that one of them has
// already had a trial — a wrong refusal, which is worse than the abuse it
// would prevent.
//
// The result is never used for delivery. The address as typed is what the
// link is sent to.
func mailboxKey(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email
	}
	local, domain := email[:at], email[at:]
	if plus := strings.Index(local, "+"); plus >= 0 {
		local = local[:plus]
	}
	return local + domain
}
