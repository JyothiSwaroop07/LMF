// Package positioning implements NR DL-TDOA position computation using
// Chan's closed-form Weighted Least Squares algorithm.
//
// Reference:
//   Y. T. Chan, K. C. Ho, "A simple and efficient estimator for hyperbolic
//   location", IEEE Transactions on Signal Processing, vol. 42, no. 8, 1994.
package positioning

import (
	"fmt"
	"math"

	"go.uber.org/zap"
)

// Physical / NR radio constants.
const (
	SpeedOfLight = 3e8       // m/s
	nrSampleRate = 30.72e6   // Hz — NR basic time unit Tc = 1/30.72 MHz for 30 kHz SCS
	tsNR         = 1.0 / nrSampleRate // ~32.55 ns per NR sample
)

// CellGeometry holds the WGS84 position and 5G NR cell identity.
type CellGeometry struct {
	NCI           string  // NR Cell Identity (28-bit, as string for readability)
	Latitude      float64 // degrees
	Longitude     float64 // degrees
	Altitude      float64 // metres (above WGS84 ellipsoid)
	AntennaHeight float64 // metres above ground level
}

// RSTDMeasurement is one Reference Signal Time Difference (RSTD) measurement.
//
// RSTD = TOA_reference − TOA_neighbor, in NR time units (Tc = 1/30.72 MHz).
// Positive RSTD means the reference cell signal arrived later than the
// neighbor cell signal.
type RSTDMeasurement struct {
	ReferenceNCI string  // NCI of the reference TRP
	NeighborNCI  string  // NCI of the measured neighbor TRP
	RSTD         float64 // Reference Signal Time Difference (NR samples)
	Quality      float64 // Measurement quality 0..1; used as WLS weight
}

// DlTdoaMeasurements bundles all RSTD observations for one UE session.
type DlTdoaMeasurements struct {
	SessionID    string
	Measurements []RSTDMeasurement
}

// PositionEstimate is the 2D position result from TDOA.
type PositionEstimate struct {
	Latitude           float64 // WGS84 degrees
	Longitude          float64 // WGS84 degrees
	Altitude           float64 // metres (taken from reference cell)
	HorizontalAccuracy float64 // ~95% confidence radius (m)
	HDOP               float64
	NumMeasurements    int

	// Uncertainty ellipse (95% confidence, metres / degrees from North).
	SemiMajorAxis float64
	SemiMinorAxis float64
	Orientation   float64 // degrees clockwise from North (East = +90°)
}

// TdoaSolver implements DL-TDOA positioning using Chan's algorithm.
type TdoaSolver struct {
	logger *zap.Logger
}

// NewTdoaSolver creates a new TdoaSolver.
func NewTdoaSolver(logger *zap.Logger) *TdoaSolver {
	return &TdoaSolver{logger: logger}
}

