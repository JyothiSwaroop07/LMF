package positioning_test

import (
	"math"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/5g-lmf/gnss-engine/internal/assistance"
	"github.com/5g-lmf/gnss-engine/internal/positioning"
)

// TestComputeSatellitePosition verifies that Keplerian orbital mechanics places
// a GPS satellite within the correct altitude shell (~26,200–26,800 km).
func TestComputeSatellitePosition(t *testing.T) {
	eph := testGPSEphemeris("G01", 1, 0.0, 1.2, 388800)

	x, y, z, clockCorr := positioning.ComputeSatellitePosition(eph, 389000)

	r := math.Sqrt(x*x + y*y + z*z)
	const minAlt = 26000e3 // 26,000 km
	const maxAlt = 27000e3 // 27,000 km

	if r < minAlt || r > maxAlt {
		t.Errorf("satellite orbital radius %.0f m outside expected GPS shell [%.0f, %.0f] m",
			r, minAlt, maxAlt)
	}

	// SV clock correction should be of order nanoseconds to microseconds (|Af0| ~ 1e-7).
	if math.Abs(clockCorr) > 1e-3 {
		t.Errorf("clock correction %.6e s is unreasonably large", clockCorr)
	}

	t.Logf("SV ECEF: x=%.0f y=%.0f z=%.0f (r=%.0f km), clockCorr=%.3e s",
		x, y, z, r/1e3, clockCorr)
}

// TestECEFToGeodetic checks round-trip accuracy for well-known ECEF coordinates.
func TestECEFToGeodetic(t *testing.T) {
	cases := []struct {
		name                    string
		x, y, z                 float64
		wantLat, wantLon, wantAlt float64
		tolDeg, tolAlt          float64
	}{
		{
			name:    "equator_prime_meridian",
			x: 6378137.0, y: 0, z: 0,
			wantLat: 0, wantLon: 0, wantAlt: 0,
			tolDeg: 1e-6, tolAlt: 1,
		},
		{
			name:    "north_pole",
			// At the north pole x=y=0; z = b (semi-minor axis ~6356752.3 m)
			x: 0, y: 0, z: 6356752.3,
			wantLat: 90, wantLon: 0, wantAlt: 0,
			tolDeg: 1e-4, tolAlt: 5,
		},
		{
			// New York City: lat≈40.7128°, lon≈-74.0060°, alt≈0 m
			// Canonical ECEF (ITRF): x=1334011, y=-4654031, z=4138411
			name:    "new_york_city",
			x: 1334011, y: -4654031, z: 4138411,
			wantLat: 40.7128, wantLon: -74.006, wantAlt: 0,
			tolDeg: 0.01, tolAlt: 200,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lat, lon, alt := positioning.ECEFToGeodetic(tc.x, tc.y, tc.z)
			if math.Abs(lat-tc.wantLat) > tc.tolDeg {
				t.Errorf("lat: got %.7f°, want %.7f° (tol %.7f°)", lat, tc.wantLat, tc.tolDeg)
			}
			if math.Abs(lon-tc.wantLon) > tc.tolDeg {
				t.Errorf("lon: got %.7f°, want %.7f° (tol %.7f°)", lon, tc.wantLon, tc.tolDeg)
			}
			if math.Abs(alt-tc.wantAlt) > tc.tolAlt {
				t.Errorf("alt: got %.2f m, want %.2f m (tol %.2f m)", alt, tc.wantAlt, tc.tolAlt)
			}
		})
	}
}

