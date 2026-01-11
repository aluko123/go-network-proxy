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

	// --- Auth Metrics ---

	// Counter: Successful authentications
	AuthSuccessTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auth_success_total",
			Help: "Total successful API key authentications",
		},
	)

	// Counter: Failed authentications
	AuthFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_failures_total",
			Help: "Total failed API key authentications",
		},
		[]string{"reason"},
	)

	// --- Worker Health Metrics ---

	// Gauge: Worker health status (1=healthy, 0=unhealthy)
	WorkerHealthGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "worker_healthy",
			Help: "Worker health status (1=healthy, 0=unhealthy)",
		},
		[]string{"worker_id"},
	)

	// Gauge: Worker GPU utilization
	WorkerGPUUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "worker_gpu_utilization",
			Help: "Worker GPU utilization percentage",
		},
		[]string{"worker_id"},
	)

	// Gauge: Worker queue depth
	WorkerQueueDepth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "worker_queue_depth",
			Help: "Worker queue depth",
		},
		[]string{"worker_id"},
	)

	// --- Prefix Cache Metrics ---

	// Counter: Prefix cache hits (routing decision used cached prefix info)
	PrefixCacheHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prefix_cache_hits_total",
			Help: "Total routing decisions where prefix cache hit influenced worker selection",
		},
		[]string{"model"},
	)

	// Counter: Prefix cache misses (no cached prefix info available)
	PrefixCacheMisses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prefix_cache_misses_total",
			Help: "Total routing decisions where no prefix cache info was available",
		},
		[]string{"model"},
	)

	// Gauge: Number of unique prefixes tracked
	PrefixCacheSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "prefix_cache_entries",
			Help: "Number of unique prefixes currently tracked in the prefix index",
		},
	)

	// Gauge: Total prefix-to-worker mappings
	PrefixCacheMappings = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "prefix_cache_mappings",
			Help: "Total number of prefix-to-worker mappings in the index",
		},
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