// ComputePosition runs Chan's TDOA algorithm on the supplied measurements.
//
// Requirements:
//   - At least 3 RSTD measurements (gives 2D fix).
//   - All measurements must share the same reference NCI.
//   - cellGeom must contain entries for all NCIs referenced in measurements.
func (s *TdoaSolver) ComputePosition(
	meas DlTdoaMeasurements,
	cellGeom map[string]CellGeometry,
) (*PositionEstimate, error) {
	if len(meas.Measurements) < 3 {
		return nil, fmt.Errorf("need at least 3 RSTD measurements, got %d", len(meas.Measurements))
	}

	// Validate: all measurements must share one reference NCI.
	refNCI := meas.Measurements[0].ReferenceNCI
	for _, m := range meas.Measurements {
		if m.ReferenceNCI != refNCI {
			return nil, fmt.Errorf("mixed reference cells: %q and %q", refNCI, m.ReferenceNCI)
		}
	}

	refCell, ok := cellGeom[refNCI]
	if !ok {
		return nil, fmt.Errorf("reference cell %q not found in geometry store", refNCI)
	}

	refLat := refCell.Latitude
	refLon := refCell.Longitude

	// Collect neighbor positions in local ENU (East-North) frame centred on
	// the reference cell, range differences in metres, and per-observation weights.
	neighborPos := make([][2]float64, 0, len(meas.Measurements))
	rangeDiff := make([]float64, 0, len(meas.Measurements))
	weights := make([]float64, 0, len(meas.Measurements))

	for _, m := range meas.Measurements {
		nbCell, found := cellGeom[m.NeighborNCI]
		if !found {
			s.logger.Warn("neighbor cell not found in geometry, skipping",
				zap.String("nci", m.NeighborNCI))
			continue
		}

		east, north := geoToENU(refLat, refLon, nbCell.Latitude, nbCell.Longitude)
		neighborPos = append(neighborPos, [2]float64{east, north})

		// Convert RSTD (NR samples) to range difference (metres).
		// Δd = RSTD · Tc · c   where Tc = 1/30.72 MHz
		deltaD := m.RSTD * SpeedOfLight * tsNR
		rangeDiff = append(rangeDiff, deltaD)

		// Weight = quality² (Chan's formulation uses variance-based weights;
		// assuming measurement variance ∝ 1/quality²).
		q := m.Quality
		if q <= 0 {
			q = 1.0
		}
		weights = append(weights, q*q)
	}

	if len(neighborPos) < 3 {
		return nil, fmt.Errorf("need ≥3 valid neighbor cells with geometry, got %d", len(neighborPos))
	}

	refPos := [2]float64{0, 0} // Reference cell at ENU origin.

	// Run Chan's two-stage WLS algorithm.
	ueLocal, cov, err := chanAlgorithm2D(refPos, neighborPos, rangeDiff, weights)
	if err != nil {
		return nil, fmt.Errorf("Chan algorithm failed: %w", err)
	}

	s.logger.Debug("TDOA Chan solution",
		zap.Float64("local_east_m", ueLocal[0]),
		zap.Float64("local_north_m", ueLocal[1]),
	)

	// Convert local ENU back to WGS84.
	lat, lon := enuToGeo(refLat, refLon, ueLocal[0], ueLocal[1])

	// Uncertainty ellipse from 2×2 position covariance.
	semiMajor, semiMinor, orientation := covEllipse95(cov)

	// HDOP from all available cell positions.
	allCells := make([][2]float64, 0, len(neighborPos)+1)
	allCells = append(allCells, refPos)
	allCells = append(allCells, neighborPos...)
	hdop := ComputeHDOP(ueLocal, allCells)

	// Horizontal accuracy: RMS σ scaled to 95% (2.45σ for 2D normal).
	hAcc := 2.45 * math.Sqrt(0.5*(math.Max(cov[0][0], 0)+math.Max(cov[1][1], 0)))
	if hAcc < 1 {
		hAcc = 1
	}

	return &PositionEstimate{
		Latitude:           lat,
		Longitude:          lon,
		Altitude:           refCell.Altitude,
		HorizontalAccuracy: hAcc,
		HDOP:               hdop,
		NumMeasurements:    len(rangeDiff),
		SemiMajorAxis:      semiMajor,
		SemiMinorAxis:      semiMinor,
		Orientation:        orientation,
	}, nil
}

