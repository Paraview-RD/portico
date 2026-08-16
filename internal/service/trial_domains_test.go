package service

import "testing"

// The two rules a trial address is read by, tested as functions.
//
// They are here rather than only behind the API because what they get wrong is
// not a status code — it is which two strings are considered the same person,
// and the interesting cases are all spellings rather than requests.

func TestAPlusSubAddressIsTheSameMailbox(t *testing.T) {
	same := [][2]string{
		{"me@example.com", "me+one@example.com"},
		{"me@example.com", "ME+Anything@Example.com"},
		{"me@example.com", "  me@example.com  "},
		// Only the first plus matters; everything after it is the tag.
		{"me@example.com", "me+one+two@example.com"},
	}
	for _, pair := range same {
		if a, b := mailboxKey(pair[0]), mailboxKey(pair[1]); a != b {
			t.Errorf("%q and %q are one mailbox, keyed as %q and %q",
				pair[0], pair[1], a, b)
		}
	}

	different := [][2]string{
		// Dots are left alone deliberately. Collapsing them is true at Gmail
		// and false almost everywhere else, and a wrong refusal lands on
		// somebody who did nothing.
		{"me.you@example.com", "meyou@example.com"},
		// A plus in the domain is not a sub-address.
		{"me@example.com", "me@plus+example.com"},
		{"me@example.com", "me@example.org"},
	}
	for _, pair := range different {
		if a, b := mailboxKey(pair[0]), mailboxKey(pair[1]); a == b {
			t.Errorf("%q and %q are different mailboxes, both keyed as %q",
				pair[0], pair[1], a)
		}
	}
}

func TestABlockedDomainCoversWhatSitsUnderIt(t *testing.T) {
	blocked := blockedEmailDomains([]string{"  @Example.Test ", "", "  "})

	refused := []string{
		"someone@mailinator.com",
		// Subdomains, which is how several of these services hand addresses
		// out. Matching the exact name only would match the one spelling
		// nobody has to use.
		"someone@mail.mailinator.com",
		"someone@a.b.guerrillamail.com",
		// The operator's own addition, however they typed it.
		"someone@example.test",
		"someone@sub.example.test",
	}
	for _, address := range refused {
		if !domainIsBlocked(blocked, address) {
			t.Errorf("%q was accepted", address)
		}
	}

	allowed := []string{
		"someone@example.com",
		// Not a subdomain of a blocked name, merely ending in the same
		// letters. Matching by suffix rather than by label would refuse this.
		"someone@notmailinator.com",
		"someone@mailinator.com.example.com",
		// Nothing to read a domain from at all.
		"not-an-address",
	}
	for _, address := range allowed {
		if domainIsBlocked(blocked, address) {
			t.Errorf("%q was refused", address)
		}
	}
}

func TestAnOperatorsListIsAddedRatherThanSubstituted(t *testing.T) {
	// Somebody blocking the one provider their own visitors abuse should not
	// have to restate two dozen defaults, and should not silently turn them
	// off by not restating them.
	blocked := blockedEmailDomains([]string{"example.test"})
	if !domainIsBlocked(blocked, "someone@mailinator.com") {
		t.Error("adding a domain switched off the built-in list")
	}
}
