// Package server implements the TdoaEngineService gRPC server.
package server

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/5g-lmf/tdoa-engine/internal/geometry"
	"github.com/5g-lmf/tdoa-engine/internal/positioning"
)

// Prometheus metrics.
var (
	tdoaMethodAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tdoa_engine",
		Name:      "method_attempts_total",
		Help:      "Total TDOA gRPC method invocations by method and status.",
	}, []string{"method", "status"})

	tdoaMethodLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "tdoa_engine",
		Name:      "method_latency_seconds",
		Help:      "TDOA gRPC method latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})

	tdoaAccuracy = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "tdoa_engine",
		Name:      "accuracy_achieved_meters",
		Help:      "Achieved horizontal accuracy per fix in metres.",
		Buckets:   []float64{5, 10, 25, 50, 100, 200, 500, 1000},
	})

	tdoaMeasurements = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "tdoa_engine",
		Name:      "rstd_measurements_per_request",
		Help:      "Number of valid RSTD measurements used per position fix.",
		Buckets:   []float64{3, 4, 5, 6, 7, 8, 10, 12, 15, 20},
	})

	tdoaHDOP = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "tdoa_engine",
		Name:      "hdop",
		Help:      "HDOP value per fix.",
		Buckets:   []float64{1, 1.5, 2, 2.5, 3, 4, 5, 7, 10, 20},
	})
)

// ---------------------------------------------------------------------------
// Request / response types (would be protobuf-generated in production)
// ---------------------------------------------------------------------------

// TdoaComputeRequest is the inbound request for a DL-TDOA position fix.
type TdoaComputeRequest struct {
	SessionID    string
	Measurements []positioning.RSTDMeasurement
}

// TdoaComputeResponse carries the computed position estimate.
type TdoaComputeResponse struct {
	SessionID string
	Position  *positioning.PositionEstimate
}

// TdoaEngineServiceServer is the interface fulfilled by TdoaServer.
type TdoaEngineServiceServer interface {
	ComputePosition(ctx context.Context, req *TdoaComputeRequest) (*TdoaComputeResponse, error)
}

// RegisterTdoaEngineServiceServer registers the TDOA server with a gRPC server.
// This is a placeholder for the protoc-generated registration function.
func RegisterTdoaEngineServiceServer(s *grpc.Server, srv TdoaEngineServiceServer) {
	_ = s
	_ = srv
}

// ---------------------------------------------------------------------------
// TdoaServer implementation
// ---------------------------------------------------------------------------

// TdoaServer implements TdoaEngineServiceServer.
type TdoaServer struct {
	cellStore       *geometry.CellGeometryStore
	solver          *positioning.TdoaSolver
	minMeasurements int
	logger          *zap.Logger
}

// NewTdoaServer creates a new TdoaServer. minMeasurements is clamped to ≥3.
func NewTdoaServer(
	cellStore *geometry.CellGeometryStore,
	solver *positioning.TdoaSolver,
	minMeasurements int,
	logger *zap.Logger,
) *TdoaServer {
	if minMeasurements < 3 {
		minMeasurements = 3
	}
	return &TdoaServer{
		cellStore:       cellStore,
		solver:          solver,
		minMeasurements: minMeasurements,
		logger:          logger,
	}
}

