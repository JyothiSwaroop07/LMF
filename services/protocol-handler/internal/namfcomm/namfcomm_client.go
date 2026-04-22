// Package namfcomm implements the Namf_Communication client per 3GPP TS 29.518.
//
// This client handles:
//   - N1N2MessageSubscribe   : register a callback URI with AMF for UE N1/N2 notifications
//   - N1N2MessageTransfer    : send LPP messages to UE via AMF N1 interface
//   - N1N2MessageUnsubscribe : cleanup subscription after positioning is complete
package namfcomm

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

// SubscriptionRequest is the body for POST /namf-comm/v1/ue-contexts/{supi}/n1-n2-messages/subscriptions
// per TS 29.518 §6.3.5.3.2
type SubscriptionRequest struct {
	N1MessageClass string `json:"n1MessageClass"` // "LPP"
	// N2InformationClass  string `json:"n2InformationClass,omitempty"` // "NRPPa"
	N1NotifyCallbackUri string `json:"n1NotifyCallbackUri"` // LMF callback URL
	// SubscribingNfID     string `json:"subscribingNfId"`              // LMF NF instance ID
}

// SubscriptionResponse is the 201 response body from AMF.
type SubscriptionResponse struct {
	N1N2NotifySubscriptionId string `json:"n1n2NotifySubscriptionId"`
}

// N1N2MessageTransferRequest is the body for POST /namf-comm/v1/ue-contexts/{supi}/n1-n2-messages
// per TS 29.518 §6.3.3.3.2
type N1N2MessageTransferRequest struct {
	N1MessageContainer *N1MessageContainer `json:"n1MessageContainer,omitempty"`
}

// N1MessageContainer wraps the LPP payload for N1 transport.
type N1MessageContainer struct {
	N1MessageClass   string `json:"n1MessageClass"`   // "LPP"
	N1MessageContent []byte `json:"n1MessageContent"` // LPP PDU bytes
}

// Client is the Namf_Communication HTTP/2 client.
type Client struct {
	amfBaseURL  string
	lmfCallback string // e.g. "http://192.168.172.53:8000/namf-comm/callback"
	lmfNfID     string // LMF NF instance ID
	httpClient  *http.Client
	logger      *zap.Logger
}

// NewClient creates a Namf_Communication client.
//
//	amfBaseURL  : e.g. "http://192.168.145.26:80"
//	lmfCallback : base callback URL reachable by AMF, e.g. "http://192.168.172.53:8000/namf-comm/callback"
//	lmfNfID     : LMF NF instance UUID
func NewClient(amfBaseURL, lmfCallback, lmfNfID string, logger *zap.Logger) *Client {
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}
	return &Client{
		amfBaseURL:  amfBaseURL,
		lmfCallback: lmfCallback,
		lmfNfID:     lmfNfID,
		httpClient: &http.Client{
			Timeout:   15 * time.Second,
			Transport: transport,
		},
		logger: logger,
	}
}

// SubscribeN1N2 registers the LMF callback with AMF for a specific UE's N1 (LPP) messages.
// Returns the subscriptionId to use for cleanup.
// Per TS 29.518 §6.3.5 Namf_Communication_N1N2MessageSubscribe.
func (c *Client) SubscribeN1N2(ctx context.Context, supi string) (string, error) {
	// Callback URI that AMF will POST LPP responses to
	// Format: <lmfCallback>/ue-contexts/<supi>/n1-n2-messages
	notificationURI := fmt.Sprintf("%s/ue-contexts/%s/n1-n2-messages", c.lmfCallback, supi)

	subReq := SubscriptionRequest{
		N1MessageClass: "LPP",
		// N2InformationClass: "NRPPa",
		N1NotifyCallbackUri: notificationURI,
		// SubscribingNfID:    c.lmfNfID,
	}

	body, err := json.Marshal(subReq)
	if err != nil {
		return "", fmt.Errorf("marshal subscription request: %w", err)
	}

	url := fmt.Sprintf("%s/namf-comm/v1/ue-contexts/%s/n1-n2-messages/subscriptions", c.amfBaseURL, supi)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create subscribe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	c.logger.Info("N1N2 subscribe sending",
		zap.String("supi", supi),
		zap.String("url", url),
		zap.String("notificationURI", notificationURI),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("N1N2 subscribe request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	c.logger.Info("N1N2 subscribe response",
		zap.Int("status", resp.StatusCode),
		zap.String("body", string(respBody)),
	)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AMF subscribe returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var subResp SubscriptionResponse
	if err := json.Unmarshal(respBody, &subResp); err != nil {
		// AMF may return subscriptionId in Location header instead
		loc := resp.Header.Get("Location")
		c.logger.Warn("could not parse subscription response body, using Location header",
			zap.String("location", loc),
			zap.Error(err),
		)
		return loc, nil
	}

	c.logger.Info("N1N2 subscribe successful",
		zap.String("supi", supi),
		zap.String("subscriptionId", subResp.N1N2NotifySubscriptionId),
	)

	return subResp.N1N2NotifySubscriptionId, nil
}

// SendLPP sends an LPP PDU to the UE via AMF N1 interface.
// Per TS 29.518 §6.3.3 Namf_Communication_N1N2MessageTransfer.
func (c *Client) SendLPP(ctx context.Context, supi string, lppPayload []byte) error {
	transferReq := N1N2MessageTransferRequest{
		N1MessageContainer: &N1MessageContainer{
			N1MessageClass:   "LPP",
			N1MessageContent: lppPayload,
		},
	}

	body, err := json.Marshal(transferReq)
	if err != nil {
		return fmt.Errorf("marshal N1N2 transfer request: %w", err)
	}

	url := fmt.Sprintf("%s/namf-comm/v1/ue-contexts/%s/n1-n2-messages", c.amfBaseURL, supi)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create N1N2 transfer request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	c.logger.Info("LPP N1N2 transfer sending",
		zap.String("supi", supi),
		zap.Int("payloadBytes", len(lppPayload)),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("N1N2 transfer failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	c.logger.Info("LPP N1N2 transfer response",
		zap.Int("status", resp.StatusCode),
		zap.String("body", string(respBody)),
	)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("AMF N1N2 transfer returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// UnsubscribeN1N2 removes the N1N2 subscription from AMF after positioning is complete.
// Per TS 29.518 §6.3.5 Namf_Communication_N1N2MessageUnsubscribe.
func (c *Client) UnsubscribeN1N2(ctx context.Context, supi, subscriptionID string) error {
	url := fmt.Sprintf("%s/namf-comm/v1/ue-contexts/%s/n1-n2-messages/subscriptions/%s",
		c.amfBaseURL, supi, subscriptionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create unsubscribe request: %w", err)
	}

	c.logger.Info("N1N2 unsubscribe sending",
		zap.String("supi", supi),
		zap.String("subscriptionId", subscriptionID),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("N1N2 unsubscribe failed: %w", err)
	}
	defer resp.Body.Close()

	c.logger.Info("N1N2 unsubscribe response",
		zap.Int("status", resp.StatusCode),
	)

	return nil
}
