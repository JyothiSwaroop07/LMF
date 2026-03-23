// Package server implements the gRPC LocationRequestService (MS-02).
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/location-request/internal/orchestrator"
	"go.uber.org/zap"
)

// LocationServer implements the LocationRequestService gRPC interface.
type LocationServer struct {
	pb.UnimplementedLocationRequestServiceServer
	orch   *orchestrator.LocationOrchestrator
	logger *zap.Logger
}

// NewLocationServer creates a LocationServer.
func NewLocationServer(orch *orchestrator.LocationOrchestrator, logger *zap.Logger) *LocationServer {
	return &LocationServer{orch: orch, logger: logger}
}

// DetermineLocation handles an incoming location request from the SBI Gateway.
func (s *LocationServer) DetermineLocation(ctx context.Context, req *pb.LocationRequestMsg) (*pb.LocationResponseMsg, error) {
	logger := s.logger.With(
		zap.String("supi", req.Supi),
		zap.String("sessionId", req.SessionId),
	)
	logger.Info("location request received at location-request service: DetermineLocation")

	session := types.LcsSession{
		SessionID: req.SessionId,
		Supi:      req.Supi,
		Pei:       req.Pei,
		Gpsi:      req.Gpsi,
	}

	estimate, err := s.orch.DetermineLocation(ctx, session)
	if err != nil {
		middleware.LocationRequestsTotal.WithLabelValues("error").Inc()
		logger.Error("location request failed", zap.Error(err))
		return nil, fmt.Errorf("determine location: %w", err)
	}

	logger.Info("location request completed",
		zap.Float64("lat", estimate.Latitude),
		zap.Float64("lon", estimate.Longitude),
		zap.Float64("horizontalUncertaintyM", estimate.HorizontalUncertainty),
	)

	// return estimate, nil
	return &pb.LocationResponseMsg{
		SessionId: req.SessionId,
		LocationEstimate: &pb.LocationEstimate{
			Shape:                 "POINT",
			Latitude:              estimate.Latitude,
			Longitude:             estimate.Longitude,
			HorizontalUncertainty: estimate.HorizontalUncertainty,
		},
		AccuracyIndicator: pb.AccuracyFulfilmentIndicator_ACCURACY_FULFILMENT_REQUESTED_FULFILLED,
	}, nil
}

// CancelLocation cancels an in-progress location request.
func (s *LocationServer) CancelLocation(_ context.Context, req *pb.CancelRequest) (*pb.CancelResponse, error) {
	s.logger.Info("location request cancelled", zap.String("sessionId", req.SessionId))
	return &pb.CancelResponse{Success: true}, nil
}

// swaroop added the below function to handle the GetSessionStatus gRPC call from SBI Gateway. This is a stub implementation that always returns "processing" status; it can be enhanced in the future to reflect real session states.
func (s *LocationServer) GetSessionStatus(ctx context.Context, req *pb.SessionStatusRequest) (*pb.SessionStatusResponse, error) {
	s.logger.Info("GetSessionStatus called", zap.String("sessionId", req.SessionId))
	return &pb.SessionStatusResponse{
		SessionId: req.SessionId,
		Status:    pb.SessionStatus_SESSION_STATUS_PROCESSING,
	}, nil
}
