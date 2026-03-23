// Package fusion implements an Extended Kalman Filter for UE position tracking.
package fusion

import (
	"math"
	"sync"
	"time"

	"github.com/5g-lmf/common/types"
)

// KalmanState is the 6D state vector [lat, lon, alt, vLat, vLon, vAlt]
// Positions are in degrees; velocities are in degrees/second.
type KalmanState struct {
	X [6]float64    // State vector
	P [6][6]float64 // State covariance
}

// KalmanFilter implements a 6-state constant velocity EKF for UE tracking
type KalmanFilter struct {
	state      KalmanState
	lastUpdate time.Time
	sigmaAccel float64 // Process noise: acceleration std dev (m/s²)
}

// NewKalmanFilter initialises a Kalman filter from an initial position estimate
func NewKalmanFilter(initial types.PositionEstimate) *KalmanFilter {
	kf := &KalmanFilter{
		sigmaAccel: 0.1, // 0.1 m/s² assumed pedestrian acceleration
		lastUpdate: initial.Timestamp,
	}

	// Initial state: position from estimate, zero velocity
	kf.state.X = [6]float64{initial.Latitude, initial.Longitude, initial.Altitude, 0, 0, 0}

	// Initial covariance: position from estimate, velocity uncertain
	sigLat := initial.SigmaLat
	if sigLat < 1e-10 {
		sigLat = 5.0 / 111319.0 // 5m default
	}
	sigLon := initial.SigmaLon
	if sigLon < 1e-10 {
		sigLon = 5.0 / 111319.0
	}
	sigAlt := initial.SigmaAlt
	if sigAlt < 0.1 {
		sigAlt = 20.0
	}
	// Velocity uncertainty: 10 m/s = ~9e-5 deg/s
	sigVel := 10.0 / 111319.0

	kf.state.P[0][0] = sigLat * sigLat
	kf.state.P[1][1] = sigLon * sigLon
	kf.state.P[2][2] = sigAlt * sigAlt
	kf.state.P[3][3] = sigVel * sigVel
	kf.state.P[4][4] = sigVel * sigVel
	kf.state.P[5][5] = (sigVel * 2) * (sigVel * 2)

	return kf
}

// Predict propagates the state forward in time using a constant velocity model
func (kf *KalmanFilter) Predict(dt float64) {
	if dt <= 0 {
		return
	}

	// State transition: F = [I3 dt*I3; 0 I3]
	// New position = old position + velocity * dt
	kf.state.X[0] += kf.state.X[3] * dt
	kf.state.X[1] += kf.state.X[4] * dt
	kf.state.X[2] += kf.state.X[5] * dt

	// Update covariance: P = F*P*F^T + Q
	// For constant velocity model (simplified):
	// P[pos][pos] += dt^2 * P[vel][vel] + 2*dt*P[pos][vel]
	for i := 0; i < 3; i++ {
		kf.state.P[i][i] += dt*dt*kf.state.P[i+3][i+3] + 2*dt*kf.state.P[i][i+3]
		kf.state.P[i+3][i+3] += 0 // velocity covariance stays
	}

	// Add process noise Q
	// Q_pos = σ_a² * dt^4/4, Q_vel = σ_a² * dt^2 (per axis)
	sigA := kf.sigmaAccel / 111319.0 // m/s² → deg/s² approximately
	qPos := sigA * sigA * dt * dt * dt * dt / 4.0
	qVel := sigA * sigA * dt * dt
	for i := 0; i < 3; i++ {
		kf.state.P[i][i] += qPos
		kf.state.P[i+3][i+3] += qVel
	}
}

