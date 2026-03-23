// Package assistance provides GNSS assistance data types, fetching logic,
// and a Redis-backed cache for the GNSS positioning engine.
package assistance

import (
	"context"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"
)

// GnssConstellation identifies a GNSS constellation.
type GnssConstellation int

const (
	ConstellationGPS     GnssConstellation = 0
	ConstellationGalileo GnssConstellation = 1
	ConstellationGLONASS GnssConstellation = 2
	ConstellationBeiDou  GnssConstellation = 3
)

// GnssEphemeris holds Keplerian orbital elements and correction terms for a
// single satellite navigation message, as defined in IS-GPS-200 / Galileo ICD.
type GnssEphemeris struct {
	Constellation GnssConstellation
	SvID          string // e.g. "G01", "E03"
	PRN           int

	// --- Keplerian orbital elements ---
	SqrtA        float64 // Square root of semi-major axis (m^0.5), typical GPS ≈ 5153.7
	Eccentricity float64 // Orbital eccentricity (dimensionless), typical GPS < 0.02
	Inclination  float64 // Inclination at reference time (rad), GPS ≈ 0.96 rad (55°)
	RAAN         float64 // Right ascension of ascending node at weekly epoch (rad)
	ArgPerigee   float64 // Argument of perigee ω (rad)
	MeanAnomaly  float64 // Mean anomaly at reference time M0 (rad)

	// --- Perturbation / correction terms ---
	DeltaN   float64 // Mean motion correction Δn (rad/s)
	IDOT     float64 // Rate of inclination angle di/dt (rad/s)
	OmegaDot float64 // Rate of right ascension dΩ/dt (rad/s)

	// Second-order harmonic corrections to orbital radius, argument of latitude, inclination
	Crs float64 // Sine harmonic correction to orbital radius (m)
	Crc float64 // Cosine harmonic correction to orbital radius (m)
	Cus float64 // Sine harmonic correction to argument of latitude (rad)
	Cuc float64 // Cosine harmonic correction to argument of latitude (rad)
	Cis float64 // Sine harmonic correction to angle of inclination (rad)
	Cic float64 // Cosine harmonic correction to angle of inclination (rad)

	// --- Reference epochs ---
	Toe float64 // Time of ephemeris (s in GPS week)
	Toc float64 // Time of clock (s in GPS week)

	// --- Satellite clock corrections ---
	Af0 float64 // Clock bias (s)
	Af1 float64 // Clock drift (s/s)
	Af2 float64 // Clock drift rate (s/s²)

	// --- Metadata ---
	Health     int
	IODE       int
	WeekNumber int
}

// KlobucharModel holds the 8-parameter Klobuchar ionospheric correction model
// broadcast in the GPS navigation message (IS-GPS-200 Section 20.3.3.5.2.5).
type KlobucharModel struct {
	// Alpha coefficients: a0..a3 (s, s/semi-circle, s/semi-circle², s/semi-circle³)
	Alpha [4]float64
	// Beta coefficients: b0..b3 (s, s/semi-circle, s/semi-circle², s/semi-circle³)
	Beta [4]float64
}

// GPSWeekSeconds encapsulates GPS time as week number and seconds-within-week.
type GPSWeekSeconds struct {
	Week    int
	Seconds float64
}

// AssistanceDataProvider fetches and distributes GNSS assistance data.
// In production this would connect to a real GNSS reference network; here it
// generates physically-realistic synthetic data with correct parameter ranges.
type AssistanceDataProvider struct {
	cache         *AssistanceDataCache
	gnssServerURL string
	logger        *zap.Logger
}

// NewAssistanceDataProvider creates a new provider.
func NewAssistanceDataProvider(
	cache *AssistanceDataCache,
	gnssServerURL string,
	logger *zap.Logger,
) *AssistanceDataProvider {
	return &AssistanceDataProvider{
		cache:         cache,
		gnssServerURL: gnssServerURL,
		logger:        logger,
	}
}

// Cache exposes the underlying cache for direct access (e.g. by the gRPC server).
func (p *AssistanceDataProvider) Cache() *AssistanceDataCache { return p.cache }

// FetchEphemeris returns ephemeris data for the requested constellations.
// GPS and Galileo are supported; 6 SVs per constellation are generated with
// realistic Keplerian parameters spread across 3 orbital planes.
func (p *AssistanceDataProvider) FetchEphemeris(
	ctx context.Context,
	constellations []GnssConstellation,
) ([]GnssEphemeris, error) {
	var all []GnssEphemeris
	gpsWeek, gpsTow := currentGPSTime()

	for _, c := range constellations {
		switch c {
		case ConstellationGPS:
			all = append(all, generateGPSEphemerides(gpsWeek, gpsTow)...)
		case ConstellationGalileo:
			all = append(all, generateGalileoEphemerides(gpsWeek, gpsTow)...)
		default:
			p.logger.Warn("unsupported constellation, skipping", zap.Int("constellation", int(c)))
		}
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no ephemeris generated for requested constellations")
	}

	if err := p.cache.Set(ctx, "ephemeris", all, TTLEphemeris); err != nil {
		p.logger.Warn("failed to cache ephemeris", zap.Error(err))
	}

	p.logger.Info("fetched ephemeris", zap.Int("count", len(all)))
	return all, nil
}

