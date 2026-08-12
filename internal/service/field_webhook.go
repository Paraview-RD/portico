package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
)

// Applying a recipient's mappings to a webhook payload.
//
// The other three recipients are assembled field by field, so a mapping is
// consulted while the claim set or the attribute list is being built. A webhook
// body is not: it is whatever the entity marshals to, and has been since before
// this feature existed. So here the mapping is an overlay rather than a recipe.
//
// # Why an overlay rather than building the body from the catalogue
//
// The catalogue does not cover the payload. `model.User` marshals `source`,
// `attachments`, `closedAt`, `lockedUntil` and `createdAt`, and
// `model.Organization` marshals `remark`, `sortOrder`, `userCount` and
// `createdAt` — none of which are catalogue keys, because none of them are
// facts anybody maps. A body rebuilt from the catalogue would drop all of them
// the moment somebody configured their first rename, and it would look like the
// rename had done it. So the default body is produced exactly as it always was
// and the rules are applied on top.
//
// # What a rule means here
//
// A mapping target is one name, so a rename of a nested field can only mean the
// top level: `profile.department → dept` puts the value at `dept` and removes
// it from `profile`. Suppression removes in place. An addition — a fact the
// payload never carried — lands at the top level too. One sentence: a mapping
// puts the value at the name you choose, at the top level of `data`.

// payloadSubject is what an event is about, which decides which default
// locations apply. Taken from the event type rather than stored on the mapping:
// `user.updated` carries an account and `organization.updated` carries an
// organization, so the event type already answers this and a column repeating
// it would be a second place for the answer to be wrong.
type payloadSubject string

const (
	subjectUser         payloadSubject = "user"
	subjectOrganization payloadSubject = "organization"
	// subjectNone is every other event. Group events land here: there is no
	// group vocabulary in the catalogue, so their payloads are delivered
	// exactly as they always were. Documented in webhooks.md, because a
	// subscription that has configured mappings and sees an unchanged group
	// body needs that to be stated behaviour rather than a surprise.
	subjectNone payloadSubject = ""
)

// subjectOf reads the subject out of an event type.
func subjectOf(eventType string) payloadSubject {
	switch {
	case strings.HasPrefix(eventType, "user."):
		return subjectUser
	case strings.HasPrefix(eventType, "organization."):
		return subjectOrganization
	default:
		return subjectNone
	}
}

// webhookUserDefaults maps a catalogue key to where that fact already sits in a
// user payload. A key absent from this table is not in the body today, so a
// rule naming it is an addition rather than a rename.
//
// Written out rather than derived from struct tags. The keys on the left are
// Portico's vocabulary and the ones on the right are a wire format that other
// people's code already parses; deriving one from the other would mean a
// renamed Go field silently changed what subscribers receive.
var webhookUserDefaults = map[string]string{
	"user_id":           "id",
	"tenant_id":         "tenantId",
	"username":          "username",
	"display_name":      "displayName",
	"phone":             "phone",
	"email":             "email",
	"role":              "role",
	"status":            "status",
	"external_id":       "externalId",
	"updated_at":        "updatedAt",
	"organization_id":   "organizationId",
	"organization_name": "organizationName",

	"name_formatted":     "profile.nameFormatted",
	"family_name":        "profile.familyName",
	"given_name":         "profile.givenName",
	"middle_name":        "profile.middleName",
	"honorific_prefix":   "profile.honorificPrefix",
	"honorific_suffix":   "profile.honorificSuffix",
	"nick_name":          "profile.nickName",
	"profile_url":        "profile.profileUrl",
	"photo_url":          "profile.photoUrl",
	"title":              "profile.title",
	"user_type":          "profile.userType",
	"preferred_language": "profile.preferredLanguage",
	"locale":             "profile.locale",
	"timezone":           "profile.timezone",
	"address_formatted":  "profile.addressFormatted",
	"street_address":     "profile.streetAddress",
	"locality":           "profile.locality",
	"region":             "profile.region",
	"postal_code":        "profile.postalCode",
	"country":            "profile.country",
	"employee_number":    "profile.employeeNumber",
	"cost_center":        "profile.costCenter",
	"department":         "profile.department",
	"manager_id":         "profile.managerId",
	"manager_name":       "profile.managerName",
}