// Update applies a position measurement to the Kalman filter
func (kf *KalmanFilter) Update(meas types.PositionEstimate) {
	now := meas.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	dt := now.Sub(kf.lastUpdate).Seconds()
	if dt > 0 && dt < 600 { // Only predict for reasonable time steps
		kf.Predict(dt)
	}
	kf.lastUpdate = now

	// Observation matrix H: observe positions (first 3 states)
	// H = [I3, 0] (3x6)
	// Innovation: z - H*x
	innovation := [3]float64{
		meas.Latitude - kf.state.X[0],
		meas.Longitude - kf.state.X[1],
		meas.Altitude - kf.state.X[2],
	}

	// Measurement noise R (3x3 diagonal)
	sigLat := meas.SigmaLat
	if sigLat < 1e-10 {
		sigLat = 5.0 / 111319.0
	}
	sigLon := meas.SigmaLon
	if sigLon < 1e-10 {
		sigLon = 5.0 / 111319.0
	}
	sigAlt := meas.SigmaAlt
	if sigAlt < 0.1 {
		sigAlt = 20.0
	}
	R := [3]float64{sigLat * sigLat, sigLon * sigLon, sigAlt * sigAlt}

	// Innovation covariance S = H*P*H^T + R = P[0:3,0:3] + R (diagonal approx)
	S := [3]float64{
		kf.state.P[0][0] + R[0],
		kf.state.P[1][1] + R[1],
		kf.state.P[2][2] + R[2],
	}

	// Kalman gain K = P*H^T * S^-1 (simplified for diagonal S)
	// K[i][j]: i=state index (0..5), j=obs index (0..2)
	// K[i][j] = P[i][j] / S[j] for j < 3, else 0
	var K [6][3]float64
	for i := 0; i < 6; i++ {
		for j := 0; j < 3; j++ {
			if math.Abs(S[j]) > 1e-20 {
				K[i][j] = kf.state.P[i][j] / S[j]
			}
		}
	}

	// State update: x = x + K * innovation
	for i := 0; i < 6; i++ {
		for j := 0; j < 3; j++ {
			kf.state.X[i] += K[i][j] * innovation[j]
		}
	}

	// Covariance update: P = (I - K*H) * P (Joseph form simplified)
	for i := 0; i < 6; i++ {
		for j := 0; j < 3; j++ {
			kf.state.P[i][j] -= K[i][j] * kf.state.P[j][j]
		}
	}
}

// GetEstimate returns the current filtered position estimate
func (kf *KalmanFilter) GetEstimate() types.PositionEstimate {
	return types.PositionEstimate{
		Latitude:   kf.state.X[0],
		Longitude:  kf.state.X[1],
		Altitude:   kf.state.X[2],
		SigmaLat:   math.Sqrt(math.Abs(kf.state.P[0][0])),
		SigmaLon:   math.Sqrt(math.Abs(kf.state.P[1][1])),
		SigmaAlt:   math.Sqrt(math.Abs(kf.state.P[2][2])),
		Confidence: 68,
		Method:     types.PositioningMethodAGNSS, // overridden by caller
		Timestamp:  kf.lastUpdate,
	}
}

// KalmanFilterManager manages per-session Kalman filters with TTL cleanup
type KalmanFilterManager struct {
	mu      sync.Mutex
	filters map[string]*kalmanEntry
	ttl     time.Duration
}

type kalmanEntry struct {
	filter    *KalmanFilter
	lastUsed  time.Time
}

// NewKalmanFilterManager creates a new manager with given TTL
func NewKalmanFilterManager(ttl time.Duration) *KalmanFilterManager {
	m := &KalmanFilterManager{
		filters: make(map[string]*kalmanEntry),
		ttl:     ttl,
	}
	go m.cleanupLoop()
	return m
}

// GetOrCreate gets or creates a Kalman filter for a session
func (m *KalmanFilterManager) GetOrCreate(sessionID string, initial types.PositionEstimate) *KalmanFilter {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.filters[sessionID]; ok {
		entry.lastUsed = time.Now()
		return entry.filter
	}

	kf := NewKalmanFilter(initial)
	m.filters[sessionID] = &kalmanEntry{filter: kf, lastUsed: time.Now()}
	return kf
}

// Update applies a measurement to a session's filter
func (m *KalmanFilterManager) Update(sessionID string, meas types.PositionEstimate) types.PositionEstimate {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.filters[sessionID]
	if !ok {
		kf := NewKalmanFilter(meas)
		m.filters[sessionID] = &kalmanEntry{filter: kf, lastUsed: time.Now()}
		return meas
	}

	entry.lastUsed = time.Now()
	entry.filter.Update(meas)
	est := entry.filter.GetEstimate()
	est.Method = meas.Method
	return est
}

// cleanupLoop removes expired filters
func (m *KalmanFilterManager) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for id, entry := range m.filters {
			if now.Sub(entry.lastUsed) > m.ttl {
				delete(m.filters, id)
			}
		}
		m.mu.Unlock()
	}
}