// FetchIonosphericModel returns the Klobuchar model using standard GPS coefficients
// from a representative almanac. Real coefficients from a 2023 GPS almanac:
//
//	α: [1.1176e-8, -1.4901e-8, -5.9605e-8, 1.1921e-7]
//	β: [1.1264e5,  -1.6384e5,  -6.5536e5,  5.2429e5 ]
func (p *AssistanceDataProvider) FetchIonosphericModel(ctx context.Context) (*KlobucharModel, error) {
	model := &KlobucharModel{
		Alpha: [4]float64{1.1176e-8, -1.4901e-8, -5.9605e-8, 1.1921e-7},
		Beta:  [4]float64{1.1264e5, -1.6384e5, -6.5536e5, 5.2429e5},
	}
	if err := p.cache.Set(ctx, "ionospheric", model, TTLIonospheric); err != nil {
		p.logger.Warn("failed to cache ionospheric model", zap.Error(err))
	}
	return model, nil
}

// FetchReferenceTime returns current UTC, GPS week, and time-of-week.
func (p *AssistanceDataProvider) FetchReferenceTime(ctx context.Context) (time.Time, GPSWeekSeconds, error) {
	now := time.Now().UTC()
	week, tow := currentGPSTime()
	gps := GPSWeekSeconds{Week: week, Seconds: tow}

	if err := p.cache.Set(ctx, "ref_time", gps, TTLRefTime); err != nil {
		p.logger.Warn("failed to cache reference time", zap.Error(err))
	}
	return now, gps, nil
}

// ComputeUTCOffset returns the GPS–UTC offset (leap seconds). As of 2024: 18 s.
func (p *AssistanceDataProvider) ComputeUTCOffset(_ context.Context) (int, error) {
	return 18, nil
}

// RefreshAll proactively refreshes all assistance data types.
func (p *AssistanceDataProvider) RefreshAll(ctx context.Context) error {
	constellations := []GnssConstellation{ConstellationGPS, ConstellationGalileo}
	if _, err := p.FetchEphemeris(ctx, constellations); err != nil {
		return fmt.Errorf("ephemeris refresh: %w", err)
	}
	if _, err := p.FetchIonosphericModel(ctx); err != nil {
		return fmt.Errorf("ionospheric refresh: %w", err)
	}
	if _, _, err := p.FetchReferenceTime(ctx); err != nil {
		return fmt.Errorf("reference time refresh: %w", err)
	}
	return nil
}

// currentGPSTime returns the current GPS week number (modulo 1024) and
// time-of-week in seconds. GPS epoch: 1980-01-06 00:00:00 UTC.
// Leap seconds (currently 18) are added because GPS time is continuous.
func currentGPSTime() (week int, tow float64) {
	gpsEpoch := time.Date(1980, 1, 6, 0, 0, 0, 0, time.UTC)
	elapsed := time.Since(gpsEpoch).Seconds() + 18 // add leap seconds
	totalWeeks := int(elapsed / (7 * 24 * 3600))
	week = totalWeeks % 1024 // GPS week rollover
	tow = elapsed - float64(totalWeeks)*7*24*3600
	return week, tow
}

