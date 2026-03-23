package store

import (
	"context"
	"fmt"
	"time"

	"github.com/5g-lmf/common/clients"
	"github.com/5g-lmf/common/types"
)

const defaultSessionTTL = 300 * time.Second

// SessionStore manages LCS sessions in Redis
type SessionStore struct {
	redis *clients.RedisClient
}

// NewSessionStore creates a new session store
func NewSessionStore(redis *clients.RedisClient) *SessionStore {
	return &SessionStore{redis: redis}
}

// SetSession stores a session in Redis
func (s *SessionStore) SetSession(ctx context.Context, session *types.LcsSession) error {
	key := clients.SessionKey(session.SessionID)
	if err := s.redis.SetJSON(ctx, key, session, defaultSessionTTL); err != nil {
		return fmt.Errorf("storing session: %w", err)
	}
	// Create SUPI reverse index
	if session.Supi != "" {
		if err := s.redis.SetAdd(ctx, clients.SupiIndexKey(session.Supi), session.SessionID, defaultSessionTTL); err != nil {
			return fmt.Errorf("creating supi index: %w", err)
		}
	}
	return nil
}

// GetSession retrieves a session by ID
func (s *SessionStore) GetSession(ctx context.Context, sessionID string) (*types.LcsSession, error) {
	key := clients.SessionKey(sessionID)
	var session types.LcsSession
	if err := s.redis.GetJSON(ctx, key, &session); err != nil {
		if err == clients.ErrSessionNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting session: %w", err)
	}
	return &session, nil
}

// GetSessionBySupi retrieves the active session for a SUPI
func (s *SessionStore) GetSessionBySupi(ctx context.Context, supi string) (*types.LcsSession, error) {
	members, err := s.redis.SetMembers(ctx, clients.SupiIndexKey(supi))
	if err != nil {
		return nil, fmt.Errorf("getting supi index: %w", err)
	}
	if len(members) == 0 {
		return nil, ErrNotFound
	}
	// Return the first active session
	return s.GetSession(ctx, members[0])
}

// UpdateSessionStatus atomically updates the status of a session
func (s *SessionStore) UpdateSessionStatus(ctx context.Context, sessionID string, status types.SessionStatus) error {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	session.Status = status
	return s.SetSession(ctx, session)
}

// DeleteSession removes a session from Redis
func (s *SessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	session, err := s.GetSession(ctx, sessionID)
	if err == nil && session.Supi != "" {
		_ = s.redis.SetRemove(ctx, clients.SupiIndexKey(session.Supi), sessionID)
	}
	return s.redis.Delete(ctx, clients.SessionKey(sessionID))
}

// ErrNotFound is returned when a session does not exist
var ErrNotFound = fmt.Errorf("session not found")
