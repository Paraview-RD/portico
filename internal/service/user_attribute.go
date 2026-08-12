package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// UserAttributeService manages the attributes a tenant defines for itself, and
// the values recorded against accounts.
//
// The twenty-five specification-derived attributes are columns and are edited
// through the profile endpoint. These are the other kind: a fact a tenant has
// about its people that SCIM's schema has no name for — a badge number, a
// contract end date, a site code. Without them the answer is to overload
// `costCenter` and hope nobody notices.
type UserAttributeService struct {
	store *store.Store
	audit *AuditService
}

// NewUserAttributeService wires the service.
func NewUserAttributeService(st *store.Store, audit *AuditService) *UserAttributeService {
	return &UserAttributeService{store: st, audit: audit}
}

const targetUserAttribute = "USER_ATTRIBUTE"

// Errors this service returns.
var (
	ErrUserAttributeNotFound = httpx.NotFound("USER_ATTRIBUTE_NOT_FOUND",
		"No such attribute.")
	// ErrUserAttributeKeyTaken covers both halves of the namespace, because to
	// the person typing it there is one namespace: a key that already names
	// something cannot name a second thing, and whether the first is built in
	// or their own colleague's does not change what they have to do.
	ErrUserAttributeKeyTaken = httpx.Conflict("USER_ATTRIBUTE_KEY_TAKEN",
		"That key is already in use. Keys have to be unique across both the built-in fields and your own.")
	ErrInvalidUserAttributeKey = httpx.BadRequest("INVALID_USER_ATTRIBUTE_KEY",
		"A key is 3 to 40 characters of lower-case letters, digits, and underscores, starting with a letter.")
	ErrInvalidUserAttributeKind = httpx.BadRequest("INVALID_USER_ATTRIBUTE_KIND",
		"The kind must be TEXT, NUMBER, BOOLEAN, DATE, or SELECT.")
	ErrUserAttributeLabelRequired = httpx.BadRequest("USER_ATTRIBUTE_LABEL_REQUIRED",
		"A label is required: it is what an operator sees on the form.")
	ErrUserAttributeNeedsValues = httpx.BadRequest("USER_ATTRIBUTE_NEEDS_VALUES",
		"A single-select attribute needs at least one permitted value.")
	// ErrTooManyUserAttributes is a bound on token size rather than on
	// storage, and says so: the number is small because every attribute is a
	// candidate for outbound mapping.
	ErrTooManyUserAttributes = httpx.UnprocessableEntity("TOO_MANY_USER_ATTRIBUTES",
		fmt.Sprintf("A tenant may define %d attributes. Each one is a candidate for outbound mapping, "+
			"and a mapped attribute is bytes in every token.", MaxCustomFieldsPerTenant))
	ErrInvalidUserAttributeValue = httpx.BadRequest("INVALID_USER_ATTRIBUTE_VALUE",
		"That value does not match the attribute's kind.")
)

// attributeKey is the same expression the schema enforces. Duplicated on
// purpose: the CHECK is the backstop and this is the message, and a caller
// deserves to be told what is wrong rather than shown a constraint violation.
var attributeKey = regexp.MustCompile(`^[a-z][a-z0-9_]{1,38}[a-z0-9]$`)

// UserAttributeInput is a definition as an administrator describes it.
type UserAttributeInput struct {
	// Key is accepted on creation and ignored on update: it is what a mapping
	// stores, so renaming it would silently stop a mapping that names it, in a
	// system Portico does not own and cannot warn.
	Key         string
	Label       string
	Description string
	Kind        string
	// AllowedValues applies to SELECT and is ignored otherwise.
	AllowedValues []string
	Required      bool
	SortOrder     int
}

// Definitions returns the tenant's attribute definitions.
func (s *UserAttributeService) Definitions(ctx context.Context, tenantID string) ([]model.UserAttributeDefinition, error) {
	rows, err := s.store.ForTenant(tenantID).ListUserAttributeDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list attribute definitions: %w", err)
	}

	out := make([]model.UserAttributeDefinition, 0, len(rows))
	for _, row := range rows {
		out = append(out, definitionFromRow(row))
	}
	return out, nil
}

