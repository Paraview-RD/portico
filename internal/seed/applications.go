package seed

import (
	"context"
	"fmt"

	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// Applications, in all three protocols.
//
// One of each would be enough to fill the screen and not enough to be useful.
// What an operator has to be able to tell apart is a confidential client from
// a public one, an application that appears in the portal from one that only
// signs people in, and a registration that is disabled from one that is gone —
// so there is a pair for each of those distinctions rather than an example of
// each protocol.

var oauthClients = []struct {
	clientID string
	name     string
	public   bool
	appType  string
	redirect []string
	logout   []string
	scopes   []string
	launch   string
	disabled bool
}{
	{
		clientID: "wiki", name: "内部 Wiki",
		appType:  "web",
		redirect: []string{"https://wiki.example.org/oauth2/callback"},
		logout:   []string{"https://wiki.example.org/signed-out"},
		scopes:   []string{"openid", "profile", "email"},
		launch:   "https://wiki.example.org/",
	},
	{
		// Public, so the console shows an application with no secret and PKCE
		// alone. Somebody who has only registered confidential clients has
		// not seen that page.
		clientID: "mobile-app", name: "员工 App",
		public:   true,
		appType:  "native",
		redirect: []string{"com.example.portico://callback", "http://127.0.0.1:8765/callback"},
		scopes:   []string{"openid", "profile", "offline_access"},
	},
	{
		// No launch URL: it signs people in and does not appear in the portal
		// as something to open. That absence is a supported state and looks
		// like a bug until you know it.
		clientID: "grafana", name: "Grafana",
		appType:  "web",
		redirect: []string{"https://grafana.example.org/login/generic_oauth"},
		scopes:   []string{"openid", "profile", "email", "groups"},
	},
	{
		clientID: "legacy-crm", name: "旧版 CRM（已停用）",
		appType:  "web",
		redirect: []string{"https://crm.example.org/callback"},
		scopes:   []string{"openid", "profile"},
		launch:   "https://crm.example.org/",
		disabled: true,
	},
}

func (s *Seeder) seedApplications(ctx context.Context, w *world) error {
	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}

	for _, c := range oauthClients {
		registered, err := s.clients.Register(ctx, t.actor, service.RegisterClientInput{
			ClientID: c.clientID, Name: c.name, Public: c.public,
			ApplicationType: c.appType, RedirectURIs: c.redirect,
			PostLogoutRedirectURIs: c.logout, Scopes: c.scopes, LaunchURL: c.launch,
		})
		if err != nil {
			return fmt.Errorf("register client %s: %w", c.clientID, err)
		}
		client := registered.Client
		if c.disabled {
			if client, err = s.clients.SetStatus(ctx, t.actor, c.clientID, model.StatusDisabled); err != nil {
				return fmt.Errorf("disable client %s: %w", c.clientID, err)
			}
		}
		t.clients = append(t.clients, client)
		w.summary.Applications++
	}

	if err := s.seedServiceProviders(ctx, w, t); err != nil {
		return err
	}
	return s.seedCASServices(ctx, w, t)
}

// A SAML service provider, registered the way the console registers one:
// by pasting the metadata document rather than by filling in fields.
//
// The certificate is a real, self-signed one generated for this fixture and
// expired long ago. Expired is fine and deliberate — Portico reads the key out
// of the metadata to verify signatures and does not check validity dates, so
// an expired fixture certificate cannot rot into a failing seed, while a
// valid one would eventually.
const demoSPMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata"
                  entityID="https://sp.example.org/saml/metadata">
  <SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="true"
                   protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</NameIDFormat>
    <AssertionConsumerService index="0" isDefault="true"
      Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
      Location="https://sp.example.org/saml/acs"/>
    <SingleLogoutService
      Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
      Location="https://sp.example.org/saml/slo"/>
  </SPSSODescriptor>
</EntityDescriptor>`

func (s *Seeder) seedServiceProviders(ctx context.Context, w *world, t *seededTenant) error {
	sp, err := s.sps.Register(ctx, t.actor, service.RegisterSPInput{
		MetadataXML: demoSPMetadata,
		Name:        "报销系统（SAML）",
		LaunchURL:   "https://sp.example.org/",
	})
	if err != nil {
		return fmt.Errorf("register service provider: %w", err)
	}
	t.sps = append(t.sps, sp)
	w.summary.Applications++
	return nil
}

func (s *Seeder) seedCASServices(ctx context.Context, w *world, t *seededTenant) error {
	for _, c := range []struct {
		name, prefix, launch string
		disabled             bool
	}{
		{name: "教务系统（CAS）", prefix: "https://jw.example.org/", launch: "https://jw.example.org/"},
		{name: "图书馆（CAS，已停用）", prefix: "https://lib.example.org/", disabled: true},
	} {
		svc, err := s.cas.Register(ctx, t.actor, service.RegisterCASInput{
			Name: c.name, URLPrefix: c.prefix, LaunchURL: c.launch,
		})
		if err != nil {
			return fmt.Errorf("register CAS service %s: %w", c.name, err)
		}
		if c.disabled {
			if svc, err = s.cas.SetStatus(ctx, t.actor, svc.URLPrefix, model.StatusDisabled); err != nil {
				return fmt.Errorf("disable CAS service %s: %w", c.name, err)
			}
		}
		t.casSvcs = append(t.casSvcs, svc)
		w.summary.Applications++
	}
	return nil
}
