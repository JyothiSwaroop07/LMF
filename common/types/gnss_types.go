package types

import "time"

// GnssConstellation identifies a GNSS satellite system
type GnssConstellation string

const (
	GnssGPS     GnssConstellation = "GPS"
	GnssGLONASS GnssConstellation = "GLONASS"
	GnssGalileo GnssConstellation = "GALILEO"
	GnssBeiDou  GnssConstellation = "BEIDOU"
	GnssQZSS    GnssConstellation = "QZSS"
)

// GnssAssistanceDataType identifies types of GNSS assistance data
type GnssAssistanceDataType string

const (
	GnssAssistReferenceTime       GnssAssistanceDataType = "REFERENCE_TIME"
	GnssAssistReferenceLocation   GnssAssistanceDataType = "REFERENCE_LOCATION"
	GnssAssistIonosphericModel    GnssAssistanceDataType = "IONOSPHERIC_MODEL"
	GnssAssistEphemeris           GnssAssistanceDataType = "EPHEMERIS"
	GnssAssistAlmanac             GnssAssistanceDataType = "ALMANAC"
	GnssAssistDiffCorrections     GnssAssistanceDataType = "DIFF_CORRECTIONS"
	GnssAssistUTCModel            GnssAssistanceDataType = "UTC_MODEL"
	GnssAssistSBAS                GnssAssistanceDataType = "SBAS"
)

// GnssEphemeris holds Keplerian orbital elements for a GNSS satellite
type GnssEphemeris struct {
	Constellation GnssConstellation `json:"constellation"`
	SVID          int               `json:"svid"`
	Toc           int64             `json:"toc"`           // Clock epoch (GPS seconds)
	Toe           int64             `json:"toe"`           // Ephemeris epoch (GPS seconds)
	SqrtA         float64           `json:"sqrtA"`         // sqrt(semi-major axis) m^0.5
	Eccentricity  float64           `json:"eccentricity"`  // dimensionless
	Inclination   float64           `json:"inclination"`   // radians at Toe
	RAAN          float64           `json:"raan"`          // Right Ascension of Ascending Node (rad)
	ArgPerigee    float64           `json:"argPerigee"`    // Argument of perigee (rad)
	MeanAnomaly   float64           `json:"meanAnomaly"`   // Mean anomaly at Toe (rad)
	Af0           float64           `json:"af0"`           // Clock bias (s)
	Af1           float64           `json:"af1"`           // Clock drift (s/s)
	Af2           float64           `json:"af2"`           // Clock drift rate (s/s^2)
	Ura           float64           `json:"ura"`           // User range accuracy (m)
	Iode          int               `json:"iode"`          // Issue of Data Ephemeris
	ValidUntil    time.Time         `json:"validUntil"`
}

// KlobucharModel holds ionospheric correction parameters per GPS ICD
type KlobucharModel struct {
	Alpha [4]float64 `json:"alpha"` // alpha0..alpha3
	Beta  [4]float64 `json:"beta"`  // beta0..beta3
}

// GnssSignalMeasurement holds a single satellite signal measurement
type GnssSignalMeasurement struct {
	SVID          int               `json:"svid"`
	Constellation GnssConstellation `json:"constellation"`
	CNR           float64           `json:"cnr"`           // Carrier-to-Noise Ratio (dB-Hz)
	Pseudorange   float64           `json:"pseudorange"`   // meters
	Doppler       float64           `json:"doppler"`       // Hz
	CarrierPhase  float64           `json:"carrierPhase"`  // cycles
}

// GnssMeasurementResult holds all UE GNSS measurements at a given time
type GnssMeasurementResult struct {
	Signals         []GnssSignalMeasurement `json:"signals"`
	MeasurementTime time.Time               `json:"measurementTime"`
	Constellations  []GnssConstellation     `json:"constellations"`
}

// GnssAssistanceData bundles all types of assistance data
type GnssAssistanceData struct {
	ReferenceTime    time.Time             `json:"referenceTime"`
	RefLocation      *LocationEstimate     `json:"referenceLocation,omitempty"`
	Ephemerides      []GnssEphemeris       `json:"ephemerides"`
	IonosphericModel *KlobucharModel       `json:"ionosphericModel,omitempty"`
	ValidUntil       time.Time             `json:"validUntil"`
}
