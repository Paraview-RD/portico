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
		"Each field can be mapped once per recipient. Two rules for one field would be settled by whichever was read first.")
	ErrDuplicateMappingTarget = httpx.BadRequest("DUPLICATE_MAPPING_TARGET",
		"Two fields are being sent under the same name. Only one of them would arrive, and which one is not something you can choose.")
	// ErrPayloadNameTaken guards a webhook rename landing on a key the event
	// already uses for something else. A mapping onto `id` would put a
	// department where a subscriber reads the account's identifier — the same
	// hazard as a claim onto `sub`, one protocol down.
	ErrPayloadNameTaken = httpx.BadRequest("PAYLOAD_NAME_TAKEN",
		"The event payload already uses that name for something else.")
	// ErrClaimNameTaken is the same guard one protocol over. OpenID Connect
	// does not reserve `tenant_id` or `role` — they are this project's own
	// claims — so nothing else would stop a department being sent as the
	// tenant, in a claim a relying party reads as the tenant.
	ErrClaimNameTaken = httpx.BadRequest("CLAIM_NAME_TAKEN",
		"This application already receives another field under that claim name.")
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

// RecipientKind is which of the four a path segment is addressing.
type RecipientKind string

// The four, which differ only in which table the id is looked up in.
const (
	RecipientOAuthClient  RecipientKind = "OAUTH_CLIENT"
	RecipientSAMLProvider RecipientKind = "SAML_SERVICE_PROVIDER"
	RecipientCASService   RecipientKind = "CAS_SERVICE"
	RecipientWebhook      RecipientKind = "WEBHOOK_SUBSCRIPTION"
)

// ErrRecipientNotFound is what naming one that is not there gets.
//
// Checked before writing rather than left to the foreign key. A mistyped id
// would otherwise surface as a constraint violation — a 500, describing a
// column, for what is an ordinary wrong-address mistake.
var ErrRecipientNotFound = httpx.NotFound("RECIPIENT_NOT_FOUND",
	"No such application or subscription.")