// chanAlgorithm2D implements Chan's two-stage closed-form WLS estimator for
// 2D TDOA positioning in a local East-North frame.
//
// Stage 1 (Gauss–Markov estimator):
//   Linearise the hyperbolic TDOA equations around an intermediate nuisance
//   variable R_r (true range from UE to reference station).  The first WLS
//   stage solves for [x, y, R_r].
//
// Stage 2 (variance-weighted refinement):
//   Exploit the nonlinear relationship between the Stage-1 solution components
//   to reduce bias and improve accuracy, yielding the final [x, y] estimate.
//
// refPos:      reference station position [x_r, y_r] (metres, local frame)
// stations:    neighbor station positions  [][x_i, y_i]
// rangeDiff:   range differences Δd_i = d_i − d_r  (metres)
// W1diag:      diagonal weight values (variance-based)
func chanAlgorithm2D(
	refPos [2]float64,
	stations [][2]float64,
	rangeDiff []float64,
	W1diag []float64,
) ([2]float64, [2][2]float64, error) {
	n := len(stations)
	if n < 3 {
		return [2]float64{}, [2][2]float64{}, fmt.Errorf("need ≥3 stations for 2D Chan, got %d", n)
	}

	xr, yr := refPos[0], refPos[1]
	kr := xr*xr + yr*yr

	// Build linearised system G_a · z_a = h_a  where z_a = [x, y, R_r]ᵀ.
	//
	// For station i (xi, yi):
	//   G_a[i]  = [x_i − x_r,  y_i − y_r,  −Δd_i]
	//   h_a[i]  = 0.5 · (Δd_i² + k_i − k_r)
	//   where k_i = x_i² + y_i²
	Ga := make([][]float64, n)
	ha := make([]float64, n)

	for i, st := range stations {
		xi, yi := st[0], st[1]
		ki := xi*xi + yi*yi
		ddi := rangeDiff[i]
		Ga[i] = []float64{xi - xr, yi - yr, -ddi}
		ha[i] = 0.5 * (ddi*ddi + ki - kr)
	}

	// Stage 1 WLS: z_a = (G_aᵀ W1 G_a)⁻¹ G_aᵀ W1 h_a
	Za, err := wls3x1(Ga, W1diag, ha)
	if err != nil {
		return [2]float64{}, [2][2]float64{}, fmt.Errorf("stage-1 WLS: %w", err)
	}

	x1, y1, Rr1 := Za[0], Za[1], Za[2]

	// Stage 2 refinement.
	// Define z_b = [x², y²]ᵀ.  The constraint equations are:
	//   z_b[0] = (x − x_r)² ≈ x1² − (x_r = 0 for our local frame with ref at origin)
	//   z_b[1] = (y − y_r)² ≈ y1²
	//   z_b[0]+z_b[1] = Rr²
	//
	// This gives the 3-row overdetermined system:
	//   G_b · z_b = h_b  where G_b = I₂ stacked with [1,1] row, h_b from Za.
	Gb := [][]float64{
		{1, 0},
		{0, 1},
		{1, 1},
	}
	hb := []float64{x1 * x1, y1 * y1, Rr1 * Rr1}

	// Weight W2 is proportional to B⁻¹ where B = diag(Za[i]²).
	eps := 1e-3 // avoid division by zero for near-zero components
	W2 := []float64{
		1.0 / math.Max(x1*x1, eps),
		1.0 / math.Max(y1*y1, eps),
		1.0 / math.Max(Rr1*Rr1, eps),
	}

	Zb, err := wls2x1(Gb, W2, hb)
	if err != nil {
		// Fall back to Stage-1 result if Stage-2 is degenerate.
		s1 := math.Sqrt(math.Abs(x1))
		if x1 < 0 {
			s1 = -s1
		}
		s2 := math.Sqrt(math.Abs(y1))
		if y1 < 0 {
			s2 = -s2
		}
		return [2]float64{s1, s2}, identityCov2(1000), nil
	}

	// Final position: signs preserved from Stage-1.
	xFinal := math.Sqrt(math.Abs(Zb[0]))
	if x1 < 0 {
		xFinal = -xFinal
	}
	yFinal := math.Sqrt(math.Abs(Zb[1]))
	if y1 < 0 {
		yFinal = -yFinal
	}

	// Position covariance: upper-left 2×2 block of (G_aᵀ W1 G_a)⁻¹.
	GtWG3, err := computeGtWG3(Ga, W1diag)
	if err != nil {
		return [2]float64{xFinal, yFinal}, identityCov2(1000), nil
	}
	inv3, err := invert3x3(GtWG3)
	if err != nil {
		return [2]float64{xFinal, yFinal}, identityCov2(1000), nil
	}

	cov := [2][2]float64{
		{inv3[0][0], inv3[0][1]},
		{inv3[1][0], inv3[1][1]},
	}

	return [2]float64{xFinal, yFinal}, cov, nil
}

// ComputeHDOP computes the Horizontal Dilution of Precision for a UE position
// surrounded by reference stations (all in the same 2D local frame).
func ComputeHDOP(uePos [2]float64, cellPositions [][2]float64) float64 {
	n := len(cellPositions)
	if n < 2 {
		return 99.9
	}

	// Build the 2-column design matrix H (unit vectors from UE to each cell).
	var HtH [2][2]float64
	for _, c := range cellPositions {
		dx := uePos[0] - c[0]
		dy := uePos[1] - c[1]
		r := math.Sqrt(dx*dx + dy*dy)
		if r < 1 {
			r = 1
		}
		lx, ly := dx/r, dy/r
		HtH[0][0] += lx * lx
		HtH[0][1] += lx * ly
		HtH[1][0] += ly * lx
		HtH[1][1] += ly * ly
	}

	det := HtH[0][0]*HtH[1][1] - HtH[0][1]*HtH[1][0]
	if math.Abs(det) < 1e-15 {
		return 99.9
	}
	// HDOP = sqrt(trace(Q_H)) where Q_H = (HᵀH)⁻¹
	//       = sqrt((HtH[1][1] + HtH[0][0]) / det)
	return math.Sqrt((HtH[0][0] + HtH[1][1]) / det)
}

