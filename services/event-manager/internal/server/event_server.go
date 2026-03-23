// Package server implements the gRPC EventManagerService.
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/event-manager/internal/subscription"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// EventServer implements the EventManagerService gRPC interface.
type EventServer struct {
	store  *subscription.SubscriptionStore
	logger *zap.Logger
}

// NewEventServer creates a new EventServer.
func NewEventServer(store *subscription.SubscriptionStore, logger *zap.Logger) *EventServer {
	return &EventServer{
		store:  store,
		logger: logger,
	}
}

// Subscribe creates a new event subscription.
func (s *EventServer) Subscribe(ctx context.Context, req types.EventSubscription) (*types.EventSubscription, error) {
	req.SubscriptionID = uuid.New().String()
	req.Active = true

	if err := s.store.Save(ctx, req); err != nil {
		return nil, fmt.Errorf("save subscription: %w", err)
	}

	middleware.ActiveSessions.WithLabelValues("event_sub").Inc()

	s.logger.Info("event subscription created",
		zap.String("subscriptionId", req.SubscriptionID),
		zap.String("supi", req.Supi),
		zap.String("eventType", string(req.EventType)),
	)

	return &req, nil
}

// Unsubscribe deletes an existing event subscription.
func (s *EventServer) Unsubscribe(ctx context.Context, subscriptionID string) error {
	if err := s.store.Delete(ctx, subscriptionID); err != nil {
		return fmt.Errorf("delete subscription %s: %w", subscriptionID, err)
	}

	middleware.ActiveSessions.WithLabelValues("event_sub").Dec()

	s.logger.Info("event subscription deleted",
		zap.String("subscriptionId", subscriptionID),
	)

	return nil
}

// GetSubscription retrieves a subscription by ID.
func (s *EventServer) GetSubscription(ctx context.Context, subscriptionID string) (*types.EventSubscription, error) {
	sub, err := s.store.Get(ctx, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("get subscription %s: %w", subscriptionID, err)
	}
	return sub, nil
}
