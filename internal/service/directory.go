package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/directory"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/secrets"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// DirectoryService registers directories and synchronizes accounts out of
// them.
//
// This is the opposite direction from SCIM, which is worth stating because
// the two land in the same place. SCIM is a server here: a directory pushes
// and Portico never reaches out. LDAP is a pull, on Portico's initiative and
// its schedule. The failure modes differ accordingly — a push that stops is
// silent at this end, while a pull that stops leaves a failed run to point
// at, which is most of why the run records exist.
type DirectoryService struct {
	store    *store.Store
	users    *UserService
	audit    *AuditService
	webhooks *WebhookService
	vault    *secrets.Vault

	// dial is the LDAP connection, indirected so the reconciliation logic
	// can be tested against a fake directory. The rules about what happens
	// to an account that vanished are the part worth testing, and they
	// should not require a container to exercise.
	dial func(directory.Config) (DirectoryReader, error)
}

// DirectoryReader is the part of a directory connection this service uses.
type DirectoryReader interface {
	Users() ([]directory.Entry, []error, error)
	Close()
}

// NewDirectoryService wires a DirectoryService.
func NewDirectoryService(st *store.Store, users *UserService, audit *AuditService, webhooks *WebhookService, vault *secrets.Vault) *DirectoryService {
	return &DirectoryService{
		store: st, users: users, audit: audit, webhooks: webhooks, vault: vault,
		dial: func(cfg directory.Config) (DirectoryReader, error) {
			return directory.Dial(cfg)
		},
	}
}

// Errors this service returns.
var (
	ErrLDAPSourceNotFound = httpx.NotFound("LDAP_SOURCE_NOT_FOUND",
		"No such directory.")
	ErrLDAPSourceNameTaken = httpx.Conflict("LDAP_SOURCE_NAME_TAKEN",
		"A directory with that name already exists.")
	ErrLDAPSourceDisabled = httpx.UnprocessableEntity("LDAP_SOURCE_DISABLED",
		"That directory is disabled.")
	ErrInvalidLDAPEncryption = httpx.BadRequest("INVALID_LDAP_ENCRYPTION",
		"Encryption must be none, starttls, or tls.")
	ErrLDAPFieldRequired = httpx.BadRequest("LDAP_FIELD_REQUIRED",
		"Host, base DN, user filter, and the username, display name, and external id attributes are all required.")
	ErrInvalidLDAPPort = httpx.BadRequest("INVALID_LDAP_PORT",
		"Port must be between 1 and 65535.")
	// ErrInvalidSyncInterval refuses rather than clamping, as the tenant
	// settings do: an operator who typed five minutes and was quietly given
	// fifteen would go on believing the directory is read four times as often
	// as it is.
	ErrInvalidSyncInterval = httpx.BadRequest("INVALID_SYNC_INTERVAL",
		"The automatic synchronization interval must be 0 to turn it off, "+
			"or between 15 minutes and 7 days.")
	// ErrNoEncryptionKey is what a deployment with no PORTICO_ENCRYPTION_KEY
	// gets when it tries to store a bind password. Refusing is the point:
	// the alternative is a service account's credential sitting in a text
	// column, and nobody would find out until the database leaked.
	ErrNoEncryptionKey = httpx.UnprocessableEntity("NO_ENCRYPTION_KEY",
		"This deployment has no PORTICO_ENCRYPTION_KEY, so a bind password cannot be stored. "+
			"Set one (openssl rand -hex 32) and restart, or use an anonymous bind.")
)

// LDAPSourceInput is a directory as an administrator describes it.
type LDAPSourceInput struct {
	Name       string
	Host       string
	Port       int
	Encryption string

	BindDN string
	// BindPassword is applied only when non-nil. A nil pointer means "leave
	// what is stored alone", which is what an edit form that cannot display
	// the current value has to be able to express — otherwise submitting it
	// unchanged would blank the credential.
	BindPassword *string

	BaseDN     string
	UserFilter string

	AttrUsername    string
	AttrDisplayName string
	AttrEmail       string
	AttrPhone       string
	AttrExternalID  string

	OrganizationID string

	// SyncIntervalMinutes is how often to synchronize without being asked.
	// Zero is off, and is the default for a directory registered without
	// mentioning it.
	SyncIntervalMinutes int
}

