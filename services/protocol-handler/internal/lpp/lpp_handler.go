// Package lpp implements LPP (LTE Positioning Protocol) message handling per 3GPP TS 36.355.
//
// LPP is transported over NAS (N1 interface via AMF) and carries:
//   - RequestCapabilities / ProvideCapabilities
//   - RequestAssistanceData / ProvideAssistanceData
//   - RequestLocationInformation / ProvideLocationInformation
//
// In production, messages are encoded/decoded using ASN.1 PER (per ETSI ASN.1 tool).
// This implementation uses a JSON-over-HTTP2 shim for clarity while preserving the
// message semantics.
package lpp

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

// MessageType identifies the LPP PDU type per TS 36.355 §6.2.1.
type MessageType string

const (
	MsgRequestCapabilities      MessageType = "requestCapabilities"
	MsgProvideCapabilities      MessageType = "provideCapabilities"
	MsgRequestAssistanceData    MessageType = "requestAssistanceData"
	MsgProvideAssistanceData    MessageType = "provideAssistanceData"
	MsgRequestLocationInfo      MessageType = "requestLocationInformation"
	MsgProvideLocationInfo      MessageType = "provideLocationInformation"
)

// LppMessage is the envelope for LPP PDUs sent over the Namf_MT interface.
type LppMessage struct {
	TransactionID uint8       `json:"transactionId"`
	SequenceNum   uint8       `json:"sequenceNum"`
	MessageType   MessageType `json:"messageType"`
	Payload       []byte      `json:"payload"`
}

// LppHandler sends and receives LPP messages via AMF (Namf_MT interface).
type LppHandler struct {
	amfBaseURL string
	httpClient *http.Client
	logger     *zap.Logger
	txCounter  uint8
}

// NewLppHandler creates an LppHandler targeting the AMF's Namf_MT endpoint.
func NewLppHandler(amfBaseURL string, logger *zap.Logger) *LppHandler {
	return &LppHandler{
		amfBaseURL: amfBaseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

// SendRequestCapabilities sends an LPP RequestCapabilities to the UE and returns its response.
func (h *LppHandler) SendRequestCapabilities(ctx context.Context, supi string) (*types.UeCapabilities, error) {
	txID := h.nextTxID()

	msg := LppMessage{
		TransactionID: txID,
		SequenceNum:   0,
		MessageType:   MsgRequestCapabilities,
	}

	if err := h.sendToUE(ctx, supi, msg); err != nil {
		return nil, fmt.Errorf("send RequestCapabilities: %w", err)
	}

	// In production: wait for ProvideCapabilities callback from AMF
	// Here we simulate a response with GPS+DL-TDOA+MultiRTT capabilities
	h.logger.Info("LPP RequestCapabilities sent",
		zap.String("supi", supi),
		zap.Uint8("txId", txID),
	)

	return &types.UeCapabilities{
		GnssSupported:      true,
		DlTdoaSupported:    true,
		MultiRttSupported:  true,
		EcidSupported:      true,
		GnssConstellations: []types.GnssConstellation{
			types.GnssGPS,
			types.GnssGalileo,
		},
	}, nil
}

// SendRequestLocationInfo triggers the UE to provide location measurements.
func (h *LppHandler) SendRequestLocationInfo(ctx context.Context, supi, sessionID string, method types.PositioningMethod) error {
	txID := h.nextTxID()

	payload, err := json.Marshal(map[string]string{
		"positioningMethod": string(method),
		"sessionId":         sessionID,
	})
	if err != nil {
		return fmt.Errorf("marshal location info request: %w", err)
	}

	msg := LppMessage{
		TransactionID: txID,
		SequenceNum:   0,
		MessageType:   MsgRequestLocationInfo,
		Payload:       payload,
	}

	if err := h.sendToUE(ctx, supi, msg); err != nil {
		return fmt.Errorf("send RequestLocationInformation: %w", err)
	}

	h.logger.Info("LPP RequestLocationInformation sent",
		zap.String("supi", supi),
		zap.String("method", string(method)),
		zap.Uint8("txId", txID),
	)

	return nil
}

// sendToUE delivers an LPP PDU to the UE via AMF Namf_MT_EnableUEReachability.
func (h *LppHandler) sendToUE(ctx context.Context, supi string, msg LppMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal LPP message: %w", err)
	}

	url := fmt.Sprintf("%s/namf-mt/v1/ue-contexts/%s/n1-n2-messages", h.amfBaseURL, supi)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create AMF request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("AMF request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("AMF returned HTTP %d", resp.StatusCode)
	}

	return nil
}

func (h *LppHandler) nextTxID() uint8 {
	h.txCounter = (h.txCounter + 1) % 255
	return h.txCounter
}
