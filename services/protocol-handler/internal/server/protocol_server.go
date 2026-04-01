// Package server implements the gRPC ProtocolHandlerService (MS-04).
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/protocol-handler/internal/lpp"
	"github.com/5g-lmf/protocol-handler/internal/nrppa"
	"go.uber.org/zap"
)

// ProtocolServer implements the ProtocolHandlerService gRPC interface.
type ProtocolServer struct {
	pb.UnimplementedProtocolHandlerServiceServer
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
func (s *ProtocolServer) GetUeCapabilities(ctx context.Context, req *pb.GetUeCapabilitiesRequest) (*pb.GetUeCapabilitiesResponse, error) {
	caps, err := s.lppHandler.SendRequestCapabilities(ctx, req.Supi)
	if err != nil {
		return nil, fmt.Errorf("LPP capabilities: %w", err)
	}
	s.logger.Info("UE capabilities fetched", zap.String("supi", req.Supi))

	return &pb.GetUeCapabilitiesResponse{
		Capabilities: &pb.UeCapabilities{
			GnssSupported:       caps.GnssSupported,
			DlTdoaSupported:     caps.DlTdoaSupported,
			MultiRttSupported:   caps.MultiRttSupported,
			EcidSupported:       caps.EcidSupported,
			WlanSupported:       caps.WlanSupported,
			BluetoothSupported:  caps.BluetoothSupported,
			BarometricSupported: caps.BarometricSupported,
		},
		FromCache: false,
	}, nil
}

//commented the below two methods to avoid confusion for now, as the method selector service is not yet implemented and these methods are not being called.
// We can re-enable and implement them once we have the method selector service ready and we want to trigger measurements from the protocol handler.

// // TriggerMeasurement sends an LPP RequestLocationInformation to the UE
// // and an NRPPa MeasurementRequest to the serving gNB.
// func (s *ProtocolServer) TriggerMeasurement(ctx context.Context, supi, sessionID string, method types.PositioningMethod) error {
// 	if err := s.lppHandler.SendRequestLocationInfo(ctx, supi, sessionID, method); err != nil {
// 		return fmt.Errorf("LPP measurement trigger: %w", err)
// 	}

// 	switch method {
// 	case types.PositioningMethodDLTDOA, types.PositioningMethodOTDOA,
// 		types.PositioningMethodNREcid, types.PositioningMethodMultiRTT:
// 		req := nrppa.MeasurementRequest{
// 			TRPMeasurementQuantities: measurementQuantities(method),
// 			ReportCharacteristics:    "onDemand",
// 		}
// 		if err := s.nrppaHandler.RequestMeasurement(ctx, "placeholder-cell-id", req); err != nil {
// 			s.logger.Warn("NRPPa measurement trigger failed", zap.Error(err))
// 		}
// 	}

// 	return nil
// }

// // GetTRPInformation fetches gNB geometry for TDOA computation.
// func (s *ProtocolServer) GetTRPInformation(ctx context.Context, cellID string) ([]nrppa.TrpInformationResponse, error) {
// 	return s.nrppaHandler.RequestTRPInformation(ctx, cellID)
// }

// func measurementQuantities(method types.PositioningMethod) []string {
// 	switch method {
// 	case types.PositioningMethodDLTDOA, types.PositioningMethodOTDOA:
// 		return []string{"RSTD", "DL-PRS-RSRP"}
// 	case types.PositioningMethodMultiRTT:
// 		return []string{"UL-RTOA", "gNB-RxTxTimeDiff"}
// 	case types.PositioningMethodNREcid:
// 		return []string{"RSRP", "RSRQ", "TimingAdvance"}
// 	default:
// 		return []string{"RSRP"}
// 	}
// }

//added the below two methods to allow the method selector service to trigger measurements via the protocol handler, even though the method selector service is not yet implemented.
//  This way we can have the protocol handler ready to receive measurement triggers once the method selector service is implemented.
// The protocol handler's lpp handler and nrppa handler actually sends the LPP and NRPPa messages in the SendLpp and SendNrppa methods, which are called by the gRPC handlers below.

// Send LPP to UE and NRPPa to gNB for measurement trigger.
func (s *ProtocolServer) SendLpp(ctx context.Context, req *pb.SendLppRequest) (*pb.SendLppResponse, error) {
	s.logger.Info("SendLpp called",
		zap.String("supi", req.GetSupi()),
		zap.String("messageType", req.GetMessageType().String()),
	)
	if err := s.lppHandler.SendRaw(ctx, req.GetSupi(), req.GetPayload()); err != nil {
		return nil, fmt.Errorf("SendLpp: %w", err)
	}
	return &pb.SendLppResponse{
		ResponseType: pb.LppMessageType_LPP_MSG_PROVIDE_CAPABILITIES,
	}, nil
}

func (s *ProtocolServer) SendNrppa(ctx context.Context, req *pb.SendNrppaRequest) (*pb.SendNrppaResponse, error) {
	s.logger.Info("SendNrppa called",
		zap.String("cellNci", req.GetCellNci()),
		zap.String("procedure", req.GetProcedure().String()),
	)
	if err := s.nrppaHandler.SendRaw(ctx, req.GetPayload()); err != nil {
		return nil, fmt.Errorf("SendNrppa: %w", err)
	}
	return &pb.SendNrppaResponse{
		Procedure: req.GetProcedure(),
	}, nil
}