// Bounds on the automatic synchronization interval.
//
// The floor is there because a synchronization has only one size: working out
// who has disappeared from a directory means listing everybody in it, so
// there is no cheap incremental pass to run every minute. Fifteen minutes is
// the shortest interval that is a schedule rather than a load test against
// somebody else's AD.
//
// The ceiling is a week, past which "automatic" stops being a useful
// description of what is happening and a person should be pressing the
// button.
const (
	MinSyncIntervalMinutes = 15
	MaxSyncIntervalMinutes = 7 * 24 * 60
)

// Register adds a directory.
func (s *DirectoryService) Register(ctx context.Context, actor auth.Principal, in LDAPSourceInput) (model.LDAPSource, error) {
	tenantID := actor.TenantID

	normalized, err := s.normalize(in)
	if err != nil {
		return model.LDAPSource{}, err
	}

	sealed := ""
	if normalized.BindPassword != nil && *normalized.BindPassword != "" {
		sealed, err = s.seal(*normalized.BindPassword)
		if err != nil {
			return model.LDAPSource{}, err
		}
	}

	now := store.Now()
	id := uuid.NewString()

	err = s.store.ForTenant(tenantID).CreateLDAPSource(ctx, sqlcgen.CreateLDAPSourceParams{
		ID:              id,
		Name:            normalized.Name,
		Host:            normalized.Host,
		Port:            narrow(normalized.Port),
		Encryption:      normalized.Encryption,
		BindDn:          normalized.BindDN,
		BindPassword:    sealed,
		BaseDn:          normalized.BaseDN,
		UserFilter:      normalized.UserFilter,
		AttrUsername:    normalized.AttrUsername,
		AttrDisplayName: normalized.AttrDisplayName,
		AttrEmail:       normalized.AttrEmail,
		AttrPhone:       normalized.AttrPhone,
		AttrExternalID:  normalized.AttrExternalID,
		OrganizationID:  optionalID(normalized.OrganizationID),
		Status:          string(model.StatusActive),
		// No last_sync_attempt_at, so a directory registered with an interval
		// is due at once rather than one interval from now. "I turned it on
		// and nothing happened all day" is the worse of the two surprises.
		SyncIntervalMinutes: narrow(normalized.SyncIntervalMinutes),
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.LDAPSource{}, ErrLDAPSourceNameTaken
		}
		return model.LDAPSource{}, fmt.Errorf("register directory: %w", err)
	}

	source, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return model.LDAPSource{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionLDAPSourceCreate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetLDAPSource, TargetID: id, TargetName: source.Name,
		Detail: fmt.Sprintf("%s:%d, base %s", source.Host, source.Port, source.BaseDN),
	})

	return source, nil
}

// Update changes a directory's settings.
func (s *DirectoryService) Update(ctx context.Context, actor auth.Principal, id string, in LDAPSourceInput) (model.LDAPSource, error) {
	tenantID := actor.TenantID

	if _, err := s.Get(ctx, tenantID, id); err != nil {
		return model.LDAPSource{}, err
	}

	normalized, err := s.normalize(in)
	if err != nil {
		return model.LDAPSource{}, err
	}

	now := store.Now()
	q := s.store.ForTenant(tenantID)

	err = q.UpdateLDAPSource(ctx, sqlcgen.UpdateLDAPSourceParams{
		ID:              id,
		Name:            normalized.Name,
		Host:            normalized.Host,
		Port:            narrow(normalized.Port),
		Encryption:      normalized.Encryption,
		BindDn:          normalized.BindDN,
		BaseDn:          normalized.BaseDN,
		UserFilter:      normalized.UserFilter,
		AttrUsername:    normalized.AttrUsername,
		AttrDisplayName: normalized.AttrDisplayName,
		AttrEmail:       normalized.AttrEmail,
		AttrPhone:       normalized.AttrPhone,
		AttrExternalID:  normalized.AttrExternalID,
		OrganizationID:  optionalID(normalized.OrganizationID),
		// Not last_sync_attempt_at: shortening an interval takes effect
		// against whenever the last attempt was, and lengthening one does not
		// hand the directory a fresh start it has not earned.
		SyncIntervalMinutes: narrow(normalized.SyncIntervalMinutes),
		UpdatedAt:           now,
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return model.LDAPSource{}, ErrLDAPSourceNameTaken
		}
		return model.LDAPSource{}, fmt.Errorf("update directory: %w", err)
	}

	// Only when the caller sent one. Omitting the field leaves the stored
	// credential alone; sending an empty string clears it, which is how an
	// operator moves a source to an anonymous bind.
	if normalized.BindPassword != nil {
		sealed := ""
		if *normalized.BindPassword != "" {
			sealed, err = s.seal(*normalized.BindPassword)
			if err != nil {
				return model.LDAPSource{}, err
			}
		}
		if err := q.UpdateLDAPSourceBindPassword(ctx, id, sealed, now); err != nil {
			return model.LDAPSource{}, fmt.Errorf("update bind password: %w", err)
		}
	}

	updated, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return model.LDAPSource{}, err
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionLDAPSourceUpdate,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetLDAPSource, TargetID: id, TargetName: updated.Name,
		Detail: fmt.Sprintf("%s:%d, base %s", updated.Host, updated.Port, updated.BaseDN),
	})

	return updated, nil
}

