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
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/5g-lmf/common/types"
	namfcomm "github.com/5g-lmf/protocol-handler/internal/namfcomm"
	"go.uber.org/zap"
)

// useRealLPP controls whether to use real LPP exchange with AMF or hardcoded fallback.
// Set to true  → real N1N2 subscribe + LPP RequestCapabilities flow with Mobileum AMF.
// Set to false → hardcoded UE capabilities (safe fallback for Open5GS or offline testing).
const useRealLPP = true

// MessageType identifies the LPP PDU type per TS 36.355 §6.2.1.
type MessageType string

const (
	MsgRequestCapabilities   MessageType = "requestCapabilities"
	MsgProvideCapabilities   MessageType = "provideCapabilities"
	MsgRequestAssistanceData MessageType = "requestAssistanceData"
	MsgProvideAssistanceData MessageType = "provideAssistanceData"
	MsgRequestLocationInfo   MessageType = "requestLocationInformation"
	MsgProvideLocationInfo   MessageType = "provideLocationInformation"
)

// LppMessage is the envelope for LPP PDUs sent over the Namf_Communication interface.
type LppMessage struct {
	TransactionID uint8       `json:"transactionId"`
	SequenceNum   uint8       `json:"sequenceNum"`
	MessageType   MessageType `json:"messageType"`
	Payload       []byte      `json:"payload,omitempty"`
}

// CallbackStore waits for AMF N1N2 callbacks delivered via Redis pub/sub.
// Implemented by callbackregistry.Registry.
type CallbackStore interface {
	WaitForCallback(ctx context.Context, supi string) ([]byte, error)
}

// LppHandler sends and receives LPP messages via AMF (Namf_Communication interface).
type LppHandler struct {
	amfBaseURL    string
	namfClient    *namfcomm.Client
	callbackStore CallbackStore
	logger        *zap.Logger
	txCounter     uint8
}

// NewLppHandler creates an LppHandler.
//
//	amfBaseURL     : e.g. "http://192.168.145.26:80"  (kept for legacy sendToUE reference)
//	namfClient     : Namf_Communication client (subscribe/send/unsubscribe)
//	callbackStore  : Redis-based registry that delivers AMF callbacks
func NewLppHandler(amfBaseURL string, namfClient *namfcomm.Client, callbackStore CallbackStore, logger *zap.Logger) *LppHandler {
	return &LppHandler{
		amfBaseURL:    amfBaseURL,
		namfClient:    namfClient,
		callbackStore: callbackStore,
		logger:        logger,
	}
}

// ── HARDCODED FALLBACK ────────────────────────────────────────────────────────

// hardcodedUeCapabilities returns a fixed UeCapabilities struct that enables
// high-accuracy positioning methods (AGNSS, DL-TDOA, Multi-RTT, E-CID).
//
// NOTE: Open5GS AMF does not expose Namf-Loc, so UE capability exchange via
// LPP RequestCapabilities/ProvideCapabilities is not possible. These values
// are hardcoded to represent a capable 5G UE for development/testing purposes.
// Replace with real LPP capability exchange when AMF-Loc becomes available.
func hardcodedUeCapabilities() *types.UeCapabilities {
	return &types.UeCapabilities{
		GnssSupported:      true,
		DlTdoaSupported:    true,
		MultiRttSupported:  true,
		EcidSupported:      true,
		WlanSupported:      false,
		BluetoothSupported: false,
		GnssConstellations: []types.GnssConstellation{
			types.GnssGPS,
			types.GnssGalileo,
		},
	}
}

// useHardcoded returns hardcoded UE capabilities without contacting AMF.
// Used when useRealLPP=false or as automatic fallback if real LPP exchange fails.
func (h *LppHandler) useHardcoded(supi string) (*types.UeCapabilities, error) {
	caps := hardcodedUeCapabilities()
	h.logger.Info("LPP RequestCapabilities: using hardcoded UE capabilities (fallback)",
		zap.String("supi", supi),
		zap.Bool("gnss", caps.GnssSupported),
		zap.Bool("dlTdoa", caps.DlTdoaSupported),
		zap.Bool("multiRtt", caps.MultiRttSupported),
		zap.Bool("ecid", caps.EcidSupported),
	)
	return caps, nil
}

// ── REAL LPP FLOW ─────────────────────────────────────────────────────────────

