package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
)

// ErrUnknownField is what naming a field the catalogue does not hold gets.
// It carries the key, because the usual cause is a typo in a mapping and the
// key is the only thing that identifies which one.
var ErrUnknownField = httpx.BadRequest("UNKNOWN_FIELD",
	"No such field. The list of what may be mapped is at /api/v1/fields.")

// The field catalogue: everything that may be mapped, in either direction.
//
// One vocabulary for two directions, which is the whole reason it exists as a
// thing rather than as two lists. Outbound, an entry is a source — a fact
// Portico holds, to be sent under whatever name an application expects.
// Inbound, an entry is a target — a fact a directory attribute may be written
// into. "Department arrives from AD and is then sent to the expense system" is
// two references to the same entry, not two configurations that happen to
// agree.
//
// It is a query rather than a constant, because a tenant may define attributes
// of its own (migration 00014) and those belong in the same vocabulary as the
// built-in ones. That is why every caller takes a context and a tenant.
//
// # Why an enumeration rather than column names
//
// The obvious cheaper design is to let a mapping name a column. `users` also
// holds `password_hash`, `token_version`, and `failed_login_attempts`. A
// configuration that can name a column is a configuration that will one day
// name one of those — and it would be a tenant administrator doing it, through
// a supported field, with no code review in the way.

// MappingDirection is which way an entry may be mapped.
type MappingDirection string

const (
	// DirectionOutbound is Portico → an application's field name.
	DirectionOutbound MappingDirection = "OUTBOUND"
	// DirectionInbound is a directory attribute → Portico.
	DirectionInbound MappingDirection = "INBOUND"
)

// Field groups, which is how the console sorts the picker. Not semantic.
const (
	FieldGroupIdentity     = "identity"
	FieldGroupProfile      = "profile"
	FieldGroupOrganization = "organization"
	FieldGroupTenant       = "tenant"
	FieldGroupCustom       = "custom"
)

// FieldKinds a value may have. The five a tenant may define, plus the two the
// built-in set needs.
const (
	FieldKindText    = "TEXT"
	FieldKindNumber  = "NUMBER"
	FieldKindBoolean = "BOOLEAN"
	FieldKindDate    = "DATE"
	FieldKindSelect  = "SELECT"
)

// Field is one entry of the catalogue.
type Field struct {
	// Key is stable and is what a mapping stores. Never translated, never
	// reused: a mapping that survives a rename would be a mapping that
	// silently changed meaning.
	Key   string `json:"key"`
	Group string `json:"group"`
	Kind  string `json:"kind"`

	// Label is filled in for tenant-defined fields, whose name is whatever
	// somebody typed. Empty for a built-in, whose label the console holds in
	// its message catalogue under `fields.<key>` — a built-in has to read the
	// same in both languages, and a stored string cannot do that.
	Label string `json:"label,omitempty"`

	// Custom distinguishes a tenant's own from the built-in set. The console
	// needs it to know which ones can be edited, and the guard tests need it
	// to know which ones must be documented.
	Custom bool `json:"custom"`

	// Inbound reports whether a directory may write this.
	Inbound bool `json:"inbound"`
	// OutboundOnlyBecause is the reason Inbound is false, and is required
	// whenever it is. Several of these are security boundaries rather than
	// omissions, and a reader of the list has to be able to tell which.
	OutboundOnlyBecause string `json:"outboundOnlyBecause,omitempty"`

	// AllowedValues constrains a SELECT. Empty otherwise.
	AllowedValues []string `json:"allowedValues,omitempty"`

	// Disabled marks a tenant-defined attribute that has been retired. Its
	// values are kept and it is neither shown on a form nor sent, and it is
	// listed rather than hidden so that it can be brought back.
	Disabled bool `json:"disabled,omitempty"`
}

// Allows reports whether the field may be mapped in a direction.
func (f Field) Allows(d MappingDirection) bool {
	if d == DirectionInbound {
		return f.Inbound
	}
	return true
}

