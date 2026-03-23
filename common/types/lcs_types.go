package types

import "time"

// GADShape represents Geographic Area Description shape types per TS 23.032
type GADShape string

const (
	GADShapePoint                        GADShape = "POINT"
	GADShapePointUncertaintyCircle       GADShape = "POINT_UNCERTAINTY_CIRCLE"
	GADShapePointUncertaintyEllipse      GADShape = "POINT_UNCERTAINTY_ELLIPSE"
	GADShapePolygon                      GADShape = "POLYGON"
	GADShapePointAltitude                GADShape = "POINT_ALTITUDE"
	GADShapePointAltitudeUncertainty     GADShape = "POINT_ALTITUDE_UNCERTAINTY"
	GADShapeEllipsoidArc                 GADShape = "ELLIPSOID_ARC"
)

// ResponseTimeClass per TS 23.273
type ResponseTimeClass string

const (
	ResponseTimeNoDelay         ResponseTimeClass = "NO_DELAY"
	ResponseTimeLowDelay        ResponseTimeClass = "LOW_DELAY"
	ResponseTimeDelayTolerant   ResponseTimeClass = "DELAY_TOLERANT"
	ResponseTimeDelayTolerantV2 ResponseTimeClass = "DELAY_TOLERANT_V2"
)

// PositioningMethod per TS 23.273
type PositioningMethod string

const (
	PositioningMethodAGNSS      PositioningMethod = "A_GNSS"
	PositioningMethodDLTDOA     PositioningMethod = "DL_TDOA"
	PositioningMethodOTDOA      PositioningMethod = "OTDOA"
	PositioningMethodNREcid     PositioningMethod = "NR_ECID"
	PositioningMethodMultiRTT   PositioningMethod = "NR_MULTI_RTT"
	PositioningMethodWLAN       PositioningMethod = "WLAN"
	PositioningMethodBluetooth  PositioningMethod = "BLUETOOTH"
	PositioningMethodBarometric PositioningMethod = "BAROMETRIC"
	PositioningMethodCellID     PositioningMethod = "CELL_ID"
	PositioningMethodAuto       PositioningMethod = "AUTO"
)

// AccuracyFulfilmentIndicator per TS 29.572
type AccuracyFulfilmentIndicator string

const (
	AccuracyFulfilled       AccuracyFulfilmentIndicator = "REQUESTED_ACCURACY_FULFILLED"
	AccuracyAttemptedFailed AccuracyFulfilmentIndicator = "ATTEMPTED_FAILED"
	AccuracyNotAttempted    AccuracyFulfilmentIndicator = "NOT_ATTEMPTED"
)

// SessionStatus for LCS session lifecycle
type SessionStatus string

const (
	SessionStatusInit                SessionStatus = "INIT"
	SessionStatusProcessing          SessionStatus = "PROCESSING"
	SessionStatusAwaitingMeasurements SessionStatus = "AWAITING_MEASUREMENTS"
	SessionStatusComputing           SessionStatus = "COMPUTING"
	SessionStatusCompleted           SessionStatus = "COMPLETED"
	SessionStatusFailed              SessionStatus = "FAILED"
	SessionStatusCancelled           SessionStatus = "CANCELLED"
)

// LcsClientType per TS 23.273
type LcsClientType string

const (
	LcsClientEmergencyServices LcsClientType = "EMERGENCY_SERVICES"
	LcsClientValueAdded        LcsClientType = "VALUE_ADDED_SERVICES"
	LcsClientPLMNOperator      LcsClientType = "PLMN_OPERATOR_SERVICES"
	LcsClientLawfulIntercept   LcsClientType = "LAWFUL_INTERCEPT"
	LcsClientGmlc              LcsClientType = "GMLC_TYPE"
)

// PosUsage indicates how positioning method was used
type PosUsage string

const (
	PosUsageUsed   PosUsage = "POSITION_USED"
	PosUsageFailed PosUsage = "POSITION_FAILED"
)

// EventType for location event subscriptions per TS 29.572
type EventType string

const (
	EventTypePeriodic             EventType = "PERIODIC"
	EventTypeAreaEvent            EventType = "AREA_EVENT"
	EventTypeMotionEvent          EventType = "MOTION_EVENT"
	EventTypeMaxInterval          EventType = "MAX_INTERVAL"
	EventTypeLocationCancellation EventType = "LOCATION_CANCELLATION"
)