// ConvertGeoToLocal converts a slice of WGS84 cell positions to local ENU
// (East, North) coordinates in metres relative to (refLat, refLon).
func ConvertGeoToLocal(refLat, refLon float64, cells []CellGeometry) [][2]float64 {
	result := make([][2]float64, len(cells))
	for i, c := range cells {
		e, n := geoToENU(refLat, refLon, c.Latitude, c.Longitude)
		result[i] = [2]float64{e, n}
	}
	return result
}

// ConvertLocalToGeo converts local ENU offsets (metres) back to WGS84.
func ConvertLocalToGeo(refLat, refLon, localEast, localNorth float64) (lat, lon float64) {
	return enuToGeo(refLat, refLon, localEast, localNorth)
}

// =============================================================================
// Coordinate conversion helpers
// =============================================================================

// geoToENU converts a WGS84 target point to local East–North offsets (metres)
// relative to the reference point (refLat, refLon).  Uses the small-angle
// approximation based on ellipsoidal radii of curvature (good to < 1 m for
// offsets up to ~50 km from the reference).
func geoToENU(refLat, refLon, lat, lon float64) (east, north float64) {
	const (
		a  = 6378137.0            // WGS84 semi-major axis (m)
		f  = 1.0 / 298.257223563  // WGS84 flattening
		e2 = 2*f - f*f            // first eccentricity squared
	)

	refLatR := refLat * math.Pi / 180
	sinRef := math.Sin(refLatR)

	// Prime vertical radius of curvature N(φ_ref)
	N := a / math.Sqrt(1-e2*sinRef*sinRef)
	// Meridian radius of curvature M(φ_ref)
	M := a * (1 - e2) / math.Pow(1-e2*sinRef*sinRef, 1.5)

	dLatR := (lat - refLat) * math.Pi / 180
	dLonR := (lon - refLon) * math.Pi / 180

	north = M * dLatR
	east = N * math.Cos(refLatR) * dLonR
	return east, north
}

// enuToGeo is the inverse of geoToENU.
func enuToGeo(refLat, refLon, east, north float64) (lat, lon float64) {
	const (
		a  = 6378137.0
		f  = 1.0 / 298.257223563
		e2 = 2*f - f*f
	)

	refLatR := refLat * math.Pi / 180
	sinRef := math.Sin(refLatR)

	N := a / math.Sqrt(1-e2*sinRef*sinRef)
	M := a * (1 - e2) / math.Pow(1-e2*sinRef*sinRef, 1.5)

	dLatR := north / M
	dLonR := east / (N * math.Cos(refLatR))

	lat = refLat + dLatR*180/math.Pi
	lon = refLon + dLonR*180/math.Pi
	return lat, lon
}

// covEllipse95 extracts the 95%-confidence uncertainty ellipse parameters from
// a 2×2 covariance matrix.  Returns (semiMajor, semiMinor, orientation).
// Orientation is in degrees clockwise from the North (y-axis) direction.
func covEllipse95(cov [2][2]float64) (semiMajor, semiMinor, orientation float64) {
	// Eigenvalues λ = [trace ± sqrt(trace² − 4·det)] / 2
	trace := cov[0][0] + cov[1][1]
	det := cov[0][0]*cov[1][1] - cov[0][1]*cov[1][0]

	disc := trace*trace - 4*det
	if disc < 0 {
		disc = 0
	}
	sqrtDisc := math.Sqrt(disc)

	lambda1 := (trace + sqrtDisc) / 2
	lambda2 := (trace - sqrtDisc) / 2
	if lambda1 < 0 {
		lambda1 = 0
	}
	if lambda2 < 0 {
		lambda2 = 0
	}

	// 95% confidence scale factor for 2D normal: χ²(2,0.95) → ×2.45
	semiMajor = math.Sqrt(lambda1) * 2.45
	semiMinor = math.Sqrt(lambda2) * 2.45

	// Orientation of the major eigenvector.
	// For symmetric matrix, the eigenvector angle θ = 0.5·atan2(2·σ_xy, σ_x²−σ_y²)
	if math.Abs(cov[0][1]) < 1e-12 && math.Abs(cov[0][0]-cov[1][1]) < 1e-12 {
		orientation = 0
	} else {
		theta := 0.5 * math.Atan2(2*cov[0][1], cov[0][0]-cov[1][1])
		orientation = theta * 180 / math.Pi
	}
	return semiMajor, semiMinor, orientation
}

