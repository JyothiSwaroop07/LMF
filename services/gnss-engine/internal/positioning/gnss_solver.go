// Package positioning implements GNSS position computation using
// Weighted Least Squares (WLS) and Keplerian orbital mechanics per IS-GPS-200N.
package positioning

import (
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"

	"github.com/5g-lmf/gnss-engine/internal/assistance"
)

// WGS84 ellipsoid parameters.
const (
	wgs84A   = 6378137.0             // Semi-major axis (m)
	wgs84F   = 1.0 / 298.257223563   // Flattening
	wgs84B   = wgs84A * (1 - wgs84F) // Semi-minor axis (m)
	wgs84E2  = 2*wgs84F - wgs84F*wgs84F // First eccentricity squared e²
	wgs84Ep2 = wgs84E2 / (1 - wgs84E2)  // Second eccentricity squared e'²
)

// Physical constants.
const (
	GmEarth    = 3.986004418e14  // Earth gravitational parameter μ (m³/s²)
	OmegaEarth = 7.2921151467e-5 // Earth rotation rate ω_E (rad/s)

	// SpeedOfLight is exported so tests can compute synthetic pseudoranges.
	SpeedOfLight = 2.99792458e8 // m/s

	// Relativistic correction constant F = -2√μ / c²  (s/√m)
	relativisticF = -4.442807633e-10
)

// Solver tuning parameters.
const (
	maxWLSIterations        = 10
	wlsConvergenceThreshold = 0.001 // metres
	keplerMaxIterations     = 10
	keplerConvergence       = 1e-12 // radians
)

// GnssSignalMeasurement is a single pseudorange observation from a GNSS satellite.
type GnssSignalMeasurement struct {
	SvID        string  // Satellite identifier (e.g. "G01", "E03")
	Pseudorange float64 // Raw pseudorange (m), uncorrected for SV clock
	CN0         float64 // Carrier-to-noise density (dB-Hz); used for WLS weighting
}

// PositionEstimate is the result of a GNSS position fix.
type PositionEstimate struct {
	Latitude           float64 // WGS84 geodetic latitude (degrees)
	Longitude          float64 // WGS84 geodetic longitude (degrees)
	Altitude           float64 // Height above WGS84 ellipsoid (m)
	HorizontalAccuracy float64 // ~95% horizontal accuracy (m)
	VerticalAccuracy   float64 // ~95% vertical accuracy (m)
	HDOP               float64
	PDOP               float64
	NumSatellites      int
	ClockBias          float64 // Receiver clock bias (seconds)
}

// svSolution holds the computed ECEF state for one satellite measurement.
type svSolution struct {
	x, y, z   float64 // ECEF position after Sagnac correction (m)
	corrRange float64 // Pseudorange corrected for SV clock (m)
	weight    float64 // WLS observation weight
}

// GnssSolver computes GNSS positions using Weighted Least Squares.
type GnssSolver struct {
	logger *zap.Logger
}

// NewGnssSolver creates a new GnssSolver.
func NewGnssSolver(logger *zap.Logger) *GnssSolver {
	return &GnssSolver{logger: logger}
}