// AreaType for geofence events
type AreaType string

const (
	AreaTypeEnter  AreaType = "ENTER"
	AreaTypeLeave  AreaType = "LEAVE"
	AreaTypeWithin AreaType = "WITHIN"
)

// PlmnId represents a Public Land Mobile Network ID
type PlmnId struct {
	Mcc string `json:"mcc"`
	Mnc string `json:"mnc"`
}

// Ecgi is an E-UTRAN Cell Global Identifier
type Ecgi struct {
	PlmnId     PlmnId `json:"plmnId"`
	EutraCellId string `json:"eutraCellId"`
}

// Ncgi is an NR Cell Global Identifier
type Ncgi struct {
	PlmnId  PlmnId `json:"plmnId"`
	NrCellId string `json:"nrCellId"`
}

// LocationEstimate represents a UE position per TS 23.032 GAD
type LocationEstimate struct {
	Shape                  GADShape  `json:"shape"`
	Latitude               float64   `json:"latitude"`
	Longitude              float64   `json:"longitude"`
	Altitude               float64   `json:"altitude,omitempty"`
	HorizontalUncertainty  float64   `json:"horizontalUncertainty,omitempty"`
	UncertaintySemiMajor   float64   `json:"uncertaintySemiMajor,omitempty"`
	UncertaintySemiMinor   float64   `json:"uncertaintySemiMinor,omitempty"`
	OrientationMajorAxis   int       `json:"orientationMajorAxis,omitempty"`
	VerticalUncertainty    float64   `json:"verticalUncertainty,omitempty"`
	Confidence             int       `json:"confidence"`
	Timestamp              time.Time `json:"timestamp"`
}

// LcsQoS defines location QoS requirements per TS 23.273
type LcsQoS struct {
	HorizontalAccuracy int               `json:"accuracy"`
	VerticalAccuracy   int               `json:"verticalAccuracy,omitempty"`
	VerticalCoordReq   bool              `json:"verticalCoordinateReq,omitempty"`
	ResponseTime       ResponseTimeClass `json:"responseTime"`
	VelocityRequested  bool              `json:"velocityReq,omitempty"`
	Confidence         int               `json:"confidenceLevel,omitempty"`
}

// VelocityEstimate carries UE velocity information
type VelocityEstimate struct {
	VelocityType  string  `json:"velocityType"`
	HSpeed        float64 `json:"hSpeed"`
	Bearing       float64 `json:"bearing"`
	VSpeed        float64 `json:"vSpeed,omitempty"`
	VDirection    string  `json:"vDirection,omitempty"`
	HUncertainty  float64 `json:"hUncertainty,omitempty"`
	VUncertainty  float64 `json:"vUncertainty,omitempty"`
}

// PositioningDataEntry records which methods were used/attempted
type PositioningDataEntry struct {
	PosMethod PositioningMethod `json:"posMethod"`
	PosUsage  PosUsage          `json:"posUsage"`
}

// LocationRequest is the full location determination request
type LocationRequest struct {
	Supi              string            `json:"supi,omitempty"`
	Pei               string            `json:"pei,omitempty"`
	Gpsi              string            `json:"gpsi,omitempty"`
	LcsQoS            LcsQoS            `json:"lcsQoS"`
	PositioningMethod PositioningMethod `json:"positioningMethod,omitempty"`
	AdditionalMethods []PositioningMethod `json:"additionalMethods,omitempty"`
	LcsClientType     LcsClientType     `json:"lcsClientType"`
	LcsLocation       string            `json:"lcsLocation,omitempty"`
	SupportedGADShapes []GADShape       `json:"supportedGADShapes,omitempty"`
	AmfId             string            `json:"amfId,omitempty"`
	HgmlcCallBackUri  string            `json:"hgmlcCallBackUri,omitempty"`
	LdrType           string            `json:"ldrType,omitempty"`
}