// =============================================================================
// Linear algebra helpers (3×3 and 2×2 WLS for Chan's algorithm)
// =============================================================================

// wls3x1 solves the 3-state WLS system (GᵀWG)z = GᵀWh  using Gaussian elim.
func wls3x1(G [][]float64, Wdiag []float64, h []float64) ([3]float64, error) {
	GtWG, err := computeGtWG3(G, Wdiag)
	if err != nil {
		return [3]float64{}, err
	}
	GtWh := computeGtWr3(G, Wdiag, h)
	inv, err := invert3x3(GtWG)
	if err != nil {
		return [3]float64{}, err
	}
	var z [3]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			z[i] += inv[i][j] * GtWh[j]
		}
	}
	return z, nil
}

// wls2x1 solves the 2-state WLS system (GᵀWG)z = GᵀWh.
func wls2x1(G [][]float64, Wdiag []float64, h []float64) ([2]float64, error) {
	var GtWG [2][2]float64
	var GtWh [2]float64
	for k, gk := range G {
		w := Wdiag[k]
		for i := 0; i < 2; i++ {
			GtWh[i] += gk[i] * w * h[k]
			for j := 0; j < 2; j++ {
				GtWG[i][j] += gk[i] * w * gk[j]
			}
		}
	}
	det := GtWG[0][0]*GtWG[1][1] - GtWG[0][1]*GtWG[1][0]
	if math.Abs(det) < 1e-20 {
		return [2]float64{}, fmt.Errorf("near-singular 2×2 normal matrix (det=%.3e)", det)
	}
	inv := [2][2]float64{
		{GtWG[1][1] / det, -GtWG[0][1] / det},
		{-GtWG[1][0] / det, GtWG[0][0] / det},
	}
	var z [2]float64
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			z[i] += inv[i][j] * GtWh[j]
		}
	}
	return z, nil
}

func computeGtWG3(G [][]float64, Wdiag []float64) ([3][3]float64, error) {
	var m [3][3]float64
	for k, gk := range G {
		if len(gk) < 3 {
			return m, fmt.Errorf("row %d has %d cols, need 3", k, len(gk))
		}
		w := Wdiag[k]
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				m[i][j] += gk[i] * w * gk[j]
			}
		}
	}
	return m, nil
}

func computeGtWr3(G [][]float64, Wdiag, r []float64) [3]float64 {
	var v [3]float64
	for k, gk := range G {
		for i := 0; i < 3; i++ {
			v[i] += gk[i] * Wdiag[k] * r[k]
		}
	}
	return v
}

// invert3x3 inverts a 3×3 matrix analytically using the adjugate.
func invert3x3(m [3][3]float64) ([3][3]float64, error) {
	det := m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	if math.Abs(det) < 1e-25 {
		return [3][3]float64{}, fmt.Errorf("near-singular 3×3 matrix (det=%.3e)", det)
	}
	var adj [3][3]float64
	adj[0][0] = m[1][1]*m[2][2] - m[1][2]*m[2][1]
	adj[0][1] = -(m[0][1]*m[2][2] - m[0][2]*m[2][1])
	adj[0][2] = m[0][1]*m[1][2] - m[0][2]*m[1][1]
	adj[1][0] = -(m[1][0]*m[2][2] - m[1][2]*m[2][0])
	adj[1][1] = m[0][0]*m[2][2] - m[0][2]*m[2][0]
	adj[1][2] = -(m[0][0]*m[1][2] - m[0][2]*m[1][0])
	adj[2][0] = m[1][0]*m[2][1] - m[1][1]*m[2][0]
	adj[2][1] = -(m[0][0]*m[2][1] - m[0][1]*m[2][0])
	adj[2][2] = m[0][0]*m[1][1] - m[0][1]*m[1][0]

	var inv [3][3]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			inv[i][j] = adj[i][j] / det
		}
	}
	return inv, nil
}

func identityCov2(scale float64) [2][2]float64 {
	return [2][2]float64{{scale, 0}, {0, scale}}
}