// ComputePosition runs the WLS GNSS positioning algorithm.
//
// Steps:
//  1. For each measurement with a matching ephemeris, compute SV ECEF position
//     using full Keplerian mechanics (IS-GPS-200N Table 20-IV).
//  2. Apply Sagnac correction for Earth rotation during signal travel time.
//  3. Correct pseudorange for SV clock offset (polynomial + relativistic term).
//  4. Weight observations by linear C/N0.
//  5. Iterate WLS for state [x, y, z, b] until convergence (< 1 mm update).
//  6. Derive covariance, rotate to ENU, compute DOP and accuracy.
func (s *GnssSolver) ComputePosition(
	measurements []GnssSignalMeasurement,
	ephemerides []assistance.GnssEphemeris,
	refTime time.Time,
) (*PositionEstimate, error) {
	if len(measurements) < 4 {
		return nil, fmt.Errorf("need at least 4 measurements, got %d", len(measurements))
	}

	// Build SV-ID → ephemeris lookup.
	ephMap := make(map[string]assistance.GnssEphemeris, len(ephemerides))
	for _, e := range ephemerides {
		ephMap[e.SvID] = e
	}

	// GPS time-of-week at the reference epoch.
	gpsEpoch := time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)
	totalSec := refTime.UTC().Sub(gpsEpoch).Seconds() + 18 // +18 leap seconds
	weekSec := totalSec - float64(int(totalSec/(7*24*3600)))*7*24*3600

	// Compute satellite solutions.
	svs := make([]svSolution, 0, len(measurements))
	for _, m := range measurements {
		eph, ok := ephMap[m.SvID]
		if !ok {
			s.logger.Debug("no ephemeris for SV, skipping", zap.String("sv", m.SvID))
			continue
		}

		// Approximate transmit time.
		travelTime := m.Pseudorange / SpeedOfLight
		transmitTime := weekSec - travelTime

		svX, svY, svZ, clockCorr := ComputeSatellitePosition(eph, transmitTime)

		// Sagnac correction: rotate SV by ω_E × travelTime about Z-axis.
		rot := OmegaEarth * travelTime
		sinR, cosR := math.Sin(rot), math.Cos(rot)
		xRot := svX*cosR + svY*sinR
		yRot := -svX*sinR + svY*cosR

		// Pseudorange corrected for SV clock error.
		corrRange := m.Pseudorange + SpeedOfLight*clockCorr

		// Weight = linear C/N0; default 1 if not provided.
		w := 1.0
		if m.CN0 > 0 {
			w = math.Pow(10.0, m.CN0/10.0)
		}

		svs = append(svs, svSolution{
			x: xRot, y: yRot, z: svZ,
			corrRange: corrRange,
			weight:    w,
		})
	}

	if len(svs) < 4 {
		return nil, fmt.Errorf("only %d SVs have matching ephemeris (need ≥4)", len(svs))
	}

	// Normalise weights so max weight = 1.
	maxW := 0.0
	for _, sv := range svs {
		if sv.weight > maxW {
			maxW = sv.weight
		}
	}
	for i := range svs {
		svs[i].weight /= maxW
	}

	// --- WLS iteration ---
	// State vector: [x, y, z, b] in ECEF metres (b = clock bias in metres).
	// Start at Earth centre; WLS converges quickly from there for GPS geometry.
	var state [4]float64 // zero-initialised

	for iter := 0; iter < maxWLSIterations; iter++ {
		n := len(svs)
		H := make([][]float64, n)
		resid := make([]float64, n)
		W := make([]float64, n)

		for i, sv := range svs {
			dx := state[0] - sv.x
			dy := state[1] - sv.y
			dz := state[2] - sv.z
			r := math.Sqrt(dx*dx + dy*dy + dz*dz)
			if r < 1.0 {
				r = 1.0
			}
			H[i] = []float64{dx / r, dy / r, dz / r, 1.0}
			resid[i] = sv.corrRange - (r + state[3])
			W[i] = sv.weight
		}

		delta, err := solveWLS4(H, W, resid)
		if err != nil {
			return nil, fmt.Errorf("WLS solve at iteration %d: %w", iter+1, err)
		}

		state[0] += delta[0]
		state[1] += delta[1]
		state[2] += delta[2]
		state[3] += delta[3]

		posUpdate := math.Sqrt(delta[0]*delta[0] + delta[1]*delta[1] + delta[2]*delta[2])
		s.logger.Debug("WLS iteration", zap.Int("iter", iter+1), zap.Float64("pos_update_m", posUpdate))
		if posUpdate < wlsConvergenceThreshold {
			break
		}
	}

	// --- Covariance and DOP ---
	n := len(svs)
	Hfin := make([][]float64, n)
	Wfin := make([]float64, n)
	for i, sv := range svs {
		dx := state[0] - sv.x
		dy := state[1] - sv.y
		dz := state[2] - sv.z
		r := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if r < 1.0 {
			r = 1.0
		}
		Hfin[i] = []float64{dx / r, dy / r, dz / r, 1.0}
		Wfin[i] = svs[i].weight
	}

	HtWH := computeHtWH4(Hfin, Wfin)
	Q, err := invert4x4(HtWH)
	if err != nil {
		s.logger.Warn("covariance inversion failed, using identity", zap.Error(err))
		Q = identity4x4()
	}

	// Convert ECEF to geodetic.
	lat, lon, alt := ECEFToGeodetic(state[0], state[1], state[2])

	// Rotate 3×3 ECEF covariance to local ENU.
	latR := lat * math.Pi / 180
	lonR := lon * math.Pi / 180
	sinLat, cosLat := math.Sin(latR), math.Cos(latR)
	sinLon, cosLon := math.Sin(lonR), math.Cos(lonR)

	// ENU rotation matrix (rows = East, North, Up):
	R := [3][3]float64{
		{-sinLon, cosLon, 0},
		{-sinLat * cosLon, -sinLat * sinLon, cosLat},
		{cosLat * cosLon, cosLat * sinLon, sinLat},
	}
	Qxyz := [3][3]float64{
		{Q[0][0], Q[0][1], Q[0][2]},
		{Q[1][0], Q[1][1], Q[1][2]},
		{Q[2][0], Q[2][1], Q[2][2]},
	}
	Qenu := mul3x3RotCov(R, Qxyz)

	sigE := math.Sqrt(math.Max(Qenu[0][0], 0))
	sigN := math.Sqrt(math.Max(Qenu[1][1], 0))
	sigU := math.Sqrt(math.Max(Qenu[2][2], 0))

	// 95% confidence: ×2.45 for 2D, ×1.96 for 1D.
	hAcc := 2.45 * math.Sqrt(sigE*sigE+sigN*sigN)
	if hAcc < 0.5 {
		hAcc = 0.5
	}
	vAcc := 1.96 * sigU
	if vAcc < 0.5 {
		vAcc = 0.5
	}

	hdop := math.Sqrt(math.Max(Q[0][0]+Q[1][1], 0))
	pdop := math.Sqrt(math.Max(Q[0][0]+Q[1][1]+Q[2][2], 0))

	return &PositionEstimate{
		Latitude:           lat,
		Longitude:          lon,
		Altitude:           alt,
		HorizontalAccuracy: hAcc,
		VerticalAccuracy:   vAcc,
		HDOP:               hdop,
		PDOP:               pdop,
		NumSatellites:      len(svs),
		ClockBias:          state[3] / SpeedOfLight,
	}, nil
}

