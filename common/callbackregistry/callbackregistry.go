package callbackregistry

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const channelPrefix = "lmf:n1n2callback:"

// Registry delivers AMF N1N2 callbacks via Redis pub/sub.
type Registry struct {
	client *redis.ClusterClient
	logger *zap.Logger
}

// NewRegistryFromClient creates a Registry from an existing cluster client.
func NewRegistryFromClient(client *redis.ClusterClient, logger *zap.Logger) *Registry {
	return &Registry{client: client, logger: logger}
}

func channelKey(supi string) string {
	return channelPrefix + supi
}

// Register pre-subscribes to the Redis channel for the given SUPI and returns
// a channel that will receive the callback body when it arrives.
// MUST be called before SubscribeN1N2/SendLPP to avoid missing callbacks
// that arrive before WaitForCallback is called.
func (r *Registry) Register(ctx context.Context, supi string) (<-chan []byte, error) {
	channel := channelKey(supi)
	pubsub := r.client.Subscribe(ctx, channel)

	// Block until the subscription is confirmed active on Redis.
	// This guarantees no message is missed between Subscribe and receive.
	if _, err := pubsub.Receive(ctx); err != nil {
		pubsub.Close()
		return nil, fmt.Errorf("redis pre-register subscribe %s: %w", channel, err)
	}

	ch := make(chan []byte, 1)
	go func() {
		defer pubsub.Close()
		msg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			// ctx cancelled or connection closed — ch stays empty,
			// WaitOnChannel will return via ctx.Done()
			return
		}
		ch <- []byte(msg.Payload)
	}()

	r.logger.Info("pre-registered AMF callback channel",
		zap.String("supi", supi),
		zap.String("channel", channel),
	)
	return ch, nil
}

// WaitOnChannel blocks until a pre-registered channel delivers a callback
// or ctx is cancelled.
func (r *Registry) WaitOnChannel(ctx context.Context, ch <-chan []byte) ([]byte, error) {
	select {
	case body := <-ch:
		return body, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("callback wait timeout: %w", ctx.Err())
	}
}

// WaitForCallback is the original single-call API — subscribes and waits.
// Use Register+WaitOnChannel instead when timing is critical.
// Kept for backward compatibility.
func (r *Registry) WaitForCallback(ctx context.Context, supi string) ([]byte, error) {
	ch, err := r.Register(ctx, supi)
	if err != nil {
		return nil, err
	}
	return r.WaitOnChannel(ctx, ch)
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
