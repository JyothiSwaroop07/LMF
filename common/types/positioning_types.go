package types

import "time"

// PositionEstimate is the internal representation of a computed position
type PositionEstimate struct {
	Latitude   float64           `json:"latitude"`
	Longitude  float64           `json:"longitude"`
	Altitude   float64           `json:"altitude"`
	SigmaLat   float64           `json:"sigmaLat"`   // 1-sigma uncertainty in degrees latitude
	SigmaLon   float64           `json:"sigmaLon"`   // 1-sigma uncertainty in degrees longitude
	SigmaAlt   float64           `json:"sigmaAlt"`   // 1-sigma uncertainty in meters altitude
	Confidence int               `json:"confidence"` // 0-100%
	Method     PositioningMethod `json:"method"`
	Timestamp  time.Time         `json:"timestamp"`
}

// RstdMeasurement holds a single DL-TDOA RSTD measurement
type RstdMeasurement struct {
	RefCellNci      string  `json:"refCellNci"`
	NeighborCellNci string  `json:"neighborCellNci"`
	Rstd            float64 `json:"rstd"`            // Reference Signal Time Difference (Ts units)
	RstdUncertainty float64 `json:"rstdUncertainty"` // Ts units
}

// DlTdoaMeasurements holds all RSTD measurements for DL-TDOA
type DlTdoaMeasurements struct {
	RefGnb          string            `json:"refGnb"`
	Measurements    []RstdMeasurement `json:"measurements"`
	MeasurementTime time.Time         `json:"measurementTime"`
}

// RsrpEntry holds RSRP/RSRQ measurement for a cell
type RsrpEntry struct {
	CellNci string  `json:"cellNci"`
	Rsrp    float64 `json:"rsrp"` // dBm
	Rsrq    float64 `json:"rsrq"` // dB
}

// EcidMeasurements holds E-CID measurements from NRPPa
type EcidMeasurements struct {
	ServingCellNci   string      `json:"servingCellNci"`
	TimingAdvance    int         `json:"timingAdvance"` // NTA value
	UeRxTxDiff       float64     `json:"ueRxTxDiff"`    // UE Rx-Tx time difference (Ts)
	GnbRxTxDiff      float64     `json:"gnbRxTxDiff"`   // gNB Rx-Tx time difference (Ts)
	RsrpMeasurements []RsrpEntry `json:"rsrpMeasurements"`
}

// RttEntry holds a single Multi-RTT measurement
type RttEntry struct {
	CellNci string  `json:"cellNci"`
	UeRxTx  float64 `json:"ueRxTx"`  // UE Rx-Tx time difference (Ts units)
	GnbRxTx float64 `json:"gnbRxTx"` // gNB Rx-Tx time difference (Ts units)
}

// MultiRttMeasurements holds all RTT entries for Multi-RTT positioning
type MultiRttMeasurements struct {
	Entries         []RttEntry `json:"entries"`
	MeasurementTime time.Time  `json:"measurementTime"`
}

// UeCapabilities holds UE positioning capability information
type UeCapabilities struct {
	GnssSupported       bool                `json:"gnssSupported"`
	GnssConstellations  []GnssConstellation `json:"gnssConstellations"`
	DlTdoaSupported     bool                `json:"dlTdoaSupported"`
	MultiRttSupported   bool                `json:"multiRttSupported"`
	WlanSupported       bool                `json:"wlanSupported"`
	BluetoothSupported  bool                `json:"bluetoothSupported"`
	BarometricSupported bool                `json:"barometricSupported"`
	EcidSupported       bool                `json:"ecidSupported"`
}

// MethodSelectionRequest carries inputs for method selection
type MethodSelectionRequest struct {
	UeCaps     UeCapabilities `json:"ueCaps"`
	LcsQoS     LcsQoS         `json:"lcsQoS"`
	IndoorHint bool           `json:"indoorHint"`
}

// MethodSelectionResult carries the selected positioning methods
type MethodSelectionResult struct {
	SelectedMethod      PositioningMethod        `json:"selectedMethod"`
	FallbackMethods     []PositioningMethod      `json:"fallbackMethods"`
	AssistanceRequired  []GnssAssistanceDataType `json:"assistanceRequired,omitempty"`
	EstimatedAccuracy   float64                  `json:"estimatedAccuracyMeters"`
	EstimatedResponseMs int                      `json:"estimatedResponseMs"`
}
