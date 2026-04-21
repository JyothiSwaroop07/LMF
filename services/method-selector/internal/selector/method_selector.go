// Package selector implements 3GPP-compliant positioning method selection logic.
package selector

import (
	"github.com/5g-lmf/common/types"
)

// MethodSelector selects optimal positioning methods based on UE capabilities and QoS
type MethodSelector struct{}

// NewMethodSelector creates a new method selector
func NewMethodSelector() *MethodSelector { return &MethodSelector{} }

// SelectMethod returns the ordered list of positioning methods to attempt
func (s *MethodSelector) SelectMethod(req types.MethodSelectionRequest) (*types.MethodSelectionResult, error) {
	caps := req.UeCaps
	qos := req.LcsQoS
	indoor := req.IndoorHint

	var primary []types.PositioningMethod

	switch qos.ResponseTime {
	case types.ResponseTimeNoDelay:
		// ≤500ms: only instant methods
		if caps.EcidSupported {
			return &types.MethodSelectionResult{
				SelectedMethod:      types.PositioningMethodNREcid,
				FallbackMethods:     []types.PositioningMethod{types.PositioningMethodCellID},
				EstimatedAccuracy:   estimateAccuracy(types.PositioningMethodNREcid),
				EstimatedResponseMs: estimateResponseMs(types.PositioningMethodNREcid),
			}, nil
		}
		return &types.MethodSelectionResult{
			SelectedMethod:      types.PositioningMethodCellID,
			FallbackMethods:     nil,
			EstimatedAccuracy:   estimateAccuracy(types.PositioningMethodCellID),
			EstimatedResponseMs: estimateResponseMs(types.PositioningMethodCellID),
		}, nil

	case types.ResponseTimeLowDelay:
		// ≤1s: fast ranging methods
		if caps.MultiRttSupported {
			primary = append(primary, types.PositioningMethodMultiRTT)
		}
		if caps.EcidSupported {
			primary = append(primary, types.PositioningMethodNREcid)
		}
		if len(primary) == 0 {
			primary = []types.PositioningMethod{types.PositioningMethodCellID}
		}
	default:
		// DELAY_TOLERANT or DELAY_TOLERANT_V2: any method
		//changed the condition below to allow testing of method selection logic without needing to set QoS in the location request. If QoS is not set, it will default to high accuracy selection logic.
		if qos.HorizontalAccuracy == 0 || qos.HorizontalAccuracy <= 10 {
			// High accuracy
			if caps.GnssSupported && !indoor {
				if caps.DlTdoaSupported {
					primary = []types.PositioningMethod{types.PositioningMethodAGNSS, types.PositioningMethodDLTDOA}
				} else {
					primary = []types.PositioningMethod{types.PositioningMethodAGNSS}
				}
			} else if caps.WlanSupported {
				primary = []types.PositioningMethod{types.PositioningMethodWLAN, types.PositioningMethodDLTDOA}
			} else if caps.BluetoothSupported {
				primary = []types.PositioningMethod{types.PositioningMethodBluetooth}
			} else if caps.DlTdoaSupported {
				primary = []types.PositioningMethod{types.PositioningMethodDLTDOA}
			}
		} else if qos.HorizontalAccuracy <= 50 {
			// Medium accuracy
			if caps.DlTdoaSupported {
				primary = []types.PositioningMethod{types.PositioningMethodDLTDOA, types.PositioningMethodMultiRTT}
			} else if caps.MultiRttSupported {
				primary = []types.PositioningMethod{types.PositioningMethodMultiRTT}
			}
		}

		if len(primary) == 0 {
			// Default: E-CID
			if caps.EcidSupported {
				primary = []types.PositioningMethod{types.PositioningMethodNREcid}
			} else {
				primary = []types.PositioningMethod{types.PositioningMethodCellID}
			}
		}
	}

	// Build fallback list (all remaining capable methods in accuracy order)
	fallbacks := buildFallbacks(primary, caps)

	selected := primary[0]
	remaining := primary[1:]
	remaining = append(remaining, fallbacks...)

	return &types.MethodSelectionResult{
		SelectedMethod:      selected,
		FallbackMethods:     remaining,
		EstimatedAccuracy:   estimateAccuracy(selected),
		EstimatedResponseMs: estimateResponseMs(selected),
	}, nil
}

// buildFallbacks returns methods not in primary that the UE supports
func buildFallbacks(primary []types.PositioningMethod, caps types.UeCapabilities) []types.PositioningMethod {
	inPrimary := make(map[types.PositioningMethod]bool)
	for _, m := range primary {
		inPrimary[m] = true
	}

	// Priority-ordered candidate fallbacks
	candidates := []types.PositioningMethod{
		types.PositioningMethodAGNSS,
		types.PositioningMethodDLTDOA,
		types.PositioningMethodMultiRTT,
		types.PositioningMethodNREcid,
		types.PositioningMethodWLAN,
		types.PositioningMethodBluetooth,
		types.PositioningMethodCellID,
	}

	capMap := map[types.PositioningMethod]bool{
		types.PositioningMethodAGNSS:     caps.GnssSupported,
		types.PositioningMethodDLTDOA:    caps.DlTdoaSupported,
		types.PositioningMethodMultiRTT:  caps.MultiRttSupported,
		types.PositioningMethodNREcid:    caps.EcidSupported,
		types.PositioningMethodWLAN:      caps.WlanSupported,
		types.PositioningMethodBluetooth: caps.BluetoothSupported,
		types.PositioningMethodCellID:    true,
	}

	var fallbacks []types.PositioningMethod
	for _, c := range candidates {
		if !inPrimary[c] && capMap[c] {
			fallbacks = append(fallbacks, c)
		}
	}
	return fallbacks
}

// estimateAccuracy returns expected accuracy in meters for a positioning method
func estimateAccuracy(method types.PositioningMethod) float64 {
	switch method {
	case types.PositioningMethodAGNSS:
		return 5.0
	case types.PositioningMethodDLTDOA:
		return 30.0
	case types.PositioningMethodOTDOA:
		return 50.0
	case types.PositioningMethodMultiRTT:
		return 30.0
	case types.PositioningMethodNREcid:
		return 150.0
	case types.PositioningMethodWLAN:
		return 20.0
	case types.PositioningMethodBluetooth:
		return 5.0
	case types.PositioningMethodBarometric:
		return 3.0 // vertical only
	case types.PositioningMethodCellID:
		return 500.0
	default:
		return 1000.0
	}
}

// estimateResponseMs returns expected response time in milliseconds
func estimateResponseMs(method types.PositioningMethod) int {
	switch method {
	case types.PositioningMethodAGNSS:
		return 10000
	case types.PositioningMethodDLTDOA, types.PositioningMethodOTDOA:
		return 3000
	case types.PositioningMethodMultiRTT:
		return 2000
	case types.PositioningMethodNREcid:
		return 500
	case types.PositioningMethodWLAN:
		return 3000
	case types.PositioningMethodBluetooth:
		return 2000
	case types.PositioningMethodCellID:
		return 100
	default:
		return 5000
	}
}