// ComputeSatellitePosition computes the ECEF position of a GPS/Galileo satellite
// at GPS time-of-week tk (seconds), following IS-GPS-200N Section 20.3.3.4.3.
//
// Returns ECEF (x, y, z) in metres and satellite clock correction in seconds.
func ComputeSatellitePosition(eph assistance.GnssEphemeris, tk float64) (x, y, z, clockCorr float64) {
	a := eph.SqrtA * eph.SqrtA // Semi-major axis (m)

	// Computed mean motion n₀ and corrected mean motion n.
	n0 := math.Sqrt(GmEarth / (a * a * a))
	n := n0 + eph.DeltaN

	// Time from ephemeris reference epoch with week-crossover handling.
	t := tk - eph.Toe
	for t > 302400 {
		t -= 604800
	}
	for t < -302400 {
		t += 604800
	}

	// Mean anomaly M(t) = M₀ + n·t
	M := eph.MeanAnomaly + n*t

	// Solve Kepler's equation M = E − e·sin(E) by Newton–Raphson iteration.
	E := M
	for i := 0; i < keplerMaxIterations; i++ {
		dE := (M - (E - eph.Eccentricity*math.Sin(E))) /
			(1.0 - eph.Eccentricity*math.Cos(E))
		E += dE
		if math.Abs(dE) < keplerConvergence {
			break
		}
	}

	// True anomaly ν = atan2(√(1−e²)·sin E,  cos E − e)
	sinNu := math.Sqrt(1.0-eph.Eccentricity*eph.Eccentricity) * math.Sin(E)
	cosNu := math.Cos(E) - eph.Eccentricity
	nu := math.Atan2(sinNu, cosNu)

	// Argument of latitude φ = ω + ν
	phi := eph.ArgPerigee + nu
	sin2phi := math.Sin(2 * phi)
	cos2phi := math.Cos(2 * phi)

	// Second-order harmonic corrections.
	deltaU := eph.Cus*sin2phi + eph.Cuc*cos2phi
	deltaR := eph.Crs*sin2phi + eph.Crc*cos2phi
	deltaI := eph.Cis*sin2phi + eph.Cic*cos2phi

	// Corrected argument of latitude u, radius r, inclination i.
	u := phi + deltaU
	r := a*(1.0-eph.Eccentricity*math.Cos(E)) + deltaR
	inc := eph.Inclination + deltaI + eph.IDOT*t

	// Orbital-plane position.
	xOrb := r * math.Cos(u)
	yOrb := r * math.Sin(u)

	// Corrected longitude of ascending node Ω(t).
	omega := eph.RAAN + (eph.OmegaDot-OmegaEarth)*t - OmegaEarth*eph.Toe

	cosOmega := math.Cos(omega)
	sinOmega := math.Sin(omega)
	cosInc := math.Cos(inc)
	sinInc := math.Sin(inc)

	// ECEF coordinates.
	x = xOrb*cosOmega - yOrb*cosInc*sinOmega
	y = xOrb*sinOmega + yOrb*cosInc*cosOmega
	z = yOrb * sinInc

	// Satellite clock correction: polynomial + relativistic correction.
	dtc := tk - eph.Toc
	for dtc > 302400 {
		dtc -= 604800
	}
	for dtc < -302400 {
		dtc += 604800
	}
	clockCorr = eph.Af0 + eph.Af1*dtc + eph.Af2*dtc*dtc
	// Relativistic correction ΔtR = F·e·√a·sin(E)
	clockCorr += relativisticF * eph.Eccentricity * eph.SqrtA * math.Sin(E)

	return x, y, z, clockCorr
}