// Recipient resolves an addressed recipient, confirming it exists in this
// tenant. The reference it returns is what every other method here takes.
func (s *FieldMappingService) Recipient(ctx context.Context, tenantID string, kind RecipientKind, id string) (store.RecipientRef, error) {
	q := s.store.ForTenant(tenantID)

	switch kind {
	case RecipientOAuthClient:
		// Addressed by the client id an integration was given, not by the row
		// id, because that is what every other route under this prefix uses
		// and what an administrator has in front of them. The mapping stores
		// the row id, which is what the foreign key points at.
		client, err := q.GetOAuthClient(ctx, id)
		if err != nil {
			return store.RecipientRef{}, ErrRecipientNotFound
		}
		return store.RecipientRef{OAuthClientID: client.ID}, nil

	case RecipientSAMLProvider:
		provider, err := q.GetSAMLServiceProviderByID(ctx, id)
		if err != nil {
			return store.RecipientRef{}, ErrRecipientNotFound
		}
		return store.RecipientRef{SAMLSPID: provider.ID}, nil

	case RecipientCASService:
		casService, err := q.GetCASServiceByID(ctx, id)
		if err != nil {
			return store.RecipientRef{}, ErrRecipientNotFound
		}
		return store.RecipientRef{CASServiceID: casService.ID}, nil

	case RecipientWebhook:
		subscription, err := q.GetWebhookSubscription(ctx, id)
		if err != nil {
			return store.RecipientRef{}, ErrRecipientNotFound
		}
		return store.RecipientRef{WebhookSubscriptionID: subscription.ID}, nil

	default:
		return store.RecipientRef{}, ErrRecipientNotFound
	}
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
func (s *FieldMappingService) Mappings(ctx context.Context, tenantID string, ref store.RecipientRef) ([]model.FieldMapping, error) {
	rows, err := s.store.ForTenant(tenantID).ListFieldMappings(ctx, ref)
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
// Which names are refused depends on the recipient rather than on a flag the
// caller passes: OpenID Connect's registered claims mean nothing to a SAML
// service provider, where an attribute called `sub` is unremarkable.
func (s *FieldMappingService) Replace(ctx context.Context, actor auth.Principal, ref store.RecipientRef, inputs []FieldMappingInput) ([]model.FieldMapping, error) {
	tenantID := actor.TenantID

	normalized, err := s.normalize(ctx, tenantID, ref, inputs)
	if err != nil {
		return nil, err
	}

	now := store.Now()
	oauthID, samlID := optionalID(ref.OAuthClientID), optionalID(ref.SAMLSPID)
	casID, hookID := optionalID(ref.CASServiceID), optionalID(ref.WebhookSubscriptionID)

	// In one transaction, because a save is a replacement: a clear that
	// committed without its rewrite would leave a recipient receiving the
	// defaults, silently, until somebody pressed save again.
	//
	// The tenant is passed explicitly here rather than through the scoped
	// wrapper, which is what every transaction in this package does — the
	// wrapper binds a connection and a transaction is a different one. The
	// statements themselves still filter on it.
	err = s.store.WithTx(func(tx *sqlcgen.Queries) error {
		err := tx.DeleteFieldMappings(ctx, sqlcgen.DeleteFieldMappingsParams{
			TenantID: tenantID, OauthClientID: oauthID, SamlSpID: samlID,
			CasServiceID: casID, WebhookSubscriptionID: hookID,
		})
		if err != nil {
			return fmt.Errorf("clear field mappings: %w", err)
		}
		for _, in := range normalized {
			err := tx.CreateFieldMapping(ctx, sqlcgen.CreateFieldMappingParams{
				ID:            uuid.NewString(),
				TenantID:      tenantID,
				OauthClientID: oauthID, SamlSpID: samlID,
				CasServiceID: casID, WebhookSubscriptionID: hookID,
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
		TargetType: targetFieldMapping, TargetID: recipientRefID(ref),
		Detail: strings.Join(summary, "; "),
	})

	return s.Mappings(ctx, tenantID, ref)
}

func (s *FieldMappingService) normalize(ctx context.Context, tenantID string, ref store.RecipientRef, inputs []FieldMappingInput) ([]FieldMappingInput, error) {
	seen := map[string]bool{}
	// Targets as well as sources. Two facts sent under one name is the same
	// ambiguity as one fact sent twice, settled the same way — by whichever
	// was written last — and it is easier to do by accident.
	seenTarget := map[string]bool{}
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
		if seenTarget[in.TargetName] {
			return nil, ErrDuplicateMappingTarget
		}
		seenTarget[in.TargetName] = true

		if ref.OAuthClientID != "" {
			if reservedClaims[strings.ToLower(in.TargetName)] {
				return nil, ErrReservedClaimName
			}
			// And the claims this system sends of its own accord, which the
			// specification does not reserve and which are just as occupied.
			if owner, taken := oidcClaimOwners[in.TargetName]; taken && owner != in.SourceKey {
				return nil, ErrClaimNameTaken
			}
		}
		if ref.WebhookSubscriptionID != "" {
			if owners, taken := webhookTopLevelOwners[in.TargetName]; taken && !owners[in.SourceKey] {
				return nil, ErrPayloadNameTaken
			}
		}
		out = append(out, in)
	}
	return out, nil
}

// recipientRefID is whichever id the reference carries, for the audit trail.
func recipientRefID(ref store.RecipientRef) string {
	switch {
	case ref.OAuthClientID != "":
		return ref.OAuthClientID
	case ref.SAMLSPID != "":
		return ref.SAMLSPID
	case ref.WebhookSubscriptionID != "":
		return ref.WebhookSubscriptionID
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
func (s *FieldMappingService) OutboundFor(ctx context.Context, tenantID string, ref store.RecipientRef) (Outbound, error) {
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
