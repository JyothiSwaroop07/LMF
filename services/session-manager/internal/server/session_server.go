package server

import (
	"context"
	"errors"
	"time"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/session-manager/internal/store"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SessionServer implements the gRPC SessionManagerService
type SessionServer struct {
	pb.UnimplementedSessionManagerServiceServer
	store  *store.SessionStore
	logger *zap.Logger
}

// NewSessionServer creates a new session server
func NewSessionServer(s *store.SessionStore, logger *zap.Logger) *SessionServer {
	return &SessionServer{store: s, logger: logger}
}

// Register registers the server with a gRPC server
// Note: In production, register generated gRPC service descriptors here.
// For now, registering as a plain server to demonstrate the pattern.
func (s *SessionServer) Register(srv *grpc.Server) {
	pb.RegisterSessionManagerServiceServer(srv, s)
	//swaroop uncommented the above line to register the SessionManagerServiceServer with the gRPC server. This is necessary for the gRPC server to route incoming requests to the SessionServer's methods.

	// Without generated protos, we demonstrate the service implementation below.
}

// CreateSession creates a new LCS session in Redis
// func (s *SessionServer) CreateSession(ctx context.Context, supi, pei, gpsi, amfInstanceID string,
// 	lcsQoS types.LcsQoS, lcsClientType types.LcsClientType, posMethod types.PositioningMethod,
// 	ttlSeconds int64) (string, error) {

// 	logger := middleware.LoggerFromContext(ctx)

// 	sessionID := uuid.New().String()
// 	now := time.Now()
// 	ttl := time.Duration(ttlSeconds) * time.Second
// 	if ttl == 0 {
// 		ttl = 300 * time.Second
// 	}

// 	session := &types.LcsSession{
// 		SessionID:         sessionID,
// 		Supi:              supi,
// 		Pei:               pei,
// 		Gpsi:              gpsi,
// 		AmfInstanceID:     amfInstanceID,
// 		LcsQoS:            lcsQoS,
// 		LcsClientType:     lcsClientType,
// 		PositioningMethod: posMethod,
// 		Status:            types.SessionStatusInit,
// 		StartTime:         now,
// 		ExpiryTime:        now.Add(ttl),
// 	}

// 	if err := s.store.SetSession(ctx, session); err != nil {
// 		logger.Error("failed to create session",
// 			zap.String("sessionId", sessionID),
// 			zap.Error(err),
// 		)
// 		return "", status.Errorf(codes.Internal, "creating session: %v", err)
// 	}

// 	middleware.SessionsCreated.Inc()
// 	middleware.ActiveSessions.WithLabelValues("immediate").Inc()

// 	logger.Info("session created",
// 		zap.String("sessionId", sessionID),
// 		zap.String("supi", supi),
// 	)

// 	return sessionID, nil
// }

func (s *SessionServer) CreateSession(ctx context.Context, req *pb.SessionCreateRequest) (*pb.SessionCreateResponse, error) {

	logger := middleware.LoggerFromContext(ctx)
	logger.Info("CreateSession called with request", zap.Any("request", req))

	sessionID := uuid.New().String()
	now := time.Now()
	ttl := time.Duration(req.TtlSeconds) * time.Second
	if ttl == 0 {
		ttl = 300 * time.Second
	}

	session := &types.LcsSession{
		SessionID:     sessionID,
		Supi:          req.Supi,
		Pei:           req.Pei,
		Gpsi:          req.Gpsi,
		AmfInstanceID: req.AmfInstanceId,
		// LcsQoS:            lcsoS,
		// LcsClientType:     lcsClientType,
		// PositioningMethod: posMethod,
		Status:     types.SessionStatusInit,
		StartTime:  now,
		ExpiryTime: now.Add(ttl),
	}

	if err := s.store.SetSession(ctx, session); err != nil {
		s.logger.Error("failed to create session",
			zap.String("sessionId", sessionID),
			zap.Error(err),
		)
		return nil, status.Errorf(codes.Internal, "creating session: %v", err)
	}

	middleware.SessionsCreated.Inc()
	middleware.ActiveSessions.WithLabelValues("immediate").Inc()

	logger.Info("session created",
		zap.String("sessionId", sessionID),
		zap.String("supi", req.Supi),
	)

	return &pb.SessionCreateResponse{SessionId: sessionID}, nil
}

// GetSession retrieves a session by ID

// func (s *SessionServer) GetSession(ctx context.Context, sessionID string) (*types.LcsSession, error) {
// 	session, err := s.store.GetSession(ctx, sessionID)
// 	if err != nil {
// 		if errors.Is(err, store.ErrNotFound) {
// 			return nil, status.Errorf(codes.NotFound, "session %s not found", sessionID)
// 		}
// 		return nil, status.Errorf(codes.Internal, "getting session: %v", err)
// 	}
// 	return session, nil
// }

func (s *SessionServer) GetSession(ctx context.Context, req *pb.SessionGetRequest) (*pb.SessionGetResponse, error) {
	session, err := s.store.GetSession(ctx, req.SessionId)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "session %s not found", req.SessionId)
		}
		return nil, status.Errorf(codes.Internal, "getting session: %v", err)
	}

	return &pb.SessionGetResponse{
		SessionId:     session.SessionID,
		Supi:          session.Supi,
		Pei:           session.Pei,
		AmfInstanceId: session.AmfInstanceID,
	}, nil
}

