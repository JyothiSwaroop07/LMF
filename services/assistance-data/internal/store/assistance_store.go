// Package store caches GNSS assistance data in Redis per 3GPP TS 23.273 §6.3.
//
// Assistance data includes:
//   - Ephemeris (GPS/Galileo/BeiDou) — refreshed every 2 hours (IS-GPS-200N validity)
//   - Almanac — refreshed every 24 hours
//   - Ionospheric models (Klobuchar for GPS, NeQuick for Galileo) — hourly
//   - UTC corrections — daily
//   - Reference location (AGPS network-level coarse location)
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/5g-lmf/common/clients"
	"github.com/5g-lmf/common/types"
)

const (
	ephemerisKeyPrefix = "assist:ephem:"
	almanacKeyPrefix   = "assist:alm:"
	ionoKeyPrefix      = "assist:iono:"
	refLocKeyPrefix    = "assist:refloc:"

	ephemerisTTL = 2 * time.Hour
	almanacTTL   = 24 * time.Hour
	ionoTTL      = 1 * time.Hour
	refLocTTL    = 30 * time.Minute
)

// AssistanceStore caches GNSS assistance data in Redis.
type AssistanceStore struct {
	redis *clients.RedisClient
}

// NewAssistanceStore creates an AssistanceStore backed by Redis.
func NewAssistanceStore(redis *clients.RedisClient) *AssistanceStore {
	return &AssistanceStore{redis: redis}
}

// SetEphemeris stores ephemeris for a given constellation.
func (s *AssistanceStore) SetEphemeris(ctx context.Context, constellation types.GnssConstellation, ephem []types.GnssEphemeris) error {
	key := ephemerisKeyPrefix + string(constellation)
	return s.redis.SetJSON(ctx, key, ephem, ephemerisTTL)
}

// GetEphemeris retrieves ephemeris for a constellation, returning nil if not cached.
func (s *AssistanceStore) GetEphemeris(ctx context.Context, constellation types.GnssConstellation) ([]types.GnssEphemeris, error) {
	key := ephemerisKeyPrefix + string(constellation)
	var ephem []types.GnssEphemeris
	if err := s.redis.GetJSON(ctx, key, &ephem); err != nil {
		return nil, nil // Cache miss → caller should fetch from upstream
	}
	return ephem, nil
}

// SetIonosphericModel caches the Klobuchar ionospheric model.
func (s *AssistanceStore) SetIonosphericModel(ctx context.Context, model types.KlobucharModel) error {
	data, err := json.Marshal(model)
	if err != nil {
		return fmt.Errorf("marshal iono model: %w", err)
	}
	return s.redis.SetJSON(ctx, ionoKeyPrefix+"klobuchar", data, ionoTTL)
}

// GetIonosphericModel retrieves the cached Klobuchar model.
func (s *AssistanceStore) GetIonosphericModel(ctx context.Context) (*types.KlobucharModel, error) {
	var model types.KlobucharModel
	err := s.redis.GetJSON(ctx, ionoKeyPrefix+"klobuchar", &model)
	if err != nil {
		return nil, nil // Cache miss
	}

	return &model, nil
}

// SetReferenceLocation caches the reference location for an area (coarse AGPS).
func (s *AssistanceStore) SetReferenceLocation(ctx context.Context, areaID string, lat, lon, uncertaintyKm float64) error {
	data, err := json.Marshal(map[string]float64{
		"lat":           lat,
		"lon":           lon,
		"uncertaintyKm": uncertaintyKm,
	})
	if err != nil {
		return fmt.Errorf("marshal reference location: %w", err)
	}
	return s.redis.SetJSON(ctx, refLocKeyPrefix+areaID, data, refLocTTL)
}
