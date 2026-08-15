// Package demo fills one already-created tenant with data that looks like a
// company of that kind has been using it.
//
// It exists for self-service trials. Somebody confirms an email address, a
// tenant and an administrator are created for them, and what they find on the
// first screen decides whether they look at the second one. An empty tenant
// answers none of the questions they came with — what does an organization
// tree look like, where do custom attributes appear, how is an application
// registered — so it gets filled before they are let in.
//
// # Why this is not internal/seed
//
// The two do different jobs and the difference is not size.
//
//   - internal/seed builds a whole deployment: two tenants, so that isolation
//     can be seen; fifty-five accounts, so that paging can be seen; ninety days
//     of audit trail, sessions and webhook deliveries written directly with
//     chosen timestamps. It is reached only from cmd/seed, a binary that is not
//     in the release image.
//
//   - This package fills a tenant that already exists, through the service
//     layer and nothing else, inside an HTTP request somebody is waiting on. It
//     is linked into the server, which is exactly why it must not pull in
//     internal/seed — that would put the history writer, which bypasses the
//     service layer by design, into the release binary. The declaration at the
//     top of internal/seed stays true because this package does not import it.
//
// Nothing is shared between them beyond the service layer both call. That
// duplicates an organization-tree walk and a create-account loop, which is
// cheaper than making one engine serve two jobs that disagree about what a
// tenant is for.
//
// # What a pack does not contain
//
// No audit history, no LDAP directory, no webhook, no identity provider. The
// first would have to be written directly, which is the thing this package
// exists not to do; the others need an external system to point at or an
// encryption key to seal a credential with, and a trial has neither. A visitor
// who wants those runs Portico themselves, which the trial is there to make
// them want.
package demo

