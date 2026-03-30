// Package server implements the gRPC MethodSelectorService.
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/method-selector/internal/selector"
	"go.uber.org/zap"
)

// SelectorServer implements the MethodSelectorService gRPC interface.
type SelectorServer struct {
	pb.UnimplementedMethodSelectorServiceServer
	sel    *selector.MethodSelector
	logger *zap.Logger
}

// NewSelectorServer creates a new SelectorServer.
func NewSelectorServer(logger *zap.Logger) *SelectorServer {
	return &SelectorServer{
		sel:    selector.NewMethodSelector(),
		logger: logger,
	}
}

// SelectMethod selects the best positioning method for the given request.
func (s *SelectorServer) SelectMethod(ctx context.Context, req *pb.MethodSelectionRequest) (*pb.MethodSelectionResponse, error) {
	domainReq, err := pbReqToTypes(req)
	if err != nil {
		s.logger.Warn("invalid method selection request", zap.Error(err))
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	s.logger.Info("method selection request received at method_selector service")

	result, err := s.sel.SelectMethod(domainReq)
	if err != nil {
		s.logger.Warn("method selection failed", zap.Error(err))
		return nil, fmt.Errorf("method selection: %w", err)
	}

	middleware.PositioningMethodAttempts.WithLabelValues(string(result.SelectedMethod)).Inc()

	s.logger.Info("positioning method selected",
		zap.String("method", string(result.SelectedMethod)),
		zap.Float64("estimatedAccuracyM", result.EstimatedAccuracy),
		zap.Int("estimatedResponseMs", result.EstimatedResponseMs),
	)

	return typesResultToPb(result), nil
}

// pbReqToTypes converts a pb.MethodSelectionRequest to the domain type.
func pbReqToTypes(req *pb.MethodSelectionRequest) (types.MethodSelectionRequest, error) {
	if req == nil {
		return types.MethodSelectionRequest{}, fmt.Errorf("nil request")
	}

	caps := types.UeCapabilities{}
	if c := req.GetUeCapabilities(); c != nil {
		caps = types.UeCapabilities{
			GnssSupported:       c.GetGnssSupported(),
			DlTdoaSupported:     c.GetDlTdoaSupported(),
			MultiRttSupported:   c.GetMultiRttSupported(),
			EcidSupported:       c.GetEcidSupported(),
			WlanSupported:       c.GetWlanSupported(),
			BluetoothSupported:  c.GetBluetoothSupported(),
			BarometricSupported: c.GetBarometricSupported(),
		}
	}

	qos := types.LcsQoS{}
	if q := req.GetLcsQos(); q != nil {
		qos = types.LcsQoS{
			HorizontalAccuracy: int(q.GetHorizontalAccuracyMeters()),
			ResponseTime:       pbResponseTimeToTypes(q.GetResponseTime()),
		}
	}

	return types.MethodSelectionRequest{
		UeCaps:     caps,
		LcsQoS:     qos,
		IndoorHint: req.GetIndoorHint(),
	}, nil
}

// typesResultToPb converts a domain MethodSelectionResult to the pb response.
func typesResultToPb(r *types.MethodSelectionResult) *pb.MethodSelectionResponse {
	fallbacks := make([]pb.PositioningMethod, 0, len(r.FallbackMethods))
	for _, m := range r.FallbackMethods {
		if pbMethod, ok := typesMethodToPb(m); ok {
			fallbacks = append(fallbacks, pbMethod)
		}
	}

	selected, _ := typesMethodToPb(r.SelectedMethod) // defaults to UNSPECIFIED on unknown

	return &pb.MethodSelectionResponse{
		SelectedMethod:          selected,
		FallbackMethods:         fallbacks,
		EstimatedAccuracyMeters: r.EstimatedAccuracy,
		EstimatedResponseMs:     int32(r.EstimatedResponseMs),
	}
}

// pbResponseTimeToTypes maps pb.ResponseTimeClass → types.ResponseTime.
// pb values: UNSPECIFIED=0, NO_DELAY=1, LOW_DELAY=2, DELAY_TOLERANT=3, DELAY_TOLERANT_V2=4
// types values: NoDelay=0, LowDelay=1, DelayTolerant=2
func pbResponseTimeToTypes(rt pb.ResponseTimeClass) types.ResponseTimeClass {
	switch rt {
	case pb.ResponseTimeClass_RESPONSE_TIME_NO_DELAY:
		return types.ResponseTimeNoDelay
	case pb.ResponseTimeClass_RESPONSE_TIME_LOW_DELAY:
		return types.ResponseTimeLowDelay
	case pb.ResponseTimeClass_RESPONSE_TIME_DELAY_TOLERANT_V2:
		return types.ResponseTimeDelayTolerantV2
	case pb.ResponseTimeClass_RESPONSE_TIME_DELAY_TOLERANT,
		pb.ResponseTimeClass_RESPONSE_TIME_UNSPECIFIED:
		return types.ResponseTimeDelayTolerant
	}
	return types.ResponseTimeDelayTolerant
}

// typesMethodToPb maps types.PositioningMethod → pb.PositioningMethod.
func typesMethodToPb(m types.PositioningMethod) (pb.PositioningMethod, bool) {
	table := map[types.PositioningMethod]pb.PositioningMethod{
		types.PositioningMethodAGNSS:      pb.PositioningMethod_POSITIONING_METHOD_A_GNSS,
		types.PositioningMethodDLTDOA:     pb.PositioningMethod_POSITIONING_METHOD_DL_TDOA,
		types.PositioningMethodOTDOA:      pb.PositioningMethod_POSITIONING_METHOD_OTDOA,
		types.PositioningMethodNREcid:     pb.PositioningMethod_POSITIONING_METHOD_NR_ECID,
		types.PositioningMethodMultiRTT:   pb.PositioningMethod_POSITIONING_METHOD_NR_MULTI_RTT,
		types.PositioningMethodWLAN:       pb.PositioningMethod_POSITIONING_METHOD_WLAN,
		types.PositioningMethodBluetooth:  pb.PositioningMethod_POSITIONING_METHOD_BLUETOOTH,
		types.PositioningMethodBarometric: pb.PositioningMethod_POSITIONING_METHOD_BAROMETRIC,
		types.PositioningMethodCellID:     pb.PositioningMethod_POSITIONING_METHOD_CELL_ID,
	}
	v, ok := table[m]
	return v, ok
}
