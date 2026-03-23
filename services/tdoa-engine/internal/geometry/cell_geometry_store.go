// Package geometry provides a Redis-backed cell geometry store with an
// in-memory L1 cache for the DL-TDOA positioning engine.
package geometry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"

	"github.com/5g-lmf/tdoa-engine/internal/positioning"
)

const (
	// L1 in-memory cache TTL.  Cells rarely change; 1 h is safe.
	cellMemCacheTTL = time.Hour

	// Redis key prefix.
	redisCellPrefix = "tdoa:cell:"

	// Redis persistence TTL — long-lived; cells don't change.
	redisCellTTL = 24 * time.Hour

	// Eviction background interval.
	evictInterval = 10 * time.Minute
)

// cacheEntry wraps a CellGeometry with its expiry timestamp.
type cacheEntry struct {
	cell      positioning.CellGeometry
	expiresAt time.Time
}

// CellGeometryStore provides cell geometry retrieval with a two-level cache:
//
//  1. In-memory map (L1) with TTL-based eviction.
//  2. Redis cluster (L2) for persistence across service restarts.
type CellGeometryStore struct {
	redis  redis.UniversalClient
	logger *zap.Logger

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

// NewCellGeometryStore creates a new store and starts the background eviction loop.
func NewCellGeometryStore(rdb redis.UniversalClient, logger *zap.Logger) *CellGeometryStore {
	s := &CellGeometryStore{
		redis:  rdb,
		logger: logger,
		cache:  make(map[string]cacheEntry),
	}
	go s.evictLoop()
	return s
}

// GetCellGeometry retrieves a single cell by its NR Cell Identity.
// Check order: L1 memory cache → Redis.
// Returns an error if the cell is not found in either layer.
func (s *CellGeometryStore) GetCellGeometry(ctx context.Context, nci string) (*positioning.CellGeometry, error) {
	// L1 lookup.
	if cell, ok := s.fromMemCache(nci); ok {
		return cell, nil
	}

	// L2 Redis lookup.
	key := redisCellPrefix + nci
	b, err := s.redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("cell %q not found", nci)
	}
	if err != nil {
		return nil, fmt.Errorf("redis get cell %q: %w", nci, err)
	}

	var cell positioning.CellGeometry
	if err := json.Unmarshal(b, &cell); err != nil {
		return nil, fmt.Errorf("unmarshal cell %q: %w", nci, err)
	}

	s.storeMemCache(nci, cell)
	return &cell, nil
}

// GetMultipleCells retrieves several cells in a single batch.
// Cells found in L1 are returned directly; the rest are fetched from Redis
// via a single MGET command, minimising round-trips.
func (s *CellGeometryStore) GetMultipleCells(ctx context.Context, ncis []string) ([]positioning.CellGeometry, error) {
	result := make([]positioning.CellGeometry, 0, len(ncis))
	missing := make([]string, 0, len(ncis))

	// L1 pass.
	s.mu.RLock()
	now := time.Now()
	for _, nci := range ncis {
		if entry, ok := s.cache[nci]; ok && now.Before(entry.expiresAt) {
			result = append(result, entry.cell)
		} else {
			missing = append(missing, nci)
		}
	}
	s.mu.RUnlock()

	if len(missing) == 0 {
		return result, nil
	}

	// L2 Redis batch get.
	keys := make([]string, len(missing))
	for i, nci := range missing {
		keys[i] = redisCellPrefix + nci
	}

	vals, err := s.redis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis mget: %w", err)
	}

	for i, val := range vals {
		if val == nil {
			s.logger.Warn("cell not found in Redis", zap.String("nci", missing[i]))
			continue
		}
		b, ok := val.(string)
		if !ok {
			s.logger.Error("unexpected Redis value type", zap.String("nci", missing[i]))
			continue
		}
		var cell positioning.CellGeometry
		if err := json.Unmarshal([]byte(b), &cell); err != nil {
			s.logger.Error("unmarshal cell from Redis",
				zap.String("nci", missing[i]),
				zap.Error(err))
			continue
		}
		s.storeMemCache(missing[i], cell)
		result = append(result, cell)
	}

	return result, nil
}

// StoreCell persists cell geometry to both Redis and the in-memory cache.
// Existing entries are overwritten.
func (s *CellGeometryStore) StoreCell(ctx context.Context, cell positioning.CellGeometry) error {
	b, err := json.Marshal(cell)
	if err != nil {
		return fmt.Errorf("marshal cell %q: %w", cell.NCI, err)
	}

	key := redisCellPrefix + cell.NCI
	if err := s.redis.Set(ctx, key, b, redisCellTTL).Err(); err != nil {
		return fmt.Errorf("redis set cell %q: %w", cell.NCI, err)
	}

	s.storeMemCache(cell.NCI, cell)
	s.logger.Debug("cell geometry stored",
		zap.String("nci", cell.NCI),
		zap.Float64("lat", cell.Latitude),
		zap.Float64("lon", cell.Longitude),
	)
	return nil
}

// MemCacheSize returns the current number of entries in the L1 cache (for diagnostics).
func (s *CellGeometryStore) MemCacheSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cache)
}

// --- private helpers ---

func (s *CellGeometryStore) fromMemCache(nci string) (*positioning.CellGeometry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.cache[nci]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	c := entry.cell // copy to avoid data race
	return &c, true
}

func (s *CellGeometryStore) storeMemCache(nci string, cell positioning.CellGeometry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[nci] = cacheEntry{
		cell:      cell,
		expiresAt: time.Now().Add(cellMemCacheTTL),
	}
}

// evictLoop periodically removes expired L1 entries to bound memory usage.
func (s *CellGeometryStore) evictLoop() {
	ticker := time.NewTicker(evictInterval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for nci, entry := range s.cache {
			if now.After(entry.expiresAt) {
				delete(s.cache, nci)
			}
		}
		s.mu.Unlock()
	}
}
