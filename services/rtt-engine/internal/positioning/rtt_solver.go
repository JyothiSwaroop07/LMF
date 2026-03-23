// Package positioning implements NR Multi-RTT positioning per 3GPP TS 38.305 §7.10.
package positioning

import (
	"fmt"
	"math"
	"time"

	"github.com/5g-lmf/common/types"
)

const (
	speedOfLight = 299792458.0 // m/s
	// NR timing measurement unit: Ts = 1/(30720000 * 2) for FR1 measurements
	// RTT measurement in 'Ts' units: range = (UeRxTx + GnbRxTx) * Ts * c / 2
	// For NR measurements reported in 1/2 Ts units:
	tsHalfNR = 1.0 / (2.0 * 30.72e6) // ~16.3 ns
)

// cellRange is the canonical type used throughout this file
type cellRange struct {
	x, y   float64
	rangeM float64
	sigma  float64
}

// RttSolver computes UE position from Multi-RTT measurements using WLS multilateration
type RttSolver struct{}

// NewRttSolver creates a new RTT solver
func NewRttSolver() *RttSolver { return &RttSolver{} }

// ComputePosition estimates UE position from Multi-RTT measurements
func (s *RttSolver) ComputePosition(
	measurements types.MultiRttMeasurements,
	cellGeometry map[string]types.CellGeometry,
) (*types.PositionEstimate, error) {
	if len(measurements.Entries) < 3 {
		return nil, fmt.Errorf("multi-RTT requires ≥3 measurements, got %d", len(measurements.Entries))
	}

	// Use first cell as reference for ENU frame
	var refCell *types.CellGeometry
	for _, entry := range measurements.Entries {
		if cell, ok := cellGeometry[entry.CellNci]; ok {
			refCell = &cell
			break
		}
	}
	if refCell == nil {
		return nil, fmt.Errorf("no valid cell geometry found")
	}

	refLatRad := refCell.Latitude * math.Pi / 180
	refLonRad := refCell.Longitude * math.Pi / 180

	var cells []cellRange
	for _, entry := range measurements.Entries {
		cell, ok := cellGeometry[entry.CellNci]
		if !ok {
			continue
		}

		// Convert cell to ENU
		latRad := cell.Latitude * math.Pi / 180
		lonRad := cell.Longitude * math.Pi / 180
		x, y := geoToENU(refLatRad, refLonRad, latRad, lonRad)

		// Compute RTT-based range
		rangeSamples := entry.UeRxTx + entry.GnbRxTx
		rangeM := rangeSamples * tsHalfNR * speedOfLight / 2.0
		if rangeM < 0 {
			rangeM = -rangeM
		}

		sigma := 10.0
		cells = append(cells, cellRange{x: x, y: y, rangeM: rangeM, sigma: sigma})
	}

	if len(cells) < 3 {
		return nil, fmt.Errorf("insufficient valid RTT cells with geometry: %d", len(cells))
	}

	ux, uy, err := wlsMultilateration(cells)
	if err != nil {
		return nil, fmt.Errorf("WLS multilateration failed: %w", err)
	}

	lat, lon := enuToGeo(refLatRad, refLonRad, ux, uy)

	resRMS := computeRangeRMSError(cells, ux, uy)
	sigmaM := resRMS
	if sigmaM < 5.0 {
		sigmaM = 5.0
	}
	sigmaLatDeg := sigmaM / 111319.0

	return &types.PositionEstimate{
		Latitude:   lat * 180 / math.Pi,
		Longitude:  lon * 180 / math.Pi,
		Altitude:   refCell.Altitude,
		SigmaLat:   sigmaLatDeg,
		SigmaLon:   sigmaLatDeg,
		SigmaAlt:   sigmaM * 2,
		Confidence: 68,
		Method:     types.PositioningMethodMultiRTT,
		Timestamp:  measurements.MeasurementTime,
	}, nil
}