// webhookOrganizationDefaults is the same for an organization payload.
//
// Much shorter, and that is the point: an organization event already carries
// nearly everything about the organization, so mappings here are almost all
// renames and suppressions. `organization_path` is the one fact worth adding,
// and it is computed rather than stored.
var webhookOrganizationDefaults = map[string]string{
	"organization_id":           "id",
	"organization_name":         "name",
	"organization_code":         "code",
	"organization_manager_name": "managerName",
	"status":                    "status",
	"updated_at":                "updatedAt",
}

// webhookTopLevelOwners is which catalogue key each top-level payload name
// already belongs to, across both subjects.
//
// It exists to refuse a rename that would land on a name the payload already
// uses for something else — `department → id` would put a department where
// every subscriber reads the identifier. The owner is recorded rather than just
// the name, because `organization_code → code` is a perfectly ordinary rule: in
// an organization event `code` is exactly where that fact already lives, and
// refusing it would be refusing a mapping to itself.
//
// Nested locations are absent by construction: only the top level can collide,
// since that is the only place a mapping can put anything.
//
// A set of owners rather than one, because a name can be owned by a different
// key in each subject — `id` is `user_id` in a user event and
// `organization_id` in an organization event, and both are mappings to
// themselves. Keeping one owner would mean picking it out of a Go map's
// iteration order, and the same save would then be accepted or refused
// depending on which one the process happened to see first.
var webhookTopLevelOwners = func() map[string]map[string]bool {
	owners := map[string]map[string]bool{}
	for _, table := range []map[string]string{webhookUserDefaults, webhookOrganizationDefaults} {
		for key, location := range table {
			if strings.Contains(location, ".") {
				continue
			}
			if owners[location] == nil {
				owners[location] = map[string]bool{}
			}
			owners[location][key] = true
		}
	}
	return owners
}()

// defaultsFor is the location table for a subject.
func defaultsFor(subject payloadSubject) map[string]string {
	switch subject {
	case subjectUser:
		return webhookUserDefaults
	case subjectOrganization:
		return webhookOrganizationDefaults
	default:
		return nil
	}
}

// applyToPayload overlays one recipient's rules onto a payload.
//
// body is consumed rather than shared: the caller decodes a fresh copy per
// subscription, because two subscriptions of the same event must not be able to
// see each other's renames.
func applyToPayload(body map[string]any, subject payloadSubject, out Outbound, values map[string]string) map[string]any {
	defaults := defaultsFor(subject)

	// Renames and suppressions, over what the body already carries.
	for key, location := range defaults {
		name, send := out.NameFor(key, location)
		if send && name == location {
			continue // No rule, or a rule that names where it already is.
		}

		value, present := takeAt(body, location)
		if !send || !present {
			continue // Suppressed, or absent — and absent stays absent.
		}
		body[name] = value
	}

	// Additions: the facts the payload never carried, which is most of the
	// catalogue and the larger half of what this feature is for.
	knownDefaults := make(map[string]bool, len(defaults))
	for key := range defaults {
		knownDefaults[key] = true
	}
	for _, rule := range out.Additions(knownDefaults) {
		value, ok := values[rule.SourceKey]
		if !ok {
			// Nothing is ever sent empty. A subscriber that never receives a
			// field it mapped should look at the account rather than at the
			// mapping.
			continue
		}
		body[rule.TargetName] = value
	}

	// An object left empty by lifting its last member is removed rather than
	// sent as `{}`. A `profile` that appears and disappears depending on
	// configuration is harder to consume than either one consistently.
	if profile, ok := body["profile"].(map[string]any); ok && len(profile) == 0 {
		delete(body, "profile")
	}
	return body
}

// WithFieldMappings attaches what applying a subscription's rules needs.
//
// Separate from the constructor, on the same terms as WithVault: a service
// built without it delivers exactly what it always did, which is what every
// test that only cares about delivery wants — and what a caller that forgot to
// wire it should get, rather than a panic in the path that notifies other
// systems that an account was disabled.
func (s *WebhookService) WithFieldMappings(catalogue *FieldCatalogue, mappings *FieldMappingService) *WebhookService {
	s.catalogue = catalogue
	s.mappings = mappings
	return s
}

