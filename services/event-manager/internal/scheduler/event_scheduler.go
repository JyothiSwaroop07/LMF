// Package scheduler polls location updates and evaluates geofence/motion triggers.
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/event-manager/internal/geofence"
	"github.com/5g-lmf/event-manager/internal/notifier"
	"github.com/5g-lmf/event-manager/internal/subscription"
	"go.uber.org/zap"
)

// LocationFetcher fetches the latest known position for a SUPI.
type LocationFetcher interface {
	GetLastKnownPosition(ctx context.Context, supi string) (*geofence.LatLon, error)
}

// EventScheduler periodically evaluates active subscriptions.
type EventScheduler struct {
	store     *subscription.SubscriptionStore
	evaluator *geofence.GeofenceEvaluator
	notifier  *notifier.Notifier
	fetcher   LocationFetcher
	interval  time.Duration
	logger    *zap.Logger
	lastPos   map[string]*geofence.LatLon // supi→last evaluated position
	mu        sync.Mutex
}

// NewEventScheduler creates an EventScheduler.
func NewEventScheduler(
	store *subscription.SubscriptionStore,
	notif *notifier.Notifier,
	fetcher LocationFetcher,
	interval time.Duration,
	logger *zap.Logger,
) *EventScheduler {
	return &EventScheduler{
		store:     store,
		evaluator: geofence.NewGeofenceEvaluator(),
		notifier:  notif,
		fetcher:   fetcher,
		interval:  interval,
		logger:    logger,
		lastPos:   make(map[string]*geofence.LatLon),
	}
}

// Run starts the evaluation loop until ctx is cancelled.
func (s *EventScheduler) Run(ctx context.Context, activeSubs []types.EventSubscription) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("event scheduler stopping")
			return
		case <-ticker.C:
			s.evaluateAll(ctx, activeSubs)
		}
	}
}

func (s *EventScheduler) evaluateAll(ctx context.Context, subs []types.EventSubscription) {
	for _, sub := range subs {
		if err := s.evaluateSub(ctx, sub); err != nil {
			s.logger.Warn("event evaluation error",
				zap.String("subscriptionId", sub.SubscriptionID),
				zap.Error(err),
			)
		}
	}
}

func (s *EventScheduler) evaluateSub(ctx context.Context, sub types.EventSubscription) error {
	current, err := s.fetcher.GetLastKnownPosition(ctx, sub.Supi)
	if err != nil || current == nil {
		return nil // No position yet; skip
	}

	s.mu.Lock()
	last := s.lastPos[sub.Supi]
	s.mu.Unlock()

	var triggered bool
	switch sub.EventType {
	case types.EventTypeAreaEvent:
		if sub.AreaEventInfo != nil && len(sub.AreaEventInfo.LocationAreas) > 0 {
			area := sub.AreaEventInfo.LocationAreas[0]
			triggered = s.evaluator.EvaluateAreaEvent(sub.AreaEventInfo.AreaType, *current, last, area)
		}
	case types.EventTypeMotionEvent:
		if last != nil {
			triggered = s.evaluator.EvaluateMotionEvent(*last, *current, sub.MotionThresholdM)
		}
	case types.EventTypePeriodic:
		triggered = true // Always notify on periodic interval
	}

	if triggered {
		n := notifier.EventNotification{
			SubscriptionID: sub.SubscriptionID,
			Supi:           sub.Supi,
			EventType:      sub.EventType,
			Timestamp:      time.Now().UTC(),
		}
		if err := s.notifier.Notify(ctx, sub.NotifUri, n); err != nil {
			s.logger.Warn("notification failed", zap.Error(err))
		}
	}

	s.mu.Lock()
	s.lastPos[sub.Supi] = current
	s.mu.Unlock()

	return nil
}
