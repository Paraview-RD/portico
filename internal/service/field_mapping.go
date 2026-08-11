package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// FieldMappingService reads and writes what each application receives.
//
// The defaults stay in the three protocol packages and are not rows here. An
// empty set means "behave exactly as before", which is what makes this feature
// safe to deploy: the upgrade changes nothing until somebody decides something.
type FieldMappingService struct {
	store     *store.Store
	audit     *AuditService
	catalogue *FieldCatalogue
}

// NewFieldMappingService wires the service.
func NewFieldMappingService(st *store.Store, audit *AuditService, catalogue *FieldCatalogue) *FieldMappingService {
	return &FieldMappingService{store: st, audit: audit, catalogue: catalogue}
}

const targetFieldMapping = "FIELD_MAPPING"

// Errors this service returns.
var (
	ErrMappingTargetRequired = httpx.BadRequest("MAPPING_TARGET_REQUIRED",
		"A mapping needs the name the application expects, unless it is suppressing the field.")
	// ErrReservedClaimName is the one refusal here with teeth. OpenID Connect
	// gives these claims meanings the protocol itself depends on, and a
	// mapping onto `sub` would tell an application that somebody is somebody
	// else — in a token it has every reason to trust.
	ErrReservedClaimName = httpx.BadRequest("RESERVED_CLAIM_NAME",
		"That claim name is reserved by OpenID Connect and carries a meaning the protocol depends on.")
	ErrDuplicateMappingSource = httpx.BadRequest("DUPLICATE_MAPPING_SOURCE",
		"Each field can be mapped once per application. Two rules for one field would be settled by whichever was read first.")
)

// reservedClaims are the names an OpenID Connect mapping may not take.
//
// The list is the registered claims a relying party or the protocol itself acts
// on, rather than every name the specification mentions: renaming `department`
// to `nickname` is somebody's business, while renaming it to `sub` is an
// impersonation.
var reservedClaims = map[string]bool{
	"iss": true, "sub": true, "aud": true, "exp": true, "iat": true,
	"auth_time": true, "nonce": true, "acr": true, "amr": true, "azp": true,
	"at_hash": true, "c_hash": true, "jti": true, "scope": true,
	"client_id": true, "token_type": true, "active": true,
}

// FieldMappingInput is one rule as an administrator describes it.
type FieldMappingInput struct {
	SourceKey    string
	TargetName   string
	FriendlyName string
	// Suppressed removes a name the default would have sent. A flag rather than
	// an empty target, because "send nothing" and "send under a name I have not
	// chosen yet" are different intentions that one empty string cannot hold.
	Suppressed bool
}

// Mappings returns one application's rules.
func (s *FieldMappingService) Mappings(ctx context.Context, tenantID string, ref store.ApplicationRef) ([]model.FieldMapping, error) {
	rows, err := s.store.ForTenant(tenantID).ListApplicationFieldMappings(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list field mappings: %w", err)
	}

	out := make([]model.FieldMapping, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.FieldMapping{
			SourceKey: row.SourceKey, TargetName: row.TargetName,
			FriendlyName: row.FriendlyName, Suppressed: row.Suppressed,
		})
	}
	return out, nil
}

