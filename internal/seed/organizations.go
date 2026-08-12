package seed

import (
	"context"
	"fmt"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// Two tenants, because isolation is the property that cannot be seen with
// one.
//
// The second holds an organization with the same code and an account with the
// same username as the first, on purpose: that is the pair somebody needs in
// front of them to believe the boundary holds, and it is the pair a
// cross-tenant leak would show up in first.
const (
	// TenantMain is the default tenant every deployment already has.
	TenantMain = "default"
	// TenantSecond is the one the seed adds.
	TenantSecond = "acme"
)

// seedTenants ensures both tenants exist and gives them deliberately
// different settings.
func (s *Seeder) seedTenants(ctx context.Context, w *world) error {
	main, err := s.tenants.EnsureDefault(ctx)
	if err != nil {
		return fmt.Errorf("ensure default tenant: %w", err)
	}

	second, err := s.tenants.Create(ctx, TenantSecond, "Acme 分公司")
	if err != nil {
		// Already there from an earlier run. Seeding is meant to be
		// repeatable, and a tenant that exists is the outcome either way.
		existing, lookupErr := s.tenants.Resolve(ctx, TenantSecond)
		if lookupErr != nil {
			return fmt.Errorf("create tenant %s: %w", TenantSecond, err)
		}
		second = existing
	}

	for _, t := range []model.Tenant{main, second} {
		w.tenants = append(w.tenants, seededTenant{
			tenant: t,
			orgs:   map[string]model.Organization{},
		})
	}
	w.summary.Tenants = len(w.tenants)

	return s.applySettings(ctx, w)
}

// applySettings gives the two tenants different policies.
//
// Different on purpose. Settings are per-tenant and almost every one of them
// changes what a screen does — a lockout threshold of zero hides the unlock
// button, a password age of zero removes the expiry column from the portal —
// so a seed where both tenants agree exercises one branch of each and leaves
// the other unvisited.
func (s *Seeder) applySettings(ctx context.Context, w *world) error {
	main := w.tenantByCode(TenantMain)
	second := w.tenantByCode(TenantSecond)

	// The strict one. Everything on, so the console shows the fields that
	// only appear when a policy is in force, and password expiry is close
	// enough that some accounts are actually near it.
	strict := s.settings.Defaults()
	strict.SystemName = "Portico 演示环境"
	strict.DefaultLocale = "zh-CN"
	strict.RegistrationEnabled = true
	strict.ShowGuides = true
	strict.LockoutThreshold = 5
	strict.LockoutDurationMinutes = 30
	// Length left at the engine's floor of eight, deliberately: the demo
	// password is one memorable string for every account, and a tenant
	// minimum above it would make this tenant the one nobody can sign in to.
	// What makes this tenant strict is the four settings below plus lockout.
	strict.PasswordRequireUppercase = true
	strict.PasswordRequireLowercase = true
	strict.PasswordRequireDigit = true
	strict.PasswordHistoryDepth = 3
	strict.PasswordMaxAgeDays = 90
	strict.AuditRetentionDays = 0 // keep everything; the trail is the record
	strict.OIDCAccessTokenTTLMinutes = 15
	strict.OIDCRefreshTokenTTLDays = 30
	strict.OIDCSessionMaxAgeDays = 90

	// And the permissive one, which is what most deployments start as.
	relaxed := s.settings.Defaults()
	relaxed.SystemName = "Acme"
	relaxed.DefaultLocale = "en-US"
	relaxed.RegistrationEnabled = false
	relaxed.ShowGuides = false
	relaxed.LockoutThreshold = 0
	relaxed.PasswordMaxAgeDays = 0
	relaxed.AuditRetentionDays = 180
	relaxed.OIDCAccessTokenTTLMinutes = 60
	relaxed.OIDCRefreshTokenTTLDays = 7
	relaxed.OIDCSessionMaxAgeDays = 0

	for _, pair := range []struct {
		tenant *seededTenant
		next   service.Settings
	}{{main, strict}, {second, relaxed}} {
		if pair.tenant == nil {
			continue
		}
		if _, err := s.settings.Update(ctx, pair.tenant.tenant.ID, pair.next); err != nil {
			return fmt.Errorf("settings for tenant %s: %w", pair.tenant.tenant.Code, err)
		}
	}
	return nil
}

// orgSpec is one node of the tree, named by the key a later stage refers to
// it by rather than by its generated id.
type orgSpec struct {
	key    string
	name   string
	code   string
	parent string
	remark string
	// disabled exercises the state where members stay and no new ones may be
	// assigned — which is a different thing from the organization being gone,
	// and the only way to see that on screen is to have one.
	disabled bool
}

// The tree, three levels deep, which is the shallowest depth that shows a
// grandchild and therefore the shallowest that shows the tree is a tree.
var mainOrgs = []orgSpec{
	{key: "hq", name: "总部", code: "HQ", remark: "顶级组织"},
	{key: "tech", name: "技术中心", code: "TECH", parent: "hq"},
	{key: "platform", name: "平台组", code: "TECH-PLAT", parent: "tech"},
	{key: "apps", name: "应用组", code: "TECH-APP", parent: "tech"},
	{key: "market", name: "市场部", code: "MKT", parent: "hq"},
	{key: "finance", name: "财务部", code: "FIN", parent: "hq",
		remark: "已停用：并入总部后保留历史", disabled: true},
	// A second root, so the list is not a single tree and the "no parent"
	// case is visible.
	{key: "external", name: "外部协作", code: "EXT", remark: "承包商与顾问"},
}

// The second tenant reuses HQ's code on purpose. Two tenants holding the same
// code is legal — codes are unique per tenant — and a query that forgot its
// tenant would surface here as one tenant seeing the other's organization.
var secondOrgs = []orgSpec{
	{key: "hq", name: "Acme HQ", code: "HQ"},
	{key: "sales", name: "Sales", code: "SALES", parent: "hq"},
}

func (s *Seeder) seedOrganizations(ctx context.Context, w *world) error {
	for _, spec := range []struct {
		code  string
		specs []orgSpec
	}{{TenantMain, mainOrgs}, {TenantSecond, secondOrgs}} {
		t := w.tenantByCode(spec.code)
		if t == nil {
			continue
		}
		// The actor is filled in by the accounts stage; until then the audit
		// entries these produce carry the seed's own name, which is honest:
		// nobody clicked anything.
		actor := auth.Principal{TenantID: t.tenant.ID, Username: "seed"}

		for _, o := range spec.specs {
			in := service.OrganizationInput{
				Name: o.name, Code: o.code, Remark: o.remark,
			}
			if o.parent != "" {
				parent, ok := t.orgs[o.parent]
				if !ok {
					return fmt.Errorf("organization %s names parent %s, which is not seeded yet",
						o.code, o.parent)
				}
				in.ParentID = parent.ID
			}

			// Created active, always. The disabled one is disabled after its
			// people are in it — see closeOffOrganizations, which explains
			// why.
			org, err := s.orgs.Create(ctx, actor, in)
			if err != nil {
				return fmt.Errorf("create organization %s: %w", o.code, err)
			}
			t.orgs[o.key] = org
			w.summary.Organizations++
		}
	}
	return nil
}

// Groups, which are the other half of the distinction the console spends a
// guide panel explaining: a person belongs to one organization and to any
// number of groups.
//
// Both sources are represented. A group the console created is editable here;
// a group a directory pushed over SCIM is marked as directory-owned and is
// not, and an operator who has only ever seen one of the two has not seen the
// part that matters.
var mainGroups = []struct {
	name        string
	description string
	source      model.GroupSource
	externalID  string
	// members names the accounts to add, by the usernames people.go issues.
	members []string
}{
	{name: "值班工程师", description: "轮值处理线上问题", source: model.GroupSourceAdmin,
		members: []string{"zhangwei", "liyan", "wangfang"}},
	{name: "预算审批人", description: "财务流程的审批环节", source: model.GroupSourceAdmin,
		members: []string{"chenjing", "zhaolei"}},
	{name: "All Staff", description: "Pushed from the directory", source: model.GroupSourceSCIM,
		externalID: "grp-all-staff",
		members:    []string{"zhangwei", "liyan", "wangfang", "chenjing", "zhaolei", "sunli"}},
	{name: "Contractors", description: "Pushed from the directory", source: model.GroupSourceSCIM,
		externalID: "grp-contractors", members: []string{"sunli"}},
}

func (s *Seeder) seedGroups(ctx context.Context, w *world) error {
	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}
	actor := auth.Principal{TenantID: t.tenant.ID, Username: "seed"}

	for _, g := range mainGroups {
		group, err := s.groups.Create(ctx, t.tenant.ID, service.GroupInput{
			DisplayName: g.name, Description: g.description, ExternalID: g.externalID,
		}, g.source, actor)
		if err != nil {
			return fmt.Errorf("create group %s: %w", g.name, err)
		}
		t.groups = append(t.groups, group)
		w.summary.Groups++
	}
	return nil
}
