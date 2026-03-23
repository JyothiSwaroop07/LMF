package selector

import (
	"testing"

	"github.com/5g-lmf/common/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fullCaps = types.UeCapabilities{
	GnssSupported:      true,
	GnssConstellations: []types.GnssConstellation{types.GnssGPS, types.GnssGalileo},
	DlTdoaSupported:    true,
	MultiRttSupported:  true,
	EcidSupported:      true,
	WlanSupported:      true,
	BluetoothSupported: false,
}

func TestSelectMethod_NoDelay_EcidOnly(t *testing.T) {
	s := NewMethodSelector()
	req := types.MethodSelectionRequest{
		UeCaps: fullCaps,
		LcsQoS: types.LcsQoS{
			ResponseTime:       types.ResponseTimeNoDelay,
			HorizontalAccuracy: 100,
		},
	}
	res, err := s.SelectMethod(req)
	require.NoError(t, err)
	assert.Equal(t, types.PositioningMethodNREcid, res.SelectedMethod)
	assert.Contains(t, res.FallbackMethods, types.PositioningMethodCellID)
	assert.LessOrEqual(t, res.EstimatedResponseMs, 1000)
}

func TestSelectMethod_LowDelay_MultiRtt(t *testing.T) {
	s := NewMethodSelector()
	req := types.MethodSelectionRequest{
		UeCaps: fullCaps,
		LcsQoS: types.LcsQoS{
			ResponseTime:       types.ResponseTimeLowDelay,
			HorizontalAccuracy: 50,
		},
	}
	res, err := s.SelectMethod(req)
	require.NoError(t, err)
	assert.Equal(t, types.PositioningMethodMultiRTT, res.SelectedMethod)
}

func TestSelectMethod_HighAccuracy_GnssAvailable(t *testing.T) {
	s := NewMethodSelector()
	req := types.MethodSelectionRequest{
		UeCaps: fullCaps,
		LcsQoS: types.LcsQoS{
			ResponseTime:       types.ResponseTimeDelayTolerant,
			HorizontalAccuracy: 5,
		},
		IndoorHint: false,
	}
	res, err := s.SelectMethod(req)
	require.NoError(t, err)
	assert.Equal(t, types.PositioningMethodAGNSS, res.SelectedMethod,
		"GNSS should be selected for high accuracy outdoor scenario")
}

func TestSelectMethod_IndoorHint_NoGnss(t *testing.T) {
	s := NewMethodSelector()
	req := types.MethodSelectionRequest{
		UeCaps: fullCaps,
		LcsQoS: types.LcsQoS{
			ResponseTime:       types.ResponseTimeDelayTolerant,
			HorizontalAccuracy: 5,
		},
		IndoorHint: true,
	}
	res, err := s.SelectMethod(req)
	require.NoError(t, err)
	// Indoors: should NOT select GNSS; WLAN or DL-TDOA expected
	assert.NotEqual(t, types.PositioningMethodAGNSS, res.SelectedMethod,
		"GNSS should not be selected for indoor scenario")
}

func TestSelectMethod_MediumAccuracy_TDOA(t *testing.T) {
	s := NewMethodSelector()
	req := types.MethodSelectionRequest{
		UeCaps: fullCaps,
		LcsQoS: types.LcsQoS{
			ResponseTime:       types.ResponseTimeDelayTolerant,
			HorizontalAccuracy: 30,
		},
	}
	res, err := s.SelectMethod(req)
	require.NoError(t, err)
	assert.Equal(t, types.PositioningMethodDLTDOA, res.SelectedMethod)
}

func TestSelectMethod_NoCapabilities_CellID(t *testing.T) {
	s := NewMethodSelector()
	req := types.MethodSelectionRequest{
		UeCaps: types.UeCapabilities{
			EcidSupported: false,
		},
		LcsQoS: types.LcsQoS{
			ResponseTime:       types.ResponseTimeNoDelay,
			HorizontalAccuracy: 100,
		},
	}
	res, err := s.SelectMethod(req)
	require.NoError(t, err)
	assert.Equal(t, types.PositioningMethodCellID, res.SelectedMethod)
}

func TestEstimateAccuracy(t *testing.T) {
	assert.Less(t, estimateAccuracy(types.PositioningMethodAGNSS),
		estimateAccuracy(types.PositioningMethodNREcid),
		"GNSS should be more accurate than E-CID")
	assert.Less(t, estimateAccuracy(types.PositioningMethodDLTDOA),
		estimateAccuracy(types.PositioningMethodCellID),
		"TDOA should be more accurate than Cell-ID")
}