// wlsMultilateration performs iterative WLS ranging-based positioning
func wlsMultilateration(cells []cellRange) (ux, uy float64, err error) {
	wSum := 0.0
	for _, c := range cells {
		ux += c.x / (c.rangeM + 1)
		uy += c.y / (c.rangeM + 1)
		wSum += 1.0 / (c.rangeM + 1)
	}
	ux /= wSum
	uy /= wSum

	const maxIter = 50
	const tol = 0.01

	for iter := 0; iter < maxIter; iter++ {
		n := len(cells)
		H := make([][]float64, n)
		r := make([]float64, n)
		W := make([]float64, n)

		for i, c := range cells {
			dx := ux - c.x
			dy := uy - c.y
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist < 0.01 {
				dist = 0.01
			}

			H[i] = []float64{dx / dist, dy / dist}
			r[i] = c.rangeM - dist
			W[i] = 1.0 / (c.sigma * c.sigma)
		}

		var A [2][2]float64
		var b [2]float64
		for i := 0; i < n; i++ {
			for j := 0; j < 2; j++ {
				for k := 0; k < 2; k++ {
					A[j][k] += H[i][j] * W[i] * H[i][k]
				}
				b[j] += H[i][j] * W[i] * r[i]
			}
		}

		delta, solveErr := solve2x2(A, b)
		if solveErr != nil {
			err = solveErr
			return
		}

		ux += delta[0]
		uy += delta[1]

		if math.Sqrt(delta[0]*delta[0]+delta[1]*delta[1]) < tol {
			break
		}
	}

	return ux, uy, nil
}

// solve2x2 solves a 2x2 linear system
func solve2x2(A [2][2]float64, b [2]float64) ([]float64, error) {
	det := A[0][0]*A[1][1] - A[0][1]*A[1][0]
	if math.Abs(det) < 1e-12 {
		return nil, fmt.Errorf("singular 2x2 matrix (det=%.2e)", det)
	}
	x := make([]float64, 2)
	x[0] = (A[1][1]*b[0] - A[0][1]*b[1]) / det
	x[1] = (A[0][0]*b[1] - A[1][0]*b[0]) / det
	return x, nil
}

// geoToENU converts WGS84 to local ENU (East-North-Up) frame
func geoToENU(refLatRad, refLonRad, latRad, lonRad float64) (eastM, northM float64) {
	const R = 6371000.0
	dLat := latRad - refLatRad
	dLon := lonRad - refLonRad
	northM = dLat * R
	eastM = dLon * R * math.Cos(refLatRad)
	return
}

// enuToGeo converts local ENU back to WGS84 radians
func enuToGeo(refLatRad, refLonRad, eastM, northM float64) (latRad, lonRad float64) {
	const R = 6371000.0
	latRad = refLatRad + northM/R
	lonRad = refLonRad + eastM/(R*math.Cos(refLatRad))
	return
}

// computeRangeRMSError computes RMS of ranging residuals at estimated position
func computeRangeRMSError(cells []cellRange, ux, uy float64) float64 {
	sumSq := 0.0
	for _, c := range cells {
		dx := ux - c.x
		dy := uy - c.y
		dist := math.Sqrt(dx*dx + dy*dy)
		res := c.rangeM - dist
		sumSq += res * res
	}
	return math.Sqrt(sumSq / float64(len(cells)))
}

// ConvertToENU is an exported helper for testing
func ConvertToENU(refLat, refLon float64, cells []types.CellGeometry) [][2]float64 {
	refLatRad := refLat * math.Pi / 180
	refLonRad := refLon * math.Pi / 180
	result := make([][2]float64, len(cells))
	for i, c := range cells {
		e, n := geoToENU(refLatRad, refLonRad, c.Latitude*math.Pi/180, c.Longitude*math.Pi/180)
		result[i] = [2]float64{e, n}
	}
	return result
}

// ConvertENUToLatLon is an exported helper for testing
func ConvertENUToLatLon(refLat, refLon, eastM, northM float64) (lat, lon float64) {
	refLatRad := refLat * math.Pi / 180
	refLonRad := refLon * math.Pi / 180
	latRad, lonRad := enuToGeo(refLatRad, refLonRad, eastM, northM)
	return latRad * 180 / math.Pi, lonRad * 180 / math.Pi
}

// CellRangeEntry is a public type for testable entry points
type CellRangeEntry struct {
	X, Y   float64
	RangeM float64
	Sigma  float64
}

// ComputePositionFromRanges is a testable entry point taking explicit ranges
func ComputePositionFromRanges(entries []CellRangeEntry) (x, y float64, err error) {
	cells := make([]cellRange, len(entries))
	for i, e := range entries {
		cells[i] = cellRange{x: e.X, y: e.Y, rangeM: e.RangeM, sigma: e.Sigma}
	}
	return wlsMultilateration(cells)
}

// mockMeasurementTime returns a measurement time for testing
func mockMeasurementTime() time.Time {
	return time.Now()
}