// ECEFToGeodetic converts Earth-Centred Earth-Fixed (x, y, z) in metres to
// WGS84 geodetic (latitude °, longitude °, height above ellipsoid m) using
// Zhu's closed-form solution (J. Zhu, "Exact conversion of earth-centered
// coordinates to geodetic coordinates", IEEE Transactions on Aerospace and
// Electronic Systems, 1994).
func ECEFToGeodetic(x, y, z float64) (lat, lon, alt float64) {
	lon = math.Atan2(y, x) * 180.0 / math.Pi

	p := math.Sqrt(x*x + y*y)
	// Zhu parametric latitude θ
	theta := math.Atan2(z*wgs84A, p*wgs84B)
	sinT := math.Sin(theta)
	cosT := math.Cos(theta)

	latR := math.Atan2(
		z+wgs84Ep2*wgs84B*sinT*sinT*sinT,
		p-wgs84E2*wgs84A*cosT*cosT*cosT,
	)
	lat = latR * 180.0 / math.Pi

	sinLat := math.Sin(latR)
	cosLat := math.Cos(latR)
	// Prime vertical radius of curvature N(φ).
	N := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)

	if math.Abs(cosLat) > 1e-10 {
		alt = p/cosLat - N
	} else {
		// Near-pole fallback.
		alt = math.Abs(z)/math.Abs(sinLat) - N*(1.0-wgs84E2)
	}

	return lat, lon, alt
}

// =============================================================================
// Linear algebra helpers
// =============================================================================

// solveWLS4 solves the 4-state WLS normal equations δ = (HᵀWH)⁻¹ HᵀW r.
// W is diagonal, supplied as a slice. Uses Gaussian elimination for robustness.
func solveWLS4(H [][]float64, W, r []float64) ([4]float64, error) {
	// Build 4×4 normal matrix A = HᵀWH and right-hand side b = HᵀWr.
	var A [4][4]float64
	var b [4]float64
	for k, hk := range H {
		w := W[k]
		for i := 0; i < 4; i++ {
			b[i] += hk[i] * w * r[k]
			for j := 0; j < 4; j++ {
				A[i][j] += hk[i] * w * hk[j]
			}
		}
	}
	return solve4x4Gauss(A, b)
}