// Replace writes an application's whole set, replacing whatever was there.
//
// A save is a table somebody edited, so it replaces rather than merges: merging
// would leave the rows the form deleted still in place, which is the one
// outcome nobody expects from a save.
//
// oidc says whether this application speaks OpenID Connect, which decides
// whether the reserved-claim refusal applies. A SAML attribute called `sub` is
// unremarkable.
func (s *FieldMappingService) Replace(ctx context.Context, actor auth.Principal, ref store.ApplicationRef, oidc bool, inputs []FieldMappingInput) ([]model.FieldMapping, error) {
	tenantID := actor.TenantID

	normalized, err := s.normalize(ctx, tenantID, oidc, inputs)
	if err != nil {
		return nil, err
	}

	now := store.Now()
	oauthID, samlID, casID := optionalID(ref.OAuthClientID), optionalID(ref.SAMLSPID), optionalID(ref.CASServiceID)

	// In one transaction, because a save is a replacement: a clear that
	// committed without its rewrite would leave an application receiving the
	// defaults, silently, until somebody pressed save again.
	//
	// The tenant is passed explicitly here rather than through the scoped
	// wrapper, which is what every transaction in this package does — the
	// wrapper binds a connection and a transaction is a different one. The
	// statements themselves still filter on it.
	err = s.store.WithTx(func(tx *sqlcgen.Queries) error {
		err := tx.DeleteApplicationFieldMappings(ctx, sqlcgen.DeleteApplicationFieldMappingsParams{
			TenantID: tenantID, OauthClientID: oauthID, SamlSpID: samlID, CasServiceID: casID,
		})
		if err != nil {
			return fmt.Errorf("clear field mappings: %w", err)
		}
		for _, in := range normalized {
			err := tx.CreateApplicationFieldMapping(ctx, sqlcgen.CreateApplicationFieldMappingParams{
				ID:            uuid.NewString(),
				TenantID:      tenantID,
				OauthClientID: oauthID, SamlSpID: samlID, CasServiceID: casID,
				SourceKey:    in.SourceKey,
				TargetName:   in.TargetName,
				FriendlyName: in.FriendlyName,
				Suppressed:   in.Suppressed,
				CreatedAt:    now,
				UpdatedAt:    now,
			})
			if err != nil {
				return fmt.Errorf("write mapping for %s: %w", in.SourceKey, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// The rules rather than any value: this entry records a configuration
	// change, and the values it will carry belong to whoever signs in later.
	summary := make([]string, 0, len(normalized))
	for _, in := range normalized {
		if in.Suppressed {
			summary = append(summary, in.SourceKey+" → (suppressed)")
			continue
		}
		summary = append(summary, in.SourceKey+" → "+in.TargetName)
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionFieldMappingReplace,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetFieldMapping, TargetID: applicationRefID(ref),
		Detail: strings.Join(summary, "; "),
	})

	return s.Mappings(ctx, tenantID, ref)
}

func (s *FieldMappingService) normalize(ctx context.Context, tenantID string, oidc bool, inputs []FieldMappingInput) ([]FieldMappingInput, error) {
	seen := map[string]bool{}
	out := make([]FieldMappingInput, 0, len(inputs))

	for _, in := range inputs {
		in.SourceKey = strings.TrimSpace(in.SourceKey)
		in.TargetName = strings.TrimSpace(in.TargetName)
		in.FriendlyName = strings.TrimSpace(in.FriendlyName)

		field, err := s.catalogue.Field(ctx, tenantID, in.SourceKey)
		if err != nil {
			return nil, err
		}
		if !field.Allows(DirectionOutbound) {
			return nil, ErrUnknownField
		}
		if seen[in.SourceKey] {
			return nil, ErrDuplicateMappingSource
		}
		seen[in.SourceKey] = true

		if in.Suppressed {
			// A suppression names no target, and one supplied alongside would
			// be a rule that says two things.
			in.TargetName, in.FriendlyName = "", ""
			out = append(out, in)
			continue
		}
		if in.TargetName == "" {
			return nil, ErrMappingTargetRequired
		}
		if oidc && reservedClaims[strings.ToLower(in.TargetName)] {
			return nil, ErrReservedClaimName
		}
		out = append(out, in)
	}
	return out, nil
}

// applicationRefID is whichever id the reference carries, for the audit trail.
func applicationRefID(ref store.ApplicationRef) string {
	switch {
	case ref.OAuthClientID != "":
		return ref.OAuthClientID
	case ref.SAMLSPID != "":
		return ref.SAMLSPID
	default:
		return ref.CASServiceID
	}
}

// Outbound is what a protocol package asks for: the rules, indexed by source
// key, ready to apply over the defaults.
//
// Returned as a type rather than a map so that the two operations a caller
// needs — "has this default been renamed or suppressed" and "what else should I
// add" — are named rather than open-coded at three call sites that would drift.
type Outbound struct {
	byKey map[string]model.FieldMapping
}

// OutboundFor reads an application's rules.
//
// An application with none gets an empty set, and every method below then leaves
// the defaults exactly as they were. That is the property the whole feature
// rests on: an upgrade changes nothing until somebody decides something.
func (s *FieldMappingService) OutboundFor(ctx context.Context, tenantID string, ref store.ApplicationRef) (Outbound, error) {
	mappings, err := s.Mappings(ctx, tenantID, ref)
	if err != nil {
		return Outbound{}, err
	}

	byKey := make(map[string]model.FieldMapping, len(mappings))
	for _, m := range mappings {
		byKey[m.SourceKey] = m
	}
	return Outbound{byKey: byKey}, nil
}

// Empty reports whether anything was configured, so a caller can take the
// default path without a lookup per field.
func (o Outbound) Empty() bool { return len(o.byKey) == 0 }

// NameFor answers what a default should be called, and whether to send it at
// all. It returns the name unchanged when no rule names this field.
func (o Outbound) NameFor(sourceKey, defaultName string) (name string, send bool) {
	rule, configured := o.byKey[sourceKey]
	switch {
	case !configured:
		return defaultName, true
	case rule.Suppressed:
		return "", false
	default:
		return rule.TargetName, true
	}
}

// Additions are the rules for fields the defaults do not send at all — which is
// most of the catalogue, and the larger half of what this feature is for.
func (o Outbound) Additions(defaults map[string]bool) []model.FieldMapping {
	added := make([]model.FieldMapping, 0, len(o.byKey))
	for key, rule := range o.byKey {
		if rule.Suppressed || defaults[key] {
			continue
		}
		added = append(added, rule)
	}
	return added
}
