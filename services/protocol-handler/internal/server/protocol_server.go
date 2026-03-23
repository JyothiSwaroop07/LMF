// Package server implements the gRPC ProtocolHandlerService (MS-04).
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/protocol-handler/internal/lpp"
	"github.com/5g-lmf/protocol-handler/internal/nrppa"
	"go.uber.org/zap"
)

// ProtocolServer implements the ProtocolHandlerService gRPC interface.
type ProtocolServer struct {
	lppHandler   *lpp.LppHandler
	nrppaHandler *nrppa.NrppaHandler
	logger       *zap.Logger
}

// NewProtocolServer creates a ProtocolServer.
func NewProtocolServer(lppH *lpp.LppHandler, nrppaH *nrppa.NrppaHandler, logger *zap.Logger) *ProtocolServer {
	return &ProtocolServer{
		lppHandler:   lppH,
		nrppaHandler: nrppaH,
		logger:       logger,
	}
}

// GetUECapabilities fetches UE positioning capabilities via LPP RequestCapabilities.
func (s *ProtocolServer) GetUECapabilities(ctx context.Context, supi string) (*types.UeCapabilities, error) {
	caps, err := s.lppHandler.SendRequestCapabilities(ctx, supi)
	if err != nil {
		return nil, fmt.Errorf("LPP capabilities: %w", err)
	}
	s.logger.Info("UE capabilities fetched", zap.String("supi", supi))
	return caps, nil
}

// TriggerMeasurement sends an LPP RequestLocationInformation to the UE
// and an NRPPa MeasurementRequest to the serving gNB.
func (s *ProtocolServer) TriggerMeasurement(ctx context.Context, supi, sessionID string, method types.PositioningMethod) error {
	if err := s.lppHandler.SendRequestLocationInfo(ctx, supi, sessionID, method); err != nil {
		return fmt.Errorf("LPP measurement trigger: %w", err)
	}

	switch method {
	case types.PositioningMethodDLTDOA, types.PositioningMethodOTDOA,
		types.PositioningMethodNREcid, types.PositioningMethodMultiRTT:
		req := nrppa.MeasurementRequest{
			TRPMeasurementQuantities: measurementQuantities(method),
			ReportCharacteristics:    "onDemand",
		}
		if err := s.nrppaHandler.RequestMeasurement(ctx, "placeholder-cell-id", req); err != nil {
			s.logger.Warn("NRPPa measurement trigger failed", zap.Error(err))
		}
	}

	return nil
}

// GetTRPInformation fetches gNB geometry for TDOA computation.
func (s *ProtocolServer) GetTRPInformation(ctx context.Context, cellID string) ([]nrppa.TrpInformationResponse, error) {
	return s.nrppaHandler.RequestTRPInformation(ctx, cellID)
}

func measurementQuantities(method types.PositioningMethod) []string {
	switch method {
	case types.PositioningMethodDLTDOA, types.PositioningMethodOTDOA:
		return []string{"RSTD", "DL-PRS-RSRP"}
	case types.PositioningMethodMultiRTT:
		return []string{"UL-RTOA", "gNB-RxTxTimeDiff"}
	case types.PositioningMethodNREcid:
		return []string{"RSRP", "RSRQ", "TimingAdvance"}
	default:
		return []string{"RSRP"}
	}
}
