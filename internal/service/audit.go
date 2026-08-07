package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/paraview/portico/internal/model"
	"github.com/paraview/portico/internal/store"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// AuditEntry describes an event to record.
//
// The tenant is not a field here: it is a separate argument to Record and
// Log, so that adding a field to this struct can never be the reason an
// event lands in the wrong tenant's trail.
type AuditEntry struct {
	Kind   model.LogKind
	Action string
	Result model.LogResult

	ActorID   string
	ActorName string

	TargetType string
	TargetID   string
	TargetName string

	Detail string
	IP     string
}

// auditWriteTimeout bounds a write that has been detached from its request's
// cancellation.
const auditWriteTimeout = 5 * time.Second

// AuditService writes and queries the audit trail.
type AuditService struct {
	store *store.Store
}

// NewAuditService returns a service backed by st.
func NewAuditService(st *store.Store) *AuditService {
	return &AuditService{store: st}
}

// Record writes one entry into a tenant's trail.
//
// A failure to write the audit trail must not fail the operation being
// audited — a user should not be unable to log in because logging is broken.
// The error is returned so callers may inspect it, but Log is the usual
// entry point and swallows it after logging.
func (s *AuditService) Record(ctx context.Context, tenantID string, e AuditEntry) error {
	if e.Result == "" {
		e.Result = model.LogSuccess
	}

	// The write outlives the request that caused it. Cancellation propagates
	// from the caller's connection, and a client that closes the tab —
	// exactly what someone does after submitting a form they expect an email
	// from — would otherwise take the audit entry with it. The event happened
	// whether or not anyone is still listening, so the record has to be made.
	//
	// The deadline is dropped along with the cancellation, so a bounded one
	// is put back: an audit write must not become the thing that hangs.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
	defer cancel()

	var actorID *string
	if e.ActorID != "" {
		actorID = &e.ActorID
	}

	return s.store.ForTenant(tenantID).CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		ID:            uuid.NewString(),
		Kind:          string(e.Kind),
		Action:        e.Action,
		ActorID:       actorID,
		ActorUsername: e.ActorName,
		TargetType:    e.TargetType,
		TargetID:      e.TargetID,
		TargetName:    e.TargetName,
		Result:        string(e.Result),
		Detail:        e.Detail,
		Ip:            e.IP,
		CreatedAt:     store.Now(),
	})
}

// Log records an entry, reporting a write failure to the process log rather
// than to the caller.
func (s *AuditService) Log(ctx context.Context, tenantID string, e AuditEntry) {
	if err := s.Record(ctx, tenantID, e); err != nil {
		slog.ErrorContext(ctx, "failed to write audit log",
			"kind", e.Kind, "action", e.Action, "tenant_id", tenantID, "error", err)
	}
}

// AuditQuery filters a log listing.
type AuditQuery struct {
	// Kind restricts results to one log kind; empty means all kinds.
	Kind model.LogKind
	// Action restricts results to one action verb; empty means all.
	Action string
	// Keyword matches the actor or target name.
	Keyword string
	// From and To bound created_at. Zero values are unbounded.
	From time.Time
	To   time.Time
}

// List returns a page of a tenant's log entries, newest first.
//
// This query is hand-written rather than generated because the filters are
// optional: sqlc would need a separate query per combination. The tenant
// predicate stays in the SQL text so it is visible here and checked by the
// guard test in internal/store.
func (s *AuditService) List(ctx context.Context, tenantID string, q AuditQuery, page Page) ([]model.AuditLog, int64, error) {
	f := tenantFilters(tenantID)

	if q.Kind != "" {
		f.Add("kind = %s", string(q.Kind))
	}
	if q.Action != "" {
		f.Add("action = %s", q.Action)
	}
	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		pattern := "%" + escapeLike(keyword) + "%"
		f.Add(`(actor_username LIKE %s ESCAPE '\' OR target_name LIKE %s ESCAPE '\')`, pattern, pattern)
	}
	if !q.From.IsZero() {
		f.Add("created_at >= %s", q.From.UTC())
	}
	if !q.To.IsZero() {
		f.Add("created_at <= %s", q.To.UTC())
	}

	clause := f.And()

	var total int64
	if err := s.store.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_logs WHERE tenant_id = $1"+clause, f.Args()...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	pageClause, args := f.Paginate(page)
	rows, err := s.store.DB().QueryContext(ctx,
		`SELECT id, kind, action, actor_id, actor_username, target_type, target_id,
		        target_name, result, detail, ip, created_at
		 FROM audit_logs WHERE tenant_id = $1`+clause+`
		 ORDER BY created_at DESC, id DESC`+pageClause, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var logs []model.AuditLog
	for rows.Next() {
		var (
			log     model.AuditLog
			actorID *string
			created time.Time
		)
		if err := rows.Scan(
			&log.ID, &log.Kind, &log.Action, &actorID, &log.ActorName,
			&log.TargetType, &log.TargetID, &log.TargetName,
			&log.Result, &log.Detail, &log.IP, &created,
		); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}
		if actorID != nil {
			log.ActorID = *actorID
		}
		log.CreatedAt = created
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit logs: %w", err)
	}

	return logs, total, nil
}
