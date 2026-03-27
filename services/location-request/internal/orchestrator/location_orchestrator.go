// Package orchestrator implements the LMF location request flow per 3GPP TS 23.273 §6.
//
// Flow:
//  1. Privacy check (Privacy Auth MS-13)
//  2. Session create (Session Manager MS-03)
//  3. UE capability fetch (Protocol Handler MS-04)
//  4. Method selection (Method Selector MS-05)
//  5. Positioning measurement trigger (Protocol Handler MS-04)
//  6. Positioning computation (GNSS/TDOA/E-CID/RTT engine)
//  7. Fusion if multiple estimates (Fusion Engine MS-10)
//  8. QoS evaluation (QoS Manager MS-14)
//  9. Session close + result return
package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
)

// Dependencies bundles all service interfaces used by the orchestrator.
type Dependencies struct {
	Privacy         PrivacyService
	SessionMgr      SessionService
	MethodSelector  MethodSelectorService
	ProtocolHandler ProtocolService
	GnssEngine      PositioningEngine
	TdoaEngine      PositioningEngine
	EcidEngine      EcidEngine
	RttEngine       RttEngine
	FusionEngine    FusionService
	QosMgr          QoSService
}

// PrivacyService checks LCS privacy per 3GPP TS 23.273 §8.
type PrivacyService interface {
	CheckPrivacy(ctx context.Context, supi, sessionID string, clientType types.LcsClientType) (allowed bool, err error)
}

// SessionService manages LCS sessions in Redis.
type SessionService interface {
	Create(ctx context.Context, supi string, qos types.LcsQoS) (sessionID string, err error)
	UpdateStatus(ctx context.Context, sessionID string, status types.SessionStatus) error
	Delete(ctx context.Context, sessionID string) error
}

// MethodSelectorService selects the best positioning method.
type MethodSelectorService interface {
	Select(ctx context.Context, req types.MethodSelectionRequest) (*types.MethodSelectionResult, error)
}

// ProtocolService sends LPP/NRPPa messages to the UE/gNB and fetches UE capabilities.
type ProtocolService interface {
	GetUECapabilities(ctx context.Context, supi string) (*types.UeCapabilities, error)
	TriggerMeasurement(ctx context.Context, supi, sessionID string, method types.PositioningMethod) error
}

// PositioningEngine computes a position from GNSS or TDOA measurements.
type PositioningEngine interface {
	Compute(ctx context.Context, supi, sessionID string) (*types.PositionEstimate, error)
}

// EcidEngine computes a position from E-CID measurements.
type EcidEngine interface {
	Compute(ctx context.Context, meas types.EcidMeasurements) (*types.PositionEstimate, error)
}

// RttEngine computes a position from Multi-RTT measurements.
type RttEngine interface {
	Compute(ctx context.Context, meas types.MultiRttMeasurements) (*types.PositionEstimate, error)
}

// FusionService fuses multiple position estimates.
type FusionService interface {
	Fuse(ctx context.Context, supi string, estimates []*types.PositionEstimate) (*types.PositionEstimate, error)
}

// QoSService evaluates whether a position estimate satisfies the requested QoS.
type QoSService interface {
	Evaluate(ctx context.Context, estimate *types.PositionEstimate, qos types.LcsQoS, elapsedMs float64) (types.AccuracyFulfilmentIndicator, error)
}

// LocationOrchestrator coordinates the end-to-end LCS location request flow.
type LocationOrchestrator struct {
	deps   Dependencies
	logger *zap.Logger
}

// NewLocationOrchestrator creates a LocationOrchestrator.
func NewLocationOrchestrator(deps Dependencies, logger *zap.Logger) *LocationOrchestrator {
	return &LocationOrchestrator{deps: deps, logger: logger}
}