// Define adds an attribute.
func (s *UserAttributeService) Define(ctx context.Context, actor auth.Principal, in UserAttributeInput) (model.UserAttributeDefinition, error) {
	tenantID := actor.TenantID

	normalized, err := s.normalize(in, true)
	if err != nil {
		return model.UserAttributeDefinition{}, err
	}

	q := s.store.ForTenant(tenantID)

	// The bound is checked here rather than in the schema because it is a
	// count. Checked before the key, so that a tenant at the limit is told
	// about the limit rather than about a collision it would hit anyway.
	count, err := q.CountUserAttributeDefinitions(ctx)
	if err != nil {
		return model.UserAttributeDefinition{}, fmt.Errorf("count attribute definitions: %w", err)
	}
	if count >= MaxCustomFieldsPerTenant {
		return model.UserAttributeDefinition{}, ErrTooManyUserAttributes
	}

	if IsBuiltInFieldKey(normalized.Key) {
		return model.UserAttributeDefinition{}, ErrUserAttributeKeyTaken
	}

	allowed, err := json.Marshal(normalized.AllowedValues)
	if err != nil {
		return model.UserAttributeDefinition{}, fmt.Errorf("encode allowed values: %w", err)
	}

	now := store.Now()
	id := uuid.NewString()

	err = q.CreateUserAttributeDefinition(ctx, sqlcgen.CreateUserAttributeDefinitionParams{
		ID: id, Key: normalized.Key, Label: normalized.Label,
		Description: normalized.Description, Kind: normalized.Kind,
		AllowedValues: allowed, Required: normalized.Required,
		SortOrder: narrow(normalized.SortOrder), Status: string(model.StatusActive),
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.UserAttributeDefinition{}, ErrUserAttributeKeyTaken
		}
		return model.UserAttributeDefinition{}, fmt.Errorf("define attribute: %w", err)
	}

	definition, err := s.definition(ctx, tenantID, id)
	if err != nil {
		return model.UserAttributeDefinition{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionUserAttributeDefine,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetUserAttribute, TargetID: id, TargetName: definition.Key,
		Detail: fmt.Sprintf("%s, kind %s", definition.Label, definition.Kind),
	})
	return definition, nil
}

// Update changes an attribute's editable parts. The key is not among them.
func (s *UserAttributeService) Update(ctx context.Context, actor auth.Principal, id string, in UserAttributeInput) (model.UserAttributeDefinition, error) {
	tenantID := actor.TenantID

	existing, err := s.definition(ctx, tenantID, id)
	if err != nil {
		return model.UserAttributeDefinition{}, err
	}

	normalized, err := s.normalize(in, false)
	if err != nil {
		return model.UserAttributeDefinition{}, err
	}

	allowed, err := json.Marshal(normalized.AllowedValues)
	if err != nil {
		return model.UserAttributeDefinition{}, fmt.Errorf("encode allowed values: %w", err)
	}

	now := store.Now()
	err = s.store.ForTenant(tenantID).UpdateUserAttributeDefinition(ctx,
		sqlcgen.UpdateUserAttributeDefinitionParams{
			ID: id, Label: normalized.Label, Description: normalized.Description,
			Kind: normalized.Kind, AllowedValues: allowed, Required: normalized.Required,
			SortOrder: narrow(normalized.SortOrder), UpdatedAt: now,
		})
	if err != nil {
		return model.UserAttributeDefinition{}, fmt.Errorf("update attribute: %w", err)
	}

	updated, err := s.definition(ctx, tenantID, id)
	if err != nil {
		return model.UserAttributeDefinition{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionUserAttributeUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetUserAttribute, TargetID: id, TargetName: existing.Key,
		Detail: fmt.Sprintf("%s, kind %s", updated.Label, updated.Kind),
	})
	return updated, nil
}

// SetStatus retires an attribute or brings it back.
//
// Retiring keeps every value already recorded. It is the ordinary way to stop
// using one, and the reason it exists beside Delete is that the values are
// often the answer to a question somebody asks later.
func (s *UserAttributeService) SetStatus(ctx context.Context, actor auth.Principal, id string, status model.Status) (model.UserAttributeDefinition, error) {
	tenantID := actor.TenantID

	if _, err := s.definition(ctx, tenantID, id); err != nil {
		return model.UserAttributeDefinition{}, err
	}
	if !status.Valid() {
		return model.UserAttributeDefinition{}, httpx.BadRequest("INVALID_STATUS",
			"Status must be ACTIVE or DISABLED.")
	}

	if err := s.store.ForTenant(tenantID).UpdateUserAttributeDefinitionStatus(ctx,
		id, string(status), store.Now()); err != nil {
		return model.UserAttributeDefinition{}, fmt.Errorf("set attribute status: %w", err)
	}

	updated, err := s.definition(ctx, tenantID, id)
	if err != nil {
		return model.UserAttributeDefinition{}, err
	}

	action := model.ActionUserAttributeEnable
	if status == model.StatusDisabled {
		action = model.ActionUserAttributeDisable
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetUserAttribute, TargetID: id, TargetName: updated.Key,
	})
	return updated, nil
}