// SetStatus enables or disables a directory. A disabled one is not
// synchronized and its accounts are left exactly as they are — disabling the
// connector must not deactivate the people it brought in.
func (s *DirectoryService) SetStatus(ctx context.Context, actor auth.Principal, id string, status model.Status) (model.LDAPSource, error) {
	tenantID := actor.TenantID

	if _, err := s.Get(ctx, tenantID, id); err != nil {
		return model.LDAPSource{}, err
	}
	if err := s.store.ForTenant(tenantID).UpdateLDAPSourceStatus(ctx, id, string(status), store.Now()); err != nil {
		return model.LDAPSource{}, fmt.Errorf("set directory status: %w", err)
	}

	updated, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return model.LDAPSource{}, err
	}

	action := model.ActionLDAPSourceDisable
	if status == model.StatusActive {
		action = model.ActionLDAPSourceEnable
	}
	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetLDAPSource, TargetID: id, TargetName: updated.Name,
	})

	return updated, nil
}

// List returns the tenant's directories.
func (s *DirectoryService) List(ctx context.Context, tenantID string) ([]model.LDAPSource, error) {
	rows, err := s.store.ForTenant(tenantID).ListLDAPSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list directories: %w", err)
	}

	sources := make([]model.LDAPSource, 0, len(rows))
	for _, row := range rows {
		source, err := s.decorate(ctx, tenantID, row)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, nil
}

// Get returns one directory.
func (s *DirectoryService) Get(ctx context.Context, tenantID, id string) (model.LDAPSource, error) {
	row, err := s.store.ForTenant(tenantID).GetLDAPSource(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return model.LDAPSource{}, ErrLDAPSourceNotFound
		}
		return model.LDAPSource{}, fmt.Errorf("get directory: %w", err)
	}
	return s.decorate(ctx, tenantID, row)
}

// Runs returns a directory's recent synchronizations, newest first.
func (s *DirectoryService) Runs(ctx context.Context, tenantID, sourceID string, limit int) ([]model.LDAPSyncRun, error) {
	if _, err := s.Get(ctx, tenantID, sourceID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := s.store.ForTenant(tenantID).ListLDAPSyncRuns(ctx, sourceID, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list sync runs: %w", err)
	}

	runs := make([]model.LDAPSyncRun, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, model.LDAPSyncRun{
			ID: row.ID, SourceID: row.SourceID, ActorName: row.ActorName,
			StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
			Outcome:          row.Outcome,
			CreatedCount:     int(row.CreatedCount),
			UpdatedCount:     int(row.UpdatedCount),
			DeactivatedCount: int(row.DeactivatedCount),
			SkippedCount:     int(row.SkippedCount),
			SkippedDetail:    row.SkippedDetail,
			ErrorCode:        row.ErrorCode,
			Error:            row.Error,
		})
	}
	return runs, nil
}

func (s *DirectoryService) decorate(ctx context.Context, tenantID string, row sqlcgen.LdapSource) (model.LDAPSource, error) {
	source := model.LDAPSource{
		ID: row.ID, TenantID: row.TenantID, Name: row.Name,
		Host: row.Host, Port: int(row.Port), Encryption: row.Encryption,
		BindDN: row.BindDn, HasBindPassword: row.BindPassword != "",
		BaseDN: row.BaseDn, UserFilter: row.UserFilter,
		AttrUsername: row.AttrUsername, AttrDisplayName: row.AttrDisplayName,
		AttrEmail: row.AttrEmail, AttrPhone: row.AttrPhone,
		AttrExternalID:      row.AttrExternalID,
		Status:              model.Status(row.Status),
		SyncIntervalMinutes: int(row.SyncIntervalMinutes),
		LastSyncedAt:        row.LastSyncedAt,
		CreatedAt:           row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}

	if row.OrganizationID != nil {
		source.OrganizationID = *row.OrganizationID
		org, err := s.store.ForTenant(tenantID).GetOrganizationByID(ctx, *row.OrganizationID)
		if err == nil {
			source.OrganizationName = org.Name
		}
	}
	return source, nil
}

