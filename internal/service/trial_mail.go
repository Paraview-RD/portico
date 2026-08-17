package service

import (
	"fmt"

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

// mailBrand is the wordmark above the title.
//
// Not translated and not configurable: it is the name of the software, and a
// deployment that renamed it here would be saying one thing in the message and
// another everywhere the product refers to itself.
const mailBrand = "Portico"

// trialLocale is the language a trial message is written in.
//
// The deployment's default, because there is nobody to ask. Every other
// message in this system resolves a locale from the account it is about and
// the tenant it belongs to; a trial applicant has neither — that is what they
// are asking for. Left unset it is English, which is i18n.Default.
func (s *TrialService) trialLocale() i18n.Locale {
	locale, ok := i18n.Parse(s.locale)
	if !ok {
		return i18n.Default
	}
	return locale
}

// confirmMail is the message carrying the link that creates the tenant.
func (s *TrialService) confirmMail(tenantName, link string) (notify.Message, error) {
	locale := s.trialLocale()
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

	doc := mailfmt.Document{
		Brand: mailBrand,
		Title: text(i18n.KeyTrialConfirmTitle),
		Intro: []string{text(i18n.KeyTrialConfirmIntro)},
		Sections: []mailfmt.Section{{
			Action: &mailfmt.Action{
				Label:    text(i18n.KeyTrialConfirmAction),
				URL:      link,
				Fallback: text(i18n.KeyTrialLinkFallback),
			},
		}},
		Footer: []string{
			text(i18n.KeyTrialConfirmExpiry),
			text(i18n.KeyTrialConfirmIgnore),
		},
	}
	return message(text(i18n.KeyTrialConfirmSubject), doc)
}

// readyMail is the message carrying the credentials.
//
// demoPassword is empty when the fill failed, and then the whole section goes
// rather than appearing with nothing in it: offering credentials for accounts
// that were never created reads as a bug in the product rather than as a fill
// that did not happen.
func (s *TrialService) readyMail(out TrialTenant) (notify.Message, error) {
	locale := s.trialLocale()
	data := i18n.TrialData{Tenant: out.TenantName}

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
		Action: &mailfmt.Action{
			Label:    text(i18n.KeyTrialReadyAction),
			URL:      out.SignInURL,
			Fallback: text(i18n.KeyTrialLinkFallback),
		},
	}

	doc := mailfmt.Document{
		Brand:    mailBrand,
		Title:    text(i18n.KeyTrialReadyTitle),
		Intro:    []string{text(i18n.KeyTrialReadyIntro)},
		Sections: []mailfmt.Section{signIn},
		Footer: []string{
			text(i18n.KeyTrialReadyRecovery),
			text(i18n.KeyTrialReadyFooter),
		},
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

// message renders a document into both parts of one email.
func message(subject string, doc mailfmt.Document) (notify.Message, error) {
	html, err := doc.HTML()
	if err != nil {
		return notify.Message{}, fmt.Errorf("render mail: %w", err)
	}
	return notify.Message{Subject: subject, Body: doc.Text(), HTML: html}, nil
}
