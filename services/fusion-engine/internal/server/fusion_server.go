// Package server implements the gRPC FusionEngineService.
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/fusion-engine/internal/fusion"
	"go.uber.org/zap"
)

// FusionServer implements the FusionEngineService gRPC interface.
type FusionServer struct {
	engine *fusion.FusionEngine
	logger *zap.Logger
}

// NewFusionServer creates a new FusionServer.
func NewFusionServer(logger *zap.Logger) *FusionServer {
	return &FusionServer{
		engine: fusion.NewFusionEngine(logger),
		logger: logger,
	}
}

// FusePositions fuses multiple position estimates into one.
// func (s *FusionServer) FusePositions(ctx context.Context, supi string, estimates []*types.PositionEstimate) (*types.PositionEstimate, error) {
// 	if len(estimates) == 0 {
// 		return nil, fmt.Errorf("no estimates to fuse")
// 	}

// 	result := s.engine.FusePositions(supi, estimates)

// 	uncertM := result.SigmaLat * 111111.0
// 	middleware.PositioningAccuracyMeters.WithLabelValues("fusion").Observe(uncertM)

// 	s.logger.Info("positions fused",
// 		zap.String("supi", supi),
// 		zap.Int("inputCount", len(estimates)),
// 		zap.Float64("lat", result.Latitude),
// 		zap.Float64("lon", result.Longitude),
// 		zap.Float64("sigmaLat", result.SigmaLat),
// 	)

// 	return result, nil
// }

func (s *FusionServer) FusePositions(ctx context.Context, sessionID string, estimates []types.PositionEstimate) (*types.PositionEstimate, error) {
	if len(estimates) == 0 {
		return nil, fmt.Errorf("no estimates to fuse")
	}

	req := fusion.FuseRequest{
		SessionID: sessionID,
		Estimates: estimates,
	}

	resp, err := s.engine.FusePositions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fusion failed: %w", err)
	}

	uncertM := resp.FusedEstimate.SigmaLat * 111111.0
	middleware.LocationAccuracyAchieved.WithLabelValues("fusion").Observe(uncertM)

	s.logger.Info("positions fused",
		zap.String("sessionID", sessionID),
		zap.Int("inputCount", len(estimates)),
		zap.Float64("lat", resp.FusedEstimate.Latitude),
		zap.Float64("lon", resp.FusedEstimate.Longitude),
		zap.Float64("sigmaLat", resp.FusedEstimate.SigmaLat),
		zap.Int("qualityIndex", resp.QualityIndex),
	)

	return &resp.FusedEstimate, nil
}
