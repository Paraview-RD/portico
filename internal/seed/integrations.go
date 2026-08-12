package seed

import (
	"context"
	"fmt"

	"github.com/Paraview-RD/portico/internal/directory"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
	"github.com/Paraview-RD/portico/internal/store"
)

// The three integrations, which between them are how accounts and events
// cross the boundary of this system: a directory pushes in over SCIM, Portico
// pulls out of an LDAP directory, and webhooks push events onward.
//
// Each is seeded in more than one state, because the states are what an
// operator has to be able to tell apart — a subscription mid-rotation from a
// settled one, a directory on a schedule from one that waits to be asked.

// seedIntegrations issues SCIM credentials, registers directories, and
// subscribes webhooks. The history that goes with them — deliveries,
// synchronization runs — is written by seedHistory, which is the stage that
// can date them.
func (s *Seeder) seedIntegrations(ctx context.Context, w *world) error {
	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}

	if err := s.seedSCIMCredentials(ctx, t); err != nil {
		return err
	}
	if err := s.seedDirectories(ctx, w, t); err != nil {
		return err
	}
	if err := s.seedSubscriptions(ctx, w, t); err != nil {
		return err
	}
	return s.seedFieldMappings(ctx, w, t)
}

// Two credentials, one of them revoked.
//
// A revoked credential is not deleted, and the reason is the same reason a
// disabled account is not deleted: the audit trail points at it. Somebody
// looking at "who provisioned this account" needs the answer to still exist.
func (s *Seeder) seedSCIMCredentials(ctx context.Context, t *seededTenant) error {
	for _, cred := range []struct {
		name    string
		revoked bool
	}{
		{name: "Okta 生产环境"},
		{name: "Entra ID（试点，已停用）", revoked: true},
	} {
		issued, err := s.scim.Create(ctx, t.actor, cred.name)
		if err != nil {
			return fmt.Errorf("issue SCIM credential %s: %w", cred.name, err)
		}
		if cred.revoked {
			if err := s.scim.SetStatus(ctx, t.actor, issued.ID, model.StatusDisabled); err != nil {
				return fmt.Errorf("revoke SCIM credential %s: %w", cred.name, err)
			}
		}
	}
	return nil
}

// LDAPHost and LDAPPort point at the directory in deploy/dev-stack.
//
// Deliberately the real one rather than an invented address. That container
// already ships a seeded tree (deploy/dev-stack/ldap/seed.ldif), so a
// developer who has the dev stack up can press Synchronize and watch accounts
// actually arrive — and a failing run against a host that is not there is
// itself one of the states this seed wants on screen.
const (
	LDAPHost = "127.0.0.1"
	LDAPPort = 3890
)

func (s *Seeder) seedDirectories(ctx context.Context, w *world, t *seededTenant) error {
	bindPassword := "portico-dev"

	for _, d := range []struct {
		name     string
		host     string
		port     int
		interval int
		baseDN   string
	}{
		{
			// The one that works, and the one on a schedule. Every six hours,
			// which is the interval a real deployment picks and is far enough
			// from the fifteen-minute floor to show that the field is a choice
			// rather than a switch.
			name: "开发目录（自动同步）", host: LDAPHost, port: LDAPPort,
			interval: 360, baseDN: "ou=people,dc=example,dc=org",
		},
		{
			// The one that does not, held at manual. Its failed runs are what
			// the run history is for, and a seed where every integration works
			// leaves that screen showing nothing worth reading.
			name: "分公司目录（连不上）", host: "ldap.acme.invalid", port: 389,
			interval: 0, baseDN: "ou=staff,dc=acme,dc=invalid",
		},
	} {
		in := service.LDAPSourceInput{
			Name: d.name, Host: d.host, Port: d.port,
			Encryption:      directory.EncryptionNone,
			BindDN:          "cn=admin,dc=example,dc=org",
			BaseDN:          d.baseDN,
			UserFilter:      "(objectClass=inetOrgPerson)",
			AttrUsername:    "uid",
			AttrDisplayName: "displayName",
			AttrEmail:       "mail",
			AttrPhone:       "telephoneNumber",
			AttrExternalID:  "entryUUID",

			SyncIntervalMinutes: d.interval,
		}
		// Only when this deployment can store one. Without
		// PORTICO_ENCRYPTION_KEY the service refuses — correctly — and the
		// seed would rather register the directory with an anonymous bind
		// than not register it at all.
		if s.canSealSecrets() {
			in.BindPassword = &bindPassword
		} else {
			in.BindDN = ""
		}
		if org, ok := t.orgs["external"]; ok {
			in.OrganizationID = org.ID
		}

		source, err := s.dirs.Register(ctx, t.actor, in)
		if err != nil {
			return fmt.Errorf("register directory %s: %w", d.name, err)
		}
		t.directories = append(t.directories, source)
		w.summary.Directories++
	}
	return nil
}

