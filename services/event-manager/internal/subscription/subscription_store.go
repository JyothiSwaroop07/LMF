// Package subscription manages LCS event subscriptions per 3GPP TS 23.273 §7.
package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/5g-lmf/common/clients"
	"github.com/5g-lmf/common/types"
)

const (
	subscriptionTTL    = 24 * time.Hour
	subKeyPrefix       = "sub:"
	supiSubIndexPrefix = "supi-subs:"
)

// SubscriptionStore persists event subscriptions in Redis.
type SubscriptionStore struct {
	redis *clients.RedisClient
}

// NewSubscriptionStore creates a new SubscriptionStore backed by Redis.
func NewSubscriptionStore(redis *clients.RedisClient) *SubscriptionStore {
	return &SubscriptionStore{redis: redis}
}

func subKey(subID string) string     { return subKeyPrefix + subID }
func supiSubsKey(supi string) string { return supiSubIndexPrefix + supi }

// Save stores a subscription and adds it to the SUPI→subscription index.
func (s *SubscriptionStore) Save(ctx context.Context, sub types.EventSubscription) error {
	data, err := json.Marshal(sub)
	if err != nil {
		return fmt.Errorf("marshal subscription: %w", err)
	}
	if err := s.redis.SetJSON(ctx, subKey(sub.SubscriptionID), data, subscriptionTTL); err != nil {
		return fmt.Errorf("redis set subscription: %w", err)
	}
	// Track which subscriptions belong to each SUPI
	if err := s.redis.SetAdd(ctx, supiSubsKey(sub.Supi), sub.SubscriptionID, subscriptionTTL); err != nil {
		return fmt.Errorf("redis index subscription: %w", err)
	}
	return nil
}

// Get retrieves a subscription by ID.
func (s *SubscriptionStore) Get(ctx context.Context, subID string) (*types.EventSubscription, error) {
	var sub types.EventSubscription
	if err := s.redis.GetJSON(ctx, subKey(subID), &sub); err != nil {
		return nil, fmt.Errorf("get subscription %s: %w", subID, err)
	}
	return &sub, nil
}

// Delete removes a subscription and its SUPI index entry.
func (s *SubscriptionStore) Delete(ctx context.Context, subID string) error {
	sub, err := s.Get(ctx, subID)
	if err != nil {
		return err
	}
	if err := s.redis.Delete(ctx, subKey(subID)); err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	_ = s.redis.SetRemove(ctx, supiSubsKey(sub.Supi), subID)
	return nil
}

// GetBySupi returns all subscription IDs for a given SUPI.
func (s *SubscriptionStore) GetBySupi(ctx context.Context, supi string) ([]string, error) {
	ids, err := s.redis.SetMembers(ctx, supiSubsKey(supi))
	if err != nil {
		return nil, fmt.Errorf("list subscriptions for supi %s: %w", supi, err)
	}
	return ids, nil
}
