package service

// Automatic synchronization: what gets claimed, when, and what a claim costs
// a directory that is failing.
//
// The interesting properties here are not "does it run" — SyncNow is tested
// next door — but the schedule's arithmetic and the two timestamps it depends
// on. A claim that reused last_synced_at would pass almost every test in this
// file and quietly report that a directory which has been broken for a week
// synchronized two minutes ago; the tests that catch that are marked.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Paraview-RD/portico/internal/directory"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
)

// setSyncInterval turns the schedule on, off, or to something else, through
// the same call an administrator's form goes through.
func (f *syncFixture) setSyncInterval(minutes int) {
	f.t.Helper()

	in := f.baseInput()
	in.SyncIntervalMinutes = minutes
	if _, err := f.svc.Update(context.Background(), f.actor, f.sourceID, in); err != nil {
		f.t.Fatalf("set interval to %d: %v", minutes, err)
	}
}

// baseInput is the fixture's directory as it was registered, so a test can
// change one field without restating the other nine.
func (f *syncFixture) baseInput() LDAPSourceInput {
	return LDAPSourceInput{
		Name: "Head office", Host: "ldap.example.test", Port: 389,
		Encryption: directory.EncryptionNone,
		BaseDN:     "dc=example,dc=org", UserFilter: "(objectClass=inetOrgPerson)",
		AttrUsername: "uid", AttrDisplayName: "cn", AttrEmail: "mail",
		AttrExternalID: "entryUUID",
	}
}

func (f *syncFixture) syncDue(at time.Time) []model.LDAPSyncRun {
	f.t.Helper()

	runs, err := f.svc.SyncDue(context.Background(), f.tenantID, at)
	if err != nil {
		f.t.Fatalf("sync due: %v", err)
	}
	return runs
}

func (f *syncFixture) runCount() int {
	f.t.Helper()

	runs, err := f.svc.Runs(context.Background(), f.tenantID, f.sourceID, 100)
	if err != nil {
		f.t.Fatalf("list runs: %v", err)
	}
	return len(runs)
}

// The default is off, and it has to stay off: this is the behaviour every
// deployment that upgrades into this feature already has, and a directory
// that started being read on a timer because somebody applied a migration
// would be Portico reaching out to a third-party system nobody asked it to.
func TestADirectoryIsNotSynchronizedOnATimerUnlessSomebodyAsked(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{entryFor("zhangsan", "张三", "uuid-1")}

	// A year on, in case anything is treating "never attempted" as "overdue".
	if runs := f.syncDue(store.Now().AddDate(1, 0, 0)); len(runs) != 0 {
		t.Errorf("a directory with no configured interval was synchronized %d times", len(runs))
	}
	if got := f.runCount(); got != 0 {
		t.Errorf("run history has %d entries, want none", got)
	}
}

// Turning it on runs it now, and then leaves it alone until the interval has
// actually elapsed.
//
// Running promptly is a decision rather than a side effect. The alternative —
// wait one interval before the first run — means an administrator who
// configures a daily synchronization sees nothing happen all day and cannot
// tell a working schedule from a broken one.
func TestTurningOnAScheduleRunsPromptlyThenOncePerInterval(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{entryFor("zhangsan", "张三", "uuid-1")}
	f.setSyncInterval(MinSyncIntervalMinutes)

	now := store.Now()

	first := f.syncDue(now)
	if len(first) != 1 {
		t.Fatalf("first pass ran %d directories, want 1", len(first))
	}
	if first[0].Outcome != model.SyncSucceeded {
		t.Fatalf("scheduled run failed: %s", first[0].Error)
	}
	if first[0].CreatedCount != 1 {
		t.Errorf("scheduled run created %d accounts, want 1", first[0].CreatedCount)
	}

	// The same instant again, and a minute later. This is the property that
	// makes a one-minute scheduler tick safe: being asked does not mean being
	// due.
	if runs := f.syncDue(now); len(runs) != 0 {
		t.Errorf("a second pass at the same instant ran %d directories, want 0", len(runs))
	}
	if runs := f.syncDue(now.Add(time.Minute)); len(runs) != 0 {
		t.Errorf("a pass one minute later ran %d directories, want 0", len(runs))
	}

	if runs := f.syncDue(now.Add(MinSyncIntervalMinutes * time.Minute)); len(runs) != 1 {
		t.Errorf("a pass a full interval later ran %d directories, want 1", len(runs))
	}
}

