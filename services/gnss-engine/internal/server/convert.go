package server

import (
	"fmt"
	"time"

	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/gnss-engine/internal/assistance"
	"github.com/5g-lmf/gnss-engine/internal/positioning"
)

// toProtoEphemerides converts internal ephemeris structs to proto messages.
func toProtoEphemerides(in []assistance.GnssEphemeris) []*pb.GnssEphemerisMsg {
	out := make([]*pb.GnssEphemerisMsg, len(in))
	for i, e := range in {
		out[i] = &pb.GnssEphemerisMsg{
			Constellation: constellationToString(e.Constellation),
			Svid:          int32(e.PRN),
			Toc:           int64(e.Toc),
			Toe:           int64(e.Toe),
			SqrtA:         e.SqrtA,
			Eccentricity:  e.Eccentricity,
			Inclination:   e.Inclination,
			Raan:          e.RAAN,
			ArgPerigee:    e.ArgPerigee,
			MeanAnomaly:   e.MeanAnomaly,
			Af0:           e.Af0,
			Af1:           e.Af1,
			Af2:           e.Af2,
			Ura:           0, // not carried on internal GnssEphemeris
		}
	}
	return out
}

// fromProtoEphemerides converts proto ephemeris messages to internal structs.
// SvID is reconstructed from constellation + PRN to match the solver's lookup
// format ("G01", "E03", etc.).
func fromProtoEphemerides(in []*pb.GnssEphemerisMsg) []assistance.GnssEphemeris {
	out := make([]assistance.GnssEphemeris, len(in))
	for i, e := range in {
		constellation := constellationFromString(e.Constellation)
		out[i] = assistance.GnssEphemeris{
			Constellation: constellation,
			SvID:          fmt.Sprintf("%s%02d", constellationPrefix(e.Constellation), e.Svid),
			PRN:           int(e.Svid),
			Toc:           float64(e.Toc),
			Toe:           float64(e.Toe),
			SqrtA:         e.SqrtA,
			Eccentricity:  e.Eccentricity,
			Inclination:   e.Inclination,
			RAAN:          e.Raan,
			ArgPerigee:    e.ArgPerigee,
			MeanAnomaly:   e.MeanAnomaly,
			Af0:           e.Af0,
			Af1:           e.Af1,
			Af2:           e.Af2,
		}
	}
	return out
}

// fromProtoSignals converts proto signal measurements to internal structs.
// SvID is reconstructed from constellation + SVID to match the solver's
// ephemeris lookup map keyed by SvID ("G01", "E03", etc.).
func fromProtoSignals(in []*pb.GnssSignalMeasurementMsg) []positioning.GnssSignalMeasurement {
	out := make([]positioning.GnssSignalMeasurement, len(in))
	for i, m := range in {
		out[i] = positioning.GnssSignalMeasurement{
			SvID:        fmt.Sprintf("%s%02d", constellationPrefix(m.Constellation), m.Svid),
			Pseudorange: m.Pseudorange,
			CN0:         m.Cnr,
		}
	}
	return out
}

// toProtoPosition converts an internal PositionEstimate to the pb.PositionEstimate.
// pb.PositionEstimate carries sigma (1-sigma std dev) rather than 95% accuracy,
// so HorizontalAccuracy (95%) is back-converted: sigma ≈ hAcc / 2.45.
// SigmaLat and SigmaLon are set equal (isotropic assumption); SigmaAlt from
// VerticalAccuracy / 1.96. Confidence is fixed at 95 per 3GPP convention.
func toProtoPosition(p *positioning.PositionEstimate) *pb.PositionEstimate {
	sigmaH := p.HorizontalAccuracy / 2.45
	sigmaV := p.VerticalAccuracy / 1.96
	return &pb.PositionEstimate{
		Latitude:        p.Latitude,
		Longitude:       p.Longitude,
		Altitude:        p.Altitude,
		SigmaLat:        sigmaH,
		SigmaLon:        sigmaH,
		SigmaAlt:        sigmaV,
		Confidence:      95,
		Method:          pb.PositioningMethod_POSITIONING_METHOD_A_GNSS,
		TimestampUnixMs: time.Now().UnixMilli(),
	}
}

// minValidUntil returns the smallest ValidUntilUnixMs across ephemerides,
// used to set the response-level validity window.
func minValidUntil(ephs []*pb.GnssEphemerisMsg) int64 {
	min := ephs[0].ValidUntilUnixMs
	for _, e := range ephs[1:] {
		if e.ValidUntilUnixMs < min {
			min = e.ValidUntilUnixMs
		}
	}
	return min
}

// constellationToString maps the internal GnssConstellation int to the string
// label used in proto messages.
func constellationToString(c assistance.GnssConstellation) string {
	switch c {
	case assistance.ConstellationGPS:
		return "GPS"
	case assistance.ConstellationGalileo:
		return "GALILEO"
	case assistance.ConstellationGLONASS:
		return "GLONASS"
	case assistance.ConstellationBeiDou:
		return "BEIDOU"
	default:
		return "GPS"
	}
}

// constellationFromString maps the proto constellation string to the internal
// GnssConstellation type.
func constellationFromString(c string) assistance.GnssConstellation {
	switch c {
	case "GPS":
		return assistance.ConstellationGPS
	case "GALILEO":
		return assistance.ConstellationGalileo
	case "GLONASS":
		return assistance.ConstellationGLONASS
	case "BEIDOU":
		return assistance.ConstellationBeiDou
	default:
		return assistance.ConstellationGPS
	}
}

// constellationPrefix maps the proto constellation string to the SvID prefix
// used internally ("G01", "E03", etc.).
func constellationPrefix(c string) string {
	switch c {
	case "GPS":
		return "G"
	case "GALILEO":
		return "E"
	case "GLONASS":
		return "R"
	case "BEIDOU":
		return "C"
	default:
		return "G"
	}
}