// LocationResponse carries the result of a location determination
type LocationResponse struct {
	LocationEstimate           LocationEstimate            `json:"locationEstimate"`
	AccuracyFulfilmentIndicator AccuracyFulfilmentIndicator `json:"accuracyFulfilmentIndicator"`
	AgeOfLocationEstimate      int                         `json:"ageOfLocationEstimate,omitempty"`
	VelocityEstimate           *VelocityEstimate           `json:"velocityEstimate,omitempty"`
	PositioningDataList        []PositioningDataEntry      `json:"positioningDataList,omitempty"`
	Ecgi                       *Ecgi                       `json:"ecgi,omitempty"`
	Ncgi                       *Ncgi                       `json:"ncgi,omitempty"`
	Timestamp                  time.Time                   `json:"timestamp"`
}

// LcsSession represents an active LCS session
type LcsSession struct {
	SessionID         string            `json:"sessionId"`
	Supi              string            `json:"supi"`
	Pei               string            `json:"pei,omitempty"`
	Gpsi              string            `json:"gpsi,omitempty"`
	AmfInstanceID     string            `json:"amfInstanceId"`
	GmlcInstanceID    string            `json:"gmlcInstanceId,omitempty"`
	LcsQoS            LcsQoS            `json:"lcsQoS"`
	LcsClientType     LcsClientType     `json:"lcsClientType"`
	PositioningMethod PositioningMethod `json:"positioningMethod"`
	Status            SessionStatus     `json:"status"`
	StartTime         time.Time         `json:"startTime"`
	ExpiryTime        time.Time         `json:"expiryTime"`
	RetryCount        int               `json:"retryCount"`
}

// AreaEventInfo for geofence subscriptions
type AreaEventInfo struct {
	AreaType      AreaType       `json:"areaType"`
	LocationAreas []LocationArea `json:"locationArea5G"`
}

// LocationArea defines a geographic area
type LocationArea struct {
	Shape  GADShape    `json:"shape"`
	Points []LatLon    `json:"points,omitempty"`
	Center *LatLon     `json:"point,omitempty"`
	Radius float64     `json:"radius,omitempty"`
}

// LatLon is a simple latitude/longitude pair
type LatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// EventSubscription represents a location event subscription
type EventSubscription struct {
	SubscriptionID    string        `json:"subscriptionId"`
	Supi              string        `json:"supi"`
	EventType         EventType     `json:"eventType"`
	NotifUri          string        `json:"notifUri"`
	NotifId           string        `json:"notifId"`
	AreaEventInfo     *AreaEventInfo `json:"areaEventInfo,omitempty"`
	LcsQoS            LcsQoS        `json:"lcsQoS,omitempty"`
	PeriodicInterval  int           `json:"samplingInterval,omitempty"`
	MaxReports        int           `json:"maxReports,omitempty"`
	ReportCount       int           `json:"reportCount"`
	MonitoringExpiry  time.Time     `json:"monitoringDuration,omitempty"`
	Active            bool          `json:"active"`
	LastReportTime    time.Time     `json:"lastReportTime,omitempty"`
	MotionThresholdM  float64       `json:"motionThresholdMeters,omitempty"`
}

// CellGeometry stores cell physical location and configuration
type CellGeometry struct {
	Nci                  string  `json:"nci"`
	PlmnMcc              string  `json:"plmnMcc"`
	PlmnMnc              string  `json:"plmnMnc"`
	Latitude             float64 `json:"latitude"`
	Longitude            float64 `json:"longitude"`
	Altitude             float64 `json:"altitude"`
	AntennaSectorAzimuth int     `json:"antennaSectorAzimuth"`
	AntennaSectorWidth   int     `json:"antennaSectorWidth"`
	SyncOffsetNs         int64   `json:"syncOffsetNs"`
}

// PrsConfiguration holds PRS (Positioning Reference Signal) config per TS 38.305
type PrsConfiguration struct {
	PrsId               int `json:"prsId"`
	NrofPrsResources    int `json:"nrofPrsResources"`
	PrsBandwidthMHz     int `json:"prsBandwidthMHz"`
	PrsSubcarrierSpacing int `json:"prsSubcarrierSpacing"`
	PrsCombSize         int `json:"prsCombSize"`
	DlPrsNumSymbols     int `json:"dlPrsNumSymbols"`
	PrsOccasionsDuration int `json:"prsOccasionsDuration"`
}
