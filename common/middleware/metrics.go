package middleware

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Location request metrics
	LocationRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_location_requests_total",
		Help: "Total number of location requests",
	}, []string{"method", "status", "client_type"})

	LocationRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "lmf_location_request_duration_seconds",
		Help:    "Location request duration in seconds",
		Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
	}, []string{"method", "accuracy_class"})

	LocationAccuracyAchieved = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "lmf_location_accuracy_achieved_meters",
		Help:    "Achieved positioning accuracy in meters",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 300, 1000},
	}, []string{"method"})

	AccuracyFulfilmentRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "lmf_accuracy_fulfilment_ratio",
		Help: "Ratio of requests where requested accuracy was fulfilled",
	}, []string{"method"})

	// Positioning method metrics
	PositioningMethodAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_positioning_method_attempts_total",
		Help: "Total positioning method attempts",
	}, []string{"method"})

	PositioningMethodSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_positioning_method_success_total",
		Help: "Total successful positioning method attempts",
	}, []string{"method"})

	PositioningMethodLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "lmf_positioning_method_latency_seconds",
		Help:    "Positioning method execution latency",
		Buckets: []float64{0.05, 0.1, 0.5, 1.0, 2.0, 5.0, 15.0, 30.0},
	}, []string{"method"})

	PositioningFallbacks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_positioning_fallback_total",
		Help: "Total positioning method fallbacks",
	}, []string{"from_method", "to_method"})

	// Session metrics
	ActiveSessions = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "lmf_active_sessions",
		Help: "Number of active LCS sessions",
	}, []string{"type"})

	SessionsCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lmf_sessions_created_total",
		Help: "Total LCS sessions created",
	})

	SessionsExpired = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lmf_sessions_expired_total",
		Help: "Total LCS sessions expired",
	})

	SessionsCancelled = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lmf_sessions_cancelled_total",
		Help: "Total LCS sessions cancelled",
	})

	// Event subscription metrics
	ActiveSubscriptions = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "lmf_active_subscriptions",
		Help: "Number of active location event subscriptions",
	}, []string{"event_type"})

	EventNotificationsSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_event_notifications_sent_total",
		Help: "Total event notifications sent",
	}, []string{"event_type"})

	EventNotificationsFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_event_notifications_failed_total",
		Help: "Total event notification delivery failures",
	}, []string{"event_type"})

	// Assistance data metrics
	AssistanceDataCacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_assistance_data_cache_hits_total",
		Help: "Total assistance data cache hits",
	}, []string{"type"})

	AssistanceDataCacheMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_assistance_data_cache_misses_total",
		Help: "Total assistance data cache misses",
	}, []string{"type"})

	AssistanceDataRefreshes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_assistance_data_refresh_total",
		Help: "Total assistance data refresh operations",
	}, []string{"type"})

	// Privacy metrics
	PrivacyChecksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_privacy_checks_total",
		Help: "Total privacy check outcomes",
	}, []string{"outcome"})

	// Protocol metrics
	LppMessagesSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_lpp_messages_sent_total",
		Help: "Total LPP messages sent",
	}, []string{"message_type"})

	LppMessagesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_lpp_messages_received_total",
		Help: "Total LPP messages received",
	}, []string{"message_type"})

	NrppaMessagesSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_nrppa_messages_sent_total",
		Help: "Total NRPPa messages sent",
	}, []string{"procedure"})

	NrppaMessagesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lmf_nrppa_messages_received_total",
		Help: "Total NRPPa messages received",
	}, []string{"procedure"})
)

// StartMetricsServer starts a Prometheus HTTP metrics server on the given port
func StartMetricsServer(port int) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	})

	addr := fmt.Sprintf(":%d", port)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			panic(fmt.Sprintf("metrics server failed: %v", err))
		}
	}()
}
