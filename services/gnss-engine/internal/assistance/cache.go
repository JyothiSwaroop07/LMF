package assistance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// Canonical TTLs for each assistance data category (3GPP TS 37.355 guidance).
const (
	TTLRefTime     = 30 * time.Second
	TTLEphemeris   = 2 * time.Hour
	TTLAlmanac     = 24 * time.Hour
	TTLIonospheric = 30 * time.Minute
	TTLDiffCorr    = 60 * time.Second
)

const cacheKeyPrefix = "gnss:assist:"

// AssistanceDataCache is a Redis-backed cache for GNSS assistance data.
// All values are JSON-serialised so that heterogeneous types can be stored
// under typed keys without separate Redis data structures.
type AssistanceDataCache struct {
	client redis.UniversalClient
	logger *zap.Logger
}

// NewAssistanceDataCache returns a new cache wrapping the given Redis client.
func NewAssistanceDataCache(client redis.UniversalClient, logger *zap.Logger) *AssistanceDataCache {
	return &AssistanceDataCache{client: client, logger: logger}
}

// Set serialises data to JSON and stores it under the prefixed dataType key
// with the given TTL. A TTL of 0 means no expiry.
func (c *AssistanceDataCache) Set(ctx context.Context, dataType string, data interface{}, ttl time.Duration) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("cache marshal %q: %w", dataType, err)
	}
	key := cacheKeyPrefix + dataType
	if err := c.client.Set(ctx, key, b, ttl).Err(); err != nil {
		return fmt.Errorf("cache redis set %q: %w", key, err)
	}
	c.logger.Debug("assistance data cached",
		zap.String("type", dataType),
		zap.Duration("ttl", ttl),
		zap.Int("bytes", len(b)),
	)
	return nil
}

// Get retrieves the value stored under dataType and deserialises it into
// target (which must be a pointer).  Returns (false, nil) on a cache miss,
// (true, nil) on a hit, and (false, err) on a Redis or unmarshal error.
func (c *AssistanceDataCache) Get(ctx context.Context, dataType string, target interface{}) (bool, error) {
	key := cacheKeyPrefix + dataType
	b, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cache redis get %q: %w", key, err)
	}
	if err := json.Unmarshal(b, target); err != nil {
		return false, fmt.Errorf("cache unmarshal %q: %w", dataType, err)
	}
	return true, nil
}

// TTLFor returns the canonical TTL for a given assistance data type identifier.
// Unknown types fall back to 5 minutes.
func TTLFor(dataType string) time.Duration {
	switch dataType {
	case "ref_time":
		return TTLRefTime
	case "ephemeris":
		return TTLEphemeris
	case "almanac":
		return TTLAlmanac
	case "ionospheric":
		return TTLIonospheric
	case "diff_corr":
		return TTLDiffCorr
	default:
		return 5 * time.Minute
	}
}