// UpdateSessionStatus updates the status of a session
// func (s *SessionServer) UpdateSessionStatus(ctx context.Context, sessionID string, newStatus types.SessionStatus) error {
// 	if err := s.store.UpdateSessionStatus(ctx, sessionID, newStatus); err != nil {
// 		if errors.Is(err, store.ErrNotFound) {
// 			return status.Errorf(codes.NotFound, "session %s not found", sessionID)
// 		}
// 		return status.Errorf(codes.Internal, "updating session: %v", err)
// 	}

// 	if newStatus == types.SessionStatusCompleted || newStatus == types.SessionStatusFailed {
// 		middleware.ActiveSessions.WithLabelValues("immediate").Dec()
// 	}
// 	if newStatus == types.SessionStatusCancelled {
// 		middleware.SessionsCancelled.Inc()
// 		middleware.ActiveSessions.WithLabelValues("immediate").Dec()
// 	}

// 	return nil
// }

// // DeleteSession removes a session
// func (s *SessionServer) DeleteSession(ctx context.Context, sessionID string) error {
// 	if err := s.store.DeleteSession(ctx, sessionID); err != nil {
// 		return status.Errorf(codes.Internal, "deleting session: %v", err)
// 	}
// 	return nil
// }

func (s *SessionServer) UpdateSessionStatus(ctx context.Context, req *pb.SessionUpdateRequest) (*pb.SessionUpdateResponse, error) {
	newStatus := mapPbSessionStatus(req.NewStatus)
	if err := s.store.UpdateSessionStatus(ctx, req.SessionId, newStatus); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "session %s not found", req.SessionId)
		}
		return nil, status.Errorf(codes.Internal, "updating session: %v", err)
	}

	if newStatus == types.SessionStatusCompleted || newStatus == types.SessionStatusFailed {
		middleware.ActiveSessions.WithLabelValues("immediate").Dec()
	}
	if newStatus == types.SessionStatusCancelled {
		middleware.SessionsCancelled.Inc()
		middleware.ActiveSessions.WithLabelValues("immediate").Dec()
	}

	return &pb.SessionUpdateResponse{Success: true}, nil
}

func (s *SessionServer) DeleteSession(ctx context.Context, req *pb.SessionDeleteRequest) (*pb.SessionDeleteResponse, error) {
	if err := s.store.DeleteSession(ctx, req.SessionId); err != nil {
		return nil, status.Errorf(codes.Internal, "deleting session: %v", err)
	}
	return &pb.SessionDeleteResponse{Success: true}, nil
}

func mapPbSessionStatus(s pb.SessionStatus) types.SessionStatus {
	switch s {
	case pb.SessionStatus_SESSION_STATUS_INIT:
		return types.SessionStatusInit
	case pb.SessionStatus_SESSION_STATUS_PROCESSING:
		return types.SessionStatusProcessing
	case pb.SessionStatus_SESSION_STATUS_AWAITING_MEASUREMENTS:
		return types.SessionStatusAwaitingMeasurements
	case pb.SessionStatus_SESSION_STATUS_COMPUTING:
		return types.SessionStatusComputing
	case pb.SessionStatus_SESSION_STATUS_COMPLETED:
		return types.SessionStatusCompleted
	case pb.SessionStatus_SESSION_STATUS_FAILED:
		return types.SessionStatusFailed
	default:
		return types.SessionStatusInit
	}
}