import (
	"context"
	"fmt"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// Filler creates the contents of a pack inside a tenant.
//
// Every service it holds is one the console itself calls. That is the whole
// design: a pack can only contain something an operator could have created by
// hand, so a trial tenant cannot end up in a state the product does not
// produce.
type Filler struct {
	orgs    *service.OrganizationService
	groups  *service.GroupService
	users   *service.UserService
	attrs   *service.UserAttributeService
	clients *service.OAuthClientService
	sps     *service.SAMLServiceProviderService
	cas     *service.CASService
}

// NewFiller wires a Filler from the services the server already builds.
func NewFiller(
	orgs *service.OrganizationService,
	groups *service.GroupService,
	users *service.UserService,
	attrs *service.UserAttributeService,
	clients *service.OAuthClientService,
	sps *service.SAMLServiceProviderService,
	cas *service.CASService,
) *Filler {
	return &Filler{
		orgs: orgs, groups: groups, users: users,
		attrs: attrs, clients: clients, sps: sps, cas: cas,
	}
}

// Industries returns the pack keys on offer, in the order the form should show
// them.
//
// Read rather than declared twice: the sign-in screen, the validator that
// rejects an unknown industry and this list would otherwise be three places to
// keep in agreement, and the first symptom of them disagreeing is a visitor
// choosing a pack that turns out not to exist.
func (f *Filler) Industries() []string {
	names := make([]string, 0, len(packs))
	for _, p := range packs {
		names = append(names, p.Key)
	}
	return names
}

// Fill creates a pack's contents inside a tenant.
//
// Ordered by dependency rather than by importance: organizations before the
// accounts that live in them, accounts before the groups that list them and
// the attributes that describe them. A stage that fails stops the run, because
// a later stage would only fail again with a worse message.
func (f *Filler) Fill(ctx context.Context, in service.TenantFill) error {
	pack := packByKey(in.Industry)
	if pack == nil {
		return fmt.Errorf("no such industry pack: %q", in.Industry)
	}
	if in.Password == "" {
		return fmt.Errorf("filling tenant %s: no password for the demonstration accounts", in.TenantID)
	}

	w := &filling{
		tenantID: in.TenantID,
		actor:    in.Actor,
		password: in.Password,
		orgs:     map[string]model.Organization{},
		users:    map[string]model.User{},
	}

	for _, stage := range []struct {
		name string
		run  func(context.Context, *filling, *Pack) error
	}{
		{"organizations", f.fillOrganizations},
		{"accounts", f.fillPeople},
		{"groups", f.fillGroups},
		{"attributes", f.fillAttributes},
		{"applications", f.fillApplications},
		{"organization states", f.closeOffOrganizations},
	} {
		if err := stage.run(ctx, w, pack); err != nil {
			return fmt.Errorf("fill %s for %s: %w", stage.name, pack.Key, err)
		}
	}
	return nil
}

// filling is what one stage hands the next.
type filling struct {
	tenantID string
	actor    auth.Principal
	password string

	orgs  map[string]model.Organization
	users map[string]model.User
}

func (f *Filler) fillOrganizations(ctx context.Context, w *filling, pack *Pack) error {
	for _, o := range pack.Orgs {
		in := service.OrganizationInput{Name: o.Name, Code: o.Code, Remark: o.Remark}
		if o.Parent != "" {
			parent, ok := w.orgs[o.Parent]
			if !ok {
				return fmt.Errorf("organization %s names parent %s, which comes later in the pack",
					o.Code, o.Parent)
			}
			in.ParentID = parent.ID
		}
		// Always created active. The disabled one is switched off after its
		// people are in it — see closeOffOrganizations.
		org, err := f.orgs.Create(ctx, w.actor, in)
		if err != nil {
			return fmt.Errorf("create organization %s: %w", o.Code, err)
		}
		w.orgs[o.Key] = org
	}
	return nil
}

func (f *Filler) fillPeople(ctx context.Context, w *filling, pack *Pack) error {
	for _, p := range pack.People {
		in := service.CreateUserInput{
			Username:    p.Username,
			DisplayName: p.DisplayName,
			Password:    w.password,
			// Every account in a pack is an ordinary user, deliberately.
			//
			// The tenant already has exactly one administrator, and its
			// password went to one mailbox. A pack that shipped a second
			// administrator would hand that power to whoever guesses the tenant
			// code, since these accounts share one password by design.
			Role:   model.RoleUser,
			Source: p.Source,
		}
		if !p.NoContact {
			in.Email, in.Phone = p.Email, p.Phone
		}
		if org, ok := w.orgs[p.Org]; ok {
			in.OrganizationID = org.ID
		}

		user, err := f.users.Create(ctx, w.tenantID, in)
		if err != nil {
			return fmt.Errorf("create account %s: %w", p.Username, err)
		}
		if p.Disabled {
			if user, err = f.users.SetStatus(ctx, w.actor, user.ID, model.StatusDisabled); err != nil {
				return fmt.Errorf("disable account %s: %w", p.Username, err)
			}
		}
		w.users[p.Username] = user
	}
	return nil
}

func (f *Filler) fillGroups(ctx context.Context, w *filling, pack *Pack) error {
	for _, g := range pack.Groups {
		group, err := f.groups.Create(ctx, w.tenantID, service.GroupInput{
			DisplayName: g.Name, Description: g.Description, ExternalID: g.ExternalID,
		}, g.Source, w.actor)
		if err != nil {
			return fmt.Errorf("create group %s: %w", g.Name, err)
		}

		ids := make([]string, 0, len(g.Members))
		for _, username := range g.Members {
			if user, ok := w.users[username]; ok {
				ids = append(ids, user.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		if err := f.groups.AddMembers(ctx, w.tenantID, group.ID, ids, w.actor); err != nil {
			return fmt.Errorf("add members to group %s: %w", g.Name, err)
		}
	}
	return nil
}

func (f *Filler) fillAttributes(ctx context.Context, w *filling, pack *Pack) error {
	for _, a := range pack.Attributes {
		if _, err := f.attrs.Define(ctx, w.actor, service.UserAttributeInput{
			Key: a.Key, Label: a.Label, Description: a.Description,
			Kind: a.Kind, AllowedValues: a.Allowed,
		}); err != nil {
			return fmt.Errorf("define attribute %s: %w", a.Key, err)
		}

		// Filled on some accounts and not others, on purpose. "Who has not
		// filled this in yet" is the question an operator asks before mapping
		// or retiring an attribute, and a pack where everybody has a value
		// cannot be asked it.
		for username, value := range a.FilledFor {
			user, ok := w.users[username]
			if !ok {
				continue
			}
			if err := f.attrs.SetValues(ctx, w.actor, user.ID,
				map[string]string{a.Key: value}); err != nil {
				return fmt.Errorf("set %s on %s: %w", a.Key, username, err)
			}
		}
	}
	return nil
}

func (f *Filler) fillApplications(ctx context.Context, w *filling, pack *Pack) error {
	for _, c := range pack.OAuth {
		registered, err := f.clients.Register(ctx, w.actor, service.RegisterClientInput{
			ClientID: c.ClientID, Name: c.Name, Public: c.Public,
			ApplicationType: c.Type, RedirectURIs: c.Redirect,
			Scopes: c.Scopes, LaunchURL: c.Launch,
		})
		if err != nil {
			return fmt.Errorf("register client %s: %w", c.ClientID, err)
		}
		if c.Disabled {
			if _, err := f.clients.SetStatus(ctx, w.actor, registered.Client.ClientID,
				model.StatusDisabled); err != nil {
				return fmt.Errorf("disable client %s: %w", c.ClientID, err)
			}
		}
	}

	for _, sp := range pack.SAML {
		if _, err := f.sps.Register(ctx, w.actor, service.RegisterSPInput{
			MetadataXML: spMetadata(sp.EntityID, sp.Host),
			Name:        sp.Name,
			LaunchURL:   sp.Launch,
		}); err != nil {
			return fmt.Errorf("register service provider %s: %w", sp.Name, err)
		}
	}

	for _, c := range pack.CAS {
		svc, err := f.cas.Register(ctx, w.actor, service.RegisterCASInput{
			Name: c.Name, URLPrefix: c.Prefix, LaunchURL: c.Launch,
		})
		if err != nil {
			return fmt.Errorf("register CAS service %s: %w", c.Name, err)
		}
		if c.Disabled {
			if _, err := f.cas.SetStatus(ctx, w.actor, svc.URLPrefix,
				model.StatusDisabled); err != nil {
				return fmt.Errorf("disable CAS service %s: %w", c.Name, err)
			}
		}
	}
	return nil
}

// closeOffOrganizations applies the states an organization cannot be created
// in.
//
// Last, and for the same reason internal/seed does it last: a disabled
// organization refuses new members, so one disabled before its people arrived
// would be empty — which teaches the opposite of what it is here to show.
// Disabling keeps the people already in it and only stops new assignments.
func (f *Filler) closeOffOrganizations(ctx context.Context, w *filling, pack *Pack) error {
	if pack.Manager.Org != "" {
		org, hasOrg := w.orgs[pack.Manager.Org]
		person, hasPerson := w.users[pack.Manager.Username]
		if hasOrg && hasPerson {
			if _, err := f.orgs.SetManager(ctx, w.actor, org.ID, person.ID); err != nil {
				return fmt.Errorf("set manager of %s: %w", org.Code, err)
			}
		}
	}

	// An organization administrator, which is a different fact from being its
	// manager and is recorded separately. One of each scope would be better
	// still; one of them is what a pack this size has room for, and the console
	// explains the other.
	if pack.OrgAdmin.Org != "" {
		org, hasOrg := w.orgs[pack.OrgAdmin.Org]
		person, hasPerson := w.users[pack.OrgAdmin.Username]
		if hasOrg && hasPerson {
			if err := f.orgs.AssignAdministrator(ctx, w.actor, org.ID, person.ID,
				pack.OrgAdmin.Scope); err != nil {
				return fmt.Errorf("record %s as an administrator of %s: %w",
					pack.OrgAdmin.Username, org.Code, err)
			}
		}
	}

	for _, o := range pack.Orgs {
		if !o.Disabled {
			continue
		}
		org, ok := w.orgs[o.Key]
		if !ok {
			continue
		}
		if _, err := f.orgs.SetStatus(ctx, w.actor, org.ID, model.StatusDisabled); err != nil {
			return fmt.Errorf("disable organization %s: %w", o.Code, err)
		}
	}
	return nil
}

// spMetadata builds the document a SAML service provider would have published.
//
// Generated rather than pasted per pack: what differs between them is the
// entity identifier and the host, and four near-identical XML literals would
// hide that they are near-identical. No certificate, which is legal metadata —
// a service provider that does not sign its requests publishes none, and that
// is the simpler of the two cases to register.
func spMetadata(entityID, host string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata"
                  entityID=%q>
  <SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="true"
                   protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</NameIDFormat>
    <AssertionConsumerService index="0" isDefault="true"
      Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
      Location="https://%s/saml/acs"/>
    <SingleLogoutService
      Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
      Location="https://%s/saml/slo"/>
  </SPSSODescriptor>
</EntityDescriptor>`, entityID, host, host)
}
