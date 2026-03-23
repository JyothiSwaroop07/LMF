// Package server implements the gRPC RttEngineService.
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/rtt-engine/internal/positioning"
	"go.uber.org/zap"
)

// RttServer implements the RttEngineService gRPC interface.
type RttServer struct {
	solver *positioning.RttSolver
	logger *zap.Logger
}

// NewRttServer creates a new RttServer.
func NewRttServer(logger *zap.Logger) *RttServer {
	return &RttServer{
		solver: positioning.NewRttSolver(),
		logger: logger,
	}
}

// ComputeRtt processes a Multi-RTT positioning request.
// func (s *RttServer) ComputeRtt(ctx context.Context, meas types.MultiRttMeasurements) (*types.PositionEstimate, error) {
// 	estimate, err := s.solver.Compute(meas)
// 	if err != nil {
// 		s.logger.Warn("RTT computation failed", zap.Error(err))
// 		return nil, fmt.Errorf("rtt computation: %w", err)
// 	}

// 	uncertM := estimate.SigmaLat * 111111.0
// 	middleware.PositioningAccuracyMeters.WithLabelValues("rtt").Observe(uncertM)

// 	s.logger.Info("RTT position computed",
// 		zap.Float64("lat", estimate.Latitude),
// 		zap.Float64("lon", estimate.Longitude),
// 		zap.Float64("sigmaLat", estimate.SigmaLat),
// 	)

// 	return estimate, nil
// }

func (s *RttServer) ComputeRtt(ctx context.Context, meas types.MultiRttMeasurements) (*types.PositionEstimate, error) {
	// cellGeometry would come from the full gRPC request in production
	cellGeometry := make(map[string]types.CellGeometry)

	estimate, err := s.solver.ComputePosition(meas, cellGeometry)
	if err != nil {
		s.logger.Warn("RTT computation failed", zap.Error(err))
		return nil, fmt.Errorf("rtt computation: %w", err)
	}

	uncertM := estimate.SigmaLat * 111111.0
	middleware.LocationAccuracyAchieved.WithLabelValues("rtt").Observe(uncertM)

	s.logger.Info("RTT position computed",
		zap.Float64("lat", estimate.Latitude),
		zap.Float64("lon", estimate.Longitude),
		zap.Float64("sigmaLat", estimate.SigmaLat),
	)

	return estimate, nil
}