// Two subscriptions, one of them carrying headers of its own.
//
// The destinations are literal addresses out of RFC 5737's documentation range
// rather than hostnames. Portico refuses a destination it cannot resolve — that
// is the guard against a subscription pointed somewhere internal — and a
// seeder that needed working DNS would fail in a sandbox, on a train, and in
// CI. A literal address skips resolution entirely and is never anybody's
// server.
//
// The rotation window is applied by seedHistory, which is the stage that can
// place it in time: a subscription mid-rotation is one whose old key has not
// yet expired, and that is a timestamp rather than a flag.
func (s *Seeder) seedSubscriptions(ctx context.Context, w *world, t *seededTenant) error {
	for _, sub := range []struct {
		name    string
		url     string
		events  []string
		headers map[string]string
	}{
		{
			name: "HR 系统", url: "https://203.0.113.10/hooks/portico",
			events: []string{
				"user.created", "user.updated", "user.disabled", "user.enabled",
				"organization.created", "organization.updated",
			},
		},
		{
			// Behind a gateway that wants an Authorization of its own. The
			// signature says who produced the body; it cannot answer whether
			// the request may come in at all.
			name: "审计归档（网关后）", url: "https://203.0.113.20/portico/events",
			events:  []string{"user.disabled", "group.members_changed", "user.locked"},
			headers: map[string]string{"Authorization": "Bearer gateway-demo-token"},
		},
	} {
		if len(sub.headers) > 0 && !s.canSealSecrets() {
			// Header values are credentials and are sealed. Without a key the
			// service refuses, so the subscription is created without them
			// rather than skipped.
			sub.headers = nil
		}

		created, err := s.webhooks.Create(ctx, t.actor, service.SubscriptionInput{
			Name: sub.name, URL: sub.url, Events: sub.events, Headers: sub.headers,
		})
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", sub.name, err)
		}
		t.subscriptions = append(t.subscriptions, created.Subscription)
		w.summary.Subscriptions++
	}
	return nil
}

// What each recipient receives, and under what name.
//
// The point of seeding these is that an empty mapping table and a feature
// nobody built look identical from the console. So each of the four kinds gets
// one, and between them they show the three things a rule can do — rename,
// suppress, add — rather than three variations of rename.
//
// The additions deliberately reach the tenant's own attributes. `badge_number`
// is defined by this tenant, filled in for three people, and sent to the door
// system: that is the whole chain the feature exists for, and it is not
// visible from any one screen.
func (s *Seeder) seedFieldMappings(ctx context.Context, w *world, t *seededTenant) error {
	type recipient struct {
		what  string
		ref   store.RecipientRef
		rules []service.FieldMappingInput
	}

	var plan []recipient

	if len(t.clients) > 0 {
		plan = append(plan, recipient{
			what: "OAuth 客户端 " + t.clients[0].Name,
			ref:  store.RecipientRef{OAuthClientID: t.clients[0].ID},
			rules: []service.FieldMappingInput{
				// The expense system reads `dept`, and always has.
				{SourceKey: "department", TargetName: "dept"},
				{SourceKey: "organization_path", TargetName: "org_path"},
				// A phone number this application has no use for. Suppression
				// is how a disclosure review ends.
				{SourceKey: "phone", Suppressed: true},
			},
		})
	}

	if len(t.sps) > 0 {
		plan = append(plan, recipient{
			what: "SAML 服务提供方 " + t.sps[0].Name,
			ref:  store.RecipientRef{SAMLSPID: t.sps[0].ID},
			rules: []service.FieldMappingInput{
				// SAML maps on the Name; the friendly name is for whoever is
				// reading the assertion in a debugger.
				{SourceKey: "employee_number", TargetName: "urn:oid:2.16.840.1.113730.3.1.3",
					FriendlyName: "employeeNumber"},
				{SourceKey: "organization_code", TargetName: "departmentCode"},
			},
		})
	}

	if len(t.casSvcs) > 0 {
		plan = append(plan, recipient{
			what: "CAS 服务 " + t.casSvcs[0].Name,
			ref:  store.RecipientRef{CASServiceID: t.casSvcs[0].ID},
			rules: []service.FieldMappingInput{
				{SourceKey: "badge_number", TargetName: "cardNo"},
				{SourceKey: "organization_code", TargetName: "orgCode"},
			},
		})
	}

	// Both subscriptions, not just the first. A reader opening the one that
	// happens to sort first and finding nothing configured cannot tell that
	// from a screen that was never wired up — which is the thing this seed
	// exists to prevent.
	subscriptionRules := [][]service.FieldMappingInput{
		{
			// Lifted out of `profile` to the top of `data`, which is where
			// this receiver's parser looks.
			{SourceKey: "department", TargetName: "dept"},
			{SourceKey: "work_mode", TargetName: "workMode"},
			{SourceKey: "phone", Suppressed: true},
		},
		{
			// An archive that keeps things for years, so it is sent as
			// little as will do: who, where, and nothing to identify them
			// by outside this system. This is what a disclosure review
			// produces, and suppression is how it is written down.
			{SourceKey: "email", Suppressed: true},
			{SourceKey: "phone", Suppressed: true},
			{SourceKey: "organization_path", TargetName: "orgPath"},
		},
	}
	for i, sub := range t.subscriptions {
		if i >= len(subscriptionRules) {
			break
		}
		plan = append(plan, recipient{
			what:  "订阅 " + sub.Name,
			ref:   store.RecipientRef{WebhookSubscriptionID: sub.ID},
			rules: subscriptionRules[i],
		})
	}

	for _, r := range plan {
		if _, err := s.mappings.Replace(ctx, t.actor, r.ref, r.rules); err != nil {
			return fmt.Errorf("map fields for %s: %w", r.what, err)
		}
		w.summary.FieldMappings += len(r.rules)
	}
	return nil
}
