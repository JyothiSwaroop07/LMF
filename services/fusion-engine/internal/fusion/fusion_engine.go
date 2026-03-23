package fusion

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
)

// FusionEngine orchestrates multi-method position fusion with optional Kalman filtering
type FusionEngine struct {
	weighted *WeightedFusion
	kalman   *KalmanFilterManager
	logger   *zap.Logger
}

// NewFusionEngine creates a new fusion engine
func NewFusionEngine(logger *zap.Logger) *FusionEngine {
	return &FusionEngine{
		weighted: NewWeightedFusion(),
		kalman:   NewKalmanFilterManager(600 * time.Second),
		logger:   logger,
	}
}

// FuseRequest is the input to FusePositions
type FuseRequest struct {
	SessionID string
	Estimates []types.PositionEstimate
}

// FuseResponse is the output of FusePositions
type FuseResponse struct {
	FusedEstimate types.PositionEstimate
	QualityIndex  int // 0-100
}

// FusePositions fuses multiple position estimates into one optimal estimate
func (f *FusionEngine) FusePositions(ctx context.Context, req FuseRequest) (*FuseResponse, error) {
	if len(req.Estimates) == 0 {
		return nil, fmt.Errorf("no estimates provided")
	}

	var fused *types.PositionEstimate

	if len(req.Estimates) == 1 {
		// Single estimate: apply Kalman filter if we have a session
		est := req.Estimates[0]
		if req.SessionID != "" {
			filtered := f.kalman.Update(req.SessionID, est)
			fused = &filtered
		} else {
			fused = &est
		}
	} else {
		// Multiple estimates: weighted fusion first
		var err error
		fused, err = f.weighted.Fuse(req.Estimates)
		if err != nil {
			return nil, fmt.Errorf("weighted fusion failed: %w", err)
		}

		// Apply Kalman filter if session-based tracking
		if req.SessionID != "" {
			filtered := f.kalman.Update(req.SessionID, *fused)
			fused = &filtered
		}
	}

	// Compute quality index: 100 * (1 - clamp(sigma_m / 1000, 0, 1))
	sigmaM := math.Sqrt(fused.SigmaLat*fused.SigmaLat+fused.SigmaLon*fused.SigmaLon) * 111319.0
	clampedSigma := sigmaM / 1000.0
	if clampedSigma > 1.0 {
		clampedSigma = 1.0
	}
	qualityIndex := int(100.0 * (1.0 - clampedSigma))
	if qualityIndex < 0 {
		qualityIndex = 0
	}

	f.logger.Debug("fusion complete",
		zap.String("sessionId", req.SessionID),
		zap.Int("inputEstimates", len(req.Estimates)),
		zap.Float64("fusedLat", fused.Latitude),
		zap.Float64("fusedLon", fused.Longitude),
		zap.Float64("sigmaM", sigmaM),
		zap.Int("qualityIndex", qualityIndex),
	)

	return &FuseResponse{
		FusedEstimate: *fused,
		QualityIndex:  qualityIndex,
	}, nil
}
