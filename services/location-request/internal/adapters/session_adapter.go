package adapters

import (
	"context"

	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// GRPC session adapter forwards calls from the orchestrator to the session manager service over gRPC
type GRPCSessionAdapter struct {
	client pb.SessionManagerServiceClient
	logger *zap.Logger
}

func NewGRPCSessionAdapter(conn *grpc.ClientConn, logger *zap.Logger) *GRPCSessionAdapter {
	return &GRPCSessionAdapter{
		client: pb.NewSessionManagerServiceClient(conn),
		logger: logger,
	}
}

func (a *GRPCSessionAdapter) Create(ctx context.Context, supi string, qos types.LcsQoS) (string, error) {
	resp, err := a.client.CreateSession(ctx, &pb.SessionCreateRequest{
		Supi:       supi,
		TtlSeconds: 300, // default TTL; in production this would be configurable
	})
	if err != nil {
		a.logger.Error("CreateSession gRPC call failed", zap.Error(err))
		return "", err
	}
	return resp.SessionId, nil
}

func (a *GRPCSessionAdapter) UpdateStatus(ctx context.Context, sessionID string, newStatus types.SessionStatus) error {
	_, err := a.client.UpdateSessionStatus(ctx, &pb.SessionUpdateRequest{
		SessionId: sessionID,
		NewStatus: mapSessionStatus(newStatus),
	})
	return err
}

func (a *GRPCSessionAdapter) Delete(ctx context.Context, sessionID string) error {
	_, err := a.client.DeleteSession(ctx, &pb.SessionDeleteRequest{
		SessionId: sessionID,
	})
	return err
}

// mapSessionStatus converts internal string status to proto enum
func mapSessionStatus(s types.SessionStatus) pb.SessionStatus {
	switch s {
	case types.SessionStatusInit:
		return pb.SessionStatus_SESSION_STATUS_INIT
	case types.SessionStatusProcessing:
		return pb.SessionStatus_SESSION_STATUS_PROCESSING
	case types.SessionStatusAwaitingMeasurements:
		return pb.SessionStatus_SESSION_STATUS_AWAITING_MEASUREMENTS
	case types.SessionStatusComputing:
		return pb.SessionStatus_SESSION_STATUS_COMPUTING
	case types.SessionStatusCompleted:
		return pb.SessionStatus_SESSION_STATUS_COMPUTING
	case types.SessionStatusFailed:
		return pb.SessionStatus_SESSION_STATUS_UNSPECIFIED
	default:
		return pb.SessionStatus_SESSION_STATUS_UNSPECIFIED
	}
}
