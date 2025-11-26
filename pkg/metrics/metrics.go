package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Counter: Total requests
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "proxy_requests_total",
			Help: "Total number of proxy requests",
		},
		[]string{"method", "status"},
	)

	//Counter: Blocked requests
	BlockedRequests = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "proxy_blocked_requests_total",
			Help: "Total blocked requests",
		},
	)

	// Histogram: Request duration
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "proxy_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method"},
	)

	// Gauge: Active connections
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "proxy_active_connections",
			Help: "Number of active proxy connections",
		},
	)

	// aggregate broken down status codes
	StatusCodeCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "proxy_requests_by_status_class_total",
			Help: "Total requests by status class",
		},
		[]string{"status_class"},
	)

	// --- Inference Metrics ---

	// Counter: Total inference requests
	InferenceRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_requests_total",
			Help: "Total inference requests",
		},
		[]string{"model", "priority", "status"},
	)

	// Histogram: End-to-end request duration (submit to completion)
	InferenceRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "inference_request_duration_seconds",
			Help:    "End-to-end inference request duration",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
		[]string{"model"},
	)

	// Histogram: Time to first token
	InferenceTimeToFirstToken = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "inference_time_to_first_token_seconds",
			Help:    "Time from request submit to first token received",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
		[]string{"model"},
	)

	// Counter: Total tokens generated
	InferenceTokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_tokens_total",
			Help: "Total tokens generated",
		},
		[]string{"model"},
	)

	// Histogram: Worker processing time (gRPC call duration)
	InferenceProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "inference_processing_seconds",
			Help:    "Worker processing time for inference requests",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
		[]string{"model", "worker_id"},
	)

	// Histogram: Queue wait time (submit to worker pickup)
	InferenceQueueWaitDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "inference_queue_wait_seconds",
			Help:    "Time request spent waiting in queue",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
		[]string{"model", "priority"},
	)

	// Counter: Per-worker request counts
	InferenceWorkerRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "inference_worker_requests_total",
			Help: "Total requests processed by each worker",
		},
		[]string{"worker_id", "status"},
	)

	// Gauge: Current queue depth
	InferenceQueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "inference_queue_depth",
			Help: "Current number of requests waiting in queue",
		},
	)

	// Gauge: In-flight requests (being processed by workers)
	InferenceInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "inference_in_flight",
			Help: "Number of requests currently being processed",
		},
	)

	// Counter: Rate limited requests
	RateLimitedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rate_limited_requests_total",
			Help: "Total requests rejected due to rate limiting",
		},
		[]string{"endpoint"},
	)
)

// PriorityLabel converts numeric priority (1-10) to low/medium/high
func PriorityLabel(priority int) string {
	switch {
	case priority >= 8:
		return "high"
	case priority >= 4:
		return "medium"
	default:
		return "low"
	}
}
