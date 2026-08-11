package seed

import (
	"context"
	"fmt"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// DemoPassword is what every seeded account signs in with.
//
// One password for everybody, printed by cmd/seed, and chosen to satisfy the
// strict policy the first tenant gets — ten characters, upper, lower, digit.
// The alternative, a different password per account, would mean the seed
// prints fifty-five of them and nobody uses any.
//
// Accounts a directory owns get it too. In a real deployment those hold a
// random password nothing can authenticate with, and this deliberately
// differs: being able to sign in as a directory-sourced account is how you
// look at what the portal shows one, which is the entire point of a seed.
const DemoPassword = "Portico-2026-demo"

// MainAccountCount is how many accounts the first tenant gets.
//
// Fifty-five, for one reason: the console paginates at twenty (web PAGE_SIZE),
// so this is the smallest number that produces a third page. A list with one
// page has never had its paging exercised, and paging is where off-by-one
// bugs live.
const MainAccountCount = 55

// person is one named account. The named ones are the accounts other parts of
// the seed refer to — group members, organization managers, audit actors — so
// they are written out rather than generated.
type person struct {
	username    string
	displayName string
	email       string
	phone       string
	org         string
	role        model.Role
	source      model.UserSource

	// disabled exercises an account that is kept for the audit trail and
	// cannot sign in.
	disabled bool
	// noContact is an account with neither an email address nor a phone
	// number. The portal warns about it, because such an account cannot
	// recover its own password — and that warning is invisible without one.
	noContact bool
}

var namedPeople = []person{
	{username: "zhangwei", displayName: "张伟", email: "zhangwei@example.org",
		phone: "13800000001", org: "platform", role: model.RoleSuperAdmin, source: model.SourceAdmin},
	{username: "liyan", displayName: "李燕", email: "liyan@example.org",
		phone: "13800000002", org: "platform", role: model.RoleUser, source: model.SourceAdmin},
	{username: "wangfang", displayName: "王芳", email: "wangfang@example.org",
		phone: "13800000003", org: "apps", role: model.RoleUser, source: model.SourceAdmin},
	{username: "chenjing", displayName: "陈静", email: "chenjing@example.org",
		phone: "13800000004", org: "finance", role: model.RoleSuperAdmin, source: model.SourceAdmin},
	{username: "zhaolei", displayName: "赵磊", email: "zhaolei@example.org",
		phone: "13800000005", org: "finance", role: model.RoleUser, source: model.SourceImport},
	{username: "sunli", displayName: "孙丽", email: "sunli@example.org",
		org: "external", role: model.RoleUser, source: model.SourceRegistration},

	// A directory's worth of people, one from each of the two directions
	// accounts arrive from. Both are marked as owned elsewhere, which is what
	// makes the console warn that edits here may be overwritten.
	{username: "mei.tanaka", displayName: "Mei Tanaka", email: "mei@example.org",
		org: "market", role: model.RoleUser, source: model.SourceSCIM},
	{username: "arjun.patel", displayName: "Arjun Patel", email: "arjun@example.org",
		phone: "13800000007", org: "market", role: model.RoleUser, source: model.SourceLDAP},

	// The awkward ones, each present because a screen behaves differently for
	// it and that behaviour is otherwise never on display.
	{username: "tomas.novak", displayName: "Tomáš Novák", email: "tomas@example.org",
		org: "apps", role: model.RoleUser, source: model.SourceLDAP},
	{username: "long.name", displayName: "欧阳建国·亚历山德罗·冯·穆勒-施密特",
		email: "long@example.org", org: "market", role: model.RoleUser, source: model.SourceImport},
	{username: "no.contact", displayName: "无联系方式", org: "external",
		role: model.RoleUser, source: model.SourceImport, noContact: true},
	{username: "left.company", displayName: "离职员工", email: "left@example.org",
		org: "market", role: model.RoleUser, source: model.SourceAdmin, disabled: true},
	{username: "gone.from.ad", displayName: "已从目录消失", email: "gone@example.org",
		org: "apps", role: model.RoleUser, source: model.SourceLDAP, disabled: true},
	{username: "locked.out", displayName: "被锁定的账号", email: "locked@example.org",
		phone: "13800000008", org: "platform", role: model.RoleUser, source: model.SourceAdmin},
	{username: "password.stale", displayName: "密码即将过期", email: "stale@example.org",
		org: "apps", role: model.RoleUser, source: model.SourceAdmin},
}

// The filler accounts, so that the list is long enough to page through.
//
// Generated rather than written out: forty of them carry no information a
// reader of this file needs, and forty literal entries would bury the fifteen
// above that do. Deterministic, so two runs produce the same list — a seed
// that shuffled would make "is this the same data as yesterday" unanswerable.
var (
	fillerSurnames = []string{"刘", "杨", "黄", "周", "吴", "徐", "胡", "郭", "何", "林"}
	fillerGiven    = []string{"敏", "强", "涛", "娜", "斌", "洋", "岩", "婷", "宇", "琳"}
	fillerOrgs     = []string{"platform", "apps", "market", "external"}
	fillerSources  = []model.UserSource{
		model.SourceAdmin, model.SourceImport, model.SourceSCIM,
		model.SourceLDAP, model.SourceRegistration,
	}
)

// fillerPeople returns however many accounts are still needed to reach
// MainAccountCount.
func fillerPeople(existing int) []person {
	people := make([]person, 0, MainAccountCount-existing)

	for i := existing; i < MainAccountCount; i++ {
		n := i - existing
		surname := fillerSurnames[n%len(fillerSurnames)]
		given := fillerGiven[(n/len(fillerSurnames))%len(fillerGiven)]

		people = append(people, person{
			username:    fmt.Sprintf("staff%02d", n+1),
			displayName: surname + given,
			email:       fmt.Sprintf("staff%02d@example.org", n+1),
			// Every third one has no phone number, which is the ordinary
			// state of a directory nobody has finished tidying.
			phone:  phoneFor(n),
			org:    fillerOrgs[n%len(fillerOrgs)],
			role:   model.RoleUser,
			source: fillerSources[n%len(fillerSources)],
		})
	}
	return people
}

func phoneFor(n int) string {
	if n%3 == 0 {
		return ""
	}
	return fmt.Sprintf("139%08d", n+1)
}

// The second tenant's accounts. Three is enough, and one of them shares a
// username with the first tenant on purpose: usernames are unique per tenant,
// and a query that forgot its tenant would show up here as a sign-in landing
// in the wrong company.
var secondPeople = []person{
	{username: "zhangwei", displayName: "Wei Zhang (Acme)", email: "wei@acme.example",
		org: "sales", role: model.RoleSuperAdmin, source: model.SourceAdmin},
	{username: "acme.rep", displayName: "Acme Sales Rep", email: "rep@acme.example",
		org: "sales", role: model.RoleUser, source: model.SourceAdmin},
	{username: "acme.viewer", displayName: "Acme Viewer", email: "viewer@acme.example",
		org: "hq", role: model.RoleUser, source: model.SourceImport},
}

func (s *Seeder) seedUsers(ctx context.Context, w *world) error {
	for _, group := range []struct {
		code   string
		people []person
	}{
		{TenantMain, append(namedPeople, fillerPeople(len(namedPeople))...)},
		{TenantSecond, secondPeople},
	} {
		t := w.tenantByCode(group.code)
		if t == nil {
			continue
		}
		actor := auth.Principal{TenantID: t.tenant.ID, Username: "seed"}

		for _, p := range group.people {
			user, err := s.createPerson(ctx, t, actor, p)
			if err != nil {
				return err
			}
			t.users = append(t.users, user)
			w.summary.Users++
		}

		// The tenant's own administrator, from here on. Audit entries the
		// later stages produce carry a real account rather than "seed", so
		// clicking through from an entry lands somewhere.
		if admin := findUser(t.users, seedActorUsername); admin.ID != "" {
			t.actor = auth.Principal{
				TenantID: t.tenant.ID, UserID: admin.ID,
				Username: admin.Username, Role: model.RoleSuperAdmin,
			}
		} else {
			t.actor = actor
		}
	}

	if err := s.assignGroupMembers(ctx, w); err != nil {
		return err
	}
	if err := s.seedCustomAttributes(ctx, w); err != nil {
		return err
	}
	return s.closeOffOrganizations(ctx, w)
}

// seedActorUsername is the administrator each tenant's later stages act as.
// Both tenants have one under this name, which is itself part of what the
// second tenant is here to demonstrate.
const seedActorUsername = "zhangwei"

func (s *Seeder) createPerson(ctx context.Context, t *seededTenant, actor auth.Principal, p person) (model.User, error) {
	in := service.CreateUserInput{
		Username: p.username, DisplayName: p.displayName, Password: DemoPassword,
		Role: p.role, Source: p.source,
	}
	if !p.noContact {
		in.Email, in.Phone = p.email, p.phone
	}
	if org, ok := t.orgs[p.org]; ok {
		in.OrganizationID = org.ID
	}

	user, err := s.users.Create(ctx, t.tenant.ID, in)
	if err != nil {
		return model.User{}, fmt.Errorf("create account %s in tenant %s: %w",
			p.username, t.tenant.Code, err)
	}

	if p.disabled {
		user, err = s.users.SetStatus(ctx, actor, user.ID, model.StatusDisabled)
		if err != nil {
			return model.User{}, fmt.Errorf("disable account %s: %w", p.username, err)
		}
	}
	return user, nil
}

// assignGroupMembers fills the groups the previous stage created. It runs here
// because a group can only take members that exist.
func (s *Seeder) assignGroupMembers(ctx context.Context, w *world) error {
	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}

	for i, spec := range mainGroups {
		if i >= len(t.groups) {
			break
		}
		ids := make([]string, 0, len(spec.members))
		for _, username := range spec.members {
			if user := findUser(t.users, username); user.ID != "" {
				ids = append(ids, user.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		if err := s.groups.AddMembers(ctx, t.tenant.ID, t.groups[i].ID, ids, t.actor); err != nil {
			return fmt.Errorf("add members to group %s: %w", spec.name, err)
		}
	}
	return nil
}

// closeOffOrganizations applies the states an organization cannot be created
// in.
//
// Disabling happens here rather than beside the creation because a disabled
// organization refuses new members — so an organization disabled before its
// people were added would end up empty, which is the opposite of what it is
// here to show. What an operator needs to see is that disabling keeps the
// people it already has and only stops new assignments.
//
// The manager is set here for the same reason: it has to be somebody.
func (s *Seeder) closeOffOrganizations(ctx context.Context, w *world) error {
	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}

	if tech, ok := t.orgs["tech"]; ok {
		if manager := findUser(t.users, "zhangwei"); manager.ID != "" {
			if _, err := s.orgs.SetManager(ctx, t.actor, tech.ID, manager.ID); err != nil {
				return fmt.Errorf("set manager of %s: %w", tech.Code, err)
			}
		}
	}

	for _, spec := range mainOrgs {
		if !spec.disabled {
			continue
		}
		org, ok := t.orgs[spec.key]
		if !ok {
			continue
		}
		updated, err := s.orgs.SetStatus(ctx, t.actor, org.ID, model.StatusDisabled)
		if err != nil {
			return fmt.Errorf("disable organization %s: %w", spec.code, err)
		}
		t.orgs[spec.key] = updated
	}
	return nil
}

func findUser(users []model.User, username string) model.User {
	for _, u := range users {
		if u.Username == username {
			return u
		}
	}
	return model.User{}
}

// Two attributes this tenant defined for itself, and values on some accounts.
//
// Two rather than one, and of different kinds, because the interesting part of
// the feature is that a tenant names its own facts: a single TEXT field would
// show that a form can hold a string, which nobody doubted. A date and a
// single-select show the two things that need validating, and the select is
// what makes the picker on a mapping form worth looking at.
//
// Values on some accounts rather than all: "who has filled this in" is the
// question asked before retiring or mapping one, and it has no answer if
// everybody has.
var customAttributes = []struct {
	key, label, description, kind string
	allowed                       []string
	// filledFor names the accounts that get a value, and what it is.
	filledFor map[string]string
}{
	{
		key: "badge_number", label: "门禁卡号",
		description: "园区门禁系统里的卡号，随人走而不随工号走",
		kind:        service.FieldKindText,
		filledFor: map[string]string{
			"zhangwei": "A-10293", "liyan": "A-10294", "wangfang": "A-11007",
		},
	},
	{
		key: "work_mode", label: "办公方式",
		description: "用于下游的座位与设备发放", kind: service.FieldKindSelect,
		allowed: []string{"ONSITE", "HYBRID", "REMOTE"},
		filledFor: map[string]string{
			"zhangwei": "ONSITE", "wangfang": "HYBRID", "sunli": "REMOTE",
		},
	},
}

func (s *Seeder) seedCustomAttributes(ctx context.Context, w *world) error {
	t := w.tenantByCode(TenantMain)
	if t == nil {
		return nil
	}

	for _, a := range customAttributes {
		if _, err := s.attrs.Define(ctx, t.actor, service.UserAttributeInput{
			Key: a.key, Label: a.label, Description: a.description,
			Kind: a.kind, AllowedValues: a.allowed,
		}); err != nil {
			return fmt.Errorf("define attribute %s: %w", a.key, err)
		}

		for username, value := range a.filledFor {
			user := findUser(t.users, username)
			if user.ID == "" {
				continue
			}
			if err := s.attrs.SetValues(ctx, t.actor, user.ID,
				map[string]string{a.key: value}); err != nil {
				return fmt.Errorf("set %s on %s: %w", a.key, username, err)
			}
		}
	}
	return nil
}