// generateGPSEphemerides generates 6 realistic GPS satellite ephemerides.
//
// GPS Block IIF/III orbital parameters:
//   - Semi-major axis a ≈ 26,560 km  →  √a ≈ 5153.7 m^0.5
//   - Inclination ≈ 55° = 0.9599 rad
//   - Eccentricity < 0.01 (nearly circular)
//   - 6 planes (A–F), 4 slots each; we pick 2 per plane for 6 total
func generateGPSEphemerides(gpsWeek int, gpsTow float64) []GnssEphemeris {
	// Representative PRNs from 3 orbital planes
	type svParams struct {
		prn         int
		raanOffset  float64 // radians
		m0Offset    float64 // radians, within-plane position
		sqrtAOffset float64 // m^0.5 variation per slot
		eccOffset   float64
	}

	params := []svParams{
		{prn: 1, raanOffset: 0, m0Offset: 0, sqrtAOffset: 0, eccOffset: 0},
		{prn: 7, raanOffset: 0, m0Offset: math.Pi, sqrtAOffset: 0.2, eccOffset: 0.0002},
		{prn: 13, raanOffset: 2 * math.Pi / 3, m0Offset: math.Pi / 6, sqrtAOffset: 0.1, eccOffset: 0.0001},
		{prn: 15, raanOffset: 2 * math.Pi / 3, m0Offset: math.Pi + math.Pi/6, sqrtAOffset: 0.3, eccOffset: 0.0003},
		{prn: 21, raanOffset: 4 * math.Pi / 3, m0Offset: math.Pi / 3, sqrtAOffset: 0.15, eccOffset: 0.0002},
		{prn: 27, raanOffset: 4 * math.Pi / 3, m0Offset: math.Pi + math.Pi/3, sqrtAOffset: 0.25, eccOffset: 0.0004},
	}

	// Toe: reference time snapped to 2-hour interval (7200 s)
	toe := math.Trunc(gpsTow/7200) * 7200

	ephs := make([]GnssEphemeris, 0, len(params))
	for i, p := range params {
		ephs = append(ephs, GnssEphemeris{
			Constellation: ConstellationGPS,
			SvID:          fmt.Sprintf("G%02d", p.prn),
			PRN:           p.prn,
			WeekNumber:    gpsWeek,

			SqrtA:        5153.7 + p.sqrtAOffset,
			Eccentricity: 0.001 + p.eccOffset,
			Inclination:  0.9599 + float64(i)*0.0005, // slight spread ±0.03°

			RAAN:        p.raanOffset + 0.01*float64(i), // small perturbation
			ArgPerigee:  0.3 + float64(i)*0.15,
			MeanAnomaly: p.m0Offset,

			DeltaN:   4.5e-9,
			IDOT:     -1.2e-10,
			OmegaDot: -8.0e-9,

			Toe: toe,
			Toc: toe,

			// Clock bias: order of 100 ns (realistic GPS SV clock error)
			Af0: float64(i)*1e-7 - 3e-7,
			Af1: 1.2e-12,
			Af2: 0,

			// Harmonic correction amplitudes (typical GPS magnitudes)
			Crs: 20.0 + float64(i)*2.5,
			Crc: 180.0 + float64(i)*3.0,
			Cus: 5.2e-6,
			Cuc: -3.1e-6,
			Cis: 5.6e-8,
			Cic: -1.3e-7,

			Health: 0,
			IODE:   i + 1,
		})
	}
	return ephs
}

// generateGalileoEphemerides generates 6 realistic Galileo satellite ephemerides.
//
// Galileo orbital parameters (Galileo ICD 2.0):
//   - Semi-major axis a ≈ 29,600 km  →  √a ≈ 5440.6 m^0.5
//   - Inclination ≈ 56° = 0.9774 rad
//   - Eccentricity < 0.001 (more circular than GPS)
//   - 3 orbital planes, nominally 8 active SVs per plane
func generateGalileoEphemerides(gpsWeek int, gpsTow float64) []GnssEphemeris {
	type svParams struct {
		svID        string
		prn         int
		raanOffset  float64
		m0Offset    float64
		sqrtAOffset float64
	}

	params := []svParams{
		{svID: "E01", prn: 1, raanOffset: 0, m0Offset: 0.1},
		{svID: "E03", prn: 3, raanOffset: 0, m0Offset: math.Pi/3 + 0.1, sqrtAOffset: 0.15},
		{svID: "E09", prn: 9, raanOffset: 2 * math.Pi / 3, m0Offset: 2*math.Pi/3 + 0.1, sqrtAOffset: 0.1},
		{svID: "E18", prn: 18, raanOffset: 2 * math.Pi / 3, m0Offset: math.Pi + 0.1, sqrtAOffset: 0.2},
		{svID: "E21", prn: 21, raanOffset: 4 * math.Pi / 3, m0Offset: 4*math.Pi/3 + 0.1, sqrtAOffset: 0.05},
		{svID: "E31", prn: 31, raanOffset: 4 * math.Pi / 3, m0Offset: 5*math.Pi/3 + 0.1, sqrtAOffset: 0.12},
	}

	toe := math.Trunc(gpsTow/3600) * 3600 // Galileo: 1-hour reference interval

	ephs := make([]GnssEphemeris, 0, len(params))
	for i, p := range params {
		ephs = append(ephs, GnssEphemeris{
			Constellation: ConstellationGalileo,
			SvID:          p.svID,
			PRN:           p.prn,
			WeekNumber:    gpsWeek,

			SqrtA:        5440.6 + p.sqrtAOffset,
			Eccentricity: 0.0002 + float64(i)*0.0001,
			Inclination:  0.9774 + float64(i)*0.0004,

			RAAN:        p.raanOffset + 0.008*float64(i),
			ArgPerigee:  0.5 + float64(i)*0.18,
			MeanAnomaly: p.m0Offset,

			DeltaN:   4.8e-9,
			IDOT:     -1.0e-10,
			OmegaDot: -7.5e-9,

			Toe: toe,
			Toc: toe,

			Af0: float64(i)*5e-8 - 1.5e-7,
			Af1: 8.0e-13,
			Af2: 0,

			Crs: 15.0 + float64(i)*2.0,
			Crc: 160.0 + float64(i)*4.0,
			Cus: 4.8e-6,
			Cuc: -2.9e-6,
			Cis: 4.2e-8,
			Cic: -1.1e-7,

			Health: 0,
			IODE:   i + 10,
		})
	}
	return ephs
}
