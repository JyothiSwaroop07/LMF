// Package server implements the gRPC QosManagerService.
package server

import (
	"context"
	"time"

	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/qos-manager/internal/evaluator"
	"go.uber.org/zap"
)

// QosServer implements the QosManagerService gRPC interface.
type QosServer struct {
	eval   *evaluator.QosEvaluator
	logger *zap.Logger
}

// NewQosServer creates a new QosServer.
func NewQosServer(logger *zap.Logger) *QosServer {
	return &QosServer{
		eval:   evaluator.NewQosEvaluator(),
		logger: logger,
	}
}

// EvaluateQoS checks whether a position estimate satisfies the requested QoS.
// requestStarted is the time when the location request began.
func (s *QosServer) EvaluateQoS(
	ctx context.Context,
	estimate types.PositionEstimate,
	qos types.LcsQoS,
	requestStarted time.Time,
) (types.AccuracyFulfilmentIndicator, bool, error) {
	acc := s.eval.EvaluateAccuracyFulfilment(qos, estimate)
	timeBreached := s.eval.IsResponseTimeBreached(requestStarted, qos.ResponseTime)

	uncertM := estimate.SigmaLat * 111111.0

	s.logger.Info("QoS evaluation complete",
		zap.String("accuracyResult", string(acc)),
		zap.Bool("timeBreached", timeBreached),
		zap.Float64("uncertaintyM", uncertM),
	)

	return acc, timeBreached, nil
}