func (s *DirectoryService) seal(plaintext string) (string, error) {
	sealed, err := s.vault.Seal(plaintext)
	if errors.Is(err, secrets.ErrNotConfigured) {
		return "", ErrNoEncryptionKey
	}
	if err != nil {
		return "", fmt.Errorf("seal bind password: %w", err)
	}
	return sealed, nil
}

func (s *DirectoryService) normalize(in LDAPSourceInput) (LDAPSourceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.BindDN = strings.TrimSpace(in.BindDN)
	in.BaseDN = strings.TrimSpace(in.BaseDN)
	in.UserFilter = strings.TrimSpace(in.UserFilter)
	in.AttrUsername = strings.TrimSpace(in.AttrUsername)
	in.AttrDisplayName = strings.TrimSpace(in.AttrDisplayName)
	in.AttrEmail = strings.TrimSpace(in.AttrEmail)
	in.AttrPhone = strings.TrimSpace(in.AttrPhone)
	in.AttrExternalID = strings.TrimSpace(in.AttrExternalID)

	if in.Name == "" {
		in.Name = in.Host
	}

	switch in.Encryption {
	case "":
		in.Encryption = directory.EncryptionSTARTTLS
	case directory.EncryptionNone, directory.EncryptionSTARTTLS, directory.EncryptionTLS:
	default:
		return in, ErrInvalidLDAPEncryption
	}

	if in.Port == 0 {
		if in.Encryption == directory.EncryptionTLS {
			in.Port = 636
		} else {
			in.Port = 389
		}
	}
	if in.Port < 1 || in.Port > 65535 {
		return in, ErrInvalidLDAPPort
	}

	if in.SyncIntervalMinutes != 0 &&
		(in.SyncIntervalMinutes < MinSyncIntervalMinutes || in.SyncIntervalMinutes > MaxSyncIntervalMinutes) {
		return in, ErrInvalidSyncInterval
	}

	if in.Host == "" || in.BaseDN == "" || in.UserFilter == "" ||
		in.AttrUsername == "" || in.AttrDisplayName == "" || in.AttrExternalID == "" {
		return in, ErrLDAPFieldRequired
	}

	// A host with a scheme or a port glued on is the commonest paste, and it
	// produces a connection error far from where the mistake was made.
	if strings.Contains(in.Host, "://") || strings.Contains(in.Host, "/") {
		return in, httpx.BadRequest("INVALID_LDAP_HOST",
			"Give the host name on its own, without a scheme or a path. The port and encryption are separate fields.")
	}
	if _, _, err := net.SplitHostPort(in.Host); err == nil {
		return in, httpx.BadRequest("INVALID_LDAP_HOST",
			"Give the host name on its own; the port is a separate field.")
	}

	return in, nil
}

// narrow converts a count to the width the schema stores it in.
//
// The values are a port, already bounded by validation, and directory sizes,
// which will not realistically reach two billion. "Realistically" is not a
// bound though, and a silent wrap would write a negative count into a run
// record somebody is reading to decide whether a synchronization went wrong
// — so it saturates rather than wrapping.
func narrow(n int) int32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(n)
	}
}

func optionalID(id string) *string {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return &id
}

