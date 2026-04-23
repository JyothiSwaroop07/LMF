// Package server implements the GnssEngineService gRPC server.
package server

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/5g-lmf/common/pb"
	"github.com/5g-lmf/gnss-engine/internal/assistance"
	"github.com/5g-lmf/gnss-engine/internal/positioning"
)

var (
	methodAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gnss_engine",
		Name:      "method_attempts_total",
		Help:      "Total gRPC method invocations by method and status.",
	}, []string{"method", "status"})

	methodLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gnss_engine",
		Name:      "method_latency_seconds",
		Help:      "gRPC method latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})

	accuracyAchieved = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "gnss_engine",
		Name:      "accuracy_achieved_meters",
		Help:      "Achieved horizontal accuracy per fix in metres.",
		Buckets:   []float64{1, 2, 5, 10, 20, 50, 100, 200, 500},
	})

	satellitesUsed = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "gnss_engine",
		Name:      "satellites_used",
		Help:      "Number of satellites used per position fix.",
		Buckets:   []float64{4, 5, 6, 7, 8, 10, 12, 15, 20},
	})
)

// GnssServer implements pb.GnssEngineServiceServer.
type GnssServer struct {
	pb.UnimplementedGnssEngineServiceServer
	provider *assistance.AssistanceDataProvider
	solver   *positioning.GnssSolver
	logger   *zap.Logger
}

// NewGnssServer creates a new server instance.
func NewGnssServer(
	provider *assistance.AssistanceDataProvider,
	solver *positioning.GnssSolver,
	logger *zap.Logger,
) *GnssServer {
	return &GnssServer{
		provider: provider,
		solver:   solver,
		logger:   logger,
	}
}

// GetAssistanceData serves GNSS assistance data.
// It checks the Redis cache first; on a miss it fetches from the provider,
// which also writes through to the cache.
func (s *GnssServer) GetAssistanceData(
	ctx context.Context,
	req *pb.GnssAssistanceRequest,
) (*pb.GnssAssistanceResponse, error) {
	start := time.Now()
	methodAttempts.WithLabelValues("GetAssistanceData", "attempt").Inc()
	defer func() {
		methodLatency.WithLabelValues("GetAssistanceData").Observe(time.Since(start).Seconds())
	}()

	constellations := make([]assistance.GnssConstellation, len(req.Constellations))
	for i, c := range req.Constellations {
		constellations[i] = constellationFromString(c)
	}

	resp := &pb.GnssAssistanceResponse{}
	cache := s.provider.Cache()

	for _, dt := range req.RequestedTypes {
		switch dt {

		case "EPHEMERIS":
			var cached []assistance.GnssEphemeris
			if ok, _ := cache.Get(ctx, "ephemeris", &cached); ok && len(cached) > 0 {
				resp.Ephemerides = toProtoEphemerides(cached)
			} else {
				ephs, err := s.provider.FetchEphemeris(ctx, constellations)
				if err != nil {
					methodAttempts.WithLabelValues("GetAssistanceData", "error").Inc()
					return nil, status.Errorf(codes.Internal, "fetch ephemeris: %v", err)
				}
				resp.Ephemerides = toProtoEphemerides(ephs)
			}

		case "IONOSPHERIC":
			var cached assistance.KlobucharModel
			if ok, _ := cache.Get(ctx, "ionospheric", &cached); ok {
				resp.IonoAlpha = cached.Alpha[:]
				resp.IonoBeta = cached.Beta[:]
			} else {
				model, err := s.provider.FetchIonosphericModel(ctx)
				if err != nil {
					methodAttempts.WithLabelValues("GetAssistanceData", "error").Inc()
					return nil, status.Errorf(codes.Internal, "fetch ionospheric model: %v", err)
				}
				resp.IonoAlpha = model.Alpha[:]
				resp.IonoBeta = model.Beta[:]
			}

		case "REF_TIME":
			t, _, err := s.provider.FetchReferenceTime(ctx)
			if err != nil {
				methodAttempts.WithLabelValues("GetAssistanceData", "error").Inc()
				return nil, status.Errorf(codes.Internal, "fetch reference time: %v", err)
			}
			resp.ReferenceTimeUnixMs = t.UnixMilli()
		}
	}

	if len(resp.Ephemerides) > 0 {
		resp.ValidUntilUnixMs = minValidUntil(resp.Ephemerides)
	}

	methodAttempts.WithLabelValues("GetAssistanceData", "success").Inc()
	s.logger.Debug("assistance data served",
		zap.Strings("constellations", req.Constellations),
		zap.Strings("types", req.RequestedTypes),
		zap.Int("ephemerides", len(resp.Ephemerides)),
	)
	return resp, nil
}

// ComputePosition processes UE GNSS measurements and returns a position fix.
func (s *GnssServer) ComputePosition(
	ctx context.Context,
	req *pb.GnssComputeRequest,
) (*pb.GnssComputeResponse, error) {
	start := time.Now()
	methodAttempts.WithLabelValues("ComputePosition", "attempt").Inc()
	defer func() {
		methodLatency.WithLabelValues("ComputePosition").Observe(time.Since(start).Seconds())
	}()

	if len(req.Signals) < 4 {
		methodAttempts.WithLabelValues("ComputePosition", "error").Inc()
		return nil, status.Errorf(codes.InvalidArgument,
			"need at least 4 GNSS measurements, got %d", len(req.Signals))
	}

	measurements := fromProtoSignals(req.Signals)
	ephemerides := fromProtoEphemerides(req.Ephemerides)

	refTime := time.Now().UTC()
	if req.MeasurementTimeUnixMs != 0 {
		refTime = time.UnixMilli(req.MeasurementTimeUnixMs).UTC()
	}

	pos, err := s.solver.ComputePosition(measurements, ephemerides, refTime)
	if err != nil {
		methodAttempts.WithLabelValues("ComputePosition", "error").Inc()
		s.logger.Error("GNSS position computation failed",
			zap.Int("signals", len(req.Signals)),
			zap.Error(err),
		)
		// Surface computation failures in-band via the proto error field
		// so callers can distinguish them from transport errors.
		return &pb.GnssComputeResponse{Error: err.Error()}, nil
	}

	accuracyAchieved.Observe(pos.HorizontalAccuracy)
	satellitesUsed.Observe(float64(pos.NumSatellites))
	methodAttempts.WithLabelValues("ComputePosition", "success").Inc()

	s.logger.Info("GNSS position computed",
		zap.Float64("lat", pos.Latitude),
		zap.Float64("lon", pos.Longitude),
		zap.Float64("alt_m", pos.Altitude),
		zap.Float64("h_acc_m", pos.HorizontalAccuracy),
		zap.Float64("hdop", pos.HDOP),
		zap.Int("sats", pos.NumSatellites),
		zap.Duration("compute_time", time.Since(start)),
	)

	return &pb.GnssComputeResponse{Estimate: toProtoPosition(pos)}, nil
}
