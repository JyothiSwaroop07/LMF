package adapters

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// GRPCProtocolAdapter implements the ProtocolHandler interface using gRPC calls to the ProtocolHandler service.
type GRPCProtocolHandlerAdapter struct {
	client pb.ProtocolHandlerServiceClient
	logger *zap.Logger
}

// NewGRPCProtocolAdapter creates a new GRPCProtocolAdapter from the existing gRPC client connection.
func NewGRPCProtocolAdapter(conn *grpc.ClientConn, logger *zap.Logger) *GRPCProtocolHandlerAdapter {
	return &GRPCProtocolHandlerAdapter{
		client: pb.NewProtocolHandlerServiceClient(conn),
		logger: logger,
	}
}

// GetUECapabilities fetches UE positioning capabilities from the protocol-handler.
func (a *GRPCProtocolHandlerAdapter) GetUECapabilities(ctx context.Context, supi string) (*types.UeCapabilities, error) {
	resp, err := a.client.GetUeCapabilities(ctx, &pb.GetUeCapabilitiesRequest{
		Supi: supi,
	})
	if err != nil {
		return nil, fmt.Errorf("GetUeCapabilities gRPC: %w", err)
	}
	if resp.GetError() != "" {
		return nil, fmt.Errorf("protocol-handler error: %s", resp.GetError())
	}

	c := resp.GetCapabilities()
	if c == nil {
		return &types.UeCapabilities{}, nil
	}

	caps := &types.UeCapabilities{
		GnssSupported:       c.GetGnssSupported(),
		DlTdoaSupported:     c.GetDlTdoaSupported(),
		MultiRttSupported:   c.GetMultiRttSupported(),
		EcidSupported:       c.GetEcidSupported(),
		WlanSupported:       c.GetWlanSupported(),
		BluetoothSupported:  c.GetBluetoothSupported(),
		BarometricSupported: c.GetBarometricSupported(),
	}

	a.logger.Info("UE capabilities received from protocol-handler",
		zap.String("supi", supi),
		zap.Bool("gnss", caps.GnssSupported),
		zap.Bool("dlTdoa", caps.DlTdoaSupported),
		zap.Bool("multiRtt", caps.MultiRttSupported),
		zap.Bool("ecid", caps.EcidSupported),
	)

	return caps, nil
}

// TriggerMeasurement sends LPP RequestLocationInformation to the UE
// and NRPPa Measurement request to the gNB via protocol-handler.
func (a *GRPCProtocolHandlerAdapter) TriggerMeasurement(ctx context.Context, supi, sessionID string, method types.PositioningMethod) error {
	// Send LPP to UE
	_, err := a.client.SendLpp(ctx, &pb.SendLppRequest{
		Supi:        supi,
		SessionId:   sessionID,
		MessageType: pb.LppMessageType_LPP_MSG_REQUEST_LOCATION_INFORMATION,
	})
	if err != nil {
		return fmt.Errorf("SendLpp gRPC: %w", err)
	}

	a.logger.Info("LPP RequestLocationInformation sent",
		zap.String("supi", supi),
		zap.String("sessionId", sessionID),
		zap.String("method", string(method)),
	)

	// Send NRPPa to gNB for network-assisted methods
	switch method {
	case types.PositioningMethodDLTDOA, types.PositioningMethodOTDOA,
		types.PositioningMethodNREcid, types.PositioningMethodMultiRTT:
		_, err = a.client.SendNrppa(ctx, &pb.SendNrppaRequest{
			SessionId: sessionID,
			Procedure: pb.NrppaProcedure_NRPPA_PROCEDURE_MEASUREMENT_REPORTING,
		})
		if err != nil {
			// Non-fatal: log and continue — LPP already sent
			a.logger.Warn("SendNrppa gRPC failed",
				zap.String("sessionId", sessionID),
				zap.Error(err),
			)
		}
	}

	return nil
}
