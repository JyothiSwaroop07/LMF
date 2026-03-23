// Package adapters contains no-op implementations of orchestrator interfaces.
// Replace these with real gRPC stub adapters that forward calls to downstream services.
package adapters

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/types"
)

// NoopPrivacy always allows location requests.
type NoopPrivacy struct{}

func (n *NoopPrivacy) CheckPrivacy(_ context.Context, _, _ string, _ types.LcsClientType) (bool, error) {
	return true, nil
}

// NoopSession returns a fixed session ID.
type NoopSession struct{}

func (n *NoopSession) Create(_ context.Context, _ string, _ types.LcsQoS) (string, error) {
	return "noop-session-0", nil
}
func (n *NoopSession) UpdateStatus(_ context.Context, _ string, _ types.SessionStatus) error {
	return nil
}
func (n *NoopSession) Delete(_ context.Context, _ string) error { return nil }

// NoopMethodSelector always returns E-CID.
type NoopMethodSelector struct{}

func (n *NoopMethodSelector) Select(_ context.Context, _ types.MethodSelectionRequest) (*types.MethodSelectionResult, error) {
	return &types.MethodSelectionResult{
		SelectedMethod:      types.PositioningMethodNREcid,
		FallbackMethods:     nil,
		EstimatedAccuracy:   100.0,
		EstimatedResponseMs: 200,
	}, nil
}

// NoopProtocol returns empty UE capabilities and does nothing for measurement trigger.
type NoopProtocol struct{}

func (n *NoopProtocol) GetUECapabilities(_ context.Context, _ string) (*types.UeCapabilities, error) {
	return &types.UeCapabilities{}, nil
}
func (n *NoopProtocol) TriggerMeasurement(_ context.Context, _, _ string, _ types.PositioningMethod) error {
	return nil
}

// NoopEngine returns a placeholder position estimate at 0,0 with 1km uncertainty.
type NoopEngine struct{}

func (n *NoopEngine) Compute(_ context.Context, _, _ string) (*types.PositionEstimate, error) {
	return &types.PositionEstimate{
		Latitude:   0,
		Longitude:  0,
		SigmaLat:   0.009, // ~1km
		SigmaLon:   0.009,
		Confidence: 39,
		Method:     types.PositioningMethodNREcid,
	}, nil
}

// NoopEcid returns a placeholder position estimate.
type NoopEcid struct{}

func (n *NoopEcid) Compute(_ context.Context, _ types.EcidMeasurements) (*types.PositionEstimate, error) {
	return &types.PositionEstimate{
		Latitude:   0,
		Longitude:  0,
		SigmaLat:   0.0045, // ~500m
		SigmaLon:   0.0045,
		Confidence: 39,
		Method:     types.PositioningMethodNREcid,
	}, nil
}

// NoopRtt returns a placeholder position estimate.
type NoopRtt struct{}

func (n *NoopRtt) Compute(_ context.Context, _ types.MultiRttMeasurements) (*types.PositionEstimate, error) {
	return &types.PositionEstimate{
		Latitude:   0,
		Longitude:  0,
		SigmaLat:   0.00045, // ~50m
		SigmaLon:   0.00045,
		Confidence: 67,
		Method:     types.PositioningMethodMultiRTT,
	}, nil
}

// NoopFusion returns the first input estimate unchanged.
type NoopFusion struct{}

func (n *NoopFusion) Fuse(_ context.Context, _ string, estimates []*types.PositionEstimate) (*types.PositionEstimate, error) {
	if len(estimates) == 0 {
		return nil, fmt.Errorf("no estimates to fuse")
	}
	return estimates[0], nil
}

// NoopQoS always returns AccuracyFulfilled.
type NoopQoS struct{}

func (n *NoopQoS) Evaluate(_ context.Context, _ *types.PositionEstimate, _ types.LcsQoS, _ float64) (types.AccuracyFulfilmentIndicator, error) {
	return types.AccuracyFulfilled, nil
}