// Delete removes an attribute and every value recorded against it.
//
// Audited with the count, because that count is the whole of what was lost and
// it is not recoverable. Disabling is the ordinary path; this is the other one.
func (s *UserAttributeService) Delete(ctx context.Context, actor auth.Principal, id string) error {
	tenantID := actor.TenantID

	existing, err := s.definition(ctx, tenantID, id)
	if err != nil {
		return err
	}

	q := s.store.ForTenant(tenantID)
	values, err := q.CountUserAttributeValues(ctx, id)
	if err != nil {
		return fmt.Errorf("count attribute values: %w", err)
	}
	if err := q.DeleteUserAttributeDefinition(ctx, id); err != nil {
		return fmt.Errorf("delete attribute: %w", err)
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionUserAttributeDelete,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetUserAttribute, TargetID: id, TargetName: existing.Key,
		Detail: fmt.Sprintf("%d recorded values discarded with it", values),
	})
	return nil
}

// Values returns one account's custom values, keyed by attribute key.
//
// Disabled attributes are left out: a value that is neither shown nor sent is
// not part of the account as anybody sees it.
func (s *UserAttributeService) Values(ctx context.Context, tenantID, userID string) (map[string]string, error) {
	rows, err := s.store.ForTenant(tenantID).ListUserAttributeValues(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list attribute values: %w", err)
	}

	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	return values, nil
}

// SetValues records values for an account, by attribute key.
//
// A key absent from the map is left alone and an empty value clears it, which
// is the same contract the profile endpoint has. Clearing removes the row
// rather than storing an empty string, so that "never filled in" and
// "deliberately blank" stay distinguishable — the outbound rule is that nothing
// is sent empty, and a stored empty string would be a value that is configured
// and silently never arrives.
func (s *UserAttributeService) SetValues(ctx context.Context, actor auth.Principal, userID string, values map[string]string) error {
	tenantID := actor.TenantID
	q := s.store.ForTenant(tenantID)

	now := store.Now()
	changed := make([]string, 0, len(values))

	for key, raw := range values {
		definition, err := q.GetUserAttributeDefinitionByKey(ctx, key)
		if err != nil {
			if store.IsNoRows(err) {
				return ErrUserAttributeNotFound
			}
			return fmt.Errorf("read attribute %s: %w", key, err)
		}

		value := strings.TrimSpace(raw)
		if value == "" {
			if err := q.DeleteUserAttributeValue(ctx, userID, definition.ID); err != nil {
				return fmt.Errorf("clear attribute %s: %w", key, err)
			}
			changed = append(changed, key)
			continue
		}

		normalized, err := normalizeAttributeValue(definition, value)
		if err != nil {
			return err
		}
		err = q.SetUserAttributeValue(ctx, sqlcgen.SetUserAttributeValueParams{
			UserID: userID, DefinitionID: definition.ID, Value: normalized, UpdatedAt: now,
		})
		if err != nil {
			return fmt.Errorf("write attribute %s: %w", key, err)
		}
		changed = append(changed, key)
	}

	if len(changed) == 0 {
		return nil
	}
	// The keys rather than the values. An audit entry that carried the values
	// would be a second copy of whatever a tenant chose to record about its
	// people, in a table with a different retention period.
	slices.Sort(changed)
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionUserAttributeSet,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "USER", TargetID: userID,
		Detail: strings.Join(changed, ", "),
	})
	return nil
}

func (s *UserAttributeService) definition(ctx context.Context, tenantID, id string) (model.UserAttributeDefinition, error) {
	row, err := s.store.ForTenant(tenantID).GetUserAttributeDefinition(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.UserAttributeDefinition{}, ErrUserAttributeNotFound
		}
		return model.UserAttributeDefinition{}, fmt.Errorf("read attribute: %w", err)
	}
	return definitionFromRow(row), nil
}

