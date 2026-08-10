package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/store"
)

// SessionService reads and ends individual sign-ins.
//
// Portico's own credential is still a JWT, and this does not turn it into a
// server-side session in the usual sense: the token is self-describing and
// is not stored. What the table adds is a name for each sign-in, so that
// "sign out" can mean this one rather than all of them, and so that somebody
// can look at what is signed in as them and end the one they do not
// recognize.
//
// The cost is a second read per authenticated request. It is small because
// the middleware was already reading the account on every request — that is
// what makes a disable take effect immediately — so this rides along on a
// round trip that was happening anyway.
type SessionService struct {
	store *store.Store
	audit *AuditService
}

// NewSessionService wires the service.
func NewSessionService(st *store.Store, audit *AuditService) *SessionService {
	return &SessionService{store: st, audit: audit}
}

// ErrSessionNotFound is returned when no live session has that id.
var ErrSessionNotFound = httpx.NotFound("SESSION_NOT_FOUND", "No such session.")

// touchInterval is how stale last_seen_at is allowed to get.
//
// Writing it on every request would turn every authenticated read into a
// write, which is a real cost for a column whose only consumer is a list
// showing "last used". A minute is well inside what that list needs.
const touchInterval = time.Minute

// CheckSession implements auth.SessionLookup: it reports whether the session
// a token names is still live, and records that it was used.
func (s *SessionService) CheckSession(ctx context.Context, tenantID, sessionID string) error {
	if sessionID == "" {
		return auth.ErrSessionNotLive
	}

	q := s.store.ForTenant(tenantID)
	now := store.Now()

	row, err := q.GetLiveSession(ctx, sessionID, now)
	if err != nil {
		if store.IsNoRows(err) {
			return auth.ErrSessionNotLive
		}
		return fmt.Errorf("look up session: %w", err)
	}

	if now.Sub(row.LastSeenAt) >= touchInterval {
		if err := q.TouchSession(ctx, sessionID, now); err != nil {
			// Losing a timestamp update is not worth failing a request the
			// caller is entitled to make.
			return nil
		}
	}
	return nil
}

// List returns an account's live sessions, most recently used first.
func (s *SessionService) List(ctx context.Context, tenantID, userID, currentSessionID string) ([]model.Session, error) {
	rows, err := s.store.ForTenant(tenantID).ListSessionsForUser(ctx, userID, store.Now())
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	sessions := make([]model.Session, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, model.Session{
			ID:        row.ID,
			IP:        row.Ip,
			UserAgent: row.UserAgent,
			// Marked so the screen can say which row is the reader, and so
			// that ending it can warn rather than surprise.
			Current:    row.ID == currentSessionID,
			CreatedAt:  row.CreatedAt,
			LastSeenAt: row.LastSeenAt,
			ExpiresAt:  row.ExpiresAt,
		})
	}
	return sessions, nil
}

// Revoke ends one session belonging to userID.
//
// The owner is a parameter rather than taken from the session row, so that
// a caller can only ever end a session they were entitled to name: their
// own, or — for an administrator — one belonging to the account they asked
// about. Without it, a session id from anywhere would do.
func (s *SessionService) Revoke(ctx context.Context, actor auth.Principal, userID, sessionID string) error {
	q := s.store.ForTenant(actor.TenantID)

	row, err := q.GetLiveSession(ctx, sessionID, store.Now())
	if err != nil || row.UserID != userID {
		// Same answer either way: a caller probing session ids learns
		// nothing about which exist.
		return ErrSessionNotFound
	}

	if err := q.RevokeSession(ctx, sessionID, store.Now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	action := model.ActionSessionRevoke
	if userID == actor.UserID {
		action = model.ActionSessionRevokeSelf
	}
	s.audit.Log(ctx, actor.TenantID, AuditEntry{
		Kind: model.LogOperation, Action: action,
		ActorID: actor.UserID, ActorName: actor.Username,
		TargetType: "SESSION", TargetID: sessionID,
		Detail: "from " + describeSession(row.Ip, row.UserAgent),
	})
	return nil
}

// RevokeAllForUser ends every session an account holds.
func (s *SessionService) RevokeAllForUser(ctx context.Context, tenantID, userID string) error {
	if err := s.store.ForTenant(tenantID).RevokeSessionsForUser(ctx, userID, store.Now()); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}

func describeSession(ip, userAgent string) string {
	if ip == "" && userAgent == "" {
		return "an unrecorded address"
	}
	if userAgent == "" {
		return ip
	}
	return ip + " (" + userAgent + ")"
}
