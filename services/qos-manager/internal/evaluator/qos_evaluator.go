// Package evaluator implements QoS fulfilment evaluation per 3GPP TS 23.273.
package evaluator

import (
	"math"
	"time"

	"github.com/5g-lmf/common/types"
)

// ResponseTimeDeadlines maps response time classes to deadlines
var responseTimeDeadlines = map[types.ResponseTimeClass]time.Duration{
	types.ResponseTimeNoDelay:         500 * time.Millisecond,
	types.ResponseTimeLowDelay:        1 * time.Second,
	types.ResponseTimeDelayTolerant:   10 * time.Second,
	types.ResponseTimeDelayTolerantV2: 30 * time.Second,
}

// QosEvaluator evaluates positioning QoS against requested thresholds
type QosEvaluator struct{}

// NewQosEvaluator creates a new QoS evaluator
func NewQosEvaluator() *QosEvaluator { return &QosEvaluator{} }

// EvaluateAccuracyFulfilment determines if a position estimate meets requested QoS
func (e *QosEvaluator) EvaluateAccuracyFulfilment(
	requested types.LcsQoS,
	achieved types.PositionEstimate,
) types.AccuracyFulfilmentIndicator {
	// Convert sigma (degrees) to meters for comparison
	// 1° latitude ≈ 111,319m
	sigmaLatM := achieved.SigmaLat * 111319.0
	sigmaLonM := achieved.SigmaLon * 111319.0 * math.Cos(achieved.Latitude*math.Pi/180)
	hSigmaM := math.Sqrt(sigmaLatM*sigmaLatM + sigmaLonM*sigmaLonM)

	// Check horizontal accuracy (use 2-sigma = ~95% confidence)
	if requested.HorizontalAccuracy > 0 {
		thresholdM := float64(requested.HorizontalAccuracy)
		// Compare 1-sigma uncertainty; threshold is typically 1-sigma
		if hSigmaM > thresholdM {
			return types.AccuracyAttemptedFailed
		}
	}

	// Check vertical accuracy if requested
	if requested.VerticalCoordReq && requested.VerticalAccuracy > 0 {
		if achieved.SigmaAlt > float64(requested.VerticalAccuracy) {
			return types.AccuracyAttemptedFailed
		}
	}

	return types.AccuracyFulfilled
}

// IsResponseTimeBreached checks if the QoS response time deadline has been exceeded
func (e *QosEvaluator) IsResponseTimeBreached(started time.Time, rtClass types.ResponseTimeClass) bool {
	deadline, ok := responseTimeDeadlines[rtClass]
	if !ok {
		deadline = 30 * time.Second // fallback
	}
	return time.Since(started) > deadline
}

// TimeRemaining returns how much time is left before the response time deadline
func (e *QosEvaluator) TimeRemaining(started time.Time, rtClass types.ResponseTimeClass) time.Duration {
	deadline, ok := responseTimeDeadlines[rtClass]
	if !ok {
		deadline = 30 * time.Second
	}
	remaining := deadline - time.Since(started)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SelectFallbackMethod picks the next best method from the available list
func (e *QosEvaluator) SelectFallbackMethod(
	failed types.PositioningMethod,
	available []types.PositioningMethod,
	qos types.LcsQoS,
) types.PositioningMethod {
	for _, m := range available {
		if m != failed {
			return m
		}
	}
	return types.PositioningMethodCellID
}

// ComputeGDOP computes Geometric Dilution of Precision for a 2D problem
// cellPositions: [][lat,lon] of base stations in degrees
// uePosition: [lat,lon] of UE in degrees
func ComputeGDOP(cellPositions [][2]float64, uePosition [2]float64) float64 {
	n := len(cellPositions)
	if n < 2 {
		return 10.0
	}

	// Build geometry matrix H (n x 2)
	H := make([][]float64, n)
	ux, uy := uePosition[0], uePosition[1]
	for i, c := range cellPositions {
		dx := ux - c[0]
		dy := uy - c[1]
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < 1e-12 {
			dist = 1e-12
		}
		H[i] = []float64{dx / dist, dy / dist}
	}

	// HtH = H^T * H (2x2)
	var HtH [2][2]float64
	for i := 0; i < n; i++ {
		HtH[0][0] += H[i][0] * H[i][0]
		HtH[0][1] += H[i][0] * H[i][1]
		HtH[1][0] += H[i][1] * H[i][0]
		HtH[1][1] += H[i][1] * H[i][1]
	}

	// GDOP = sqrt(trace((HtH)^-1)) for 2x2 system
	det := HtH[0][0]*HtH[1][1] - HtH[0][1]*HtH[1][0]
	if math.Abs(det) < 1e-12 {
		return 10.0
	}
	// Trace of inverse = (HtH[1][1] + HtH[0][0]) / det
	traceinv := (HtH[1][1] + HtH[0][0]) / det
	if traceinv < 0 {
		return 10.0
	}
	return math.Sqrt(traceinv)
}
