// Package fusion implements multi-method position estimate fusion.
package fusion

import (
	"fmt"
	"math"
	"sort"

	"github.com/5g-lmf/common/types"
)

// WeightedFusion fuses multiple position estimates using inverse-variance weighting
type WeightedFusion struct{}

// NewWeightedFusion creates a new weighted fusion instance
func NewWeightedFusion() *WeightedFusion { return &WeightedFusion{} }

// Fuse combines multiple position estimates into one optimal estimate
func (f *WeightedFusion) Fuse(estimates []types.PositionEstimate) (*types.PositionEstimate, error) {
	if len(estimates) == 0 {
		return nil, fmt.Errorf("no estimates to fuse")
	}
	if len(estimates) == 1 {
		e := estimates[0]
		return &e, nil
	}

	// Outlier rejection: remove estimates with sigma > 3x median sigma
	filtered := rejectOutliers(estimates)
	if len(filtered) == 0 {
		filtered = estimates // fallback: use all if all would be rejected
	}

	// Inverse-variance weighted average
	var wLatSum, wLonSum, wAltSum float64
	var wSumLat, wSumLon, wSumAlt float64
	var wConfSum, wConfWeight float64

	for _, e := range filtered {
		sigLat := e.SigmaLat
		if sigLat < 1e-10 {
			sigLat = 1.0 / 111319.0 // 1m floor
		}
		sigLon := e.SigmaLon
		if sigLon < 1e-10 {
			sigLon = 1.0 / 111319.0
		}
		sigAlt := e.SigmaAlt
		if sigAlt < 0.1 {
			sigAlt = 100.0
		}

		wl := 1.0 / (sigLat * sigLat)
		wn := 1.0 / (sigLon * sigLon)
		wa := 1.0 / (sigAlt * sigAlt)
		wc := wl // weight confidence by lat precision

		wLatSum += wl * e.Latitude
		wLonSum += wn * e.Longitude
		wAltSum += wa * e.Altitude
		wSumLat += wl
		wSumLon += wn
		wSumAlt += wa
		wConfSum += wc * float64(e.Confidence)
		wConfWeight += wc
	}

	fusedLat := wLatSum / wSumLat
	fusedLon := wLonSum / wSumLon
	fusedAlt := wAltSum / wSumAlt
	fusedSigmaLat := 1.0 / math.Sqrt(wSumLat)
	fusedSigmaLon := 1.0 / math.Sqrt(wSumLon)
	fusedSigmaAlt := 1.0 / math.Sqrt(wSumAlt)
	fusedConfidence := int(wConfSum / wConfWeight)
	if fusedConfidence > 100 {
		fusedConfidence = 100
	}

	// Use best method (smallest sigma)
	bestMethod := filtered[0].Method
	bestSigma := filtered[0].SigmaLat
	for _, e := range filtered {
		if e.SigmaLat < bestSigma {
			bestSigma = e.SigmaLat
			bestMethod = e.Method
		}
	}

	return &types.PositionEstimate{
		Latitude:   fusedLat,
		Longitude:  fusedLon,
		Altitude:   fusedAlt,
		SigmaLat:   fusedSigmaLat,
		SigmaLon:   fusedSigmaLon,
		SigmaAlt:   fusedSigmaAlt,
		Confidence: fusedConfidence,
		Method:     bestMethod,
		Timestamp:  filtered[0].Timestamp,
	}, nil
}

// rejectOutliers removes estimates with sigma > 3x median sigma
func rejectOutliers(estimates []types.PositionEstimate) []types.PositionEstimate {
	if len(estimates) < 3 {
		return estimates
	}

	sigmas := make([]float64, len(estimates))
	for i, e := range estimates {
		sigmas[i] = math.Sqrt(e.SigmaLat*e.SigmaLat+e.SigmaLon*e.SigmaLon) * 111319 // degrees → meters
	}

	sorted := make([]float64, len(sigmas))
	copy(sorted, sigmas)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	threshold := 3.0 * median

	var filtered []types.PositionEstimate
	for i, e := range estimates {
		if sigmas[i] <= threshold {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
