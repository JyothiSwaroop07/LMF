package positioning_test

import (
	"math"
	"testing"

	"go.uber.org/zap"

	"github.com/5g-lmf/tdoa-engine/internal/positioning"
)

// TestTdoaSolver_KnownPosition places a UE at a known WGS84 position, generates
// noise-free RSTDs from surrounding cell towers, and verifies that Chan's
// algorithm recovers the true position to within 50 m.
func TestTdoaSolver_KnownPosition(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	solver := positioning.NewTdoaSolver(logger)

	// True UE position: San Francisco downtown area.
	trueLat := 37.7749
	trueLon := -122.4194

	// Five cell towers surrounding the UE.
	cells := map[string]positioning.CellGeometry{
		"REF": {NCI: "REF", Latitude: 37.780, Longitude: -122.420, Altitude: 10, AntennaHeight: 30},
		"NB1": {NCI: "NB1", Latitude: 37.770, Longitude: -122.410, Altitude: 10, AntennaHeight: 30},
		"NB2": {NCI: "NB2", Latitude: 37.760, Longitude: -122.425, Altitude: 10, AntennaHeight: 30},
		"NB3": {NCI: "NB3", Latitude: 37.775, Longitude: -122.430, Altitude: 10, AntennaHeight: 30},
		"NB4": {NCI: "NB4", Latitude: 37.785, Longitude: -122.415, Altitude: 10, AntennaHeight: 30},
	}

	meas := positioning.DlTdoaMeasurements{
		SessionID:    "test-sf-001",
		Measurements: syntheticRSTDs("REF", cells, trueLat, trueLon),
	}
	if len(meas.Measurements) < 3 {
		t.Fatal("not enough synthetic measurements generated")
	}

	result, err := solver.ComputePosition(meas, cells)
	if err != nil {
		t.Fatalf("ComputePosition failed: %v", err)
	}

	errM := haversineMeters(trueLat, trueLon, result.Latitude, result.Longitude)
	t.Logf("True:   lat=%.6f° lon=%.6f°", trueLat, trueLon)
	t.Logf("Result: lat=%.6f° lon=%.6f°", result.Latitude, result.Longitude)
	t.Logf("Error:  %.2f m | HDOP=%.2f | semiMajor=%.1f m | semiMinor=%.1f m | n=%d",
		errM, result.HDOP, result.SemiMajorAxis, result.SemiMinorAxis, result.NumMeasurements)

	const maxErrM = 50.0
	if errM > maxErrM {
		t.Errorf("position error %.2f m exceeds tolerance %.0f m", errM, maxErrM)
	}
}

// TestConvertGeoToLocal verifies directional correctness of ENU conversion.
func TestConvertGeoToLocal(t *testing.T) {
	refLat := 48.8566
	refLon := 2.3522

	cells := []positioning.CellGeometry{
		// NE of reference — expect east > 0, north > 0
		{NCI: "C_NE", Latitude: 48.8620, Longitude: 2.3650},
		// SW of reference — expect east < 0, north < 0
		{NCI: "C_SW", Latitude: 48.8490, Longitude: 2.3350},
	}

	local := positioning.ConvertGeoToLocal(refLat, refLon, cells)

	if len(local) != 2 {
		t.Fatalf("expected 2 local positions, got %d", len(local))
	}

	// NE cell
	if local[0][0] <= 0 {
		t.Errorf("NE cell: expected east > 0, got %.2f m", local[0][0])
	}
	if local[0][1] <= 0 {
		t.Errorf("NE cell: expected north > 0, got %.2f m", local[0][1])
	}

	// SW cell
	if local[1][0] >= 0 {
		t.Errorf("SW cell: expected east < 0, got %.2f m", local[1][0])
	}
	if local[1][1] >= 0 {
		t.Errorf("SW cell: expected north < 0, got %.2f m", local[1][1])
	}

	// NE cell should be roughly 300–2000 m away.
	dNE := math.Sqrt(local[0][0]*local[0][0] + local[0][1]*local[0][1])
	if dNE < 100 || dNE > 5000 {
		t.Errorf("NE cell local distance %.0f m seems implausible", dNE)
	}

	t.Logf("NE local: E=%.1f m, N=%.1f m (dist=%.1f m)", local[0][0], local[0][1], dNE)
	t.Logf("SW local: E=%.1f m, N=%.1f m", local[1][0], local[1][1])
}

