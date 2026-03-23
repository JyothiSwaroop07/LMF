package geofence

import (
	"math"
	"testing"

	"github.com/5g-lmf/common/types"
	"github.com/stretchr/testify/assert"
)

func TestIsPointInPolygon_InsideSquare(t *testing.T) {
	g := NewGeofenceEvaluator()
	// Square: lat 37-38°N, lon -122 to -121°W
	polygon := []LatLon{
		{Lat: 37.0, Lon: -122.0},
		{Lat: 38.0, Lon: -122.0},
		{Lat: 38.0, Lon: -121.0},
		{Lat: 37.0, Lon: -121.0},
	}
	// Point in center
	assert.True(t, g.IsPointInPolygon(37.5, -121.5, polygon), "center point should be inside")
}

func TestIsPointInPolygon_OutsideSquare(t *testing.T) {
	g := NewGeofenceEvaluator()
	polygon := []LatLon{
		{Lat: 37.0, Lon: -122.0},
		{Lat: 38.0, Lon: -122.0},
		{Lat: 38.0, Lon: -121.0},
		{Lat: 37.0, Lon: -121.0},
	}
	assert.False(t, g.IsPointInPolygon(36.0, -121.5, polygon), "south of square should be outside")
	assert.False(t, g.IsPointInPolygon(37.5, -123.0, polygon), "west of square should be outside")
}

func TestIsPointInPolygon_Triangle(t *testing.T) {
	g := NewGeofenceEvaluator()
	// Right triangle: (0,0)-(0,2)-(2,0)
	polygon := []LatLon{
		{Lat: 0.0, Lon: 0.0},
		{Lat: 0.0, Lon: 2.0},
		{Lat: 2.0, Lon: 0.0},
	}
	// Point clearly inside
	assert.True(t, g.IsPointInPolygon(0.5, 0.5, polygon))
	// Point clearly outside (far corner)
	assert.False(t, g.IsPointInPolygon(1.5, 1.5, polygon))
}

func TestHaversineDistance_LondonParis(t *testing.T) {
	// London: 51.5074°N, 0.1278°W
	// Paris: 48.8566°N, 2.3522°E
	// Expected distance: ~334 km
	distM := HaversineDistanceM(51.5074, -0.1278, 48.8566, 2.3522)
	distKm := distM / 1000

	assert.InDelta(t, 334.0, distKm, 10.0, "London-Paris distance should be ~334 km")
}

func TestHaversineDistance_SamePoint(t *testing.T) {
	dist := HaversineDistanceM(37.7749, -122.4194, 37.7749, -122.4194)
	assert.InDelta(t, 0.0, dist, 0.001, "distance to same point should be 0")
}

func TestHaversineDistance_Equator(t *testing.T) {
	// One degree of longitude at equator ≈ 111.32 km
	dist := HaversineDistanceM(0, 0, 0, 1)
	expected := math.Pi * 6371000 / 180
	assert.InDelta(t, expected, dist, 1000.0, "1° lon at equator should be ~111km")
}

func TestEvaluateMotionEvent_AboveThreshold(t *testing.T) {
	g := NewGeofenceEvaluator()
	// ~1km apart
	last := LatLon{Lat: 37.7749, Lon: -122.4194}
	current := LatLon{Lat: 37.7839, Lon: -122.4194} // ~1km north
	assert.True(t, g.EvaluateMotionEvent(last, current, 500.0), "should trigger motion event >500m")
}

func TestEvaluateMotionEvent_BelowThreshold(t *testing.T) {
	g := NewGeofenceEvaluator()
	// ~10m apart
	last := LatLon{Lat: 37.7749, Lon: -122.4194}
	current := LatLon{Lat: 37.77491, Lon: -122.4194} // ~1m north
	assert.False(t, g.EvaluateMotionEvent(last, current, 500.0), "should not trigger motion event <500m")
}

func TestEvaluateAreaEvent_Enter(t *testing.T) {
	g := NewGeofenceEvaluator()
	polygon := []LatLon{
		{Lat: 37.0, Lon: -122.0},
		{Lat: 38.0, Lon: -122.0},
		{Lat: 38.0, Lon: -121.0},
		{Lat: 37.0, Lon: -121.0},
	}
	area := types.LocationArea{Points: polygon}
	outside := LatLon{Lat: 36.0, Lon: -121.5}
	inside := LatLon{Lat: 37.5, Lon: -121.5}

	// Was outside, now inside → ENTER should trigger
	assert.True(t, g.EvaluateAreaEvent(types.AreaTypeEnter, inside, &outside, area))
	// Was inside, now inside → ENTER should NOT trigger
	assert.False(t, g.EvaluateAreaEvent(types.AreaTypeEnter, inside, &inside, area))
}

func TestEvaluateAreaEvent_Leave(t *testing.T) {
	g := NewGeofenceEvaluator()
	polygon := []LatLon{
		{Lat: 37.0, Lon: -122.0},
		{Lat: 38.0, Lon: -122.0},
		{Lat: 38.0, Lon: -121.0},
		{Lat: 37.0, Lon: -121.0},
	}
	area := types.LocationArea{Points: polygon}
	outside := LatLon{Lat: 36.0, Lon: -121.5}
	inside := LatLon{Lat: 37.5, Lon: -121.5}

	// Was inside, now outside → LEAVE should trigger
	assert.True(t, g.EvaluateAreaEvent(types.AreaTypeLeave, outside, &inside, area))
	// Was outside, still outside → LEAVE should NOT trigger
	assert.False(t, g.EvaluateAreaEvent(types.AreaTypeLeave, outside, &outside, area))
}