func (s *UserAttributeService) normalize(in UserAttributeInput, creating bool) (UserAttributeInput, error) {
	// Trimmed but not lower-cased. A key typed as "Badge" is refused rather
	// than quietly stored as "badge": it becomes a claim name and a line in
	// somebody else's configuration, and the two would then disagree with
	// nothing to say so. The same reasoning as refusing an out-of-range
	// synchronization interval instead of rounding it.
	in.Key = strings.TrimSpace(in.Key)
	in.Label = strings.TrimSpace(in.Label)
	in.Description = strings.TrimSpace(in.Description)
	in.Kind = strings.ToUpper(strings.TrimSpace(in.Kind))

	if creating && !attributeKey.MatchString(in.Key) {
		return in, ErrInvalidUserAttributeKey
	}
	if in.Label == "" {
		return in, ErrUserAttributeLabelRequired
	}

	switch in.Kind {
	case FieldKindText, FieldKindNumber, FieldKindBoolean, FieldKindDate:
		// Values are only meaningful for a select, and a stale list left on a
		// field that stopped being one would be shown by nothing and refuse
		// nothing.
		in.AllowedValues = nil
	case FieldKindSelect:
		cleaned := make([]string, 0, len(in.AllowedValues))
		for _, v := range in.AllowedValues {
			if v = strings.TrimSpace(v); v != "" {
				cleaned = append(cleaned, v)
			}
		}
		if len(cleaned) == 0 {
			return in, ErrUserAttributeNeedsValues
		}
		in.AllowedValues = cleaned
	default:
		return in, ErrInvalidUserAttributeKind
	}

	return in, nil
}

// normalizeAttributeValue checks a value against its kind and returns the form
// to store.
//
// Stored in a canonical form rather than as typed, because the value leaves in
// a token: a boolean recorded as "Yes" and another as "true" would arrive at an
// application as two different facts.
func normalizeAttributeValue(definition sqlcgen.UserAttributeDefinition, value string) (string, error) {
	switch definition.Kind {
	case FieldKindText:
		return value, nil

	case FieldKindNumber:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return "", ErrInvalidUserAttributeValue
		}
		return value, nil

	case FieldKindBoolean:
		// Wider than strconv.ParseBool, and deliberately so. This value can
		// arrive from a directory, where "TRUE", "yes", and "Y" are all
		// ordinary, and the point of storing a canonical form is that those
		// three do not reach an application as three different facts.
		//
		// Wider is not open, though: anything outside this set is refused
		// rather than guessed at, because guessing which way "pending" leans
		// would be inventing an answer.
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "t", "yes", "y", "on", "1":
			return "true", nil
		case "false", "f", "no", "n", "off", "0":
			return "false", nil
		default:
			return "", ErrInvalidUserAttributeValue
		}

	case FieldKindDate:
		// Date only, not a timestamp. An attribute holding "contract ends" has
		// no time of day, and accepting one would mean two tenants recording
		// the same fact in two formats.
		parsed, err := time.Parse(time.DateOnly, value)
		if err != nil {
			return "", ErrInvalidUserAttributeValue
		}
		return parsed.Format(time.DateOnly), nil

	case FieldKindSelect:
		var allowed []string
		if err := json.Unmarshal(definition.AllowedValues, &allowed); err != nil {
			return "", fmt.Errorf("read allowed values for %s: %w", definition.Key, err)
		}
		for _, candidate := range allowed {
			if candidate == value {
				return value, nil
			}
		}
		return "", ErrInvalidUserAttributeValue

	default:
		return "", ErrInvalidUserAttributeKind
	}
}

func definitionFromRow(row sqlcgen.UserAttributeDefinition) model.UserAttributeDefinition {
	var allowed []string
	if len(row.AllowedValues) > 0 {
		_ = json.Unmarshal(row.AllowedValues, &allowed)
	}
	return model.UserAttributeDefinition{
		ID: row.ID, TenantID: row.TenantID, Key: row.Key,
		Label: row.Label, Description: row.Description, Kind: row.Kind,
		AllowedValues: allowed, Required: row.Required,
		SortOrder: int(row.SortOrder), Status: model.Status(row.Status),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