// builtInFields is the fixed vocabulary.
//
// The keys are Portico's own rather than any one protocol's. They are not
// claim names and not LDAP attribute names: this list is what mappings point
// *from*, and the whole point of the feature is that the name on the wire is
// somebody else's decision. Where a name matches an existing one it is
// because the concept is the same, not because the wire format leaked in.
var builtInFields = []Field{
	// --- Identity: what signing in produces --------------------------------
	{Key: "user_id", Group: FieldGroupIdentity, Kind: FieldKindText,
		OutboundOnlyBecause: "Portico issues it; a directory that could set it could take over an existing account"},
	{Key: "username", Group: FieldGroupIdentity, Kind: FieldKindText, Inbound: true},
	{Key: "display_name", Group: FieldGroupIdentity, Kind: FieldKindText, Inbound: true},
	{Key: "email", Group: FieldGroupIdentity, Kind: FieldKindText, Inbound: true},
	{Key: "phone", Group: FieldGroupIdentity, Kind: FieldKindText, Inbound: true},
	{Key: "email_verified", Group: FieldGroupIdentity, Kind: FieldKindBoolean,
		OutboundOnlyBecause: "it records that Portico checked the address, and a directory saying so is not Portico having checked"},
	{Key: "phone_verified", Group: FieldGroupIdentity, Kind: FieldKindBoolean,
		OutboundOnlyBecause: "the same: it is a record of a check made here"},
	{Key: "role", Group: FieldGroupIdentity, Kind: FieldKindSelect,
		AllowedValues:       []string{"SUPER_ADMIN", "USER"},
		OutboundOnlyBecause: "a directory attribute that granted administrator would put privilege escalation in a system Portico does not own"},
	{Key: "status", Group: FieldGroupIdentity, Kind: FieldKindSelect,
		AllowedValues:       []string{"ACTIVE", "DISABLED"},
		OutboundOnlyBecause: "an entry disappearing is already how a directory deactivates; a second, attribute-driven channel would fight with it"},
	{Key: "external_id", Group: FieldGroupIdentity, Kind: FieldKindText, Inbound: true},
	{Key: "updated_at", Group: FieldGroupIdentity, Kind: FieldKindDate,
		OutboundOnlyBecause: "it is when this row last changed, which only this database knows"},

	// --- Profile: the twenty-five from SCIM's schema (migration 00007) -----
	{Key: "name_formatted", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "family_name", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "given_name", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "middle_name", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "honorific_prefix", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "honorific_suffix", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "nick_name", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "profile_url", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "photo_url", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "title", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "user_type", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "preferred_language", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "locale", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "timezone", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "address_formatted", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "street_address", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "locality", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "region", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "postal_code", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "country", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "employee_number", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "cost_center", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "department", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "manager_id", Group: FieldGroupProfile, Kind: FieldKindText, Inbound: true},
	{Key: "manager_name", Group: FieldGroupProfile, Kind: FieldKindText,
		OutboundOnlyBecause: "it is read from the manager's own account, so writing it here would let the two disagree"},

	// --- Organization: where the person is ---------------------------------
	//
	// All outbound-only, and for one reason rather than several: ldap.md
	// settled it. "The directory says who somebody is; where they belong is
	// decided here." A source may file the accounts it creates into an
	// organization, and that is applied once, at creation.
	{Key: "organization_id", Group: FieldGroupOrganization, Kind: FieldKindText,
		OutboundOnlyBecause: "membership is decided here, not by a directory attribute — see ldap.md"},
	{Key: "organization_name", Group: FieldGroupOrganization, Kind: FieldKindText,
		OutboundOnlyBecause: "it belongs to the organization row, not to the account"},
	{Key: "organization_code", Group: FieldGroupOrganization, Kind: FieldKindText,
		OutboundOnlyBecause: "the same, and this is the one most downstream systems actually want"},
	{Key: "organization_parent_code", Group: FieldGroupOrganization, Kind: FieldKindText,
		OutboundOnlyBecause: "read from the tree above the account"},
	{Key: "organization_path", Group: FieldGroupOrganization, Kind: FieldKindText,
		OutboundOnlyBecause: "computed from the tree: the codes from the root down, joined by /"},
	{Key: "organization_manager_name", Group: FieldGroupOrganization, Kind: FieldKindText,
		OutboundOnlyBecause: "read from the organization's manager account"},

	// --- Tenant -----------------------------------------------------------
	{Key: "tenant_id", Group: FieldGroupTenant, Kind: FieldKindText,
		OutboundOnlyBecause: "a tenant is not a property of an account, and no account may change its own"},
	{Key: "tenant_code", Group: FieldGroupTenant, Kind: FieldKindText,
		OutboundOnlyBecause: "the same"},
}

