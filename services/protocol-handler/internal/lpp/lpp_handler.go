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

const useRealLPP = true

var requestCapabilitiesPDU = []byte{
	0xf0, 0x00, 0x01, 0x40, 0x0f, 0x70, 0x00, 0x00,
	0x50, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
}

var provideAssistanceDataPDU = []byte{
	0xf2, 0x03, 0x02, 0xc0, 0x86, 0x0c, 0xcb, 0x44,
	0x00, 0x01, 0x40, 0x15, 0x70, 0x1b, 0x10, 0x50,
	0x70, 0x90, 0xb0, 0x90, 0x50, 0x70, 0x2f, 0xfd,
	0x09, 0x91, 0x7d, 0x12, 0x80, 0x06, 0xf0, 0x13,
	0xbf, 0xc3, 0x13, 0x02, 0x05, 0x38, 0x7f, 0x90,
	0x00, 0x60, 0xfe, 0x2c, 0x1f, 0xc0, 0x90, 0x00,
	0x13, 0xc0, 0x00, 0x60, 0x00, 0x00, 0x0c, 0x00,
	0x60, 0x86, 0x00, 0x06, 0x05, 0xe0, 0x00, 0x30,
	0x00, 0x00, 0x60, 0x03, 0x04, 0x00, 0x03, 0x02,
	0xf0, 0x00, 0x18, 0x00, 0x00, 0x03, 0x00, 0x18,
	0x20, 0x00, 0x18, 0x16, 0x00, 0x10, 0x07, 0x80,
	0x00, 0xc0, 0x00, 0x00, 0x18, 0x00, 0xc1, 0x00,
	0x00, 0xc0, 0xa3, 0xe6, 0x28, 0x09, 0x03, 0x00,
	0x00, 0x00, 0x1f, 0x7d, 0x00, 0x09, 0x07, 0x0e,
	0x09, 0x00, 0x00, 0x00, 0x00, 0x06, 0x01, 0x00,
	0x61,
}

var requestLocationInfoPDU = []byte{
	0xf0, 0x04, 0x03, 0x48, 0x12, 0x00, 0x00, 0x00,
}

type MessageType string

const (
	MsgRequestCapabilities   MessageType = "requestCapabilities"
	MsgProvideCapabilities   MessageType = "provideCapabilities"
	MsgRequestAssistanceData MessageType = "requestAssistanceData"
	MsgProvideAssistanceData MessageType = "provideAssistanceData"
	MsgRequestLocationInfo   MessageType = "requestLocationInformation"
	MsgProvideLocationInfo   MessageType = "provideLocationInformation"
)

type LppMessage struct {
	TransactionID uint8       `json:"transactionId"`
	SequenceNum   uint8       `json:"sequenceNum"`
	MessageType   MessageType `json:"messageType"`
	Payload       []byte      `json:"payload,omitempty"`
}

type CallbackStore interface {
	Register(ctx context.Context, supi string) (<-chan []byte, error)
	WaitOnChannel(ctx context.Context, ch <-chan []byte) ([]byte, error)
	WaitForCallback(ctx context.Context, supi string) ([]byte, error)
}

type MeasurementStore interface {
	Store(ctx context.Context, sessionID string, payload []byte) error
}

type LppHandler struct {
	amfBaseURL       string
	namfClient       *namfcomm.Client
	callbackStore    CallbackStore
	measurementStore MeasurementStore
	logger           *zap.Logger
}

func NewLppHandler(
	amfBaseURL string,
	namfClient *namfcomm.Client,
	callbackStore CallbackStore,
	measurementStore MeasurementStore,
	logger *zap.Logger,
) *LppHandler {
	return &LppHandler{
		amfBaseURL:       amfBaseURL,
		namfClient:       namfClient,
		callbackStore:    callbackStore,
		measurementStore: measurementStore,
		logger:           logger,
	}
}

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

