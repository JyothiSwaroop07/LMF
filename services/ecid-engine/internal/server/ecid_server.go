// Package server implements the gRPC EcidEngineService.
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/ecid-engine/internal/positioning"
	"go.uber.org/zap"
)

// EcidServer implements the EcidEngineService gRPC interface.
type EcidServer struct {
	solver *positioning.EcidSolver
	logger *zap.Logger
}

// NewEcidServer creates a new EcidServer.
func NewEcidServer(logger *zap.Logger) *EcidServer {
	return &EcidServer{
		solver: positioning.NewEcidSolver(),
		logger: logger,
	}
}

// ComputeEcid processes an E-CID positioning request.
// func (s *EcidServer) ComputeEcid(ctx context.Context, meas types.EcidMeasurements) (*types.PositionEstimate, error) {
// 	estimate, err := s.solver.Compute(meas)
// 	if err != nil {
// 		s.logger.Warn("E-CID computation failed", zap.Error(err))
// 		return nil, fmt.Errorf("ecid computation: %w", err)
// 	}

// 	uncertM := estimate.SigmaLat * 111111.0
// 	middleware.PositioningAccuracyMeters.WithLabelValues("ecid").Observe(uncertM)

// 	s.logger.Info("E-CID position computed",
// 		zap.Float64("lat", estimate.Latitude),
// 		zap.Float64("lon", estimate.Longitude),
// 		zap.Float64("sigmaLat", estimate.SigmaLat),
// 	)

// 	return estimate, nil
// }

func (s *EcidServer) ComputeEcid(ctx context.Context, meas types.EcidMeasurements) (*types.PositionEstimate, error) {
	// ServingCell and neighborCells would come from the request in production;
	// using zero-value defaults here until the full gRPC stub is wired up.
	servingCell := types.CellGeometry{}
	var neighborCells []types.CellGeometry

	estimate, err := s.solver.ComputePosition(meas, servingCell, neighborCells)
	if err != nil {
		s.logger.Warn("E-CID computation failed", zap.Error(err))
		return nil, fmt.Errorf("ecid computation: %w", err)
	}

	uncertM := estimate.SigmaLat * 111111.0
	middleware.LocationAccuracyAchieved.WithLabelValues("ecid").Observe(uncertM)

	s.logger.Info("E-CID position computed",
		zap.Float64("lat", estimate.Latitude),
		zap.Float64("lon", estimate.Longitude),
		zap.Float64("sigmaLat", estimate.SigmaLat),
	)

	return estimate, nil
}
