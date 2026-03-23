// Package positioning implements NR Enhanced Cell ID (E-CID) positioning
// per 3GPP TS 38.305 using Timing Advance, beam sector, and RSRP fingerprinting.
package positioning

import (
	"fmt"
	"math"
	"time"

	"github.com/5g-lmf/common/types"
)

const (
	speedOfLight = 299792458.0 // m/s
	// NR TA step size for numerology 0 (15 kHz SCS):
	// TA_offset = 624 Ts, 1 NTA step = 16*64 Ts = 1024 Ts ≈ 33.3ns → 10m
	taNR15kHz = 10.0 // meters per TA unit for 15kHz SCS (approximate)

	// Log-distance path loss exponent (urban macro)
	pathLossExponent = 3.5
	// Reference distance (m)
	d0 = 1.0
	// Transmit power assumed for gNB (dBm)
	txPowerDBm = 23.0
	// Path loss at d0=1m (free space at 3.5GHz ≈ 43.5dB)
	pl0dB = 43.5
)

// EcidSolver computes UE position using Enhanced Cell ID methods
type EcidSolver struct{}

// NewEcidSolver creates a new E-CID solver
func NewEcidSolver() *EcidSolver { return &EcidSolver{} }

// ComputePosition estimates UE position from E-CID measurements
func (s *EcidSolver) ComputePosition(
	measurements types.EcidMeasurements,
	servingCell types.CellGeometry,
	neighborCells []types.CellGeometry,
) (*types.PositionEstimate, error) {
	if measurements.ServingCellNci == "" {
		return nil, fmt.Errorf("serving cell NCI is required")
	}

	var estimates []posEstimate

	// Method 1: TA-based range estimation
	if measurements.TimingAdvance > 0 {
		est := s.computeTAEstimate(measurements, servingCell)
		estimates = append(estimates, est)
	}

	// Method 2: RSRP weighted centroid (if ≥2 neighbor cells)
	allCells := append([]types.CellGeometry{servingCell}, neighborCells...)
	if len(measurements.RsrpMeasurements) >= 2 {
		est, err := s.computeRsrpCentroid(measurements.RsrpMeasurements, allCells)
		if err == nil {
			estimates = append(estimates, est)
		}
	}

	if len(estimates) == 0 {
		// Fallback: serving cell position with large uncertainty
		return &types.PositionEstimate{
			Latitude:   servingCell.Latitude,
			Longitude:  servingCell.Longitude,
			Altitude:   servingCell.Altitude,
			SigmaLat:   0.005,  // ~550m
			SigmaLon:   0.005,
			SigmaAlt:   200.0,
			Confidence: 39,
			Method:     types.PositioningMethodCellID,
			Timestamp:  time.Now(),
		}, nil
	}

	if len(estimates) == 1 {
		return estimates[0].toPositionEstimate(), nil
	}

	// Fuse multiple estimates with inverse-variance weighting
	return fuseEstimates(estimates), nil
}

type posEstimate struct {
	lat, lon, alt     float64
	sigmaLat, sigmaLon float64
	confidence        int
}

func (p posEstimate) toPositionEstimate() *types.PositionEstimate {
	return &types.PositionEstimate{
		Latitude:   p.lat,
		Longitude:  p.lon,
		Altitude:   p.alt,
		SigmaLat:   p.sigmaLat,
		SigmaLon:   p.sigmaLon,
		SigmaAlt:   p.sigmaLat * 111319 * 3,
		Confidence: p.confidence,
		Method:     types.PositioningMethodNREcid,
		Timestamp:  time.Now(),
	}
}

// computeTAEstimate estimates position using Timing Advance
// TA gives a range ring around the serving cell.
// With beam sector, we narrow it to an arc.
func (s *EcidSolver) computeTAEstimate(m types.EcidMeasurements, cell types.CellGeometry) posEstimate {
	// Range from TA: for NR 15kHz SCS
	// NTA = TimingAdvance (integer)
	// Range = NTA * 16 * Ts * c / 2 where Ts = 1/(15000*2048) = 32.55ns
	// Simplified: range ≈ TA * 10m (approx for 15kHz)
	rangeM := float64(m.TimingAdvance) * taNR15kHz
	if rangeM < 1.0 {
		rangeM = 1.0
	}

	// Uncertainty in range due to TA quantization
	sigmaRangeM := taNR15kHz // 1 TA step

	// Convert range to angular displacement
	// 1 degree latitude ≈ 111319m
	sigmaLatDeg := sigmaRangeM / 111319.0
	sigmaLonDeg := sigmaRangeM / (111319.0 * math.Cos(cell.Latitude*math.Pi/180))

	// Estimate position: cell + range in beam direction
	azimuthRad := float64(cell.AntennaSectorAzimuth) * math.Pi / 180
	dLat := (rangeM / 111319.0) * math.Cos(azimuthRad)
	dLon := (rangeM / 111319.0) * math.Sin(azimuthRad) / math.Cos(cell.Latitude*math.Pi/180)

	// Position uncertainty: range uncertainty in all directions (arc)
	// If sector known: narrower perpendicular to azimuth
	perpSigmaM := sigmaRangeM * 2 // 2x range sigma perpendicular (sector uncertainty)
	perpSigmaLatDeg := perpSigmaM / 111319.0

	// Choose larger of the two for a conservative estimate
	if perpSigmaLatDeg > sigmaLatDeg {
		sigmaLatDeg = perpSigmaLatDeg
	}
	_ = sigmaLonDeg

	return posEstimate{
		lat:        cell.Latitude + dLat,
		lon:        cell.Longitude + dLon,
		alt:        cell.Altitude,
		sigmaLat:   sigmaLatDeg,
		sigmaLon:   sigmaLatDeg / math.Cos(cell.Latitude*math.Pi/180),
		confidence: 68,
	}
}