// solve4x4Gauss solves A·x = b by Gaussian elimination with partial pivoting.
func solve4x4Gauss(A [4][4]float64, b [4]float64) ([4]float64, error) {
	const n = 4
	// Augmented matrix [A|b].
	var aug [4][5]float64
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			aug[i][j] = A[i][j]
		}
		aug[i][n] = b[i]
	}

	// Forward elimination with partial pivoting.
	for col := 0; col < n; col++ {
		// Find pivot row.
		pivotRow := col
		pivotVal := math.Abs(aug[col][col])
		for row := col + 1; row < n; row++ {
			if v := math.Abs(aug[row][col]); v > pivotVal {
				pivotVal = v
				pivotRow = row
			}
		}
		if pivotVal < 1e-20 {
			return [4]float64{}, fmt.Errorf("near-singular normal matrix at column %d (pivot=%.3e)", col, pivotVal)
		}
		aug[col], aug[pivotRow] = aug[pivotRow], aug[col]

		// Eliminate below pivot.
		for row := col + 1; row < n; row++ {
			factor := aug[row][col] / aug[col][col]
			for j := col; j <= n; j++ {
				aug[row][j] -= factor * aug[col][j]
			}
		}
	}

	// Back substitution.
	var x [4]float64
	for i := n - 1; i >= 0; i-- {
		x[i] = aug[i][n]
		for j := i + 1; j < n; j++ {
			x[i] -= aug[i][j] * x[j]
		}
		x[i] /= aug[i][i]
	}
	return x, nil
}

// computeHtWH4 computes the 4×4 normal matrix HᵀWH.
func computeHtWH4(H [][]float64, W []float64) [4][4]float64 {
	var m [4][4]float64
	for k, hk := range H {
		w := W[k]
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				m[i][j] += hk[i] * w * hk[j]
			}
		}
	}
	return m
}

// invert4x4 inverts a 4×4 matrix via the cofactor/adjugate method.
func invert4x4(m [4][4]float64) ([4][4]float64, error) {
	det := det4x4(m)
	if math.Abs(det) < 1e-30 {
		return [4][4]float64{}, fmt.Errorf("near-singular 4×4 matrix (det=%.3e)", det)
	}
	// Adjugate (transpose of cofactor matrix).
	var adj [4][4]float64
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			adj[j][i] = cofactor4(m, i, j) // transposed
		}
	}
	var inv [4][4]float64
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			inv[i][j] = adj[i][j] / det
		}
	}
	return inv, nil
}

func det4x4(m [4][4]float64) float64 {
	var d float64
	for j := 0; j < 4; j++ {
		d += m[0][j] * cofactor4(m, 0, j)
	}
	return d
}

func cofactor4(m [4][4]float64, row, col int) float64 {
	sign := 1.0
	if (row+col)%2 != 0 {
		sign = -1.0
	}
	return sign * det3x3(minor3x3from4(m, row, col))
}

func minor3x3from4(m [4][4]float64, skipRow, skipCol int) [3][3]float64 {
	var out [3][3]float64
	ri := 0
	for i := 0; i < 4; i++ {
		if i == skipRow {
			continue
		}
		rj := 0
		for j := 0; j < 4; j++ {
			if j == skipCol {
				continue
			}
			out[ri][rj] = m[i][j]
			rj++
		}
		ri++
	}
	return out
}

func det3x3(m [3][3]float64) float64 {
	return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
}

func identity4x4() [4][4]float64 {
	var m [4][4]float64
	for i := 0; i < 4; i++ {
		m[i][i] = 1
	}
	return m
}

// mul3x3RotCov computes R·Q·Rᵀ for 3×3 matrices (rotation of covariance matrix).
func mul3x3RotCov(R, Q [3][3]float64) [3][3]float64 {
	var tmp [3][3]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				tmp[i][j] += R[i][k] * Q[k][j]
			}
		}
	}
	var result [3][3]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				result[i][j] += tmp[i][k] * R[j][k]
			}
		}
	}
	return result
}
