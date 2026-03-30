package adapters

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// grpc method selector adapter forwards calls from the orchestrator to the method selector service over gRPC
type GRPCMethodSelectorAdapter struct {
	client pb.MethodSelectorServiceClient
	logger *zap.Logger
}

// NewGRPCMethodSelectorAdapter creates a new GRPCMethodSelectorAdapter
func NewGRPCMethodSelectorAdapter(conn *grpc.ClientConn, logger *zap.Logger) *GRPCMethodSelectorAdapter {
	return &GRPCMethodSelectorAdapter{
		client: pb.NewMethodSelectorServiceClient(conn),
		logger: logger,
	}
}

// SelectMethod forwards the method selection request to the method selector service and returns the result
func (a *GRPCMethodSelectorAdapter) Select(ctx context.Context, req types.MethodSelectionRequest) (*types.MethodSelectionResult, error) {
	pbReq := &pb.MethodSelectionRequest{
		UeCapabilities: &pb.UeCapabilities{
			GnssSupported:       req.UeCaps.GnssSupported,
			DlTdoaSupported:     req.UeCaps.DlTdoaSupported,
			MultiRttSupported:   req.UeCaps.MultiRttSupported,
			EcidSupported:       req.UeCaps.EcidSupported,
			WlanSupported:       req.UeCaps.WlanSupported,
			BluetoothSupported:  req.UeCaps.BluetoothSupported,
			BarometricSupported: req.UeCaps.BarometricSupported,
		},
		LcsQos: &pb.LcsQoS{
			HorizontalAccuracyMeters: int32(req.LcsQoS.HorizontalAccuracy),
			ResponseTime:             typesResponseTimeToPb(req.LcsQoS.ResponseTime),
		},
		IndoorHint: req.IndoorHint,
	}

	resp, err := a.client.SelectMethod(context.Background(), pbReq)
	if err != nil {
		a.logger.Error("SelectMethod gRPC call failed", zap.Error(err))
		return nil, fmt.Errorf("SelectMethod gRPC call: %w", err)
	}

	fallbacks := make([]types.PositioningMethod, 0, len(resp.FallbackMethods))
	for _, m := range resp.GetFallbackMethods() {
		if t, ok := pbMethodToTypes(m); ok {
			fallbacks = append(fallbacks, t)
		}
	}

	selected, _ := pbMethodToTypes(resp.GetSelectedMethod()) // zero value = CellID as safe default

	a.logger.Info("method selection result received",
		zap.String("selected", string(selected)),
		zap.Int("fallbacks", len(fallbacks)),
		zap.Float64("estimatedAccuracyM", resp.GetEstimatedAccuracyMeters()),
		zap.Int32("estimatedResponseMs", resp.GetEstimatedResponseMs()),
	)

	return &types.MethodSelectionResult{
		SelectedMethod:      selected,
		FallbackMethods:     fallbacks,
		EstimatedAccuracy:   resp.GetEstimatedAccuracyMeters(),
		EstimatedResponseMs: int(resp.GetEstimatedResponseMs()),
	}, nil
}

// typesResponseTimeToPb maps types.ResponseTime → pb.ResponseTimeClass.
// types iota: NoDelay=0, LowDelay=1, DelayTolerant=2
// pb enum:    NO_DELAY=1, LOW_DELAY=2, DELAY_TOLERANT=3
// typesResponseTimeToPb maps types.ResponseTimeClass → pb.ResponseTimeClass.
func typesResponseTimeToPb(rt types.ResponseTimeClass) pb.ResponseTimeClass {
	switch rt {
	case types.ResponseTimeNoDelay:
		return pb.ResponseTimeClass_RESPONSE_TIME_NO_DELAY
	case types.ResponseTimeLowDelay:
		return pb.ResponseTimeClass_RESPONSE_TIME_LOW_DELAY
	case types.ResponseTimeDelayTolerantV2:
		return pb.ResponseTimeClass_RESPONSE_TIME_DELAY_TOLERANT_V2
	case types.ResponseTimeDelayTolerant:
		return pb.ResponseTimeClass_RESPONSE_TIME_DELAY_TOLERANT
	}
	return pb.ResponseTimeClass_RESPONSE_TIME_DELAY_TOLERANT
}

// pbMethodToTypes maps pb.PositioningMethod → types.PositioningMethod.
func pbMethodToTypes(m pb.PositioningMethod) (types.PositioningMethod, bool) {
	table := map[pb.PositioningMethod]types.PositioningMethod{
		pb.PositioningMethod_POSITIONING_METHOD_A_GNSS:       types.PositioningMethodAGNSS,
		pb.PositioningMethod_POSITIONING_METHOD_DL_TDOA:      types.PositioningMethodDLTDOA,
		pb.PositioningMethod_POSITIONING_METHOD_OTDOA:        types.PositioningMethodOTDOA,
		pb.PositioningMethod_POSITIONING_METHOD_NR_ECID:      types.PositioningMethodNREcid,
		pb.PositioningMethod_POSITIONING_METHOD_NR_MULTI_RTT: types.PositioningMethodMultiRTT,
		pb.PositioningMethod_POSITIONING_METHOD_WLAN:         types.PositioningMethodWLAN,
		pb.PositioningMethod_POSITIONING_METHOD_BLUETOOTH:    types.PositioningMethodBluetooth,
		pb.PositioningMethod_POSITIONING_METHOD_BAROMETRIC:   types.PositioningMethodBarometric,
		pb.PositioningMethod_POSITIONING_METHOD_CELL_ID:      types.PositioningMethodCellID,
	}
	v, ok := table[m]
	return v, ok
}