// computeRsrpCentroid estimates position using RSRP-based weighted centroid
func (s *EcidSolver) computeRsrpCentroid(rsrpMeas []types.RsrpEntry, cells []types.CellGeometry) (posEstimate, error) {
	// Build cell lookup
	cellMap := make(map[string]types.CellGeometry)
	for _, c := range cells {
		cellMap[c.Nci] = c
	}

	type cellWeight struct {
		cell   types.CellGeometry
		weight float64
		distM  float64
	}
	var cws []cellWeight

	for _, m := range rsrpMeas {
		cell, ok := cellMap[m.CellNci]
		if !ok {
			continue
		}
		// Convert RSRP to distance using log-distance path loss:
		// RSRP = TxPower - PL0 - 10*n*log10(d/d0)
		// d = d0 * 10^((TxPower - PL0 - RSRP)/(10*n))
		plDb := txPowerDBm - m.Rsrp
		exponent := (plDb - pl0dB) / (10.0 * pathLossExponent)
		distM := d0 * math.Pow(10, exponent)
		if distM < 1.0 {
			distM = 1.0
		}
		if distM > 10000.0 {
			distM = 10000.0
		}

		// Weight = 1 / d^2
		weight := 1.0 / (distM * distM)
		cws = append(cws, cellWeight{cell: cell, weight: weight, distM: distM})
	}

	if len(cws) < 2 {
		return posEstimate{}, fmt.Errorf("insufficient cells with known geometry")
	}

	// Weighted centroid
	wLat, wLon, wSum := 0.0, 0.0, 0.0
	for _, cw := range cws {
		wLat += cw.weight * cw.cell.Latitude
		wLon += cw.weight * cw.cell.Longitude
		wSum += cw.weight
	}

	lat := wLat / wSum
	lon := wLon / wSum

	// Uncertainty: weighted average distance / sqrt(n) as proxy
	avgDist := 0.0
	for _, cw := range cws {
		avgDist += cw.weight * cw.distM
	}
	avgDist /= wSum
	sigmaM := avgDist / math.Sqrt(float64(len(cws)))
	sigmaLatDeg := sigmaM / 111319.0

	return posEstimate{
		lat:        lat,
		lon:        lon,
		alt:        cws[0].cell.Altitude,
		sigmaLat:   sigmaLatDeg,
		sigmaLon:   sigmaLatDeg / math.Cos(lat*math.Pi/180),
		confidence: 68,
	}, nil
}

// fuseEstimates merges position estimates using inverse-variance weighting
func fuseEstimates(estimates []posEstimate) *types.PositionEstimate {
	wLat, wLon, wSumLat, wSumLon := 0.0, 0.0, 0.0, 0.0
	for _, e := range estimates {
		wl := 1.0 / (e.sigmaLat * e.sigmaLat)
		wln := 1.0 / (e.sigmaLon * e.sigmaLon)
		wLat += wl * e.lat
		wLon += wln * e.lon
		wSumLat += wl
		wSumLon += wln
	}
	fusedLat := wLat / wSumLat
	fusedLon := wLon / wSumLon
	fusedSigmaLat := 1.0 / math.Sqrt(wSumLat)
	fusedSigmaLon := 1.0 / math.Sqrt(wSumLon)

	return &types.PositionEstimate{
		Latitude:   fusedLat,
		Longitude:  fusedLon,
		Altitude:   estimates[0].alt,
		SigmaLat:   fusedSigmaLat,
		SigmaLon:   fusedSigmaLon,
		SigmaAlt:   fusedSigmaLat * 111319 * 2,
		Confidence: 68,
		Method:     types.PositioningMethodNREcid,
		Timestamp:  time.Now(),
	}
}

// ComputeUncertaintyFromTA returns positional uncertainty (m) based on TA granularity
func ComputeUncertaintyFromTA(ta int, scsKHz float64) float64 {
	// Ts = 1 / (scs_hz * 2048)
	ts := 1.0 / (scsKHz * 1000 * 2048)
	// 1 TA step = 16 samples
	taStepSeconds := 16 * ts
	// range_step = c * ta_step / 2
	return speedOfLight * taStepSeconds / 2.0
}
