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
	"github.com/paraview/portico/internal/store/dbtime"
	"github.com/paraview/portico/internal/store/sqlcgen"
)

// AuditEntry describes an event to record.
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

// AuditService writes and queries the audit trail.
type AuditService struct {
	store *store.Store
}

// NewAuditService returns a service backed by st.
func NewAuditService(st *store.Store) *AuditService {
	return &AuditService{store: st}
}

// Record writes one entry.
//
// A failure to write the audit trail must not fail the operation being
// audited — a user should not be unable to log in because logging is broken.
// The error is returned so callers may inspect it, but Log is the usual
// entry point and swallows it after logging.
func (s *AuditService) Record(ctx context.Context, e AuditEntry) error {
	if e.Result == "" {
		e.Result = model.LogSuccess
	}

	var actorID *string
	if e.ActorID != "" {
		actorID = &e.ActorID
	}

	return s.store.Queries.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
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
func (s *AuditService) Log(ctx context.Context, e AuditEntry) {
	if err := s.Record(ctx, e); err != nil {
		slog.ErrorContext(ctx, "failed to write audit log",
			"kind", e.Kind, "action", e.Action, "error", err)
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

// List returns a page of log entries, newest first.
//
// This query is hand-written rather than generated because the filters are
// optional: sqlc would need a separate query per combination.
func (s *AuditService) List(ctx context.Context, q AuditQuery, page Page) ([]model.AuditLog, int64, error) {
	var (
		where []string
		args  []any
	)

	if q.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, string(q.Kind))
	}
	if q.Action != "" {
		where = append(where, "action = ?")
		args = append(args, q.Action)
	}
	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		where = append(where, `(actor_username LIKE ? ESCAPE '\' OR target_name LIKE ? ESCAPE '\')`)
		pattern := "%" + escapeLike(keyword) + "%"
		args = append(args, pattern, pattern)
	}
	if !q.From.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, q.From.UTC().Format(time.RFC3339))
	}
	if !q.To.IsZero() {
		where = append(where, "created_at <= ?")
		args = append(args, q.To.UTC().Format(time.RFC3339))
	}

	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	if err := s.store.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_logs"+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := s.store.DB().QueryContext(ctx,
		`SELECT id, kind, action, actor_id, actor_username, target_type, target_id,
		        target_name, result, detail, ip, created_at
		 FROM audit_logs`+clause+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT ? OFFSET ?`,
		append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var logs []model.AuditLog
	for rows.Next() {
		var (
			log     model.AuditLog
			actorID *string
			created dbtime.Time
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
		log.CreatedAt = created.Time
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit logs: %w", err)
	}

	return logs, total, nil
}
