// Package provider fetches GNSS assistance data from upstream sources.
//
// In production, ephemeris data is fetched from:
//   - GPS: IGS (International GNSS Service) RINEX navigation files
//   - Galileo: EDAS (Galileo Data Access Service) or E-SPAD
//   - BeiDou: CSNO broadcast ephemeris
//
// For now, this generates structurally valid synthetic ephemeris data.
package provider

import (
	"context"
	"math"
	"time"

	"github.com/5g-lmf/common/types"
	"go.uber.org/zap"
)

// GNSSProvider fetches and refreshes GNSS assistance data.
type GNSSProvider struct {
	logger *zap.Logger
}

// NewGNSSProvider creates a GNSSProvider.
func NewGNSSProvider(logger *zap.Logger) *GNSSProvider {
	return &GNSSProvider{logger: logger}
}

// FetchEphemeris generates synthetic GPS ephemeris for numSVs satellites.
// Replace with actual RINEX parsing or SUPL A-GPS server fetch in production.
func (p *GNSSProvider) FetchEphemeris(ctx context.Context, constellation types.GnssConstellation, numSVs int) ([]types.GnssEphemeris, error) {
	now := time.Now().UTC()
	// toc := float64(now.Unix())
	toc := now.Unix() //int64

	ephems := make([]types.GnssEphemeris, numSVs)
	for i := 0; i < numSVs; i++ {
		svID := i + 1
		// Distribute SVs uniformly in RAAN to get good geometry
		raan := float64(i) * (2.0 * math.Pi / float64(numSVs))

		ephems[i] = types.GnssEphemeris{
			SVID:          svID,
			Constellation: constellation,
			Toc:           toc,
			Toe:           toc,
			SqrtA:         5153.796, // GPS nominal semi-major axis sqrt ≈ 26560 km
			Eccentricity:  0.0001,   // Near-circular
			Inclination:   0.9599,   // 55° inclination in radians
			RAAN:          raan,
			ArgPerigee:    0.5,
			MeanAnomaly:   float64(i) * math.Pi / float64(numSVs),
			// DeltaN:        4.4e-9,
			// Idot:          1e-10,
			// OmegaDot:      -8.0e-9,
			// Crs:           5.0,
			// Crc:           5.0,
			// Cus:           2e-6,
			// Cuc:           -3e-6,
			// Cis:           1e-7,
			// Cic:           -1e-7,
			Af0: 1e-4, // Clock bias (seconds)
			Af1: 1e-12,
			Af2: 0,
			// IODC:     uint16(svID),
			// IODE:     i + 1,
			// TGD:      5e-9,
			// URA:      2.0,
			// SvHealth: 0, // Healthy
			ValidUntil: now.Add(2 * time.Hour),
		}
	}

	p.logger.Info("GNSS ephemeris fetched",
		zap.String("constellation", string(constellation)),
		zap.Int("numSVs", numSVs),
	)

	return ephems, nil
}

// FetchKlobuchar returns current Klobuchar ionospheric model coefficients.
// In production, parse from GPS L1 NAV message or BroadCastIono RINEX header.
func (p *GNSSProvider) FetchKlobuchar(ctx context.Context) (types.KlobucharModel, error) {
	// Real GPS Klobuchar broadcast parameters from a recent almanac
	return types.KlobucharModel{
		Alpha: [4]float64{1.118e-8, 1.490e-8, -5.960e-8, -1.192e-7},
		Beta:  [4]float64{9.011e4, 1.639e5, -6.554e4, -5.243e5},
	}, nil
}
