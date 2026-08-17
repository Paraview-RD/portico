package service

import (
	"github.com/Paraview-RD/portico/internal/i18n"
	"github.com/Paraview-RD/portico/internal/mailfmt"
	"github.com/Paraview-RD/portico/internal/notify"
)

// The two messages a trial sends, assembled from localized parts.
//
// Separate from trial.go so they can be built without sending them, which is
// what lets a test read what an applicant will actually receive — in both
// languages and in both renderings — rather than asserting on a fake mailer's
// record of a string that was already composed.

// trialLocale is the language a trial message is written in.
//
// Asked of the visitor first, and the deployment second. Every other message
// in this system resolves a locale from the account it is about and the tenant
// it belongs to; a trial applicant has neither — that is what they are asking
// for — so this used to go straight to the deployment default, and somebody
// who filled in a Chinese form received an English email.
//
// They had told us, though, in the only way available to them: they read the
// page in a language and typed into it. So `requested` is that language, as
// the interface reported it, and it is believed only if this build has it —
// it arrives in a request body, which makes it a claim rather than a fact,
// and an unknown tag must not produce an empty message.
func (s *TrialService) trialLocale(requested string) i18n.Locale {
	if locale, ok := i18n.Parse(requested); ok {
		return locale
	}
	if locale, ok := i18n.Parse(s.locale); ok {
		return locale
	}
	return i18n.Default
}

// confirmMail is the message carrying the link that creates the tenant.
func (s *TrialService) confirmMail(tenantName, link, requested string) (notify.Message, error) {
	locale := s.trialLocale(requested)
	data := i18n.TrialData{Tenant: tenantName, Hours: int(TrialTokenTTL.Hours())}

	text := func(key string) string {
		out, err := s.messages.Render(locale, key, data)
		if err != nil {
			// A missing key is a build-time mistake — the catalogue test
			// renders every key in every locale — so this cannot be a reason
			// to refuse somebody their trial. The key is worse than the
			// sentence and better than nothing.
			return key
		}
		return out
	}

	doc := linkMail{
		Brand:    mailBrand,
		Title:    text(i18n.KeyTrialConfirmTitle),
		Intro:    text(i18n.KeyTrialConfirmIntro),
		Action:   text(i18n.KeyTrialConfirmAction),
		Link:     link,
		Fallback: text(i18n.KeyMailLinkFallback),
		Footer: []string{
			text(i18n.KeyTrialConfirmExpiry),
			text(i18n.KeyTrialConfirmIgnore),
		},
	}.document()
	return message(text(i18n.KeyTrialConfirmSubject), doc)
}

// readyMail is the message carrying the credentials.
//
// demoPassword is empty when the fill failed, and then the whole section goes
// rather than appearing with nothing in it: offering credentials for accounts
// that were never created reads as a bug in the product rather than as a fill
// that did not happen.
func (s *TrialService) readyMail(out TrialTenant, requested string) (notify.Message, error) {
	locale := s.trialLocale(requested)
	data := i18n.TrialData{
		Tenant: out.TenantName,
		Days:   int(TrialTenantTTL.Hours() / 24),
	}

	text := func(key string) string {
		rendered, err := s.messages.Render(locale, key, data)
		if err != nil {
			return key
		}
		return rendered
	}

	signIn := mailfmt.Section{
		Heading: text(i18n.KeyTrialReadySignIn),
		Facts: []mailfmt.Fact{
			{Label: text(i18n.KeyTrialReadyLabelTenant), Value: out.TenantCode, Code: true},
			{Label: text(i18n.KeyTrialReadyLabelUsername), Value: out.AdminUsername, Code: true},
			{Label: text(i18n.KeyTrialReadyLabelPassword), Value: out.AdminPassword, Code: true},
		},
		// Beside the password rather than in the footer, which is where it
		// was. "This message is the only copy" is the one sentence in this
		// email somebody has to act on before closing it, and four paragraphs
		// below the value it is about, it reads as small print.
		Notice: text(i18n.KeyTrialReadyRecovery),
		Action: &mailfmt.Action{
			Label:    text(i18n.KeyTrialReadyAction),
			URL:      out.SignInURL,
			Fallback: text(i18n.KeyMailLinkFallback),
		},
	}

	doc := mailfmt.Document{
		Brand: mailBrand,
		Title: text(i18n.KeyTrialReadyTitle),
		// The fortnight is said here rather than in the footer. Somebody
		// reading this is deciding how much to build in the next few minutes,
		// and "how long do I have" is part of that decision — under the small
		// print it is read after they have already decided.
		Intro: []string{
			text(i18n.KeyTrialReadyIntro),
			text(i18n.KeyTrialReadyExpiry),
		},
		Sections: []mailfmt.Section{signIn},
		Footer:   []string{text(i18n.KeyTrialReadyFooter)},
	}

	if out.DemoPassword != "" {
		doc.Sections = append(doc.Sections, mailfmt.Section{
			Heading: text(i18n.KeyTrialReadyDemoHeading),
			Body:    []string{text(i18n.KeyTrialReadyDemoBody)},
			Facts: []mailfmt.Fact{
				{Label: text(i18n.KeyTrialReadyDemoLabel), Value: out.DemoPassword, Code: true},
			},
		})
	}

	return message(text(i18n.KeyTrialReadySubject), doc)
}
