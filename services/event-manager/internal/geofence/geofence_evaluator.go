// Package geofence implements geographic trigger evaluation for LCS events.
package geofence

import (
	"math"

	"github.com/5g-lmf/common/types"
)

const earthRadiusM = 6371000.0 // mean Earth radius in meters

// LatLon represents a geographic coordinate
type LatLon = types.LatLon

// GeofenceEvaluator evaluates area and motion triggers
type GeofenceEvaluator struct{}

// NewGeofenceEvaluator creates a new evaluator
func NewGeofenceEvaluator() *GeofenceEvaluator { return &GeofenceEvaluator{} }

// IsPointInPolygon uses the ray casting algorithm to determine if a point
// is inside a polygon defined by a list of vertices.
// Returns true if the point is inside the polygon.
func (g *GeofenceEvaluator) IsPointInPolygon(lat, lon float64, polygon []LatLon) bool {
	n := len(polygon)
	if n < 3 {
		return false
	}

	inside := false
	j := n - 1

	for i := 0; i < n; i++ {
		// Edge from polygon[j] to polygon[i]
		latI := polygon[i].Lat
		lonI := polygon[i].Lon
		latJ := polygon[j].Lat
		lonJ := polygon[j].Lon

		// Check if ray from (lat, lon) heading east crosses this edge
		if ((latI > lat) != (latJ > lat)) &&
			(lon < (lonJ-lonI)*(lat-latI)/(latJ-latI)+lonI) {
			inside = !inside
		}
		j = i
	}

	return inside
}

// HaversineDistanceM computes the great-circle distance between two points in meters
func HaversineDistanceM(lat1, lon1, lat2, lon2 float64) float64 {
	lat1r := lat1 * math.Pi / 180
	lat2r := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1r)*math.Cos(lat2r)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

// EvaluateAreaEvent checks if an area event should trigger based on current and last position
// For ENTER: returns true if now inside and was outside
// For LEAVE: returns true if now outside and was inside
// For WITHIN: returns true if inside (stateless)
func (g *GeofenceEvaluator) EvaluateAreaEvent(
	areaType types.AreaType,
	current LatLon,
	lastKnown *LatLon, // nil for first evaluation
	area types.LocationArea,
) bool {
	var polygon []LatLon
	if len(area.Points) > 0 {
		polygon = area.Points
	} else {
		return false
	}

	nowInside := g.IsPointInPolygon(current.Lat, current.Lon, polygon)

	switch areaType {
	case types.AreaTypeWithin:
		return nowInside

	case types.AreaTypeEnter:
		if lastKnown == nil {
			return nowInside // First check: trigger if inside
		}
		wasInside := g.IsPointInPolygon(lastKnown.Lat, lastKnown.Lon, polygon)
		return nowInside && !wasInside

	case types.AreaTypeLeave:
		if lastKnown == nil {
			return !nowInside // First check: trigger if outside
		}
		wasInside := g.IsPointInPolygon(lastKnown.Lat, lastKnown.Lon, polygon)
		return !nowInside && wasInside
	}

	return false
}

// EvaluateMotionEvent checks if UE has moved more than threshold meters
func (g *GeofenceEvaluator) EvaluateMotionEvent(lastPos, currentPos LatLon, thresholdMeters float64) bool {
	dist := HaversineDistanceM(lastPos.Lat, lastPos.Lon, currentPos.Lat, currentPos.Lon)
	return dist >= thresholdMeters
}