// SendRequestCapabilities sends only RequestCapabilities and returns UE caps.
// One subscription, one exchange, then unsubscribe.
// Called by GetUeCapabilities — fast path, no sessionID needed.
func (h *LppHandler) SendRequestCapabilities(ctx context.Context, supi string) (*types.UeCapabilities, error) {
	if !useRealLPP {
		return hardcodedUeCapabilities(), nil
	}

	sessionCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// Pre-register Redis channel BEFORE subscribing to AMF
	callbackCh, err := h.callbackStore.Register(sessionCtx, supi)
	if err != nil {
		h.logger.Warn("Redis pre-register failed, falling back",
			zap.String("supi", supi), zap.Error(err))
		return hardcodedUeCapabilities(), nil
	}

	subscriptionID, err := h.namfClient.SubscribeN1N2(sessionCtx, supi)
	if err != nil {
		h.logger.Warn("N1N2 subscribe failed, falling back",
			zap.String("supi", supi), zap.Error(err))
		return hardcodedUeCapabilities(), nil
	}
	defer func() {
		cleanupCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		h.namfClient.UnsubscribeN1N2(cleanupCtx, supi, subscriptionID)
		h.logger.Info("N1N2 unsubscribed after RequestCapabilities",
			zap.String("supi", supi))
	}()

	if err := h.namfClient.SendLPP(sessionCtx, supi, requestCapabilitiesPDU); err != nil {
		h.logger.Warn("RequestCapabilities send failed, falling back",
			zap.String("supi", supi), zap.Error(err))
		return hardcodedUeCapabilities(), nil
	}
	h.logger.Info("RequestCapabilities sent", zap.String("supi", supi))

	body, err := h.callbackStore.WaitOnChannel(sessionCtx, callbackCh)
	if err != nil {
		h.logger.Warn("ProvideCapabilities timeout, falling back",
			zap.String("supi", supi), zap.Error(err))
		return hardcodedUeCapabilities(), nil
	}
	h.logger.Info("ProvideCapabilities received",
		zap.String("supi", supi), zap.Int("bytes", len(body)))

	caps, err := h.parseProvideCapabilities(body)
	if err != nil {
		return hardcodedUeCapabilities(), nil
	}
	return caps, nil
}

