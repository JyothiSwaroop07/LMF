package measurementstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	keyPrefix = "lmf:gnss:measurements:"
	ttl       = 60 * time.Second
)

type Store struct {
	client *redis.ClusterClient
	logger *zap.Logger
}

func NewStore(client *redis.ClusterClient, logger *zap.Logger) *Store {
	return &Store{client: client, logger: logger}
}

func (s *Store) Store(ctx context.Context, sessionID string, payload []byte) error {
	key := keyPrefix + sessionID
	if err := s.client.Set(ctx, key, payload, ttl).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key, err)
	}
	s.logger.Info("measurements stored",
		zap.String("key", key),
		zap.Int("bytes", len(payload)),
	)
	return nil
}