// eventOverlay is what applying mappings to one event needs.
//
// One per event rather than one per subscription: the rules differ per
// subscriber but the facts do not, so the queries that resolve an addition run
// at most once however many subscriptions selected the event.
type eventOverlay struct {
	svc      *WebhookService
	tenantID string
	subject  payloadSubject
	data     any

	// raw is the default body, marshalled once and decoded fresh per
	// subscription. Two subscribers to the same event must not be able to see
	// each other's renames, and one shared tree would let them.
	raw []byte

	values   map[string]string
	resolved bool
	usable   bool
}

// overlayFor prepares to apply mappings to one event.
func (s *WebhookService) overlayFor(tenantID, eventType string, data any) *eventOverlay {
	return &eventOverlay{
		svc: s, tenantID: tenantID, subject: subjectOf(eventType), data: data,
	}
}

// dataFor is what one subscription should receive.
//
// Every path that cannot produce a mapped body returns the default one. This
// is called from publish, which is called from operations that have already
// succeeded — an account exists, an organization was renamed — so a failure
// here must cost a rename, never an event.
func (o *eventOverlay) dataFor(ctx context.Context, subscriptionID string) any {
	if o.svc.mappings == nil || o.subject == subjectNone {
		return o.data
	}

	ref := store.RecipientRef{WebhookSubscriptionID: subscriptionID}
	out, err := o.svc.mappings.OutboundFor(ctx, o.tenantID, ref)
	if err != nil {
		slog.ErrorContext(ctx, "could not read webhook field mappings; delivering the default payload",
			"tenant", o.tenantID, "subscription", subscriptionID, "error", err)
		return o.data
	}
	if out.Empty() {
		return o.data
	}

	if !o.resolve(ctx) {
		return o.data
	}
	var body map[string]any
	if err := json.Unmarshal(o.raw, &body); err != nil {
		slog.ErrorContext(ctx, "could not read back the event payload; delivering the default",
			"tenant", o.tenantID, "error", err)
		return o.data
	}
	return applyToPayload(body, o.subject, out, o.values)
}

// resolve works out the event's facts, once, and reports whether a mapped body
// can be built at all.
func (o *eventOverlay) resolve(ctx context.Context) bool {
	if o.resolved {
		return o.usable
	}
	o.resolved = true

	raw, err := json.Marshal(o.data)
	if err != nil {
		slog.ErrorContext(ctx, "could not render event payload for mapping; delivering the default",
			"tenant", o.tenantID, "error", err)
		return false
	}
	o.raw = raw

	values, err := o.resolveValues(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "could not resolve mapped field values; delivering the default payload",
			"tenant", o.tenantID, "error", err)
		return false
	}
	o.values = values

	o.usable = true
	return true
}

// resolveValues is the facts an addition can name, for this event's subject.
func (o *eventOverlay) resolveValues(ctx context.Context) (map[string]string, error) {
	switch subject := o.data.(type) {
	case model.User:
		return o.svc.catalogue.FieldValues(ctx, o.tenantID, subject)
	case model.Organization:
		// Narrow on purpose. An organization event already carries nearly
		// everything about the organization, so the only facts worth adding
		// are the computed ones — and addOrganizationValues computes exactly
		// those, given the organization's own id rather than an account's.
		values := map[string]string{"tenant_id": o.tenantID}
		if err := o.svc.catalogue.addOrganizationValues(ctx, o.tenantID, subject.ID, values); err != nil {
			return nil, err
		}
		for key, value := range values {
			if strings.TrimSpace(value) == "" {
				delete(values, key)
			}
		}
		return values, nil
	default:
		// A subject the event type claimed but the payload is not. Nothing to
		// add; renames and suppressions over the body still work.
		return map[string]string{}, nil
	}
}

// takeAt reads a value out of the body and removes it, following at most one
// level of nesting — which is all the payloads have, and keeping it at one
// means a location string cannot describe a traversal nobody can predict.
func takeAt(body map[string]any, location string) (any, bool) {
	parent, leaf, nested := strings.Cut(location, ".")
	if !nested {
		value, ok := body[parent]
		delete(body, parent)
		return value, ok
	}

	object, ok := body[parent].(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := object[leaf]
	delete(object, leaf)
	return value, ok
}

// The body is round-tripped through JSON rather than reflected over, so that
// what gets overlaid is exactly what would otherwise have been delivered —
// including every `omitempty` that fired and every custom marshaller. It is
// also why a subscription with no rules is handed the original value untouched
// rather than a re-encoded one: identical output is worth more than uniform
// code on the path that ninety-nine deployments in a hundred stay on.