// SyncDue synchronizes the tenant's directories whose interval has elapsed,
// and returns the runs it performed. Nothing due is not an error.
//
// The caller supplies the time, as the sweeps do, so a test can ask what
// would happen tomorrow without waiting for it.
//
// It runs through SyncNow rather than beside it, with an actor that has no
// name. That is what keeps a scheduled run indistinguishable from a manual
// one everywhere it matters — the same run record, the same audit entry, the
// same refusal to act on an empty result — and the empty name is what the
// console renders as "scheduled", a distinction the schema reserved for it
// from the start.
//
// One directory's failure does not stop the next. A source pointed at a host
// that has gone away is a common state, and letting it hold up the other
// directories in the tenant would make one team's mistake everybody's.
//
// Claimed one at a time, immediately before each is read, rather than all at
// once up front. The difference shows when a pass does not finish: a directory
// this loop never reached has not been claimed, so it is still due — instead
// of carrying an attempt timestamp for a synchronization that never happened
// and waiting out an interval for a run nobody performed.
func (s *DirectoryService) SyncDue(ctx context.Context, tenantID string, now time.Time) ([]model.LDAPSyncRun, error) {
	q := s.store.ForTenant(tenantID)
	scheduler := auth.Principal{TenantID: tenantID}

	var runs []model.LDAPSyncRun
	for {
		// Terminates because the claim advances the row's attempt timestamp,
		// which is the same condition it selects on: every iteration removes
		// one directory from the due set and nothing puts one back.
		sourceID, err := q.ClaimNextDueLDAPSource(ctx, now)
		if store.IsNoRows(err) {
			return runs, nil
		}
		if err != nil {
			return runs, fmt.Errorf("claim due directory: %w", err)
		}

		// A failed synchronization is not an error here: it is a run record
		// saying why, which is the whole point of the run records. What
		// reaches this branch is the layer beneath failing — a source deleted
		// between the claim and the read, or a database that has gone away.
		run, err := s.SyncNow(ctx, scheduler, sourceID)
		if err != nil {
			slog.WarnContext(ctx, "scheduled directory synchronization could not run",
				"source", sourceID, "error", err)
			continue
		}
		runs = append(runs, run)
	}
}

// SyncNow reads the directory and reconciles what it returns against the
// accounts this source owns.
//
// The run record is opened before the directory is contacted and closed
// whatever happens, so a sync that dies mid-flight leaves evidence rather
// than nothing at all.
func (s *DirectoryService) SyncNow(ctx context.Context, actor auth.Principal, sourceID string) (model.LDAPSyncRun, error) {
	tenantID := actor.TenantID

	source, err := s.Get(ctx, tenantID, sourceID)
	if err != nil {
		return model.LDAPSyncRun{}, err
	}
	if source.Status != model.StatusActive {
		return model.LDAPSyncRun{}, ErrLDAPSourceDisabled
	}

	q := s.store.ForTenant(tenantID)
	runID := uuid.NewString()
	startedAt := store.Now()

	err = q.StartLDAPSyncRun(ctx, sqlcgen.StartLDAPSyncRunParams{
		ID: runID, SourceID: sourceID, ActorName: actor.Username, StartedAt: startedAt,
	})
	if err != nil {
		return model.LDAPSyncRun{}, fmt.Errorf("open sync run: %w", err)
	}

	counts, syncErr := s.runSync(ctx, tenantID, sourceID)

	outcome := model.SyncSucceeded
	message, code := "", ""
	if syncErr != nil {
		outcome = model.SyncFailed
		message = syncErr.Error()
		code = refusalCode(syncErr)
	}

	finishedAt := store.Now()
	if err := q.FinishLDAPSyncRun(ctx, sqlcgen.FinishLDAPSyncRunParams{
		ID: runID, FinishedAt: &finishedAt, Outcome: outcome,
		CreatedCount: narrow(counts.created), UpdatedCount: narrow(counts.updated),
		DeactivatedCount: narrow(counts.deactivated), SkippedCount: narrow(counts.skipped),
		SkippedDetail: counts.skips.String(),
		ErrorCode:     code, Error: message,
	}); err != nil {
		return model.LDAPSyncRun{}, fmt.Errorf("close sync run: %w", err)
	}

	if syncErr == nil {
		if err := q.MarkLDAPSourceSynced(ctx, sourceID, finishedAt); err != nil {
			return model.LDAPSyncRun{}, fmt.Errorf("mark synced: %w", err)
		}
	}

	s.audit.Log(ctx, tenantID, AuditEntry{
		Kind: model.LogOperation, Action: model.ActionLDAPSync,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: targetLDAPSource, TargetID: sourceID, TargetName: source.Name,
		Result: syncResult(syncErr),
		Detail: fmt.Sprintf("created %d, updated %d, deactivated %d, skipped %d",
			counts.created, counts.updated, counts.deactivated, counts.skipped),
	})

	return model.LDAPSyncRun{
		ID: runID, SourceID: sourceID, ActorName: actor.Username,
		StartedAt: startedAt, FinishedAt: &finishedAt, Outcome: outcome,
		CreatedCount: counts.created, UpdatedCount: counts.updated,
		DeactivatedCount: counts.deactivated, SkippedCount: counts.skipped,
		SkippedDetail: counts.skips.String(),
		ErrorCode:     code, Error: message,
	}, nil
}

