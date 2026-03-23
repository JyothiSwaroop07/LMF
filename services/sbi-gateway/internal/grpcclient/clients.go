package grpcclient

import (
	"context"
	"fmt"
	"time"

	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/sbi-gateway/internal/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.uber.org/zap"
	//added for logging in grpcclient
)

// Clients holds gRPC connections to downstream LMF services
type Clients struct {
	locationRequestConn *grpc.ClientConn
	eventManagerConn    *grpc.ClientConn
	logger              *zap.Logger //swaroop added logger to clients struct
}

// New creates gRPC client connections to downstream services
func New(cfg *config.Config, logger *zap.Logger) (*Clients, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// grpc.WithBlock(), Connection will be established in background; calls will wait until ready
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	locConn, err := grpc.DialContext(ctx, cfg.Services.LocationRequest, opts...)
	if err != nil {
		return nil, fmt.Errorf("connecting to location-request: %w", err)
	}

	evtConn, err := grpc.DialContext(ctx, cfg.Services.EventManager, opts...)
	if err != nil {
		locConn.Close()
		return nil, fmt.Errorf("connecting to event-manager: %w", err)
	}

	return &Clients{
		locationRequestConn: locConn,
		eventManagerConn:    evtConn,
		logger:              logger, //swaroop added the logger to clients struct
	}, nil
}

// DetermineLocation forwards a location request to the location-request service
// In production this would use the generated gRPC stub; here we show the pattern.
func (c *Clients) DetermineLocation(ctx context.Context, sessionID string, req *api.LocationContextData) (*api.LocationContextDataResp, error) {
	// In a fully generated proto setup this would call the stub:
	// client := pb.NewLocationRequestServiceClient(c.locationRequestConn)
	// protoReq := mapToProto(req)
	// resp, err := client.DetermineLocation(ctx, protoReq)
	// return mapFromProto(resp), err

	// Simulated response for compilation/demonstration purposes
	// Replace with actual gRPC stub call when proto generation is done

	//swaroop changes for request forwarding to location request service
	client := pb.NewLocationRequestServiceClient(c.locationRequestConn)

	responseTime := pb.ResponseTimeClass_RESPONSE_TIME_LOW_DELAY
	switch req.LcsQoS.ResponseTime {
	case "DELAY_TOLERANT":
		responseTime = pb.ResponseTimeClass_RESPONSE_TIME_DELAY_TOLERANT
	case "LOW_DELAY":
		responseTime = pb.ResponseTimeClass_RESPONSE_TIME_LOW_DELAY
	}

	clientType := pb.LcsClientType_LCS_CLIENT_TYPE_UNSPECIFIED
	if v, ok := pb.LcsClientType_value["LCS_CLIENT_TYPE_"+req.LcsClientType]; ok {
		clientType = pb.LcsClientType(v)
	}

	protoReq := &pb.LocationRequestMsg{
		SessionId: sessionID,
		Supi:      req.Supi,
		Pei:       req.Pei,
		Gpsi:      req.Gpsi,
		LcsQos: &pb.LcsQoS{
			HorizontalAccuracyMeters: int32(req.LcsQoS.Accuracy),
			ResponseTime:             responseTime,
			ConfidencePercent:        int32(req.LcsQoS.ConfidenceLevel),
		},
		LcsClientType: clientType,
	}

	resp, err := client.DetermineLocation(ctx, protoReq)
	if err != nil {
		return nil, fmt.Errorf("location request g-RPC failed: %w", err)
	}

	//logging the response from location request service
	c.logger.Info("location response received from location-request service")

	result := &api.LocationContextDataResp{
		AccuracyFulfilmentIndicator: resp.AccuracyIndicator.String(),
	}
	if resp.LocationEstimate != nil {
		result.LocationEstimate = api.LocationEstimateJson{
			Shape:       resp.LocationEstimate.Shape,
			Point:       &api.LatLon{Lat: resp.LocationEstimate.Latitude, Lon: resp.LocationEstimate.Longitude},
			Altitude:    resp.LocationEstimate.Altitude,
			Uncertainty: resp.LocationEstimate.HorizontalUncertainty,
		}
	}

	// resp := &api.LocationContextDataResp{
	// 	LocationEstimate: api.LocationEstimateJson{
	// 		Shape:       "POINT_ALTITUDE_UNCERTAINTY",
	// 		Point:       &api.LatLon{Lat: 37.4219983, Lon: -122.084},
	// 		Altitude:    25.5,
	// 		Uncertainty: 8.5,
	// 		Confidence:  95,
	// 	},
	// 	AccuracyFulfilmentIndicator: "REQUESTED_ACCURACY_FULFILLED",
	// 	PositioningDataList: []api.PositioningDataEntry{
	// 		{PosMethod: "A_GNSS", PosUsage: "POSITION_USED"},
	// 	},
	// }
	return result, nil
}

// CancelLocation cancels an ongoing location session
func (c *Clients) CancelLocation(ctx context.Context, sessionRef string) error {
	// client := pb.NewLocationRequestServiceClient(c.locationRequestConn)
	// _, err := client.CancelLocation(ctx, &pb.CancelRequest{SessionId: sessionRef})
	return nil
}

// Subscribe creates an event subscription via the event manager
func (c *Clients) Subscribe(ctx context.Context, req *api.SubscriptionRequest) (string, error) {
	// client := pb.NewEventManagerServiceClient(c.eventManagerConn)
	// resp, err := client.Subscribe(ctx, mapSubToProto(req))
	// return resp.SubscriptionId, err
	return fmt.Sprintf("sub-%d", time.Now().UnixNano()), nil
}

// Unsubscribe cancels an event subscription
func (c *Clients) Unsubscribe(ctx context.Context, subID string) error {
	// client := pb.NewEventManagerServiceClient(c.eventManagerConn)
	// _, err := client.Unsubscribe(ctx, &pb.UnsubscribeRequest{SubscriptionId: subID})
	return nil
}

// Close closes all gRPC connections
func (c *Clients) Close() {
	if c.locationRequestConn != nil {
		c.locationRequestConn.Close()
	}
	if c.eventManagerConn != nil {
		c.eventManagerConn.Close()
	}
}