// SendRequestCapabilities sends LPP RequestCapabilities to UE via AMF and waits
// for ProvideCapabilities callback via Redis pub/sub.
//
// Flow (when useRealLPP=true):
//  1. N1N2 Subscribe → Mobileum AMF (register LMF callback URI)
//  2. Send LPP RequestCapabilities → AMF → UE (simulated by Mobileum SPR)
//  3. Wait for AMF callback via Redis channel "lmf:n1n2callback:<supi>"
//  4. Parse UeCapabilities from callback body
//  5. N1N2 Unsubscribe → AMF (cleanup)
//
// Automatically falls back to hardcoded if useRealLPP=false or any step fails.
func (h *LppHandler) SendRequestCapabilities(ctx context.Context, supi string) (*types.UeCapabilities, error) {
	// ── TOGGLE: flip useRealLPP const at top of file to switch modes ──
	if !useRealLPP {
		return h.useHardcoded(supi)
	}

	h.logger.Info("LPP RequestCapabilities: starting real LPP flow",
		zap.String("supi", supi),
		zap.String("amf", h.amfBaseURL),
	)

	// Step 1: Subscribe to N1N2 notifications for this UE
	subscriptionID, err := h.namfClient.SubscribeN1N2(ctx, supi)
	if err != nil {
		h.logger.Warn("N1N2 subscribe failed, falling back to hardcoded",
			zap.String("supi", supi),
			zap.Error(err),
		)
		return h.useHardcoded(supi)
	}
	h.logger.Info("N1N2 subscribe successful",
		zap.String("supi", supi),
		zap.String("subscriptionId", subscriptionID),
	)

	// Cleanup subscription when done regardless of outcome
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.namfClient.UnsubscribeN1N2(cleanupCtx, supi, subscriptionID); err != nil {
			h.logger.Warn("N1N2 unsubscribe failed",
				zap.String("supi", supi),
				zap.Error(err),
			)
		} else {
			h.logger.Info("N1N2 unsubscribe successful", zap.String("supi", supi))
		}
	}()

	// Step 2: Build and send LPP RequestCapabilities to UE via AMF
	txID := h.nextTxID()
	lppMsg := LppMessage{
		TransactionID: txID,
		SequenceNum:   0,
		MessageType:   MsgRequestCapabilities,
	}
	lppBytes, err := json.Marshal(lppMsg)
	if err != nil {
		h.logger.Warn("marshal LPP RequestCapabilities failed, using hardcoded", zap.Error(err))
		return h.useHardcoded(supi)
	}

	if err := h.namfClient.SendLPP(ctx, supi, lppBytes); err != nil {
		h.logger.Warn("LPP RequestCapabilities send failed, using hardcoded",
			zap.String("supi", supi),
			zap.Error(err),
		)
		return h.useHardcoded(supi)
	}
	h.logger.Info("LPP RequestCapabilities sent to UE via AMF",
		zap.String("supi", supi),
		zap.Uint8("txId", txID),
	)

	// Step 3: Wait for AMF callback with LPP ProvideCapabilities via Redis
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer waitCancel()

	h.logger.Info("waiting for LPP ProvideCapabilities callback from AMF via Redis",
		zap.String("supi", supi),
	)

	callbackBody, err := h.callbackStore.WaitForCallback(waitCtx, supi)
	if err != nil {
		h.logger.Warn("LPP ProvideCapabilities callback timeout, using hardcoded",
			zap.String("supi", supi),
			zap.Error(err),
		)
		return h.useHardcoded(supi)
	}

	h.logger.Info("LPP ProvideCapabilities callback received",
		zap.String("supi", supi),
		zap.String("rawBody", string(callbackBody)),
	)

	// Step 4: Parse capabilities from callback body
	caps, err := h.parseProvideCapabilities(callbackBody)
	if err != nil {
		h.logger.Warn("parse LPP ProvideCapabilities failed, using hardcoded",
			zap.String("supi", supi),
			zap.String("body", string(callbackBody)),
			zap.Error(err),
		)
		return h.useHardcoded(supi)
	}

	h.logger.Info("LPP ProvideCapabilities parsed successfully",
		zap.String("supi", supi),
		zap.Bool("gnss", caps.GnssSupported),
		zap.Bool("ecid", caps.EcidSupported),
	)

	return caps, nil
}

// parseProvideCapabilities parses the AMF callback body into UeCapabilities.
// Logs the raw body so we can observe Mobileum AMF's actual response format.
func (h *LppHandler) parseProvideCapabilities(body []byte) (*types.UeCapabilities, error) {
	// Try LppMessage envelope first
	var lppMsg LppMessage
	if err := json.Unmarshal(body, &lppMsg); err == nil &&
		lppMsg.MessageType == MsgProvideCapabilities &&
		len(lppMsg.Payload) > 0 {

		h.logger.Info("LPP ProvideCapabilities envelope parsed",
			zap.Uint8("txId", lppMsg.TransactionID),
			zap.Int("payloadBytes", len(lppMsg.Payload)),
		)

		var caps types.UeCapabilities
		if err := json.Unmarshal(lppMsg.Payload, &caps); err != nil {
			return nil, fmt.Errorf("parse UeCapabilities payload: %w", err)
		}
		return &caps, nil
	}

	// Try direct UeCapabilities parse (Mobileum may use different format)
	var caps types.UeCapabilities
	if err := json.Unmarshal(body, &caps); err == nil {
		return &caps, nil
	}

	return nil, fmt.Errorf("unknown ProvideCapabilities format: %s", string(body))
}

// ── LOCATION INFO ─────────────────────────────────────────────────────────────

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

	lppBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal LPP message: %w", err)
	}

	if err := h.namfClient.SendLPP(ctx, supi, lppBytes); err != nil {
		return fmt.Errorf("send RequestLocationInformation: %w", err)
	}

	h.logger.Info("LPP RequestLocationInformation sent",
		zap.String("supi", supi),
		zap.String("method", string(method)),
		zap.Uint8("txId", txID),
	)

	return nil
}

// SendRaw delivers a raw LPP payload to the UE via AMF.
func (h *LppHandler) SendRaw(ctx context.Context, supi string, payload []byte) error {
	return h.namfClient.SendLPP(ctx, supi, payload)
}

func (h *LppHandler) nextTxID() uint8 {
	h.txCounter = (h.txCounter + 1) % 255
	return h.txCounter
}