type syncCounts struct {
	created, updated, deactivated, skipped int
	skips                                  skipLog
}

// skipLog is why entries were skipped, grouped by reason.
//
// Grouped rather than listed: a source pointed at the wrong attribute skips
// every entry for the same reason, and a line per entry would be a row per
// account in the directory. What an operator needs is the shape — "5 ×
// invalid phone number, 1 × username already taken" — and one example of
// each to go and look at.
type skipLog struct {
	order    []string
	byReason map[string]*skipGroup
}

type skipGroup struct {
	count    int
	examples []string
}

// maxSkipExamples bounds one reason's examples, and maxSkipReasons bounds how
// many reasons are kept. A directory can be wrong in more ways than a column
// can hold, and a run record is not a log.
const (
	maxSkipExamples = 3
	maxSkipReasons  = 5
)

// record adds one skipped entry. The subject is whatever identifies it to
// somebody about to go and look: a username where there is one, and nothing
// where there is not — an entry skipped for having no username is precisely
// the one that cannot be named by it.
func (l *skipLog) record(subject string, err error) {
	if l.byReason == nil {
		l.byReason = map[string]*skipGroup{}
	}

	reason := skipReason(err)
	group, known := l.byReason[reason]
	if !known {
		group = &skipGroup{}
		l.byReason[reason] = group
		l.order = append(l.order, reason)
	}
	group.count++
	if len(group.examples) < maxSkipExamples && subject != "" {
		group.examples = append(group.examples, subject)
	}
}

// skipReason is the sentence an operator reads. An httpx error already
// carries one written for a reader; anything else is reported as it is.
func skipReason(err error) string {
	var httpErr *httpx.Error
	if errors.As(err, &httpErr) {
		return httpErr.Message
	}
	return err.Error()
}

// String renders the groups for the run record, bounded at both ends.
func (l skipLog) String() string {
	if len(l.order) == 0 {
		return ""
	}

	reasons := l.order
	omitted := 0
	if len(reasons) > maxSkipReasons {
		omitted = len(reasons) - maxSkipReasons
		reasons = reasons[:maxSkipReasons]
	}

	parts := make([]string, 0, len(reasons)+1)
	for _, reason := range reasons {
		group := l.byReason[reason]
		part := fmt.Sprintf("%d × %s", group.count, reason)
		if len(group.examples) > 0 {
			part += " (" + strings.Join(group.examples, ", ")
			if group.count > len(group.examples) {
				part += ", …"
			}
			part += ")"
		}
		parts = append(parts, part)
	}
	if omitted > 0 {
		parts = append(parts, fmt.Sprintf("and %d further reason(s)", omitted))
	}
	return strings.Join(parts, "; ")
}

func syncResult(err error) model.LogResult {
	if err != nil {
		return model.LogFailure
	}
	return model.LogSuccess
}

// ErrDirectoryReturnedNothing stops the single worst thing this code could
// do.
//
// A search that matches nothing looks exactly like a directory in which
// everybody has left. The first is a typo in a base DN or a filter and
// happens regularly; the second essentially never happens, and if it did,
// nobody would want it applied automatically at three in the morning. So an
// empty result set against a source that owns accounts fails the run and
// changes nothing, and an operator reads the reason.
var ErrDirectoryReturnedNothing = refusal{
	code: "DIRECTORY_RETURNED_NOTHING",
	text: "The directory returned no entries while this source owns accounts here. " +
		"Nothing was changed: an empty result is far more often a wrong base DN or " +
		"user filter than a directory everyone has left, and acting on it would " +
		"deactivate every one of those accounts.",
}