// MaxCustomFieldsPerTenant bounds how many attributes a tenant may define.
//
// Not about storage. Every definition is a candidate for outbound mapping and
// a mapped attribute is bytes in an id_token, so an unbounded number of them
// makes token size something a tenant chooses by accident. Fifty is far past
// any real use and near enough to notice.
const MaxCustomFieldsPerTenant = 50

// FieldCatalogue answers what may be mapped, for one tenant.
type FieldCatalogue struct {
	store *store.Store
}

// NewFieldCatalogue wires a catalogue.
func NewFieldCatalogue(st *store.Store) *FieldCatalogue {
	return &FieldCatalogue{store: st}
}

// Fields returns the built-in vocabulary followed by the tenant's own.
//
// Order is stable — built-ins in the order declared above, then custom ones by
// their sort order — because this list is drawn as a picker and a picker whose
// order changes between page loads is one nobody can build a habit with.
func (c *FieldCatalogue) Fields(ctx context.Context, tenantID string) ([]Field, error) {
	custom, err := c.customFields(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	fields := make([]Field, 0, len(builtInFields)+len(custom))
	fields = append(fields, builtInFields...)
	return append(fields, custom...), nil
}

// Field looks one up by key, across both halves.
func (c *FieldCatalogue) Field(ctx context.Context, tenantID, key string) (Field, error) {
	fields, err := c.Fields(ctx, tenantID)
	if err != nil {
		return Field{}, err
	}
	for _, f := range fields {
		if f.Key == key {
			return f, nil
		}
	}
	return Field{}, ErrUnknownField
}

// BuiltInFields is the fixed half, for the tests that hold the documentation
// and the message catalogue in step with it. Copied rather than returned
// directly: a caller that sorted it in place would reorder every picker.
func BuiltInFields() []Field {
	out := make([]Field, len(builtInFields))
	copy(out, builtInFields)
	return out
}

// IsBuiltInFieldKey reports whether a key is taken by the built-in half. A
// tenant-defined attribute may not use one: a mapping stores a key, and two
// entries under one key would make the mapping ambiguous.
func IsBuiltInFieldKey(key string) bool {
	for _, f := range builtInFields {
		if f.Key == key {
			return true
		}
	}
	return false
}

// customFields reads the tenant's own definitions into catalogue entries.
//
// A disabled definition is present and marked rather than absent, because the
// console has to be able to enable it again — and because a mapping that names
// it should read as "configured but switched off" rather than as a mapping to
// nothing.
func (c *FieldCatalogue) customFields(ctx context.Context, tenantID string) ([]Field, error) {
	rows, err := c.store.ForTenant(tenantID).ListUserAttributeDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list attribute definitions: %w", err)
	}

	fields := make([]Field, 0, len(rows))
	for _, row := range rows {
		var allowed []string
		if len(row.AllowedValues) > 0 {
			// A malformed value here would be a row this application wrote, so
			// it is a bug rather than input — but a picker that failed to draw
			// would hide every other field too, so it degrades to no
			// constraint rather than to an error.
			_ = json.Unmarshal(row.AllowedValues, &allowed)
		}

		fields = append(fields, Field{
			Key:           row.Key,
			Group:         FieldGroupCustom,
			Kind:          row.Kind,
			Label:         row.Label,
			Custom:        true,
			Disabled:      row.Status != string(model.StatusActive),
			AllowedValues: allowed,
			// Tenant-defined attributes carry no built-in semantics, so there
			// is nothing for a directory to escalate through and both
			// directions are open.
			Inbound: true,
		})
	}
	return fields, nil
}
