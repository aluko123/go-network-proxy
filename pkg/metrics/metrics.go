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

	QueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "inference_queue_depth",
		Help: "Current number of requests in queue",
	})

	BatchSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "inference_batch_size",
		Help: "Size of processed batches",
	})

	Latency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "inference_latency_seconds",
		Help:    "Time from request to first token",
		Buckets: []float64{.01, .05, .1, .5, 1, 2, 5},
	})
)