// directoryEntriesUnreadable is the other way to end a run with nothing to
// reconcile against, and it is not the same fault.
//
// The directory answered, with entries, and the reader could make an account
// of none of them — no username, or no external id, because the attribute
// map names a field that is not there. That is the commonest way to
// misconfigure a source.
//
// It used to report ErrDirectoryReturnedNothing, whose troubleshooting entry
// sends the reader to the base DN and the user filter. Those are precisely
// the two things that were working: the search matched, and every match was
// then discarded. Somebody following that advice changes the one part of the
// configuration that was right.
//
// Which attributes and how many of each are already on the run, in
// skippedDetail, which is written whether the run succeeded or failed.
func directoryEntriesUnreadable(count int) refusal {
	return refusal{
		code: "DIRECTORY_ENTRIES_UNREADABLE",
		text: fmt.Sprintf("The directory returned %d entries and none of them "+
			"could be read as an account, while this source owns accounts "+
			"here. Nothing was changed. The base DN and the user filter "+
			"matched, so what to check is the attribute map — the username "+
			"and external id attributes above all. The reasons are on this "+
			"run.", count),
	}
}

// refusal is a failure Portico decided on rather than one the directory
// reported, and the difference is what decides whether a console can
// translate it.
//
// An error from the LDAP server keeps its own wording: "No Such Object" is
// what an operator will paste into a search engine, and a translated version
// would be worse than useless. A refusal made here has a code, so the reader
// sees it in their own language.
type refusal struct {
	code string
	text string
}

func (r refusal) Error() string { return r.text }

func refusalCode(err error) string {
	var r refusal
	if errors.As(err, &r) {
		return r.code
	}
	return ""
}

func (s *DirectoryService) runSync(ctx context.Context, tenantID, sourceID string) (syncCounts, error) {
	var counts syncCounts

	q := s.store.ForTenant(tenantID)

	row, err := q.GetLDAPSource(ctx, sourceID)
	if err != nil {
		return counts, fmt.Errorf("read directory: %w", err)
	}

	bindPassword, err := s.vault.Open(row.BindPassword)
	if err != nil {
		return counts, fmt.Errorf("read bind password: %w", err)
	}

	client, err := s.dial(directory.Config{
		Host: row.Host, Port: int(row.Port), Encryption: row.Encryption,
		BindDN: row.BindDn, BindPassword: bindPassword,
		BaseDN: row.BaseDn, UserFilter: row.UserFilter,
		Attributes: directory.AttributeMap{
			Username: row.AttrUsername, DisplayName: row.AttrDisplayName,
			Email: row.AttrEmail, Phone: row.AttrPhone, ExternalID: row.AttrExternalID,
		},
		Timeout: 60 * time.Second,
	})
	if err != nil {
		return counts, err
	}
	defer client.Close()

	entries, skipped, err := client.Users()
	if err != nil {
		return counts, err
	}
	counts.skipped = len(skipped)
	for _, err := range skipped {
		// Entries the directory returned that could not be read as an account
		// at all — no username, no external id. There is nothing to name them
		// by beyond what the error carries.
		counts.skips.record("", err)
	}

	owned, err := q.ListUsersFromLDAPSource(ctx, sourceID)
	if err != nil {
		return counts, fmt.Errorf("read owned accounts: %w", err)
	}

	if len(entries) == 0 && len(owned) > 0 {
		// Two ways to arrive here, and they send an operator to opposite
		// ends of the configuration. See directoryEntriesUnreadable.
		if len(skipped) > 0 {
			return counts, directoryEntriesUnreadable(len(skipped))
		}
		return counts, ErrDirectoryReturnedNothing
	}

	byExternalID := make(map[string]sqlcgen.User, len(owned))
	for _, user := range owned {
		if user.ExternalID != nil {
			byExternalID[*user.ExternalID] = user
		}
	}

	seen := make(map[string]bool, len(entries))
	actor := auth.Principal{TenantID: tenantID, Username: DirectoryActor, Role: model.RoleSuperAdmin}

	for _, entry := range entries {
		seen[entry.ExternalID] = true

		existing, known := byExternalID[entry.ExternalID]
		if known {
			changed, err := s.updateFromDirectory(ctx, actor, existing, entry)
			if err != nil {
				counts.skipped++
				counts.skips.record(entry.Username, err)
				continue
			}
			if changed {
				counts.updated++
			}
			continue
		}

		created, err := s.createFromDirectory(ctx, tenantID, sourceID, entry, row)
		if err != nil {
			counts.skips.record(entry.Username, err)
			// A username another account already holds, most often. Skipped
			// rather than fatal, and never resolved by taking the name: an
			// administrator's account must not be silently re-owned by a
			// directory because somebody there happens to share a username.
			counts.skipped++
			continue
		}
		if created {
			counts.created++
		}
	}

	for externalID, user := range byExternalID {
		if seen[externalID] || user.Status != string(model.StatusActive) {
			continue
		}
		if _, err := s.users.SetStatus(ctx, actor, user.ID, model.StatusDisabled); err != nil {
			counts.skipped++
			counts.skips.record(user.Username, err)
			continue
		}
		counts.deactivated++
	}

	return counts, nil
}