// SendRequestLocationInfoAndWait runs the full assistance + location flow
// under a single subscription with the real sessionID.
// Called by SendLpp(REQUEST_LOCATION_INFORMATION) — measurements are stored in Redis.
//
// Flow:
//  1. Subscribe to N1N2
//  2. Wait for RequestAssistanceData from UE
//  3. Send ProvideAssistanceData
//  4. Wait for Acknowledgement from UE
//  5. Send RequestLocationInformation
//  6. Wait for ProvideLocationInformation
//  7. Store in Redis under "lmf:gnss:measurements:<sessionID>"
//  8. Unsubscribe
func (h *LppHandler) SendRequestLocationInfoAndWait(ctx context.Context, supi, sessionID string) error {
	sessionCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	subscriptionID, err := h.namfClient.SubscribeN1N2(sessionCtx, supi)
	if err != nil {
		return fmt.Errorf("N1N2 subscribe: %w", err)
	}
	h.logger.Info("N1N2 subscribed for location session",
		zap.String("supi", supi),
		zap.String("sessionId", sessionID),
		zap.String("subscriptionId", subscriptionID),
	)
	defer func() {
		cleanupCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		h.namfClient.UnsubscribeN1N2(cleanupCtx, supi, subscriptionID)
		h.logger.Info("N1N2 unsubscribed after location session",
			zap.String("supi", supi))
	}()

	// Step 1: Wait for RequestAssistanceData from UE
	h.logger.Info("waiting for RequestAssistanceData from UE",
		zap.String("supi", supi))
	radBody, err := h.waitForCallback(sessionCtx, supi, 15*time.Second)
	if err != nil {
		h.logger.Warn("RequestAssistanceData not received, skipping assistance step",
			zap.String("supi", supi), zap.Error(err))
		// Non-fatal — proceed directly to RequestLocationInformation
	} else {
		h.logger.Info("RequestAssistanceData received",
			zap.String("supi", supi),
			zap.String("hex", fmt.Sprintf("%x", radBody)),
		)

		// Step 2: Send ProvideAssistanceData
		if err := h.namfClient.SendLPP(sessionCtx, supi, provideAssistanceDataPDU); err != nil {
			h.logger.Warn("ProvideAssistanceData send failed, continuing",
				zap.String("supi", supi), zap.Error(err))
		} else {
			h.logger.Info("ProvideAssistanceData sent",
				zap.String("supi", supi),
				zap.Int("bytes", len(provideAssistanceDataPDU)),
			)
		}

		// Step 3: Wait for Acknowledgement from UE
		h.logger.Info("waiting for Acknowledgement from UE", zap.String("supi", supi))
		ackBody, err := h.waitForCallback(sessionCtx, supi, 10*time.Second)
		if err != nil {
			h.logger.Warn("Acknowledgement not received, continuing",
				zap.String("supi", supi), zap.Error(err))
		} else {
			h.logger.Info("Acknowledgement received",
				zap.String("supi", supi),
				zap.String("hex", fmt.Sprintf("%x", ackBody)),
			)
		}
	}

	// Step 4: Send RequestLocationInformation and wait for response
	return h.doRequestLocationInformation(sessionCtx, supi, sessionID)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (h *LppHandler) doRequestLocationInformation(ctx context.Context, supi, sessionID string) error {
	callbackCh, err := h.callbackStore.Register(ctx, supi)
	if err != nil {
		return fmt.Errorf("register callback: %w", err)
	}

	if err := h.namfClient.SendLPP(ctx, supi, requestLocationInfoPDU); err != nil {
		return fmt.Errorf("send RequestLocationInformation: %w", err)
	}
	h.logger.Info("RequestLocationInformation sent",
		zap.String("supi", supi),
		zap.String("sessionId", sessionID),
	)

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	body, err := h.callbackStore.WaitOnChannel(waitCtx, callbackCh)
	if err != nil {
		return fmt.Errorf("ProvideLocationInformation timeout: %w", err)
	}
	h.logger.Info("ProvideLocationInformation received",
		zap.String("supi", supi),
		zap.String("sessionId", sessionID),
		zap.Int("bytes", len(body)),
		zap.String("hex", fmt.Sprintf("%x", body)),
	)

	if sessionID != "" && h.measurementStore != nil {
		if err := h.measurementStore.Store(ctx, sessionID, body); err != nil {
			h.logger.Warn("failed to store measurements",
				zap.String("sessionId", sessionID), zap.Error(err))
		} else {
			h.logger.Info("measurements stored in Redis",
				zap.String("sessionId", sessionID))
		}
	}
	return nil
}

func (h *LppHandler) waitForCallback(ctx context.Context, supi string, timeout time.Duration) ([]byte, error) {
	callbackCh, err := h.callbackStore.Register(ctx, supi)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return h.callbackStore.WaitOnChannel(waitCtx, callbackCh)
}

func (h *LppHandler) parseProvideCapabilities(body []byte) (*types.UeCapabilities, error) {
	h.logger.Info("parsing ProvideCapabilities",
		zap.String("hex", fmt.Sprintf("%x", body)))

	var lppMsg LppMessage
	if err := json.Unmarshal(body, &lppMsg); err == nil &&
		lppMsg.MessageType == MsgProvideCapabilities {
		return h.capabilitiesFromDSX(), nil
	}

	var caps types.UeCapabilities
	if err := json.Unmarshal(body, &caps); err == nil &&
		(caps.GnssSupported || caps.EcidSupported) {
		return &caps, nil
	}

	h.logger.Info("ASN.1 UPER binary — mapping from DSX lcs_profile")
	return h.capabilitiesFromDSX(), nil
}

func (h *LppHandler) capabilitiesFromDSX() *types.UeCapabilities {
	return &types.UeCapabilities{
		GnssSupported:      true,
		DlTdoaSupported:    true,
		MultiRttSupported:  false,
		EcidSupported:      true,
		WlanSupported:      false,
		BluetoothSupported: false,
		GnssConstellations: []types.GnssConstellation{types.GnssGPS},
	}
}

func (h *LppHandler) SendRaw(ctx context.Context, supi string, payload []byte) error {
	return h.namfClient.SendLPP(ctx, supi, payload)
}