// TestConvertLocalToGeo verifies round-trip accuracy of the geo↔local conversion.
func TestConvertLocalToGeo(t *testing.T) {
	refLat := 51.5074 // London
	refLon := -0.1278

	// A point ~500 m east, ~300 m north of the reference.
	testLat := 51.5101
	testLon := -0.1204

	cells := []positioning.CellGeometry{{Latitude: testLat, Longitude: testLon}}
	local := positioning.ConvertGeoToLocal(refLat, refLon, cells)
	east, north := local[0][0], local[0][1]

	// Inverse.
	lat2, lon2 := positioning.ConvertLocalToGeo(refLat, refLon, east, north)

	const tolDeg = 1e-5 // roughly 1 m
	if math.Abs(lat2-testLat) > tolDeg {
		t.Errorf("round-trip lat: got %.7f°, want %.7f° (tol %.7f°)", lat2, testLat, tolDeg)
	}
	if math.Abs(lon2-testLon) > tolDeg {
		t.Errorf("round-trip lon: got %.7f°, want %.7f° (tol %.7f°)", lon2, testLon, tolDeg)
	}

	t.Logf("ENU: E=%.2f m, N=%.2f m → lat=%.7f° lon=%.7f°", east, north, lat2, lon2)
}

// TestChanAlgorithm_ThreeCells tests the minimum 3-cell TDOA configuration.
// The reference and three neighbors are positioned near (0°, 0°) for
// numerical simplicity.
func TestChanAlgorithm_ThreeCells(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	solver := positioning.NewTdoaSolver(logger)

	// Cells at small WGS84 offsets near the equator/prime meridian.
	// 0.009° ≈ 1 km.
	cells := map[string]positioning.CellGeometry{
		"REF": {NCI: "REF", Latitude: 0.000, Longitude: 0.000},
		"NB1": {NCI: "NB1", Latitude: 0.000, Longitude: 0.009}, // ~1 km East
		"NB2": {NCI: "NB2", Latitude: 0.009, Longitude: 0.000}, // ~1 km North
		"NB3": {NCI: "NB3", Latitude: 0.006, Longitude: 0.006}, // ~NE
	}

	// UE at roughly (200 m East, 150 m North) of REF.
	trueUELat := 0.00135 // ~150 m North
	trueUELon := 0.00180 // ~200 m East

	measurements := syntheticRSTDs("REF", cells, trueUELat, trueUELon)
	// Force exactly 3 measurements.
	if len(measurements) > 3 {
		measurements = measurements[:3]
	}
	if len(measurements) < 3 {
		t.Skip("insufficient neighbors for 3-cell test")
	}

	meas := positioning.DlTdoaMeasurements{
		SessionID:    "test-three-cell",
		Measurements: measurements,
	}

	result, err := solver.ComputePosition(meas, cells)
	if err != nil {
		t.Fatalf("ComputePosition (3 cells) failed: %v", err)
	}

	errM := haversineMeters(trueUELat, trueUELon, result.Latitude, result.Longitude)
	t.Logf("3-cell error: %.2f m | semiMajor=%.1f m", errM, result.SemiMajorAxis)

	// 3-cell TDOA has one fewer hyperbola; accept up to 200 m.
	const maxErrM = 200.0
	if errM > maxErrM {
		t.Errorf("3-cell position error %.2f m exceeds tolerance %.0f m", errM, maxErrM)
	}
}

// =============================================================================
// Test helpers
// =============================================================================

const (
	testSpeedOfLight = 3e8
	testNRSampleRate = 30.72e6
	testTsNR         = 1.0 / testNRSampleRate
)

// syntheticRSTDs generates noise-free RSTD measurements for a UE at (ueLat, ueLon).
// RSTD_i = (d_ref − d_i) / c / Ts  where distances are Haversine great-circle.
func syntheticRSTDs(
	refNCI string,
	cells map[string]positioning.CellGeometry,
	ueLat, ueLon float64,
) []positioning.RSTDMeasurement {
	refCell := cells[refNCI]
	dRef := haversineMeters(ueLat, ueLon, refCell.Latitude, refCell.Longitude)

	var meas []positioning.RSTDMeasurement
	for nci, c := range cells {
		if nci == refNCI {
			continue
		}
		dNb := haversineMeters(ueLat, ueLon, c.Latitude, c.Longitude)
		// RSTD = (TOA_ref − TOA_nb) in NR samples
		// TOA_ref = d_ref / c / Ts
		rstdSamples := (dRef - dNb) / testSpeedOfLight / testTsNR
		meas = append(meas, positioning.RSTDMeasurement{
			ReferenceNCI: refNCI,
			NeighborNCI:  nci,
			RSTD:         rstdSamples,
			Quality:      1.0,
		})
	}
	return meas
}

// haversineMeters returns the great-circle distance between two WGS84 points.
func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000.0 // Earth mean radius (m)
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	sinDLat := math.Sin(dLat / 2)
	sinDLon := math.Sin(dLon / 2)
	a := sinDLat*sinDLat +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*sinDLon*sinDLon
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
