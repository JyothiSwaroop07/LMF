// Package server implements the gRPC MethodSelectorService.
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/method-selector/internal/selector"
	"go.uber.org/zap"
)

// SelectorServer implements the MethodSelectorService gRPC interface.
type SelectorServer struct {
	sel    *selector.MethodSelector
	logger *zap.Logger
}

// NewSelectorServer creates a new SelectorServer.
func NewSelectorServer(logger *zap.Logger) *SelectorServer {
	return &SelectorServer{
		sel:    selector.NewMethodSelector(),
		logger: logger,
	}
}

// SelectMethod selects the best positioning method for the given request.
func (s *SelectorServer) SelectMethod(ctx context.Context, req types.MethodSelectionRequest) (*types.MethodSelectionResult, error) {
	result, err := s.sel.SelectMethod(req)
	if err != nil {
		s.logger.Warn("method selection failed", zap.Error(err))
		return nil, fmt.Errorf("method selection: %w", err)
	}

	middleware.PositioningMethodAttempts.WithLabelValues(string(result.SelectedMethod), "selected").Inc()

	s.logger.Info("positioning method selected",
		zap.String("method", string(result.SelectedMethod)),
		zap.Float64("estimatedAccuracyM", result.EstimatedAccuracy),
		zap.Int("estimatedResponseMs", result.EstimatedResponseMs),
	)

	return result, nil
}
