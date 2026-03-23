package handler

import "time"

// LocationContextData is the Nllmf DetermineLocation request body per TS 29.572
type LocationContextData struct {
	Supi               string            `json:"supi,omitempty"`
	Pei                string            `json:"pei,omitempty"`
	Gpsi               string            `json:"gpsi,omitempty"`
	LcsQoS             LcsQoSJson        `json:"lcsQoS"`
	PositioningMethod  string            `json:"positioningMethod,omitempty"`
	AdditionalMethods  []string          `json:"additionalMethods,omitempty"`
	LcsClientType      string            `json:"lcsClientType"`
	LcsLocation        string            `json:"lcsLocation,omitempty"`
	SupportedGADShapes []string          `json:"supportedGADShapes,omitempty"`
	AmfId              string            `json:"amfId,omitempty"`
	HgmlcCallBackUri   string            `json:"hgmlcCallBackUri,omitempty"`
	LdrType            string            `json:"ldrType,omitempty"`
	PeriodicEventInfo  *PeriodicEventInfo `json:"periodicEventInfo,omitempty"`
	AreaEventInfo      *AreaEventInfoJson `json:"areaEventInfo,omitempty"`
}

// LcsQoSJson is the JSON representation of LCS QoS
type LcsQoSJson struct {
	Accuracy             int    `json:"accuracy"`
	VerticalAccuracy     int    `json:"verticalAccuracy,omitempty"`
	VerticalCoordinateReq bool  `json:"verticalCoordinateReq,omitempty"`
	ResponseTime         string `json:"responseTime"`
	VelocityReq          bool   `json:"velocityReq,omitempty"`
	ConfidenceLevel      int    `json:"confidenceLevel,omitempty"`
}

// PeriodicEventInfo for periodic location reporting
type PeriodicEventInfo struct {
	ReportingAmount   int `json:"reportingAmount"`
	ReportingInterval int `json:"reportingInterval"`
}

// AreaEventInfoJson for area event subscriptions
type AreaEventInfoJson struct {
	AreaType     string      `json:"areaType"`
	LocationArea interface{} `json:"locationArea5G,omitempty"`
}

// LocationContextDataResp is the DetermineLocation response per TS 29.572
type LocationContextDataResp struct {
	LocationEstimate            LocationEstimateJson   `json:"locationEstimate"`
	AccuracyFulfilmentIndicator string                 `json:"accuracyFulfilmentIndicator"`
	AgeOfLocationEstimate       int                    `json:"ageOfLocationEstimate,omitempty"`
	VelocityEstimate            *VelocityEstimateJson  `json:"velocityEstimate,omitempty"`
	PositioningDataList         []PositioningDataEntry `json:"positioningDataList,omitempty"`
	GnssPositioningMethodUsage  []GnssMethodUsage      `json:"gnssPositioningMethodAndUsage,omitempty"`
	Ecgi                        *EcgiJson              `json:"ecgi,omitempty"`
	Ncgi                        *NcgiJson              `json:"ncgi,omitempty"`
	Timestamp                   time.Time              `json:"timestamp"`
}

// LocationEstimateJson is the JSON representation of a location estimate
type LocationEstimateJson struct {
	Shape                string  `json:"shape"`
	Point                *LatLon `json:"point,omitempty"`
	Altitude             float64 `json:"altitude,omitempty"`
	Uncertainty          float64 `json:"uncertainty,omitempty"`
	UncertaintySemiMajor float64 `json:"uncertaintySemiMajor,omitempty"`
	UncertaintySemiMinor float64 `json:"uncertaintySemiMinor,omitempty"`
	OrientationMajorAxis int     `json:"orientationMajorAxis,omitempty"`
	UncertaintyAltitude  float64 `json:"uncertaintyAltitude,omitempty"`
	Confidence           int     `json:"confidence"`
}

// LatLon is a JSON lat/lon pair
type LatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// VelocityEstimateJson is the JSON velocity estimate
type VelocityEstimateJson struct {
	VelocityType string  `json:"velocityType"`
	HSpeed       float64 `json:"hSpeed"`
	Bearing      float64 `json:"bearing"`
	VSpeed       float64 `json:"vSpeed,omitempty"`
	VDirection   string  `json:"vDirection,omitempty"`
	HUncertainty float64 `json:"hUncertainty,omitempty"`
	VUncertainty float64 `json:"vUncertainty,omitempty"`
}

// PositioningDataEntry records method usage
type PositioningDataEntry struct {
	PosMethod string `json:"posMethod"`
	PosUsage  string `json:"posUsage"`
}

// GnssMethodUsage records GNSS usage
type GnssMethodUsage struct {
	Mode  string `json:"mode"`
	Gnss  string `json:"gnss"`
	Usage string `json:"usage"`
}

// EcgiJson is a JSON E-UTRAN cell identifier
type EcgiJson struct {
	PlmnId      PlmnIdJson `json:"plmnId"`
	EutraCellId string     `json:"eutraCellId"`
}

// NcgiJson is a JSON NR cell identifier
type NcgiJson struct {
	PlmnId   PlmnIdJson `json:"plmnId"`
	NrCellId string     `json:"nrCellId"`
}

// PlmnIdJson is a JSON PLMN identifier
type PlmnIdJson struct {
	Mcc string `json:"mcc"`
	Mnc string `json:"mnc"`
}

// SubscriptionRequest is the event subscription request body
type SubscriptionRequest struct {
	Supi              string             `json:"supi"`
	EventType         string             `json:"eventType"`
	NotifUri          string             `json:"notifUri"`
	NotifId           string             `json:"notifId"`
	AreaEventInfo     *AreaEventInfoJson `json:"areaEventInfo,omitempty"`
	LcsQoS            *LcsQoSJson        `json:"lcsQoS,omitempty"`
	SamplingInterval  int                `json:"samplingInterval,omitempty"`
	MaxReports        int                `json:"maxReports,omitempty"`
	MonitoringDuration string            `json:"monitoringDuration,omitempty"`
	MotionThreshold   float64            `json:"motionThresholdMeters,omitempty"`
}

// SubscriptionResponse is the subscription creation response
type SubscriptionResponse struct {
	SubscriptionId string `json:"subscriptionId"`
}

// ProblemDetails per TS 29.571
type ProblemDetails struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}
