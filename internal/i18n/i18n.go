// Package i18n holds the text Portico sends rather than displays.
//
// The console has its own translations, compiled into the frontend bundle,
// and they cover everything a reader sees while looking at a screen. This
// package is for the text that arrives somewhere else: a password-reset
// message, a confirmation link. Its reader never had a chance to pick a
// language from a menu, so the language has to be chosen for them — which is
// what Resolve does, and why the choice is written down rather than guessed
// at each call site.
//
// Messages are Go templates with named fields rather than printf verbs. That
// is not a style preference: a translator reordering "%s expires in %d
// minutes" into a language with different word order has no way to reorder
// the arguments, so positional verbs in translated strings produce a
// message with the tenant name where the number should be. A named field
// moves with the sentence.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
	"text/template"
)

// Locale is a BCP 47 tag Portico ships messages for.
type Locale string

const (
	// EnUS is the fallback, and the language every message is written in
	// first. A locale is added by adding a directory under messages/ and a
	// line to supported below — no code elsewhere changes.
	EnUS Locale = "en-US"
	// ZhCN is 简体中文.
	ZhCN Locale = "zh-CN"
)

// Default is used when nothing else resolves. It is the one locale that is
// guaranteed complete, because the parity test measures every other against
// it.
const Default = EnUS

var supported = []Locale{EnUS, ZhCN}

// Supported returns the locales this build ships messages for, in the order
// they should be offered.
func Supported() []Locale {
	out := make([]Locale, len(supported))
	copy(out, supported)
	return out
}

// Parse turns a tag into a locale this build has.
//
// Exact match first, then the language subtag alone: "zh", "zh-Hans", and
// "zh-TW" all land on zh-CN. That last one is a deliberate approximation —
// 简体 text is wrong for a 繁體 reader, but it is far closer than English,
// and shipping zh-TW is a translation rather than a lookup rule.
func Parse(tag string) (Locale, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", false
	}
	for _, locale := range supported {
		if strings.EqualFold(tag, string(locale)) {
			return locale, true
		}
	}
	language, _, _ := strings.Cut(tag, "-")
	for _, locale := range supported {
		prefix, _, _ := strings.Cut(string(locale), "-")
		if strings.EqualFold(language, prefix) {
			return locale, true
		}
	}
	return "", false
}

// Resolve picks the language a message is written in, most specific first.
//
// The order is the point, and it is one order for every message Portico
// sends: what the account asked for, then what the tenant runs in, then what
// the deployment was configured with, then English. Each step is somebody's
// stated preference, and a later one only applies because the earlier one
// said nothing.
//
// Unparseable values are skipped rather than rejected. These arrive from a
// directory synchronisation and a SCIM push as often as from a form, and an
// account carrying preferredLanguage="Klingon" should get the tenant's
// language, not an error on a password reset.
func Resolve(accountPreference, tenantDefault, deploymentDefault string) Locale {
	for _, candidate := range []string{accountPreference, tenantDefault, deploymentDefault} {
		if locale, ok := Parse(candidate); ok {
			return locale
		}
	}
	return Default
}

// Message keys. Named here rather than written as strings at the call site,
// so that a key which exists in no locale is a compile error somewhere
// rather than an empty email.
const (
	KeyRecoveryEmailSubject     = "recovery.email.subject"
	KeyRecoveryEmailBody        = "recovery.email.body"
	KeyRecoverySMS              = "recovery.sms"
	KeyVerificationEmailSubject = "verification.email.subject"
	KeyVerificationEmailBody    = "verification.email.body"
	KeyVerificationSMS          = "verification.sms"
)

// RecoveryData is what the password-recovery messages are rendered with.
//
// A struct rather than a map: a template naming a field that does not exist
// fails when it is rendered, and the test renders every message in every
// locale — so a translator's typo is caught by the build rather than by
// somebody who cannot reset their password.
type RecoveryData struct {
	Tenant   string
	Name     string
	Username string
	Link     string
	Minutes  int
}

// VerificationData is what the address-confirmation messages are rendered
// with.
type VerificationData struct {
	Tenant   string
	Name     string
	Username string
	Link     string
	Hours    int
}

//go:embed all:messages
var files embed.FS

// Catalog is the loaded set of message templates.
type Catalog struct {
	templates map[Locale]map[string]*template.Template
}

var (
	loadOnce sync.Once
	loaded   *Catalog
	loadErr  error
)

// Load reads the embedded messages. It is safe to call repeatedly; the
// parsing happens once.
func Load() (*Catalog, error) {
	loadOnce.Do(func() {
		loaded, loadErr = parse()
	})
	return loaded, loadErr
}

// MustLoad is Load for a caller with nowhere to put an error.
//
// A failure here means the binary was built with a malformed message file,
// which no deployment can fix and every request would hit — so it is a
// startup panic rather than a per-message error nobody would see until a
// password reset failed at three in the morning.
func MustLoad() *Catalog {
	catalog, err := Load()
	if err != nil {
		panic("i18n: " + err.Error())
	}
	return catalog
}

func parse() (*Catalog, error) {
	catalog := &Catalog{templates: make(map[Locale]map[string]*template.Template)}

	for _, locale := range supported {
		dir := path.Join("messages", string(locale))
		entries, err := fs.ReadDir(files, dir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}

		catalog.templates[locale] = make(map[string]*template.Template)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			raw, err := fs.ReadFile(files, path.Join(dir, entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("read %s/%s: %w", dir, entry.Name(), err)
			}
			var messages map[string]string
			if err := json.Unmarshal(raw, &messages); err != nil {
				return nil, fmt.Errorf("parse %s/%s: %w", dir, entry.Name(), err)
			}
			for key, text := range messages {
				name := string(locale) + ":" + key
				parsed, err := template.New(name).Option("missingkey=error").Parse(text)
				if err != nil {
					return nil, fmt.Errorf("parse template %s: %w", name, err)
				}
				catalog.templates[locale][key] = parsed
			}
		}
	}
	return catalog, nil
}

// Keys returns the message keys a locale has, for the tests that compare
// locales against each other.
func (c *Catalog) Keys(locale Locale) []string {
	keys := make([]string, 0, len(c.templates[locale]))
	for key := range c.templates[locale] {
		keys = append(keys, key)
	}
	return keys
}

// Render produces one message.
//
// A key missing from the requested locale falls back to Default rather than
// failing: a half-translated locale should deliver an English reset link,
// not no reset link. The parity test is what keeps that from being how
// anything actually ships.
func (c *Catalog) Render(locale Locale, key string, data any) (string, error) {
	tmpl, ok := c.templates[locale][key]
	if !ok {
		tmpl, ok = c.templates[Default][key]
		if !ok {
			return "", fmt.Errorf("i18n: no message %q in %s or %s", key, locale, Default)
		}
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("i18n: render %q in %s: %w", key, locale, err)
	}
	return out.String(), nil
}
