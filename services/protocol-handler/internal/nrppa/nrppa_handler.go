// Package nrppa implements NRPPa (NR Positioning Protocol A) per 3GPP TS 38.455.
//
// NRPPa is transported over the N2 interface between LMF and gNB (via AMF).
// It carries:
//   - Positioning Information Request/Response (PRS assistance data)
//   - Measurement Request/Response (DL-TDOA RSTD, E-CID, Multi-RTT)
//   - TRP Information Request/Response (cell geometry)
//
// Messages use ASN.1 PER encoding. This implementation uses a JSON shim.
package nrppa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// ProcedureCode identifies the NRPPa procedure per TS 38.455 §7.
type ProcedureCode uint8

const (
	ProcE_CID_Measurement       ProcedureCode = 1
	ProcOTDOAInformation        ProcedureCode = 2
	ProcPositioningInformation  ProcedureCode = 3
	ProcMeasurement             ProcedureCode = 4
	ProcTRPInformation          ProcedureCode = 5
)

// NrppaMessage is the envelope for NRPPa PDUs.
type NrppaMessage struct {
	LmfUENGAPID  uint64        `json:"lmfUeNgapId"`
	RanUENGAPID  uint64        `json:"ranUeNgapId"`
	ProcedureCode ProcedureCode `json:"procedureCode"`
	Criticality   string        `json:"criticality"` // reject | ignore | notify
	Payload       []byte        `json:"payload"`
}

// MeasurementRequest is the payload for a Measurement procedure request.
type MeasurementRequest struct {
	TRPMeasurementQuantities []string `json:"trpMeasurementQuantities"` // e.g. "UL-RTOA", "gNB-RxTxTimeDiff"
	ReportCharacteristics    string   `json:"reportCharacteristics"`    // "onDemand" | "periodic"
	MeasurementPeriodicity   int      `json:"measurementPeriodicity,omitempty"` // ms
}

// TrpInformationResponse carries gNB/TRP geometry used for TDOA computation.
type TrpInformationResponse struct {
	GlobalCellID string  `json:"globalCellId"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	AltitudeM    float64 `json:"altitudeM,omitempty"`
}

// NrppaHandler sends/receives NRPPa messages via AMF N2.
type NrppaHandler struct {
	amfBaseURL string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewNrppaHandler creates an NrppaHandler targeting the AMF N2 endpoint.
func NewNrppaHandler(amfBaseURL string, logger *zap.Logger) *NrppaHandler {
	return &NrppaHandler{
		amfBaseURL: amfBaseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		logger:     logger,
	}
}

// RequestTRPInformation fetches TRP (gNB) geometry from the serving cell.
// Used by the TDOA engine to obtain anchor positions.
func (h *NrppaHandler) RequestTRPInformation(ctx context.Context, cellID string) ([]TrpInformationResponse, error) {
	payload, err := json.Marshal(map[string]string{"globalCellId": cellID})
	if err != nil {
		return nil, fmt.Errorf("marshal TRP info request: %w", err)
	}

	msg := NrppaMessage{
		ProcedureCode: ProcTRPInformation,
		Criticality:   "reject",
		Payload:       payload,
	}

	if err := h.sendToGnB(ctx, msg); err != nil {
		return nil, fmt.Errorf("NRPPa TRPInformation request: %w", err)
	}

	h.logger.Info("NRPPa TRPInformation requested", zap.String("cellId", cellID))

	// In production: decode ASN.1 PER response from gNB callback.
	// Simulation: return synthetic TRP positions.
	return []TrpInformationResponse{
		{GlobalCellID: cellID, Latitude: 37.7749, Longitude: -122.4194, AltitudeM: 30},
	}, nil
}

// RequestMeasurement triggers the gNB to collect DL-TDOA / Multi-RTT measurements.
func (h *NrppaHandler) RequestMeasurement(ctx context.Context, cellID string, req MeasurementRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal measurement request: %w", err)
	}

	msg := NrppaMessage{
		ProcedureCode: ProcMeasurement,
		Criticality:   "reject",
		Payload:       payload,
	}

	if err := h.sendToGnB(ctx, msg); err != nil {
		return fmt.Errorf("NRPPa Measurement request: %w", err)
	}

	h.logger.Info("NRPPa Measurement requested",
		zap.String("cellId", cellID),
		zap.Strings("quantities", req.TRPMeasurementQuantities),
	)

	return nil
}

// sendToGnB delivers an NRPPa PDU to the gNB via AMF N2 (UplinkNonUEAssociatedNRPPaTransport).
func (h *NrppaHandler) sendToGnB(ctx context.Context, msg NrppaMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal NRPPa message: %w", err)
	}

	url := h.amfBaseURL + "/namf-comm/v1/non-ue-n2-messages/transfer"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create AMF N2 request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("AMF N2 request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("AMF N2 returned HTTP %d", resp.StatusCode)
	}

	return nil
}