// TestWLSConvergence places a receiver at a known geodetic position (Paris),
// synthesises noise-free pseudoranges from 6 GPS satellites, and verifies that
// the WLS solver recovers the true position to within 10 m.
func TestWLSConvergence(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	solver := positioning.NewGnssSolver(logger)

	// True receiver position: Paris, France
	trueLat, trueLon, trueAlt := 48.8566, 2.3522, 35.0

	// Convert to ECEF.
	rx, ry, rz := geodeticToECEF(trueLat, trueLon, trueAlt)

	// Build 6 GPS ephemerides spread across 3 planes.
	_, gpsTow := currentGPSTime()
	toe := math.Trunc(gpsTow/7200) * 7200

	ephs := []assistance.GnssEphemeris{
		testGPSEphemeris("G01", 1, 0.0, 0.0, toe),
		testGPSEphemeris("G07", 7, math.Pi/3, 1.0, toe),
		testGPSEphemeris("G13", 13, 2*math.Pi/3, 2.0, toe),
		testGPSEphemeris("G15", 15, math.Pi, 3.0, toe),
		testGPSEphemeris("G21", 21, 4*math.Pi/3, 4.0, toe),
		testGPSEphemeris("G27", 27, 5*math.Pi/3, 5.0, toe),
	}

	refTime := time.Now().UTC()
	gpsEpoch := time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)
	totalSec := refTime.Sub(gpsEpoch).Seconds() + 18
	weekSec := totalSec - float64(int(totalSec/(7*24*3600)))*7*24*3600

	// Simulated receiver clock bias.
	const receiverClockBiasS = 0.0003 // 300 µs

	meas := make([]positioning.GnssSignalMeasurement, 0, len(ephs))
	for _, eph := range ephs {
		// Use approximate transmit time (good enough for test geometry).
		approxRange := 25000e3 // ~25,000 km first-guess
		transmitTime := weekSec - approxRange/positioning.SpeedOfLight

		svX, svY, svZ, clockCorr := positioning.ComputeSatellitePosition(eph, transmitTime)

		dx := rx - svX
		dy := ry - svY
		dz := rz - svZ
		trueRange := math.Sqrt(dx*dx + dy*dy + dz*dz)

		// pseudorange = geometricRange + rcvClockBias - svClockCorr
		pseudorange := trueRange +
			receiverClockBiasS*positioning.SpeedOfLight -
			clockCorr*positioning.SpeedOfLight

		meas = append(meas, positioning.GnssSignalMeasurement{
			SvID:        eph.SvID,
			Pseudorange: pseudorange,
			CN0:         45.0, // strong signal
		})
	}

	result, err := solver.ComputePosition(meas, ephs, refTime)
	if err != nil {
		t.Fatalf("ComputePosition failed: %v", err)
	}

	// Convert result to ECEF and measure 3D error.
	resX, resY, resZ := geodeticToECEF(result.Latitude, result.Longitude, result.Altitude)
	err3D := math.Sqrt(
		(resX-rx)*(resX-rx)+
			(resY-ry)*(resY-ry)+
			(resZ-rz)*(resZ-rz),
	)

	t.Logf("True:   lat=%.6f° lon=%.6f° alt=%.1f m", trueLat, trueLon, trueAlt)
	t.Logf("Result: lat=%.6f° lon=%.6f° alt=%.1f m", result.Latitude, result.Longitude, result.Altitude)
	t.Logf("3D error: %.3f m | HDOP=%.2f | sats=%d | clockBias=%.3e s",
		err3D, result.HDOP, result.NumSatellites, result.ClockBias)

	const maxErrorM = 10.0
	if err3D > maxErrorM {
		t.Errorf("3D position error %.3f m exceeds tolerance %.0f m", err3D, maxErrorM)
	}

	// Clock bias should be recovered near the injected value.
	const maxClockErrS = 1e-6 // 1 µs tolerance
	clockErr := math.Abs(result.ClockBias - receiverClockBiasS)
	if clockErr > maxClockErrS {
		t.Errorf("clock bias error %.3e s > tolerance %.3e s", clockErr, maxClockErrS)
	}
}

// =============================================================================
// Test helpers
// =============================================================================

// testGPSEphemeris creates a realistic GPS ephemeris for a given SV.
func testGPSEphemeris(svID string, prn int, raan, m0, toe float64) assistance.GnssEphemeris {
	return assistance.GnssEphemeris{
		Constellation: assistance.ConstellationGPS,
		SvID:          svID,
		PRN:           prn,
		WeekNumber:    234,

		SqrtA:        5153.7,
		Eccentricity: 0.001,
		Inclination:  0.9599, // 55°

		RAAN:        raan,
		ArgPerigee:  0.3,
		MeanAnomaly: m0,

		DeltaN:   4.5e-9,
		IDOT:     -1.2e-10,
		OmegaDot: -8.0e-9,

		Toe: toe,
		Toc: toe,

		Af0: 0,
		Af1: 0,
		Af2: 0,

		Crs: 20.0,
		Crc: 180.0,
		Cus: 5.2e-6,
		Cuc: -3.1e-6,
		Cis: 5.6e-8,
		Cic: -1.3e-7,

		Health: 0,
		IODE:   prn,
	}
}

// WGS84 constants duplicated here to avoid import of internal package constants.
const (
	testWgs84A  = 6378137.0
	testWgs84F  = 1.0 / 298.257223563
	testWgs84E2 = 2*testWgs84F - testWgs84F*testWgs84F
)

// geodeticToECEF converts WGS84 geodetic to ECEF.
func geodeticToECEF(lat, lon, alt float64) (x, y, z float64) {
	latR := lat * math.Pi / 180
	lonR := lon * math.Pi / 180
	sinLat, cosLat := math.Sin(latR), math.Cos(latR)
	sinLon, cosLon := math.Sin(lonR), math.Cos(lonR)
	N := testWgs84A / math.Sqrt(1-testWgs84E2*sinLat*sinLat)
	x = (N + alt) * cosLat * cosLon
	y = (N + alt) * cosLat * sinLon
	z = (N*(1-testWgs84E2) + alt) * sinLat
	return
}

// currentGPSTime returns the current GPS week and time-of-week.
func currentGPSTime() (week int, tow float64) {
	gpsEpoch := time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)
	elapsed := time.Since(gpsEpoch).Seconds() + 18
	totalWeeks := int(elapsed / (7 * 24 * 3600))
	week = totalWeeks % 1024
	tow = elapsed - float64(totalWeeks)*7*24*3600
	return
}