// A directory that cannot be reached is retried on the next interval, not on
// the next tick.
//
// This is the test that fails if the claim is ever moved onto last_synced_at,
// which is written on success only: a failing source would stay permanently
// due and Portico would connect to somebody's directory every minute for as
// long as it stayed broken. Which is to say the interval would silently stop
// applying at exactly the moment it matters most.
func TestAFailedScheduledRunWaitsOutTheIntervalRatherThanRetryingAtOnce(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.err = context.DeadlineExceeded
	f.setSyncInterval(MinSyncIntervalMinutes)

	now := store.Now()

	first := f.syncDue(now)
	if len(first) != 1 || first[0].Outcome != model.SyncFailed {
		t.Fatalf("first pass = %+v, want one failed run", first)
	}

	if runs := f.syncDue(now.Add(time.Minute)); len(runs) != 0 {
		t.Fatalf("a failed directory was retried after a minute (%d runs); "+
			"a misconfigured source would hammer the directory server", len(runs))
	}
	if got := f.runCount(); got != 1 {
		t.Errorf("run history has %d entries, want 1", got)
	}

	if runs := f.syncDue(now.Add(MinSyncIntervalMinutes * time.Minute)); len(runs) != 1 {
		t.Errorf("a failed directory was not retried after a full interval (%d runs)", len(runs))
	}
}

// A failed attempt must not look like a successful one.
//
// last_synced_at is what the console shows and what an operator reads to
// answer "is this directory still the source of truth". The schedule keeps its
// own timestamp precisely so that advancing the schedule — which every
// attempt does, successful or not — cannot answer that question wrongly.
func TestAFailedScheduledRunDoesNotClaimToHaveSynchronized(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.err = context.DeadlineExceeded
	f.setSyncInterval(MinSyncIntervalMinutes)

	if runs := f.syncDue(store.Now()); len(runs) != 1 {
		t.Fatalf("scheduled pass ran %d directories, want 1", len(runs))
	}

	source, err := f.svc.Get(context.Background(), f.tenantID, f.sourceID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if source.LastSyncedAt != nil {
		t.Errorf("lastSyncedAt = %v after a failed run; the console would report "+
			"a broken directory as freshly synchronized", source.LastSyncedAt)
	}
}

// The scheduler is not a user and must not be recorded as one.
//
// An empty actor is what the console renders as "scheduled", and the audit
// entry has to be equally clear: attributing an unattended run to whoever
// last edited the directory would put a person's name against accounts they
// did not deactivate.
func TestAScheduledRunIsRecordedWithoutAnActor(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{entryFor("zhangsan", "张三", "uuid-1")}
	f.setSyncInterval(MinSyncIntervalMinutes)

	runs := f.syncDue(store.Now())
	if len(runs) != 1 {
		t.Fatalf("scheduled pass ran %d directories, want 1", len(runs))
	}
	if runs[0].ActorName != "" {
		t.Errorf("scheduled run recorded actor %q, want nobody", runs[0].ActorName)
	}

	stored, err := f.svc.Runs(context.Background(), f.tenantID, f.sourceID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(stored) != 1 || stored[0].ActorName != "" {
		t.Errorf("stored run = %+v, want one with no actor", stored)
	}
}

// Disabling a directory stops it being read, including on a timer.
//
// The connector and the accounts are different things — disabling one leaves
// the people alone — but a disabled connector that a scheduler kept using
// would make the switch meaningless during exactly the situation it exists
// for, which is a directory migration.
func TestADisabledDirectoryIsNotSynchronizedOnATimer(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{entryFor("zhangsan", "张三", "uuid-1")}
	f.setSyncInterval(MinSyncIntervalMinutes)

	if _, err := f.svc.SetStatus(context.Background(), f.actor, f.sourceID, model.StatusDisabled); err != nil {
		t.Fatalf("disable source: %v", err)
	}

	if runs := f.syncDue(store.Now()); len(runs) != 0 {
		t.Errorf("a disabled directory was synchronized %d times", len(runs))
	}
}

// Two instances of Portico reaching the same directory at the same moment
// enumerate it once between them.
//
// This is what the claim is for. Both passes are due, both start, and the one
// whose UPDATE arrives second finds the row already claimed — so the
// directory server sees one enumeration and the history gets one run, rather
// than two of each per interval per instance.
func TestTwoInstancesClaimingAtOnceEnumerateTheDirectoryOnce(t *testing.T) {
	f := newSyncFixture(t)

	f.directory.entries = []directory.Entry{entryFor("zhangsan", "张三", "uuid-1")}
	f.setSyncInterval(MinSyncIntervalMinutes)

	now := store.Now()
	counts := make([]int, 2)

	var wg sync.WaitGroup
	for i := range counts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runs, err := f.svc.SyncDue(context.Background(), f.tenantID, now)
			if err != nil {
				t.Errorf("instance %d: sync due: %v", i, err)
				return
			}
			counts[i] = len(runs)
		}(i)
	}
	wg.Wait()

	if total := counts[0] + counts[1]; total != 1 {
		t.Errorf("two instances performed %d runs between them, want 1", total)
	}
	if got := f.runCount(); got != 1 {
		t.Errorf("run history has %d entries, want 1", got)
	}
}

