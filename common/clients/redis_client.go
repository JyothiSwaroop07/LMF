package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/5g-lmf/common/config"
	"github.com/redis/go-redis/v9"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

const (
	sessionKeyPrefix    = "lmf:session:"
	supiIndexKeyPrefix  = "lmf:supi:"
	defaultSessionTTL   = 300 * time.Second
)

// RedisClient wraps the go-redis cluster client with LMF-specific operations
type RedisClient struct {
	client *redis.ClusterClient
}

// NewRedisClient creates a new Redis cluster client
func NewRedisClient(cfg *config.Config) (*RedisClient, error) {
	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:      cfg.Redis.Addresses,
		Password:   cfg.Redis.Password,
		MaxRetries: cfg.Redis.MaxRetries,
		PoolSize:   cfg.Redis.PoolSize,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connecting to redis: %w", err)
	}

	return &RedisClient{client: client}, nil
}

// Ping checks Redis connectivity
func (r *RedisClient) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// SetJSON serializes value to JSON and stores it with given TTL
func (r *RedisClient) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshalling value: %w", err)
	}
	return r.client.Set(ctx, key, data, ttl).Err()
}

// GetJSON retrieves and deserializes a JSON value
func (r *RedisClient) GetJSON(ctx context.Context, key string, target interface{}) error {
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("getting key %s: %w", key, err)
	}
	return json.Unmarshal(data, target)
}

// Delete removes a key from Redis
func (r *RedisClient) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// SetAdd adds a member to a Redis set
func (r *RedisClient) SetAdd(ctx context.Context, key, member string, ttl time.Duration) error {
	pipe := r.client.Pipeline()
	pipe.SAdd(ctx, key, member)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// SetRemove removes a member from a Redis set
func (r *RedisClient) SetRemove(ctx context.Context, key, member string) error {
	return r.client.SRem(ctx, key, member).Err()
}

// SetMembers returns all members of a Redis set
func (r *RedisClient) SetMembers(ctx context.Context, key string) ([]string, error) {
	return r.client.SMembers(ctx, key).Result()
}

// Exists checks whether a key exists
func (r *RedisClient) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	return n > 0, err
}

// Close closes the Redis client
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// SessionKey returns the Redis key for a session
func SessionKey(sessionID string) string {
	return sessionKeyPrefix + sessionID
}

// SupiIndexKey returns the Redis key for the SUPI → sessionId index
func SupiIndexKey(supi string) string {
	return supiIndexKeyPrefix + supi
}
