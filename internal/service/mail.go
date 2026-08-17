package service

import (
	"fmt"

	"github.com/Paraview-RD/portico/internal/mailfmt"
	"github.com/Paraview-RD/portico/internal/notify"
)

// The shape three of this system's messages share, and the one function that
// turns a description into a sendable message.
//
// Password recovery, address confirmation and a trial's first mail all exist
// to deliver one address and nothing else: a sentence saying why it arrived,
// a button, and the small print underneath. Written once here so that a
// change to any of that — the wording of the fallback line, where the brand
// sits, what the footer is separated by — lands on all three rather than on
// whichever one somebody remembered.

// mailBrand is the wordmark on a message that belongs to no tenant.
//
// Only the trial messages: an applicant has no tenant yet, so the name at the
// top is the product's. Everything sent to somebody who already has an
// account is branded with their tenant's name instead — a password reset for
// Acme's user is from Acme, whatever software Acme happens to run.
const mailBrand = "Portico"

// linkMail is a message whose entire purpose is one address.
type linkMail struct {
	// Brand is the name above the title: a tenant's, or the product's.
	Brand string
	Title string
	// Intro says who this is about and why it arrived.
	Intro string
	// Action labels the button; Link is where it goes.
	Action string
	Link   string
	// Fallback introduces the written-out address under the button.
	Fallback string
	// Footer is the small print: how long the link lasts, and what to do if
	// this was not you.
	Footer []string
}

func (m linkMail) document() mailfmt.Document {
	return mailfmt.Document{
		Brand: m.Brand,
		Title: m.Title,
		Intro: []string{m.Intro},
		Sections: []mailfmt.Section{{
			Action: &mailfmt.Action{
				Label:    m.Action,
				URL:      m.Link,
				Fallback: m.Fallback,
			},
		}},
		Footer: m.Footer,
	}
}

// message renders a document into both parts of one email.
func message(subject string, doc mailfmt.Document) (notify.Message, error) {
	html, err := doc.HTML()
	if err != nil {
		return notify.Message{}, fmt.Errorf("render mail: %w", err)
	}
	return notify.Message{Subject: subject, Body: doc.Text(), HTML: html}, nil
}
