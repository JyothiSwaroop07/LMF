// Package server implements the gRPC AssistanceDataService (MS-11).
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/assistance-data/internal/provider"
	"github.com/5g-lmf/assistance-data/internal/store"
	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
)

// AssistanceServer implements the AssistanceDataService gRPC interface.
type AssistanceServer struct {
	store    *store.AssistanceStore
	provider *provider.GNSSProvider
	logger   *zap.Logger
}

// NewAssistanceServer creates an AssistanceServer.
func NewAssistanceServer(s *store.AssistanceStore, p *provider.GNSSProvider, logger *zap.Logger) *AssistanceServer {
	return &AssistanceServer{store: s, provider: p, logger: logger}
}

// GetAssistanceData returns GNSS assistance data for the requested constellations.
// Checks the Redis cache first; falls back to the GNSS provider on cache miss.
func (s *AssistanceServer) GetAssistanceData(ctx context.Context, constellations []types.GnssConstellation) (*types.GnssAssistanceData, error) {
	resp := &types.GnssAssistanceData{}

	if len(constellations) == 0 {
		constellations = []types.GnssConstellation{types.GnssGPS}
	}

	for _, constellation := range constellations {
		// Check cache
		ephems, err := s.store.GetEphemeris(ctx, constellation)
		if err != nil {
			return nil, fmt.Errorf("get cached ephemeris: %w", err)
		}

		if len(ephems) == 0 {
			// Cache miss: fetch from provider
			s.logger.Info("ephemeris cache miss, fetching",
				zap.String("constellation", string(constellation)),
			)
			ephems, err = s.provider.FetchEphemeris(ctx, constellation, 12)
			if err != nil {
				return nil, fmt.Errorf("fetch ephemeris for %s: %w", constellation, err)
			}
			// Cache for next request
			if cacheErr := s.store.SetEphemeris(ctx, constellation, ephems); cacheErr != nil {
				s.logger.Warn("failed to cache ephemeris", zap.Error(cacheErr))
			}
		}

		resp.Ephemerides = append(resp.Ephemerides, ephems...)
	}

	// Add ionospheric model
	iono, err := s.store.GetIonosphericModel(ctx)
	if err == nil && iono == nil {
		// Cache miss
		fetched, fetchErr := s.provider.FetchKlobuchar(ctx)
		if fetchErr == nil {
			resp.IonosphericModel = &fetched
			_ = s.store.SetIonosphericModel(ctx, fetched)
		}
	} else if iono != nil {
		resp.IonosphericModel = iono
	}

	s.logger.Info("assistance data provided",
		zap.Int("numEphemerides", len(resp.Ephemerides)),
	)

	return resp, nil
}
