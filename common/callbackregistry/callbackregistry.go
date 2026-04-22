package callbackregistry

// Package callbackregistry implements Redis pub/sub based delivery of AMF N1N2
// callbacks between sbi-gateway (receiver) and protocol-handler (waiter).
//
// Flow:
//  1. protocol-handler calls WaitForCallback(ctx, supi)
//     → subscribes to Redis channel "lmf:n1n2callback:<supi>"
//  2. AMF POSTs callback to sbi-gateway
//     → sbi-gateway calls Deliver(supi, body)
//     → publishes body to Redis channel "lmf:n1n2callback:<supi>"
//  3. protocol-handler's WaitForCallback unblocks and returns body

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	channelPrefix = "lmf:n1n2callback:"
)

// Registry delivers AMF N1N2 callbacks via Redis pub/sub.
type Registry struct {
	client *redis.ClusterClient
	logger *zap.Logger
}

// NewRegistryFromClient creates a Registry from an existing cluster client.
// Use this when the caller already has a RedisClient to avoid duplicate connections.
func NewRegistryFromClient(client *redis.ClusterClient, logger *zap.Logger) *Registry {
	return &Registry{client: client, logger: logger}
}

// channelKey returns the Redis pub/sub channel name for a given SUPI.
func channelKey(supi string) string {
	return channelPrefix + supi
}

// WaitForCallback subscribes to the Redis channel for the given SUPI and
// blocks until a callback is published or ctx is cancelled.
// Called by protocol-handler's LppHandler.
// Implements lpp.CallbackStore interface.
func (r *Registry) WaitForCallback(ctx context.Context, supi string) ([]byte, error) {
	channel := channelKey(supi)

	pubsub := r.client.Subscribe(ctx, channel)
	defer pubsub.Close()

	r.logger.Info("waiting for AMF callback on Redis channel",
		zap.String("supi", supi),
		zap.String("channel", channel),
	)

	select {
	case msg := <-pubsub.Channel():
		r.logger.Info("AMF callback received from Redis",
			zap.String("supi", supi),
			zap.String("payload", msg.Payload),
		)
		return []byte(msg.Payload), nil

	case <-ctx.Done():
		return nil, fmt.Errorf("callback wait timeout for supi %s: %w", supi, ctx.Err())
	}
}

// Deliver publishes the AMF callback body to the Redis channel for the given SUPI.
// Called by sbi-gateway's CallbackHandler when AMF POSTs a notification.
func (r *Registry) Deliver(supi string, body []byte) bool {
	channel := channelKey(supi)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := r.client.Publish(ctx, channel, string(body)).Result()
	if err != nil {
		r.logger.Error("failed to publish AMF callback to Redis",
			zap.String("supi", supi),
			zap.String("channel", channel),
			zap.Error(err),
		)
		return false
	}

	r.logger.Info("AMF callback published to Redis",
		zap.String("supi", supi),
		zap.Int64("receivers", result),
	)

	return result > 0
}