// DirectoryActor is who a synchronization's changes are attributed to.
//
// Not a user id, because there is no account: the scheduler ran, or an
// administrator pressed a button and that press is audited separately as
// LDAP_SYNC. Recording a real administrator against every account the sync
// touched would put thousands of entries in the trail under somebody who
// made one decision.
const DirectoryActor = "directory sync"

func (s *DirectoryService) createFromDirectory(ctx context.Context, tenantID, sourceID string, entry directory.Entry, row sqlcgen.LdapSource) (bool, error) {
	organizationID := ""
	if row.OrganizationID != nil {
		organizationID = *row.OrganizationID
	}

	user, err := s.users.Create(ctx, tenantID, CreateUserInput{
		Username:       entry.Username,
		DisplayName:    entry.DisplayName,
		Password:       uuid.NewString(),
		Phone:          entry.Phone,
		Email:          entry.Email,
		Role:           model.RoleUser,
		OrganizationID: organizationID,
		Source:         model.SourceLDAP,
	})
	if err != nil {
		return false, err
	}

	now := store.Now()
	q := s.store.ForTenant(tenantID)
	if err := q.SetUserExternalID(ctx, sqlcgen.SetUserExternalIDParams{
		ID: user.ID, ExternalID: &entry.ExternalID, UpdatedAt: now,
	}); err != nil {
		return false, err
	}
	if err := q.BindUserToLDAPSource(ctx, user.ID, sourceID, now); err != nil {
		return false, err
	}
	return true, nil
}

func (s *DirectoryService) updateFromDirectory(ctx context.Context, actor auth.Principal, existing sqlcgen.User, entry directory.Entry) (bool, error) {
	// Whatever it is now, which for an existing account is this side's
	// decision. A source names an organization so that the accounts it
	// *creates* land somewhere; reasserting it on every run would undo an
	// administrator's move the next time anything else about that person
	// changed — surviving until then, and then silently not. The directory
	// says who somebody is; where they belong is answered here.
	organizationID := ""
	if existing.OrganizationID != nil {
		organizationID = *existing.OrganizationID
	}

	sameDetails := existing.DisplayName == entry.DisplayName &&
		existing.Email == entry.Email &&
		existing.Phone == entry.Phone

	// A rename is its own change, and its own statement. The details above go
	// through UserService.Update, which does not write a username — in the
	// console a rename is an administrator's decision, and the form that
	// edits somebody would otherwise carry one along. Here the directory is
	// the system of record: the external id already said this is the same
	// person, so declining the new name would leave the two sides
	// permanently disagreeing, with every later run attempting it again.
	renamed := existing.Username != entry.Username

	// An account the directory still lists is an account that should work
	// here, so a previous deactivation is undone. That is the whole point of
	// treating a directory as the source of truth, and it is also why
	// disabling the *source* leaves its accounts alone: otherwise the two
	// controls would fight.
	reactivate := existing.Status != string(model.StatusActive)

	if sameDetails && !renamed && !reactivate {
		return false, nil
	}

	if renamed {
		q := s.store.ForTenant(actor.TenantID)
		if err := q.RenameUser(ctx, sqlcgen.RenameUserParams{
			ID: existing.ID, Username: entry.Username, UpdatedAt: store.Now(),
		}); err != nil {
			// A name the directory has moved onto somebody else's account
			// here. Reported as the collision it is, so the entry is skipped
			// with a reason rather than failing the run.
			if taken := takenFieldError(err); taken != nil {
				return false, taken
			}
			return false, fmt.Errorf("rename user: %w", err)
		}
	}

	if !sameDetails {
		if _, err := s.users.Update(ctx, actor, existing.ID, UpdateUserInput{
			DisplayName:    entry.DisplayName,
			Phone:          entry.Phone,
			Email:          entry.Email,
			Role:           model.Role(existing.Role),
			OrganizationID: organizationID,
		}); err != nil {
			return false, err
		}
	}
	if reactivate {
		if _, err := s.users.SetStatus(ctx, actor, existing.ID, model.StatusActive); err != nil {
			return false, err
		}
	}
	return true, nil
}