// ComputePosition performs a DL-TDOA position fix.
//
// Steps:
//  1. Validate minimum RSTD measurement count.
//  2. Collect all unique NCIs from the measurements.
//  3. Fetch cell geometry for all NCIs from the store.
//  4. Invoke TdoaSolver.ComputePosition (Chan's algorithm).
//  5. Record Prometheus metrics and return the result.
func (s *TdoaServer) ComputePosition(
	ctx context.Context,
	req *TdoaComputeRequest,
) (*TdoaComputeResponse, error) {
	start := time.Now()
	tdoaMethodAttempts.WithLabelValues("ComputePosition", "attempt").Inc()
	defer func() {
		tdoaMethodLatency.WithLabelValues("ComputePosition").Observe(time.Since(start).Seconds())
	}()

	// --- Validation ---
	if len(req.Measurements) < s.minMeasurements {
		tdoaMethodAttempts.WithLabelValues("ComputePosition", "error_insufficient_meas").Inc()
		return nil, status.Errorf(codes.InvalidArgument,
			"need at least %d RSTD measurements, got %d",
			s.minMeasurements, len(req.Measurements))
	}

	// Ensure all measurements share the same reference NCI.
	refNCI := req.Measurements[0].ReferenceNCI
	for _, m := range req.Measurements[1:] {
		if m.ReferenceNCI != refNCI {
			tdoaMethodAttempts.WithLabelValues("ComputePosition", "error_mixed_ref").Inc()
			return nil, status.Errorf(codes.InvalidArgument,
				"all measurements must share the same reference NCI; got %q and %q",
				refNCI, m.ReferenceNCI)
		}
	}

	// --- Collect unique NCIs ---
	nciSet := make(map[string]struct{}, len(req.Measurements)+1)
	nciSet[refNCI] = struct{}{}
	for _, m := range req.Measurements {
		nciSet[m.NeighborNCI] = struct{}{}
	}

	ncis := make([]string, 0, len(nciSet))
	for nci := range nciSet {
		ncis = append(ncis, nci)
	}

	// --- Fetch cell geometry ---
	cells, err := s.cellStore.GetMultipleCells(ctx, ncis)
	if err != nil {
		tdoaMethodAttempts.WithLabelValues("ComputePosition", "error_cell_fetch").Inc()
		s.logger.Error("failed to fetch cell geometry",
			zap.String("session", req.SessionID),
			zap.Error(err),
		)
		return nil, status.Errorf(codes.Internal, "fetch cell geometry: %v", err)
	}

	if len(cells) == 0 {
		tdoaMethodAttempts.WithLabelValues("ComputePosition", "error_no_cells").Inc()
		return nil, status.Errorf(codes.NotFound, "no cell geometry found for any NCI in request")
	}

	// Index cells by NCI.
	cellMap := make(map[string]positioning.CellGeometry, len(cells))
	for _, c := range cells {
		cellMap[c.NCI] = c
	}

	// Validate that the reference cell exists.
	if _, ok := cellMap[refNCI]; !ok {
		tdoaMethodAttempts.WithLabelValues("ComputePosition", "error_no_ref_cell").Inc()
		return nil, status.Errorf(codes.NotFound,
			"reference cell %q not found in geometry store", refNCI)
	}

	// --- Run TDOA solver ---
	measData := positioning.DlTdoaMeasurements{
		SessionID:    req.SessionID,
		Measurements: req.Measurements,
	}

	pos, err := s.solver.ComputePosition(measData, cellMap)
	if err != nil {
		tdoaMethodAttempts.WithLabelValues("ComputePosition", "error_solver").Inc()
		s.logger.Error("TDOA solver failed",
			zap.String("session", req.SessionID),
			zap.Int("measurements", len(req.Measurements)),
			zap.Int("cells", len(cellMap)),
			zap.Error(err),
		)
		return nil, status.Errorf(codes.Internal, "TDOA solve: %v", err)
	}

	// --- Metrics ---
	tdoaAccuracy.Observe(pos.HorizontalAccuracy)
	tdoaMeasurements.Observe(float64(pos.NumMeasurements))
	tdoaHDOP.Observe(pos.HDOP)
	tdoaMethodAttempts.WithLabelValues("ComputePosition", "success").Inc()

	s.logger.Info("TDOA position computed",
		zap.String("session", req.SessionID),
		zap.Float64("lat", pos.Latitude),
		zap.Float64("lon", pos.Longitude),
		zap.Float64("h_acc_m", pos.HorizontalAccuracy),
		zap.Float64("hdop", pos.HDOP),
		zap.Int("measurements", pos.NumMeasurements),
		zap.Duration("compute_time", time.Since(start)),
	)

	return &TdoaComputeResponse{
		SessionID: req.SessionID,
		Position:  pos,
	}, nil
}
