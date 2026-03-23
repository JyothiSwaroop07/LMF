// Package notifier delivers LCS event notifications to registered callback URIs.
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
)

// EventNotification is the payload sent to callback URIs.
type EventNotification struct {
	SubscriptionID  string              `json:"subscriptionId"`
	Supi            string              `json:"supi"`
	EventType       types.EventType     `json:"eventType"`
	Timestamp       time.Time           `json:"timestamp"`
	LocationEstimate *types.LocationEstimate `json:"locationEstimate,omitempty"`
}

// Notifier sends HTTP notifications to LCS clients.
type Notifier struct {
	httpClient *http.Client
	logger     *zap.Logger
}

// NewNotifier creates a Notifier with a reasonable HTTP timeout.
func NewNotifier(logger *zap.Logger) *Notifier {
	return &Notifier{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     logger,
	}
}

// Notify sends an event notification to the given callback URI.
func (n *Notifier) Notify(ctx context.Context, callbackURI string, notification EventNotification) error {
	body, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURI, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		n.logger.Warn("notification delivery failed",
			zap.String("callbackURI", callbackURI),
			zap.String("subscriptionId", notification.SubscriptionID),
			zap.Error(err),
		)
		return fmt.Errorf("deliver notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		n.logger.Warn("notification returned non-2xx",
			zap.String("callbackURI", callbackURI),
			zap.Int("statusCode", resp.StatusCode),
		)
		return fmt.Errorf("notification rejected: HTTP %d", resp.StatusCode)
	}

	n.logger.Info("notification delivered",
		zap.String("callbackURI", callbackURI),
		zap.String("subscriptionId", notification.SubscriptionID),
		zap.String("eventType", string(notification.EventType)),
	)
	return nil
}