// DetermineLocation executes the full LMF positioning flow for the given session.
func (o *LocationOrchestrator) DetermineLocation(ctx context.Context, session types.LcsSession) (*types.LocationEstimate, error) {
	start := time.Now()
	logger := o.logger.With(
		zap.String("supi", session.Supi),
		zap.String("sessionId", session.SessionID),
	)

	logger.Info("starting location determination flow from location-request:orchestrator.DetermineLocation")

	// Step 1: Privacy check
	allowed, err := o.deps.Privacy.CheckPrivacy(ctx, session.Supi, session.SessionID, session.LcsClientType)
	if err != nil {
		return nil, fmt.Errorf("privacy check: %w", err)
	}
	if !allowed {
		middleware.LocationRequestsTotal.WithLabelValues("unknown", "denied", string(session.LcsClientType)).Inc()
		return nil, fmt.Errorf("location request denied by privacy policy")
	}
	logger.Info("privacy check passed")

	// Step 2: Create session
	sessionID, err := o.deps.SessionMgr.Create(ctx, session.Supi, session.LcsQoS)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer func() {
		_ = o.deps.SessionMgr.Delete(context.Background(), sessionID)
	}()
	logger.Info("session created", zap.String("newSessionId", sessionID))

	// Step 3: Fetch UE capabilities
	caps, err := o.deps.ProtocolHandler.GetUECapabilities(ctx, session.Supi)
	if err != nil {
		logger.Warn("UE capabilities fetch failed, using defaults", zap.Error(err))
		caps = &types.UeCapabilities{} // Degraded mode
	}

	// Step 4: Select positioning method
	selReq := types.MethodSelectionRequest{
		UeCaps: *caps,
		LcsQoS: session.LcsQoS,
	}
	selResult, err := o.deps.MethodSelector.Select(ctx, selReq)
	if err != nil {
		return nil, fmt.Errorf("method selection: %w", err)
	}
	logger.Info("positioning method selected",
		zap.String("method", string(selResult.SelectedMethod)),
	)

	// Step 5: Trigger measurement via protocol handler
	if err := o.deps.ProtocolHandler.TriggerMeasurement(ctx, session.Supi, sessionID, selResult.SelectedMethod); err != nil {
		return nil, fmt.Errorf("measurement trigger: %w", err)
	}

	// Step 6: Run positioning engine(s)
	estimates, err := o.runEngines(ctx, session.Supi, sessionID, selResult)
	if err != nil {
		return nil, fmt.Errorf("positioning engines: %w", err)
	}
	if len(estimates) == 0 {
		return nil, fmt.Errorf("no position estimate obtained")
	}

	// Step 7: Fuse estimates if multiple
	var final *types.PositionEstimate
	if len(estimates) > 1 {
		final, err = o.deps.FusionEngine.Fuse(ctx, session.Supi, estimates)
		if err != nil {
			logger.Warn("fusion failed, using best single estimate", zap.Error(err))
			final = bestEstimate(estimates)
		}
	} else {
		final = estimates[0]
	}

	// Step 8: QoS evaluation
	elapsedMs := float64(time.Since(start).Milliseconds())
	acc, err := o.deps.QosMgr.Evaluate(ctx, final, session.LcsQoS, elapsedMs)
	if err != nil {
		logger.Warn("QoS evaluation failed", zap.Error(err))
	}
	logger.Info("QoS evaluation complete",
		zap.String("accuracy", string(acc)),
		zap.Float64("elapsedMs", elapsedMs),
	)

	// Step 9: Update session status
	_ = o.deps.SessionMgr.UpdateStatus(context.Background(), sessionID, types.SessionStatusCompleted)

	// Compute horizontal uncertainty in meters (1-sigma lat/lon degrees → meters)
	// 1° latitude ≈ 111111 m; use larger of sigmaLat/sigmaLon in meters as horizontal uncertainty
	uncertMeters := final.SigmaLat * 111111.0
	if lonUncert := final.SigmaLon * 111111.0; lonUncert > uncertMeters {
		uncertMeters = lonUncert
	}

	middleware.LocationRequestsTotal.WithLabelValues(string(selResult.SelectedMethod), "success", string(session.LcsClientType)).Inc()
	middleware.LocationAccuracyAchieved.WithLabelValues(string(selResult.SelectedMethod)).Observe(uncertMeters)

	return &types.LocationEstimate{
		Latitude:              final.Latitude,
		Longitude:             final.Longitude,
		Altitude:              final.Altitude,
		HorizontalUncertainty: uncertMeters,
		Confidence:            final.Confidence,
		Shape:                 types.GADShapePoint,
		Timestamp:             final.Timestamp,
	}, nil
}

// runEngines runs the selected primary engine (and optionally fallbacks).
func (o *LocationOrchestrator) runEngines(
	ctx context.Context,
	supi, sessionID string,
	sel *types.MethodSelectionResult,
) ([]*types.PositionEstimate, error) {
	type engineFn func(context.Context) (*types.PositionEstimate, error)

	pickEngine := func(method types.PositioningMethod) engineFn {
		switch method {
		case types.PositioningMethodAGNSS:
			return func(ctx context.Context) (*types.PositionEstimate, error) {
				return o.deps.GnssEngine.Compute(ctx, supi, sessionID)
			}
		case types.PositioningMethodDLTDOA, types.PositioningMethodOTDOA:
			return func(ctx context.Context) (*types.PositionEstimate, error) {
				return o.deps.TdoaEngine.Compute(ctx, supi, sessionID)
			}
		case types.PositioningMethodNREcid:
			return func(ctx context.Context) (*types.PositionEstimate, error) {
				return o.deps.EcidEngine.Compute(ctx, types.EcidMeasurements{ServingCellNci: supi})
			}
		case types.PositioningMethodMultiRTT:
			return func(ctx context.Context) (*types.PositionEstimate, error) {
				return o.deps.RttEngine.Compute(ctx, types.MultiRttMeasurements{})
			}
		default:
			return func(ctx context.Context) (*types.PositionEstimate, error) {
				return o.deps.EcidEngine.Compute(ctx, types.EcidMeasurements{ServingCellNci: supi})
			}
		}
	}

	primaryFn := pickEngine(sel.SelectedMethod)
	est, err := primaryFn(ctx)
	if err != nil {
		o.logger.Warn("primary engine failed, trying fallback",
			zap.String("primary", string(sel.SelectedMethod)),
			zap.Error(err),
		)
		for _, fb := range sel.FallbackMethods {
			fbFn := pickEngine(fb)
			est, err = fbFn(ctx)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("all engines failed: %w", err)
		}
	}

	return []*types.PositionEstimate{est}, nil
}

func bestEstimate(estimates []*types.PositionEstimate) *types.PositionEstimate {
	if len(estimates) == 0 {
		return nil
	}
	best := estimates[0]
	for _, e := range estimates[1:] {
		if e.SigmaLat < best.SigmaLat {
			best = e
		}
	}
	return best
}
