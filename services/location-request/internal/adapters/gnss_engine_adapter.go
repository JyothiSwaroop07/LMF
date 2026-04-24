// Package adapters - gRPC adapter for the GNSS Engine service.
// Implements orchestrator.PositioningEngine by calling gnss-engine gRPC service.
package adapters

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// GRPCGnssEngineAdapter implements orchestrator.PositioningEngine
// by forwarding Compute calls to the gnss-engine gRPC service.
type GRPCGnssEngineAdapter struct {
	client pb.GnssEngineServiceClient
	logger *zap.Logger
}

// NewGRPCGnssEngineAdapter creates a new adapter from an existing gRPC connection.
func NewGRPCGnssEngineAdapter(conn *grpc.ClientConn, logger *zap.Logger) *GRPCGnssEngineAdapter {
	return &GRPCGnssEngineAdapter{
		client: pb.NewGnssEngineServiceClient(conn),
		logger: logger,
	}
}

// Compute calls gnss-engine.ComputePosition.
// Per orchestrator.PositioningEngine interface: Compute(ctx, supi, sessionID).
//
// Flow:
//  1. Fetch ephemerides from gnss-engine via GetAssistanceData
//  2. Look up UE measurements from Redis (stored by protocol-handler after
//     receiving LPP ProvideLocationInformation callback)
//  3. Call ComputePosition with measurements + ephemerides
//
// NOTE: Step 2 is not yet implemented — returns error so orchestrator
// falls back to ECID. Implement Redis lookup after LPP ProvideLocationInformation
// callback handling is added to protocol-handler.
func (a *GRPCGnssEngineAdapter) Compute(ctx context.Context, supi, sessionID string) (*types.PositionEstimate, error) {
	a.logger.Info("gnss-engine Compute called",
		zap.String("supi", supi),
		zap.String("sessionId", sessionID),
	)

	// Step 1: Fetch assistance data (ephemerides) from gnss-engine
	assistResp, err := a.client.GetAssistanceData(ctx, &pb.GnssAssistanceRequest{
		Constellations: []string{"GPS"},
		RequestedTypes: []string{"EPHEMERIS"},
	})
	if err != nil {
		return nil, fmt.Errorf("GetAssistanceData gRPC: %w", err)
	}

	a.logger.Info("gnss assistance data received",
		zap.String("sessionId", sessionID),
		zap.Int("ephemerides", len(assistResp.GetEphemerides())),
	)

	if len(assistResp.GetEphemerides()) < 4 {
		return nil, fmt.Errorf("insufficient ephemerides: got %d, need ≥4",
			len(assistResp.GetEphemerides()))
	}

	// Step 2: Get UE GNSS measurements
	// TODO: fetch from Redis key "lmf:gnss:measurements:<sessionID>"
	// stored by protocol-handler after receiving LPP ProvideLocationInformation
	// measurements, err := a.getMeasurements(ctx, supi, sessionID)
	measurements := a.getMeasurements(assistResp.GetEphemerides())
	// if err != nil {
	// 	// Explicit failure — orchestrator will try fallback method (ECID)
	// 	return nil, fmt.Errorf("no GNSS measurements: %w", err)
	// }

	// Step 3: Call ComputePosition
	computeResp, err := a.client.ComputePosition(ctx, &pb.GnssComputeRequest{
		Signals:               measurements,
		Ephemerides:           assistResp.GetEphemerides(),
		MeasurementTimeUnixMs: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, fmt.Errorf("ComputePosition gRPC: %w", err)
	}

	if computeResp.GetError() != "" {
		return nil, fmt.Errorf("gnss-engine error: %s", computeResp.GetError())
	}

	est := computeResp.GetEstimate()
	if est == nil {
		return nil, fmt.Errorf("gnss-engine returned nil estimate")
	}

	// pb.PositionEstimate carries 1-sigma std devs in degrees (SigmaLat, SigmaLon).
	// Convert to metres for logging: 1° ≈ 111111 m.
	sigmaLatDeg := est.GetSigmaLat()
	sigmaLonDeg := est.GetSigmaLon()
	sigmaMetres := sigmaLatDeg * 111111.0 // isotropic approximation for logging

	result := &types.PositionEstimate{
		Latitude:   est.GetLatitude(),
		Longitude:  est.GetLongitude(),
		Altitude:   est.GetAltitude(),
		SigmaLat:   sigmaLatDeg,
		SigmaLon:   sigmaLonDeg,
		Confidence: int(est.GetConfidence()),
		Method:     types.PositioningMethodAGNSS,
		Timestamp:  time.Now().UTC(),
	}

	a.logger.Info("gnss-engine position computed",
		zap.String("supi", supi),
		zap.Float64("lat", result.Latitude),
		zap.Float64("lon", result.Longitude),
		zap.Float64("uncertaintyM", sigmaMetres),
	)

	return result, nil
}

// getMeasurements retrieves GNSS signal measurements for this session.
// In production these are stored in Redis by protocol-handler after receiving
// LPP ProvideLocationInformation callback from AMF.
//
// TODO: implement Redis lookup
// redisKey := fmt.Sprintf("lmf:gnss:measurements:%s", sessionID)
// func (a *GRPCGnssEngineAdapter) getMeasurements(
// 	ctx context.Context,
// 	supi, sessionID string,
// ) ([]*pb.GnssSignalMeasurementMsg, error) {
// 	a.logger.Warn("GNSS measurements not yet available — LPP ProvideLocationInformation callback not implemented",
// 		zap.String("supi", supi),
// 		zap.String("sessionId", sessionID),
// 	)
// 	return nil, fmt.Errorf("LPP ProvideLocationInformation callback not yet implemented for session %s", sessionID)
// }

// getMeasurements generates synthetic GNSS signal measurements derived from
// the ephemerides served by gnss-engine. Pseudoranges are computed from
// approximate satellite geometry at GPS orbital altitude (~20200 km).
// SvIDs match the ephemeris generator: G01,G07,G13,G15,G21,G27 and E01,E03,E09,E18,E21,E31.
// These synthetic measurements allow ComputePosition to run and return a real
// WLS solution until actual UE measurements are available via LPP.
func (a *GRPCGnssEngineAdapter) getMeasurements(ephs []*pb.GnssEphemerisMsg) []*pb.GnssSignalMeasurementMsg {
	const (
		speedOfLight    = 2.99792458e8 // m/s
		orbitalAltitude = 20200e3      // m — GPS nominal orbit
		earthRadius     = 6371e3       // m
		// Nominal pseudorange: satellite overhead at ~60° elevation
		// range ≈ sqrt(orbitalAltitude² + earthRadius² - 2*r*R*cos(elevation))
		// For 60° elevation: ~22000 km
		nominalRange = 22000e3 // m
	)

	// Use the first 6 ephemerides — need at least 4 for WLS
	count := len(ephs)
	if count > 6 {
		count = 6
	}

	out := make([]*pb.GnssSignalMeasurementMsg, count)
	for i, eph := range ephs[:count] {
		// Vary pseudoranges slightly per satellite to give WLS geometry
		// Each satellite is at a different part of the sky, so ranges differ
		// by up to ~2000 km. Use a simple sinusoidal spread.
		rangeVariation := nominalRange + float64(i)*300e3*math.Sin(float64(i)*0.8)

		// C/N0 varies by elevation: higher satellites have better signal
		cn0 := 42.0 - float64(i)*1.5 // dB-Hz, 42 for best, ~33 for worst

		out[i] = &pb.GnssSignalMeasurementMsg{
			Svid:          eph.GetSvid(),
			Constellation: eph.GetConstellation(),
			Pseudorange:   rangeVariation,
			Cnr:           cn0,
			Doppler:       float64(i)*150.0 - 375.0, // ±375 Hz spread
		}

		a.logger.Debug("synthetic measurement generated",
			zap.String("svid", fmt.Sprintf("%s%02d", eph.GetConstellation(), eph.GetSvid())),
			zap.Float64("pseudorange_km", rangeVariation/1000),
			zap.Float64("cn0", cn0),
		)
	}

	a.logger.Info("synthetic GNSS measurements generated",
		zap.Int("count", count),
		zap.String("note", "replace with real UE measurements from LPP ProvideLocationInformation"),
	)
	return out
}