// A pass that stops halfway leaves the directories it never reached due.
//
// This is what claiming one directory at a time buys, and it is the difference
// between a restart costing nothing and a restart costing an interval. Claim
// everything due up front and the timestamps say every directory was attempted;
// the process then stops after the first, and the rest wait out a full interval
// for a synchronization that never happened and left no run record to say so.
//
// The context is cancelled from inside the first connection, which is as close
// as a test gets to the process going away mid-pass.
func TestAPassThatStopsHalfwayLeavesTheRestDue(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()

	second := f.baseInput()
	second.Name = "Branch office"
	second.SyncIntervalMinutes = MinSyncIntervalMinutes
	if _, err := f.svc.Register(ctx, f.actor, second); err != nil {
		t.Fatalf("register second directory: %v", err)
	}
	f.setSyncInterval(MinSyncIntervalMinutes)

	f.directory.entries = []directory.Entry{entryFor("zhangsan", "张三", "uuid-1")}

	interrupted, cancel := context.WithCancel(ctx)
	f.svc.dial = func(directory.Config) (DirectoryReader, error) {
		cancel()
		return f.directory, nil
	}

	now := store.Now()
	// Whatever this returns, it stopped early. The assertion is about what it
	// left behind, not about how it reported stopping.
	_, _ = f.svc.SyncDue(interrupted, f.tenantID, now)

	f.svc.dial = func(directory.Config) (DirectoryReader, error) { return f.directory, nil }

	// Same instant, so nothing has become due through the passage of time.
	// Anything claimed here is something the interrupted pass never reached.
	if runs := f.syncDue(now); len(runs) != 1 {
		t.Errorf("a pass after an interrupted one ran %d directories, want 1: "+
			"the directory the first pass never reached is not due, so its "+
			"schedule was advanced for a synchronization that never happened", len(runs))
	}
}

// An interval shorter than the floor is refused rather than rounded up.
//
// Rounding would leave an operator believing the directory is read four times
// as often as it is, which is the kind of wrong belief that only surfaces
// during an incident. The floor itself is there because a synchronization has
// only one size — finding out who has left means listing everybody — so a
// one-minute schedule is a load test against somebody else's directory
// server.
func TestAnIntervalOutsideTheAllowedRangeIsRefused(t *testing.T) {
	f := newSyncFixture(t)

	for _, minutes := range []int{1, MinSyncIntervalMinutes - 1, MaxSyncIntervalMinutes + 1, -5} {
		in := f.baseInput()
		in.SyncIntervalMinutes = minutes

		if _, err := f.svc.Update(context.Background(), f.actor, f.sourceID, in); err == nil {
			t.Errorf("an interval of %d minutes was accepted", minutes)
		}
	}

	// The two ends of the range, and off, all have to remain acceptable.
	for _, minutes := range []int{0, MinSyncIntervalMinutes, MaxSyncIntervalMinutes} {
		in := f.baseInput()
		in.SyncIntervalMinutes = minutes

		source, err := f.svc.Update(context.Background(), f.actor, f.sourceID, in)
		if err != nil {
			t.Fatalf("an interval of %d minutes was refused: %v", minutes, err)
		}
		if source.SyncIntervalMinutes != minutes {
			t.Errorf("stored interval = %d, want %d", source.SyncIntervalMinutes, minutes)
		}
	}
}
