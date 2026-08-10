package service

// Reconciliation, against a fake directory.
//
// The rules worth testing here are the destructive ones — what happens to an
// account that has stopped appearing, and what happens when the directory
// answers with nothing at all — and none of them needs a real LDAP server to
// exercise. There is a container test elsewhere for the wire protocol; this
// is for the decisions.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/directory"
	"github.com/Paraview-RD/portico/internal/metrics"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/secrets"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
	"github.com/Paraview-RD/portico/internal/testdb"
)

type fakeDirectory struct {
	entries []directory.Entry
	err     error
}

func (f *fakeDirectory) Users() ([]directory.Entry, []error, error) {
	return f.entries, nil, f.err
}
func (f *fakeDirectory) Close() {}

type syncFixture struct {
	t         *testing.T
	svc       *DirectoryService
	users     *UserService
	store     *store.Store
	tenantID  string
	sourceID  string
	actor     auth.Principal
	directory *fakeDirectory
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open("postgres", testdb.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := store.Now()
	tenantID := "tenant-sync"
	if err := st.Queries.CreateTenant(ctx, sqlcgen.CreateTenantParams{
		ID: tenantID, Code: "sync", Name: "Sync", Status: "ACTIVE",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	audit := NewAuditService(st)
	settings := NewSettingsService(st, 0)
	users := NewUserService(st, audit, settings, auth.NewTokenService([]byte("0123456789abcdef0123456789abcdef")), metrics.New())

	key := make([]byte, secrets.KeyLength)
	for i := range key {
		key[i] = byte(i)
	}
	vault, err := secrets.NewVault(key)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}

	fake := &fakeDirectory{}
	svc := NewDirectoryService(st, users, audit, nil, vault)
	svc.dial = func(directory.Config) (DirectoryReader, error) { return fake, nil }

	actor := auth.Principal{TenantID: tenantID, UserID: "admin-id", Username: "admin", Role: model.RoleSuperAdmin}

	source, err := svc.Register(ctx, actor, LDAPSourceInput{
		Name: "Head office", Host: "ldap.example.test", Port: 389,
		Encryption: directory.EncryptionNone,
		BaseDN:     "dc=example,dc=org", UserFilter: "(objectClass=inetOrgPerson)",
		AttrUsername: "uid", AttrDisplayName: "cn", AttrEmail: "mail",
		AttrExternalID: "entryUUID",
	})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}

	return &syncFixture{
		t: t, svc: svc, users: users, store: st,
		tenantID: tenantID, sourceID: source.ID, actor: actor, directory: fake,
	}
}

func (f *syncFixture) sync() model.LDAPSyncRun {
	f.t.Helper()
	run, err := f.svc.SyncNow(context.Background(), f.actor, f.sourceID)
	if err != nil {
		f.t.Fatalf("sync: %v", err)
	}
	return run
}

func (f *syncFixture) user(username string) model.User {
	f.t.Helper()
	users, _, err := f.users.List(context.Background(), f.tenantID, UserQuery{}, Page{Limit: 100})
	if err != nil {
		f.t.Fatalf("list users: %v", err)
	}
	for _, u := range users {
		if u.Username == username {
			return u
		}
	}
	f.t.Fatalf("no user %q", username)
	return model.User{}
}

func entryFor(uid, name, uuid string) directory.Entry {
	return directory.Entry{
		DN:       "uid=" + uid + ",dc=example,dc=org",
		Username: uid, DisplayName: name, ExternalID: uuid,
	}
}

func TestSyncCreatesThenUpdatesRatherThanDuplicating(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{entryFor("zhangsan", "张三", "uuid-1")}
	first := f.sync()
	if first.CreatedCount != 1 {
		t.Fatalf("first run created %d, want 1 (%s)", first.CreatedCount, first.Error)
	}

	// The directory renames them. Same identifier, different display name:
	// this must update the account rather than create a second one, which is
	// the entire reason reconciliation keys on externalId.
	f.directory.entries = []directory.Entry{entryFor("zhangsan", "张三丰", "uuid-1")}
	second := f.sync()

	if second.CreatedCount != 0 {
		t.Errorf("a rename created %d accounts; reconciliation is not matching "+
			"on the directory's identifier", second.CreatedCount)
	}
	if second.UpdatedCount != 1 {
		t.Errorf("a rename updated %d accounts, want 1", second.UpdatedCount)
	}
	if got := f.user("zhangsan").DisplayName; got != "张三丰" {
		t.Errorf("display name = %q, want the directory's new value", got)
	}
}

// An account that stops appearing is deactivated — that is the contract.
func TestAccountThatVanishesFromTheDirectoryIsDeactivated(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{
		entryFor("stays", "Stays", "uuid-stays"),
		entryFor("leaves", "Leaves", "uuid-leaves"),
	}
	f.sync()

	f.directory.entries = []directory.Entry{entryFor("stays", "Stays", "uuid-stays")}
	run := f.sync()

	if run.DeactivatedCount != 1 {
		t.Errorf("deactivated %d, want 1", run.DeactivatedCount)
	}
	if got := f.user("leaves").Status; got != model.StatusDisabled {
		t.Errorf("the departed account is %s, want DISABLED", got)
	}
	if got := f.user("stays").Status; got != model.StatusActive {
		t.Errorf("the remaining account is %s, want ACTIVE", got)
	}
}

// And comes back when the directory lists them again, which is what treating
// a directory as the source of truth means.
func TestReappearingAccountIsReactivated(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{entryFor("returner", "Returner", "uuid-r")}
	f.sync()

	f.directory.entries = nil
	// Deactivation of the only account would trip the empty-result guard, so
	// give the directory something else to return.
	f.directory.entries = []directory.Entry{entryFor("other", "Other", "uuid-o")}
	f.sync()
	if got := f.user("returner").Status; got != model.StatusDisabled {
		t.Fatalf("setup: account is %s, want DISABLED", got)
	}

	f.directory.entries = []directory.Entry{
		entryFor("other", "Other", "uuid-o"),
		entryFor("returner", "Returner", "uuid-r"),
	}
	run := f.sync()

	if run.UpdatedCount != 1 {
		t.Errorf("updated %d, want 1 for the reactivated account", run.UpdatedCount)
	}
	if got := f.user("returner").Status; got != model.StatusActive {
		t.Errorf("the returning account is %s, want ACTIVE", got)
	}
}

// The one that matters most.
//
// A search matching nothing looks exactly like a directory everyone has left.
// The first is a typo in a base DN and happens regularly; the second
// essentially never happens, and nobody would want it applied automatically
// at three in the morning. So it fails, loudly, and changes nothing.
func TestEmptyDirectoryResultDeactivatesNobody(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{
		entryFor("one", "One", "uuid-1"),
		entryFor("two", "Two", "uuid-2"),
	}
	f.sync()

	f.directory.entries = nil
	run := f.sync()

	if run.Outcome != model.SyncFailed {
		t.Errorf("an empty directory result was reported as %s; it has to fail "+
			"so somebody looks at the filter", run.Outcome)
	}
	if run.DeactivatedCount != 0 {
		t.Errorf("deactivated %d accounts on an empty result; a wrong base DN "+
			"would have locked out the whole company", run.DeactivatedCount)
	}
	for _, username := range []string{"one", "two"} {
		if got := f.user(username).Status; got != model.StatusActive {
			t.Errorf("%s is %s after an empty result, want ACTIVE", username, got)
		}
	}
}

// An account Portico owns is never touched by a sync, however the names line
// up. An administrator whose username happens to match somebody in the
// directory must not be re-owned, renamed, or deactivated.
func TestSyncNeverTouchesAnAccountItDoesNotOwn(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()

	local, err := f.users.Create(ctx, f.tenantID, CreateUserInput{
		Username: "shared", DisplayName: "Local Administrator",
		Password: "local-password-1", Role: model.RoleSuperAdmin,
		Source: model.SourceAdmin,
	})
	if err != nil {
		t.Fatalf("create local account: %v", err)
	}

	f.directory.entries = []directory.Entry{
		entryFor("shared", "Somebody Else Entirely", "uuid-shared"),
		entryFor("genuine", "Genuine", "uuid-genuine"),
	}
	run := f.sync()

	if run.SkippedCount != 1 {
		t.Errorf("skipped %d, want 1 for the colliding username", run.SkippedCount)
	}

	after := f.user("shared")
	if after.ID != local.ID {
		t.Fatal("the local account was replaced by the directory's entry")
	}
	if after.DisplayName != "Local Administrator" {
		t.Errorf("display name = %q; a sync renamed an account it does not own",
			after.DisplayName)
	}
	if after.Role != model.RoleSuperAdmin {
		t.Errorf("role = %s; a sync demoted an administrator", after.Role)
	}

	// And a later sync that no longer lists the colliding name must not
	// deactivate the local account either.
	f.directory.entries = []directory.Entry{entryFor("genuine", "Genuine", "uuid-genuine")}
	f.sync()
	if got := f.user("shared").Status; got != model.StatusActive {
		t.Errorf("the local account is %s after it vanished from the directory "+
			"— which it was never in", got)
	}
}

// A failed run is recorded rather than swallowed, because "when did this
// start" is the question asked afterwards.
func TestFailedSyncLeavesARunToRead(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.err = context.DeadlineExceeded
	run := f.sync()

	if run.Outcome != model.SyncFailed {
		t.Errorf("outcome = %s, want FAILED", run.Outcome)
	}
	if run.Error == "" {
		t.Error("a failed run recorded no reason, so nobody can act on it")
	}

	runs, err := f.svc.Runs(context.Background(), f.tenantID, f.sourceID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Outcome != model.SyncFailed {
		t.Errorf("run history = %+v, want one failed run", runs)
	}
}

// A skipped entry has to say why.
//
// The count alone was what the run carried, and the documentation's "most
// often a username collision" is a wrong lead whenever it is not one. It cost
// a walkthrough several rounds to discover that every account was being
// refused for a phone number formatted with spaces — a thing the run knew and
// did not record.
func TestASkippedEntryRecordsTheReasonAndAnExample(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{
		{ExternalID: "ext-ok", Username: "keiko", DisplayName: "Keiko Tanaka"},
		// Formatted the way a person writes one, which the account rules
		// refuse. This is the case that started all of it.
		{ExternalID: "ext-phone", Username: "rafael", DisplayName: "Rafael Costa",
			Phone: "+44 20 7946 0100"},
	}

	run := f.sync()

	if run.SkippedCount != 1 {
		t.Fatalf("skipped = %d, want 1", run.SkippedCount)
	}
	if run.SkippedDetail == "" {
		t.Fatal("the run records how many were skipped and not why, which is " +
			"the state this test exists to prevent")
	}
	if !strings.Contains(run.SkippedDetail, "rafael") {
		t.Errorf("skippedDetail = %q, want it to name the entry an operator "+
			"has to go and look at", run.SkippedDetail)
	}
	if !strings.Contains(strings.ToLower(run.SkippedDetail), "phone") {
		t.Errorf("skippedDetail = %q, want it to say what was wrong rather "+
			"than that something was", run.SkippedDetail)
	}

	// The one that was fine is still there: a skip is per entry, not a run.
	if got := f.user("keiko").Username; got != "keiko" {
		t.Errorf("keiko did not arrive alongside the skipped entry")
	}
}

// Grouped, not listed. A source pointed at the wrong attribute skips every
// entry for the same reason, and a line per entry would be a row per account
// in the directory.
func TestManyEntriesFailingTheSameWayAreOneLine(t *testing.T) {
	f := newSyncFixture(t)

	for i := range 6 {
		f.directory.entries = append(f.directory.entries, directory.Entry{
			ExternalID:  fmt.Sprintf("ext-%d", i),
			Username:    fmt.Sprintf("person%d", i),
			DisplayName: "Person",
			Phone:       "+44 20 7946 010" + fmt.Sprint(i),
		})
	}

	run := f.sync()

	if run.SkippedCount != 6 {
		t.Fatalf("skipped = %d, want 6", run.SkippedCount)
	}
	if !strings.HasPrefix(run.SkippedDetail, "6 × ") {
		t.Errorf("skippedDetail = %q, want it to lead with the count for one "+
			"reason rather than repeating the reason six times", run.SkippedDetail)
	}
	// Bounded: three examples and an ellipsis, not six names.
	if strings.Contains(run.SkippedDetail, "person3") {
		t.Errorf("skippedDetail = %q lists more than the examples it promised",
			run.SkippedDetail)
	}
	if !strings.Contains(run.SkippedDetail, "…") {
		t.Errorf("skippedDetail = %q does not say it is showing only some",
			run.SkippedDetail)
	}
}
